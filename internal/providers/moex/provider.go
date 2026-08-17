package moex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rufond/fpr-backend/internal/currency"
	"github.com/rufond/fpr-backend/internal/dateonly"
	"github.com/rufond/fpr-backend/internal/decimal"
	"github.com/rufond/fpr-backend/internal/fx"
	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/providers"
)

const (
	defaultBaseURL  = "https://iss.moex.com"
	requestTimeout  = 15 * time.Second
	maxResponseSize = 1 << 20
	maxHistoryPages = 100

	usdRUBSecurityID = "USD000UTSTOM"
	usdRUBBoardID    = "CETS"
)

var moscowLocation = time.FixedZone("MSK", 3*60*60)

var (
	fundUnitSecurity = marketSecurity{
		Engine:     "stock",
		Market:     "shares",
		SecurityID: prices.FundUnitISIN,
	}
	usdRUBSecurity = marketSecurity{
		Engine:     "currency",
		Market:     "selt",
		SecurityID: usdRUBSecurityID,
	}
)

var (
	_ prices.Source = (*Provider)(nil)
	_ fx.Source     = (*Provider)(nil)
)

type Provider struct {
	baseURL string
	client  *http.Client
	now     func() time.Time

	boardMu sync.Mutex
	boards  map[marketSecurity]cachedBoard
}

type marketSecurity struct {
	Engine     string
	Market     string
	SecurityID string
}

type cachedBoard struct {
	Board board
	Date  string
}

type board struct {
	ID       string
	Currency string
}

type marketQuote struct {
	Value    string
	Currency string
	PricedAt time.Time
	Source   string
}

type issBlock struct {
	Columns []string `json:"columns"`
	Data    [][]any  `json:"data"`
}

type issSecurityResponse struct {
	Boards issBlock `json:"boards"`
}

type issSecuritiesResponse struct {
	Securities issBlock `json:"securities"`
}

type issQuoteResponse struct {
	MarketData issBlock `json:"marketdata"`
	Securities issBlock `json:"securities"`
}

type issCandlesResponse struct {
	Candles issBlock `json:"candles"`
}

func NewProvider(baseURL string, client *http.Client) *Provider {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}

	return &Provider{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  client,
		now:     time.Now,
		boards:  make(map[marketSecurity]cachedBoard),
	}
}

func (p *Provider) FetchFundUnitQuote(ctx context.Context) (prices.SourceQuote, error) {
	quote, err := p.fetchQuoteWithBoardRefresh(ctx, fundUnitSecurity)
	if err != nil {
		return prices.SourceQuote{}, fmt.Errorf("fetch MOEX fund unit quote: %w", err)
	}

	return prices.SourceQuote{
		UnitValue: quote.Value,
		Currency:  quote.Currency,
		PricedAt:  quote.PricedAt,
		Source:    quote.Source,
	}, nil
}

func (p *Provider) FetchUSDRUB(ctx context.Context) (fx.SourceRate, error) {
	quote, err := p.fetchQuote(ctx, usdRUBSecurity, board{
		ID:       usdRUBBoardID,
		Currency: currency.RUB,
	})
	if err != nil {
		return fx.SourceRate{}, fmt.Errorf("fetch MOEX USD/RUB quote: %w", err)
	}

	return fx.SourceRate{
		Provider:      fx.ProviderMOEX,
		BaseCurrency:  currency.USD,
		QuoteCurrency: currency.RUB,
		Rate:          quote.Value,
		PricedAt:      quote.PricedAt,
		Source:        quote.Source,
	}, nil
}

