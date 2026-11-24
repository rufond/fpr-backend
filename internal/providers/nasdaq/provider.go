package nasdaq

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/rufond/fpr-backend/internal/decimal"
	"github.com/rufond/fpr-backend/internal/prices"
)

var symbolPattern = regexp.MustCompile(`^[A-Z0-9.-]{1,16}$`)

type Provider struct {
	client       *http.Client
	exchangeTime *time.Location
}

type quotePayload struct {
	Data struct {
		Symbol     string `json:"symbol"`
		AssetClass string `json:"assetClass"`
		Primary    struct {
			Price     string `json:"lastSalePrice"`
			Change    string `json:"netChange"`
			TradeTime string `json:"lastTradeTimestamp"`
		} `json:"primaryData"`
	} `json:"data"`
}

func NewProvider() *Provider {
	exchangeTime, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic(err)
	}

	return &Provider{client: &http.Client{Timeout: 8 * time.Second}, exchangeTime: exchangeTime}
}

func (p *Provider) FetchSecurityPrices(ctx context.Context, symbols []string) (prices.MOEXSourceResult, error) {
	result := prices.MOEXSourceResult{RequestedSymbols: len(symbols), QuotesBySymbol: make(map[string]prices.SourceQuote)}
	for _, symbol := range symbols {
		quote, err := p.fetch(ctx, symbol)
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		if err != nil {
			result.Issues = append(result.Issues, prices.MOEXQuoteIssue{Symbol: symbol, Error: err.Error()})
			continue
		}

		result.QuotesBySymbol[symbol] = quote
	}

	return result, nil
}

func (p *Provider) fetch(ctx context.Context, symbol string) (prices.SourceQuote, error) {
	if !symbolPattern.MatchString(symbol) {
		return prices.SourceQuote{}, fmt.Errorf("invalid Nasdaq symbol")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.nasdaq.com/api/quote/"+symbol+"/info?assetclass=stocks", nil)
	if err != nil {
		return prices.SourceQuote{}, err
	}

	var payload quotePayload
	if errFetch := p.fetchJSON(request, &payload); errFetch != nil {
		return prices.SourceQuote{}, errFetch
	}

	return p.parseQuote(symbol, payload)
}

func (p *Provider) fetchJSON(request *http.Request, target any) error {
	request.Header.Set("User-Agent", "Mozilla/5.0")
	request.Header.Set("Accept", "application/json")
	response, err := p.client.Do(request)
	if err != nil {
		return err
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(response.Body)

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Nasdaq returned HTTP %d", response.StatusCode)
	}

	return json.NewDecoder(io.LimitReader(response.Body, 256<<10)).Decode(target)
}

func (p *Provider) parseQuote(symbol string, payload quotePayload) (prices.SourceQuote, error) {
	primary := payload.Data.Primary
	valueText := strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(primary.Price), "$"), ",", "")
	value, valid := decimal.Parse(valueText)
	stamp, err := time.ParseInLocation("Jan 2, 2006 3:04 PM", strings.TrimSuffix(strings.TrimSpace(primary.TradeTime), " ET"), p.exchangeTime)
	if payload.Data.Symbol != symbol || !strings.EqualFold(payload.Data.AssetClass, "stocks") || !valid || value.Sign() <= 0 || err != nil {
		return prices.SourceQuote{}, fmt.Errorf("Nasdaq returned no dated USD quote for %s", symbol)
	}

	quote := prices.SourceQuote{UnitValue: valueText, Currency: "USD", Source: prices.ProviderNasdaq, PricedAt: stamp.UTC()}
	if change, ok := decimal.Parse(strings.ReplaceAll(primary.Change, ",", "")); ok {
		previous := new(big.Rat).Sub(value, change)
		if previous.Sign() > 0 {
			quote.PreviousClose = previous.FloatString(8)
		}
	}

	return quote, nil
}

func (p *Provider) FetchDailyPrices(ctx context.Context, symbols []string, till time.Time) (prices.HistoricalSourceResult, error) {
	result := prices.HistoricalSourceResult{RequestedSymbols: len(symbols), PricesBySymbol: make(map[string]prices.SourceDailyPrice)}
	for _, symbol := range symbols {
		price, found, err := p.fetchDailyPrice(ctx, symbol, till)
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		if err != nil {
			result.Issues = append(result.Issues, prices.HistoricalPriceIssue{Symbol: symbol, Error: err.Error()})
		} else if found {
			result.PricesBySymbol[symbol] = price
		} else {
			result.MissingSymbols = append(result.MissingSymbols, symbol)
		}
	}

	return result, nil
}

func (p *Provider) fetchDailyPrice(ctx context.Context, symbol string, till time.Time) (prices.SourceDailyPrice, bool, error) {
	if !symbolPattern.MatchString(symbol) {
		return prices.SourceDailyPrice{}, false, fmt.Errorf("invalid Nasdaq symbol")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.nasdaq.com/api/quote/"+symbol+"/historical", nil)
	if err != nil {
		return prices.SourceDailyPrice{}, false, err
	}

	query := request.URL.Query()
	query.Set("assetclass", "stocks")
	query.Set("fromdate", till.AddDate(0, 0, -10).Format(time.DateOnly))
	query.Set("todate", till.Format(time.DateOnly))
	query.Set("limit", "12")
	request.URL.RawQuery = query.Encode()
	var payload struct {
		Data struct {
			Symbol string `json:"symbol"`
			Trades struct {
				Rows []struct {
					Date  string `json:"date"`
					Close string `json:"close"`
				} `json:"rows"`
			} `json:"tradesTable"`
		} `json:"data"`
	}
	if errFetch := p.fetchJSON(request, &payload); errFetch != nil {
		return prices.SourceDailyPrice{}, false, errFetch
	}

	if payload.Data.Symbol != symbol {
		return prices.SourceDailyPrice{}, false, fmt.Errorf("Nasdaq historical response did not identify %s", symbol)
	}

	result := prices.SourceDailyPrice{}
	for _, row := range payload.Data.Trades.Rows {
		date, dateErr := time.Parse("01/02/2006", row.Date)
		value := strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(row.Close), "$"), ",", "")
		price, priceOK := decimal.Parse(value)
		if dateErr != nil || !priceOK || price.Sign() <= 0 || date.Format(time.DateOnly) > till.Format(time.DateOnly) || !date.After(result.PriceDate) {
			continue
		}

		closeTime := time.Date(date.Year(), date.Month(), date.Day(), 16, 0, 0, 0, p.exchangeTime)
		result = prices.SourceDailyPrice{PriceDate: date, UnitValue: value, Currency: "USD", PricedAt: closeTime.UTC()}
	}

	return result, !result.PriceDate.IsZero(), nil
}
