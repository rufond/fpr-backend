package yahoo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	http "github.com/bogdanfinn/fhttp"

	"github.com/rufond/fpr-backend/internal/dateonly"
	"github.com/rufond/fpr-backend/internal/prices"
)

const (
	yahooChartURL          = "https://query1.finance.yahoo.com/v8/finance/chart/"
	historicalLookbackDays = 45
)

type chartEnvelope struct {
	Chart chartPayload `json:"chart"`
}

type chartPayload struct {
	Result []chartResult   `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type chartResult struct {
	Meta       chartMeta       `json:"meta"`
	Timestamps []int64         `json:"timestamp"`
	Indicators chartIndicators `json:"indicators"`
}

type chartMeta struct {
	Currency             string `json:"currency"`
	ExchangeTimezoneName string `json:"exchangeTimezoneName"`
}

type chartIndicators struct {
	Quote []chartQuote `json:"quote"`
}

type chartQuote struct {
	Close []decimalNumber `json:"close"`
}

func (p *Provider) FetchDailyPrices(ctx context.Context, symbols []string, till time.Time) (prices.HistoricalSourceResult, error) {
	if len(symbols) == 0 {
		return prices.HistoricalSourceResult{PricesBySymbol: map[string]prices.SourceDailyPrice{}}, nil
	}

	run, closeRun, errRun := p.runProvider(ctx)
	if errRun != nil {
		return prices.HistoricalSourceResult{}, errRun
	}
	defer closeRun()

	return run.fetchDailyPrices(ctx, symbols, till)
}

func (p *Provider) fetchDailyPrices(ctx context.Context, symbols []string, till time.Time) (prices.HistoricalSourceResult, error) {
	result := prices.HistoricalSourceResult{
		RequestedSymbols: len(symbols),
		PricesBySymbol:   make(map[string]prices.SourceDailyPrice, len(symbols)),
	}

	for index, symbol := range symbols {
		price, exists, errPrice := p.fetchDailyPrice(ctx, symbol, till)
		if errPrice != nil {
			if ctx.Err() != nil {
				return prices.HistoricalSourceResult{}, ctx.Err()
			}

			result.Issues = append(result.Issues, prices.HistoricalPriceIssue{
				Symbol: symbol,
				Error:  errPrice.Error(),
			})
		} else if !exists {
			result.MissingSymbols = append(result.MissingSymbols, symbol)
		} else {
			result.PricesBySymbol[symbol] = price
		}

		if index+1 < len(symbols) {
			if errDelay := p.waitRequestDelay(ctx); errDelay != nil {
				return prices.HistoricalSourceResult{}, errDelay
			}
		}
	}

	return result, nil
}

func (p *Provider) fetchDailyPrice(ctx context.Context, symbol string, till time.Time) (prices.SourceDailyPrice, bool, error) {
	till = dateonly.UTC(till)
	from := till.AddDate(0, 0, -historicalLookbackDays)
	before := till.AddDate(0, 0, 1)

	requestURL, errURL := url.Parse(yahooChartURL + url.PathEscape(symbol))
	if errURL != nil {
		return prices.SourceDailyPrice{}, false, fmt.Errorf("build Yahoo daily chart URL for %s: %w", symbol, errURL)
	}

	query := requestURL.Query()
	query.Set("period1", strconv.FormatInt(from.Unix(), 10))
	query.Set("period2", strconv.FormatInt(before.Unix(), 10))
	query.Set("interval", "1d")
	query.Set("includePrePost", "false")
	requestURL.RawQuery = query.Encode()

	req, errRequest := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if errRequest != nil {
		return prices.SourceDailyPrice{}, false, fmt.Errorf("create Yahoo daily chart request for %s: %w", symbol, errRequest)
	}
	req = req.WithContext(ctx)
	req.Header = browserHeaders()

	resp, errDo := p.client.Do(req)
	if errDo != nil {
		return prices.SourceDailyPrice{}, false, fmt.Errorf("request Yahoo daily chart for %s: %w", symbol, errDo)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, errRead := readResponseBody(resp.Body)
	if errRead != nil {
		return prices.SourceDailyPrice{}, false, fmt.Errorf("read Yahoo daily chart for %s: %w", symbol, errRead)
	}

	if resp.StatusCode != http.StatusOK {
		return prices.SourceDailyPrice{}, false, fmt.Errorf(
			"Yahoo daily chart for %s returned HTTP %d: %s",
			symbol,
			resp.StatusCode,
			responsePreview(body),
		)
	}

	var envelope chartEnvelope
	if errDecode := json.Unmarshal(body, &envelope); errDecode != nil {
		return prices.SourceDailyPrice{}, false, fmt.Errorf("decode Yahoo daily chart for %s: %w", symbol, errDecode)
	}

	if len(envelope.Chart.Error) > 0 && string(envelope.Chart.Error) != "null" {
		return prices.SourceDailyPrice{}, false, fmt.Errorf(
			"Yahoo daily chart for %s returned error: %s",
			symbol,
			strings.TrimSpace(string(envelope.Chart.Error)),
		)
	}

	if len(envelope.Chart.Result) == 0 {
		return prices.SourceDailyPrice{}, false, nil
	}

	chart := envelope.Chart.Result[0]
	if len(chart.Indicators.Quote) == 0 {
		return prices.SourceDailyPrice{}, false, nil
	}

	location, errLocation := time.LoadLocation(strings.TrimSpace(chart.Meta.ExchangeTimezoneName))
	if errLocation != nil {
		return prices.SourceDailyPrice{}, false, fmt.Errorf(
			"load Yahoo exchange timezone %q for %s: %w",
			chart.Meta.ExchangeTimezoneName,
			symbol,
			errLocation,
		)
	}

	currencyCode, errCurrency := normalizeQuoteCurrency(chart.Meta.Currency)
	if errCurrency != nil {
		return prices.SourceDailyPrice{}, false, fmt.Errorf("Yahoo daily chart for %s: %w", symbol, errCurrency)
	}

	closes := chart.Indicators.Quote[0].Close
	count := min(len(chart.Timestamps), len(closes))
	var latest prices.SourceDailyPrice
	found := false

	for index := range count {
		if closes[index].Text == "" {
			continue
		}

		pricedAt := time.Unix(chart.Timestamps[index], 0).UTC()
		local := pricedAt.In(location)
		priceDate := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
		if priceDate.After(till) || found && !priceDate.After(latest.PriceDate) {
			continue
		}

		unitValue, errValue := normalizePositiveDecimal(closes[index].Text)
		if errValue != nil {
			return prices.SourceDailyPrice{}, false, fmt.Errorf("Yahoo daily close for %s: %w", symbol, errValue)
		}

		latest = prices.SourceDailyPrice{
			PriceDate: priceDate,
			UnitValue: unitValue,
			Currency:  currencyCode,
			PricedAt:  pricedAt,
		}

		found = true
	}

	return latest, found, nil
}