func (p *Provider) ResolveSecuritySymbols(ctx context.Context, isins []string) (prices.MOEXSymbolResolutionResult, error) {
	result := prices.MOEXSymbolResolutionResult{
		RequestedISINs: len(isins),
		SymbolsByISIN:  make(map[string]string, len(isins)),
	}

	for _, isin := range isins {
		requestURL, errURL := url.Parse(p.baseURL + "/iss/securities.json")
		if errURL != nil {
			return prices.MOEXSymbolResolutionResult{}, fmt.Errorf("build MOEX security search URL for %s: %w", isin, errURL)
		}

		query := requestURL.Query()
		query.Set("q", isin)
		query.Set("iss.meta", "off")
		query.Set("iss.only", "securities")
		query.Set("securities.columns", "secid,isin,is_traded,group")
		requestURL.RawQuery = query.Encode()

		var payload issSecuritiesResponse
		if err := p.fetchJSON(ctx, requestURL, &payload); err != nil {
			return prices.MOEXSymbolResolutionResult{}, fmt.Errorf("search MOEX security by ISIN %s: %w", isin, err)
		}

		symbols := make(map[string]struct{})
		for _, data := range payload.Securities.Data {
			row, ok := rowMap(payload.Securities.Columns, data)
			if !ok {
				continue
			}

			returnedISIN, okISIN := stringValue(row["isin"])
			if !okISIN || returnedISIN != isin {
				continue
			}

			isTraded, okTraded := boolFlag(row["is_traded"])
			group, okGroup := stringValue(row["group"])
			if !okTraded || !isTraded || !okGroup || group != "stock_shares" {
				continue
			}

			symbol, okSymbol := stringValue(row["secid"])
			if okSymbol {
				symbols[symbol] = struct{}{}
			}
		}

		switch len(symbols) {
		case 0:
			result.MissingISINs = append(result.MissingISINs, isin)
		case 1:
			for symbol := range symbols {
				result.SymbolsByISIN[isin] = symbol
			}
		default:
			return prices.MOEXSymbolResolutionResult{}, fmt.Errorf("MOEX returned multiple traded share symbols for ISIN %s", isin)
		}
	}

	return result, nil
}

func (p *Provider) FetchSecurityPrices(ctx context.Context, symbols []string) (prices.MOEXSourceResult, error) {
	result := prices.MOEXSourceResult{
		RequestedSymbols: len(symbols),
		QuotesBySymbol:   make(map[string]prices.SourceQuote, len(symbols)),
	}

	for _, symbol := range symbols {
		quote, errQuote := p.fetchQuoteWithBoardRefresh(ctx, marketSecurity{
			Engine:     "stock",
			Market:     "shares",
			SecurityID: symbol,
		})
		if errQuote != nil {
			if ctx.Err() != nil {
				return prices.MOEXSourceResult{}, ctx.Err()
			}

			result.Issues = append(result.Issues, prices.MOEXQuoteIssue{
				Symbol: symbol,
				Error:  errQuote.Error(),
			})
			continue
		}

		result.QuotesBySymbol[symbol] = prices.SourceQuote{
			UnitValue: quote.Value,
			Currency:  quote.Currency,
			PricedAt:  quote.PricedAt,
			Source:    quote.Source,
		}
	}

	return result, nil
}

func (p *Provider) FetchFundUnitDailyPrices(ctx context.Context, from time.Time) ([]prices.SourceDailyPrice, error) {
	from = dateonly.UTC(from)
	till := p.previousMoscowDate()
	if from.After(till) {
		return []prices.SourceDailyPrice{}, nil
	}

	currentBoard, errBoard := p.resolveBoard(ctx, fundUnitSecurity, false)
	if errBoard != nil {
		return nil, fmt.Errorf("resolve MOEX fund unit board for daily prices: %w", errBoard)
	}

	items := make([]prices.SourceDailyPrice, 0, 512)
	start := 0

	for range maxHistoryPages {
		requestURL, errURL := url.Parse(
			p.baseURL + "/iss/engines/stock/markets/shares/securities/" +
				url.PathEscape(prices.FundUnitISIN) + "/candles.json",
		)
		if errURL != nil {
			return nil, fmt.Errorf("build MOEX fund unit daily candles URL: %w", errURL)
		}

		query := requestURL.Query()
		query.Set("iss.meta", "off")
		query.Set("iss.only", "candles")
		query.Set("candles.columns", "close,begin")
		query.Set("interval", "24")
		query.Set("from", from.Format(time.DateOnly))
		query.Set("till", till.Format(time.DateOnly))
		query.Set("start", strconv.Itoa(start))
		requestURL.RawQuery = query.Encode()

		var payload issCandlesResponse
		if err := p.fetchJSON(ctx, requestURL, &payload); err != nil {
			return nil, fmt.Errorf("request MOEX fund unit daily candles: %w", err)
		}

		if len(payload.Candles.Data) == 0 {
			return normalizeDailyPrices(items)
		}

		for _, data := range payload.Candles.Data {
			row, ok := rowMap(payload.Candles.Columns, data)
			if !ok {
				return nil, fmt.Errorf("MOEX fund unit daily candle has invalid row shape")
			}

			if row["close"] == nil {
				continue
			}

			unitValue, okValue := positiveDecimal(row["close"])
			if !okValue {
				return nil, fmt.Errorf("MOEX fund unit daily candle has invalid close value")
			}

			begin, okBegin := stringValue(row["begin"])
			if !okBegin || len(begin) < len(time.DateOnly) {
				return nil, fmt.Errorf("MOEX fund unit daily candle has invalid begin value")
			}

			priceDate, errDate := time.Parse(time.DateOnly, begin[:len(time.DateOnly)])
			if errDate != nil {
				return nil, fmt.Errorf("parse MOEX fund unit daily candle date %q: %w", begin, errDate)
			}

			items = append(items, prices.SourceDailyPrice{
				PriceDate: priceDate,
				UnitValue: unitValue,
				Currency:  currentBoard.Currency,
			})
		}

		start += len(payload.Candles.Data)
	}

	return nil, fmt.Errorf("MOEX fund unit daily candles exceeded %d pages", maxHistoryPages)
}

func (p *Provider) previousMoscowDate() time.Time {
	now := p.now().In(moscowLocation)
	today := dateonly.UTC(now)

	return today.AddDate(0, 0, -1)
}

func normalizeDailyPrices(items []prices.SourceDailyPrice) ([]prices.SourceDailyPrice, error) {
	slices.SortFunc(items, func(left, right prices.SourceDailyPrice) int {
		return left.PriceDate.Compare(right.PriceDate)
	})

	result := make([]prices.SourceDailyPrice, 0, len(items))
	for _, item := range items {
		item.PriceDate = dateonly.UTC(item.PriceDate)

		if len(result) != 0 && result[len(result)-1].PriceDate.Equal(item.PriceDate) {
			previous := result[len(result)-1]
			if previous.UnitValue != item.UnitValue || previous.Currency != item.Currency {
				return nil, fmt.Errorf("MOEX returned conflicting daily candles for %s", item.PriceDate.Format(time.DateOnly))
			}
			continue
		}

		result = append(result, item)
	}

	return result, nil
}

func (p *Provider) fetchQuoteWithBoardRefresh(ctx context.Context, security marketSecurity) (*marketQuote, error) {
	currentBoard, errBoard := p.resolveBoard(ctx, security, false)
	if errBoard != nil {
		return nil, fmt.Errorf("resolve board for %s: %w", security.SecurityID, errBoard)
	}

	quote, errQuote := p.fetchQuote(ctx, security, currentBoard)
	if errQuote == nil {
		return quote, nil
	}

	refreshedBoard, errRefresh := p.resolveBoard(ctx, security, true)
	if errRefresh != nil {
		return nil, fmt.Errorf("fetch quote for %s on board %s: %v; refresh board: %w", security.SecurityID, currentBoard.ID, errQuote, errRefresh)
	}

	quote, errRetry := p.fetchQuote(ctx, security, refreshedBoard)
	if errRetry != nil {
		return nil, fmt.Errorf("fetch quote for %s on board %s after refresh: %w", security.SecurityID, refreshedBoard.ID, errRetry)
	}

	return quote, nil
}

func (p *Provider) resolveBoard(ctx context.Context, security marketSecurity, force bool) (board, error) {
	date := p.now().In(moscowLocation).Format(time.DateOnly)

	p.boardMu.Lock()
	cached, exists := p.boards[security]
	if !force && exists && cached.Board.ID != "" && cached.Date == date {
		p.boardMu.Unlock()
		return cached.Board, nil
	}
	p.boardMu.Unlock()

	resolved, err := p.fetchBoard(ctx, security)
	if err != nil {
		return board{}, err
	}

	p.boardMu.Lock()
	p.boards[security] = cachedBoard{Board: resolved, Date: date}
	p.boardMu.Unlock()

	return resolved, nil
}

func (p *Provider) fetchBoard(ctx context.Context, security marketSecurity) (board, error) {
	requestURL, errURL := url.Parse(p.baseURL + "/iss/securities/" + url.PathEscape(security.SecurityID) + ".json")
	if errURL != nil {
		return board{}, fmt.Errorf("build boards URL for %s: %w", security.SecurityID, errURL)
	}

	query := requestURL.Query()
	query.Set("iss.meta", "off")
	query.Set("iss.only", "boards")
	query.Set("boards.columns", "boardid,engine,market,is_traded,is_primary,currencyid")
	requestURL.RawQuery = query.Encode()

	var payload issSecurityResponse
	if err := p.fetchJSON(ctx, requestURL, &payload); err != nil {
		return board{}, fmt.Errorf("request boards for %s: %w", security.SecurityID, err)
	}

	var primary *board
	var tradedBoard board

	tradedCount := 0
	for _, data := range payload.Boards.Data {
		row, ok := rowMap(payload.Boards.Columns, data)
		if !ok {
			continue
		}

		engine, okEngine := stringValue(row["engine"])
		market, okMarket := stringValue(row["market"])
		if !okEngine || !okMarket || engine != security.Engine || market != security.Market {
			continue
		}

		isTraded, okTraded := boolFlag(row["is_traded"])
		if !okTraded || !isTraded {
			continue
		}

		boardID, okBoard := stringValue(row["boardid"])
		currencyText, okCurrency := stringValue(row["currencyid"])
		currencyCode, okNormalizedCurrency := normalizeCurrency(currencyText)
		if !okBoard || !okCurrency || !okNormalizedCurrency {
			continue
		}

		candidate := board{ID: boardID, Currency: currencyCode}
		tradedCount++
		tradedBoard = candidate

		isPrimary, okPrimary := boolFlag(row["is_primary"])
		if !okPrimary || !isPrimary {
			continue
		}

		if primary != nil && *primary != candidate {
			return board{}, fmt.Errorf("MOEX returned more than one traded primary board for %s", security.SecurityID)
		}
		primary = &candidate
	}

	if primary != nil {
		return *primary, nil
	}
	if tradedCount == 1 {
		return tradedBoard, nil
	}
	if tradedCount > 1 {
		return board{}, fmt.Errorf("MOEX returned multiple traded boards and no primary board for %s", security.SecurityID)
	}

	return board{}, fmt.Errorf("MOEX returned no traded board for %s", security.SecurityID)
}

func (p *Provider) fetchQuote(ctx context.Context, security marketSecurity, currentBoard board) (*marketQuote, error) {
	requestURL, errURL := url.Parse(
		p.baseURL + "/iss/engines/" + url.PathEscape(security.Engine) +
			"/markets/" + url.PathEscape(security.Market) +
			"/boards/" + url.PathEscape(currentBoard.ID) +
			"/securities/" + url.PathEscape(security.SecurityID) + "/securities.json",
	)
	if errURL != nil {
		return nil, fmt.Errorf("build quote URL for %s: %w", security.SecurityID, errURL)
	}

	query := requestURL.Query()
	query.Set("iss.meta", "off")
	query.Set("iss.only", "marketdata,securities")
	query.Set("marketdata.columns", "LAST,TRADEDATE,TIME,UPDATETIME,SYSTIME")
	query.Set("securities.columns", "PREVPRICE,PREVDATE")
	requestURL.RawQuery = query.Encode()

	var payload issQuoteResponse
	if err := p.fetchJSON(ctx, requestURL, &payload); err != nil {
		return nil, fmt.Errorf("request quote for %s: %w", security.SecurityID, err)
	}

	if quote, ok := currentQuote(payload.MarketData, currentBoard.Currency); ok {
		return quote, nil
	}
	if quote, ok := previousQuote(payload.Securities, currentBoard.Currency); ok {
		return quote, nil
	}

	return nil, fmt.Errorf("quote for %s does not contain a usable price", security.SecurityID)
}

func (p *Provider) fetchJSON(ctx context.Context, requestURL *url.URL, target any) error {
	request, errRequest := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if errRequest != nil {
		return fmt.Errorf("create MOEX ISS request: %w", errRequest)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cache-Control", "no-cache")
	request.Header.Set("User-Agent", providers.UserAgent)

	response, errDo := p.client.Do(request)
	if errDo != nil {
		return fmt.Errorf("perform MOEX ISS request: %w", errDo)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return fmt.Errorf("MOEX ISS returned HTTP %d", response.StatusCode)
	}

	body, errRead := io.ReadAll(io.LimitReader(response.Body, maxResponseSize+1))
	if errRead != nil {
		return fmt.Errorf("read MOEX ISS response: %w", errRead)
	}
	if len(body) > maxResponseSize {
		return fmt.Errorf("MOEX ISS response exceeds %d bytes", maxResponseSize)
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if errDecode := decoder.Decode(target); errDecode != nil {
		return fmt.Errorf("decode MOEX ISS response: %w", errDecode)
	}

	return nil
}

func currentQuote(block issBlock, currency string) (*marketQuote, bool) {
	row, ok := firstRow(block)
	if !ok {
		return nil, false
	}

	value, ok := positiveDecimal(row["LAST"])
	if !ok {
		return nil, false
	}

	pricedAt, okPricedAt := currentQuoteTime(row)
	if !okPricedAt {
		return nil, false
	}

	return &marketQuote{
		Value:    value,
		Currency: currency,
		PricedAt: pricedAt.UTC(),
		Source:   "last",
	}, true
}

func currentQuoteTime(row map[string]any) (time.Time, bool) {
	tradeDate, hasTradeDate := stringValue(row["TRADEDATE"])
	tradeTime, hasTradeTime := stringValue(row["TIME"])
	if hasTradeDate && hasTradeTime {
		pricedAt, err := time.ParseInLocation(time.DateTime, tradeDate+" "+tradeTime, moscowLocation)
		return pricedAt, err == nil
	}

	systemTime, hasSystemTime := stringValue(row["SYSTIME"])
	if hasSystemTime {
		systemAt, err := time.ParseInLocation(time.DateTime, systemTime, moscowLocation)
		if err == nil {
			if hasTradeTime {
				pricedAt, errTradeTime := time.ParseInLocation(
					time.DateTime,
					systemAt.Format(time.DateOnly)+" "+tradeTime,
					moscowLocation,
				)
				if errTradeTime == nil {
					return pricedAt, true
				}
			}
			return systemAt, true
		}
	}

	updateTime, hasUpdateTime := stringValue(row["UPDATETIME"])
	if hasTradeDate && hasUpdateTime {
		pricedAt, err := time.ParseInLocation(time.DateTime, tradeDate+" "+updateTime, moscowLocation)
		return pricedAt, err == nil
	}

	return time.Time{}, false
}

func previousQuote(block issBlock, currency string) (*marketQuote, bool) {
	row, ok := firstRow(block)
	if !ok {
		return nil, false
	}

	value, ok := positiveDecimal(row["PREVPRICE"])
	if !ok {
		return nil, false
	}

	previousDate, okDate := stringValue(row["PREVDATE"])
	if !okDate {
		return nil, false
	}

	pricedAt, err := time.ParseInLocation(time.DateOnly, previousDate, moscowLocation)
	if err != nil {
		return nil, false
	}

	return &marketQuote{
		Value:    value,
		Currency: currency,
		PricedAt: pricedAt.UTC(),
		Source:   "previous",
	}, true
}

func firstRow(block issBlock) (map[string]any, bool) {
	if len(block.Columns) == 0 || len(block.Data) == 0 {
		return nil, false
	}

	return rowMap(block.Columns, block.Data[0])
}

func rowMap(columns []string, data []any) (map[string]any, bool) {
	if len(columns) == 0 || len(data) != len(columns) {
		return nil, false
	}

	result := make(map[string]any, len(columns))
	for index, column := range columns {
		result[column] = data[index]
	}
	return result, true
}

func boolFlag(value any) (bool, bool) {
	switch typed := value.(type) {
	case json.Number:
		if typed.String() == "1" {
			return true, true
		}
		if typed.String() == "0" {
			return false, true
		}
	case string:
		text := strings.TrimSpace(typed)
		if text == "1" {
			return true, true
		}
		if text == "0" {
			return false, true
		}
	case bool:
		return typed, true
	}

	return false, false
}

func positiveDecimal(value any) (string, bool) {
	var text string

	switch typed := value.(type) {
	case json.Number:
		text = typed.String()
	case string:
		text = typed
	default:
		return "", false
	}

	canonical, err := decimal.Canonical(text)
	if err != nil {
		return "", false
	}

	number, ok := decimal.Parse(canonical)
	if !ok || number.Sign() <= 0 {
		return "", false
	}

	return canonical, true
}

func stringValue(value any) (string, bool) {
	text, ok := value.(string)
	text = strings.TrimSpace(text)

	return text, ok && text != ""
}

func normalizeCurrency(value string) (string, bool) {
	code := strings.ToUpper(strings.TrimSpace(value))
	if code == "SUR" {
		code = "RUB"
	}

	if !currency.ValidCode(code) {
		return "", false
	}

	return code, true
}
