package moex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/providers"
)

const (
	defaultBaseURL  = "https://iss.moex.com"
	requestTimeout  = 15 * time.Second
	maxResponseSize = 1 << 20
	maxHistoryPages = 100
)

var moscowLocation = time.FixedZone("MSK", 3*60*60)

var _ prices.Source = (*Provider)(nil)

type Provider struct {
	baseURL string
	client  *http.Client
	now     func() time.Time

	boardMu   sync.Mutex
	board     board
	boardDate string
}

type board struct {
	ID       string
	Currency string
}

type issBlock struct {
	Columns []string `json:"columns"`
	Data    [][]any  `json:"data"`
}

type issSecurityResponse struct {
	Boards issBlock `json:"boards"`
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
	}
}

func (p *Provider) FetchFundUnitQuote(ctx context.Context) (*prices.SourceQuote, error) {
	currentBoard, errBoard := p.resolvePrimaryBoard(ctx, false)
	if errBoard != nil {
		return nil, fmt.Errorf("resolve MOEX fund unit primary board: %w", errBoard)
	}

	quote, errQuote := p.fetchFundUnitQuote(ctx, currentBoard)
	if errQuote == nil {
		return quote, nil
	}

	refreshedBoard, errRefresh := p.resolvePrimaryBoard(ctx, true)
	if errRefresh != nil {
		return nil, fmt.Errorf(
			"fetch MOEX fund unit quote on board %s: %v; refresh primary board: %w",
			currentBoard.ID,
			errQuote,
			errRefresh,
		)
	}

	quote, errRetry := p.fetchFundUnitQuote(ctx, refreshedBoard)
	if errRetry != nil {
		return nil, fmt.Errorf("fetch MOEX fund unit quote on board %s after refresh: %w", refreshedBoard.ID, errRetry)
	}

	return quote, nil
}

func (p *Provider) FetchFundUnitDailyPrices(ctx context.Context, from time.Time) ([]prices.SourceDailyPrice, error) {
	from = calendarDateUTC(from)
	till := p.previousMoscowDate()
	if from.After(till) {
		return []prices.SourceDailyPrice{}, nil
	}

	currentBoard, errBoard := p.resolvePrimaryBoard(ctx, false)
	if errBoard != nil {
		return nil, fmt.Errorf("resolve MOEX fund unit primary board for daily prices: %w", errBoard)
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
		query.Set("from", from.Format("2006-01-02"))
		query.Set("till", till.Format("2006-01-02"))
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
			if !okBegin || len(begin) < len("2006-01-02") {
				return nil, fmt.Errorf("MOEX fund unit daily candle has invalid begin value")
			}

			priceDate, errDate := time.Parse("2006-01-02", begin[:10])
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
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return today.AddDate(0, 0, -1)
}

func normalizeDailyPrices(items []prices.SourceDailyPrice) ([]prices.SourceDailyPrice, error) {
	sort.Slice(items, func(left int, right int) bool {
		return items[left].PriceDate.Before(items[right].PriceDate)
	})

	result := make([]prices.SourceDailyPrice, 0, len(items))
	for _, item := range items {
		item.PriceDate = calendarDateUTC(item.PriceDate)

		if len(result) != 0 && result[len(result)-1].PriceDate.Equal(item.PriceDate) {
			previous := result[len(result)-1]
			if previous.UnitValue != item.UnitValue || previous.Currency != item.Currency {
				return nil, fmt.Errorf("MOEX returned conflicting daily candles for %s", item.PriceDate.Format("2006-01-02"))
			}
			continue
		}

		result = append(result, item)
	}

	return result, nil
}

func calendarDateUTC(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func (p *Provider) resolvePrimaryBoard(ctx context.Context, force bool) (board, error) {
	date := p.now().In(moscowLocation).Format("2006-01-02")

	p.boardMu.Lock()
	if !force && p.board.ID != "" && p.boardDate == date {
		cached := p.board
		p.boardMu.Unlock()
		return cached, nil
	}
	p.boardMu.Unlock()

	resolved, err := p.fetchPrimaryBoard(ctx)
	if err != nil {
		return board{}, err
	}

	p.boardMu.Lock()
	p.board = resolved
	p.boardDate = date
	p.boardMu.Unlock()

	return resolved, nil
}

func (p *Provider) fetchPrimaryBoard(ctx context.Context) (board, error) {
	requestURL, errURL := url.Parse(p.baseURL + "/iss/securities/" + url.PathEscape(prices.FundUnitISIN) + ".json")
	if errURL != nil {
		return board{}, fmt.Errorf("build MOEX fund unit boards URL: %w", errURL)
	}

	query := requestURL.Query()
	query.Set("iss.meta", "off")
	query.Set("iss.only", "boards")
	query.Set("boards.columns", "boardid,is_traded,is_primary,currencyid")
	requestURL.RawQuery = query.Encode()

	var payload issSecurityResponse
	if err := p.fetchJSON(ctx, requestURL, &payload); err != nil {
		return board{}, fmt.Errorf("request MOEX fund unit boards: %w", err)
	}

	var primary *board
	for _, data := range payload.Boards.Data {
		row, ok := rowMap(payload.Boards.Columns, data)
		if !ok {
			continue
		}

		isPrimary, okPrimary := boolFlag(row["is_primary"])
		isTraded, okTraded := boolFlag(row["is_traded"])
		if !okPrimary || !okTraded || !isPrimary || !isTraded {
			continue
		}

		boardID, okBoard := stringValue(row["boardid"])
		currencyText, okCurrency := stringValue(row["currencyid"])
		currency, okNormalizedCurrency := normalizeCurrency(currencyText)
		if !okBoard || !okCurrency || !okNormalizedCurrency {
			continue
		}

		candidate := board{ID: boardID, Currency: currency}
		if primary != nil && *primary != candidate {
			return board{}, fmt.Errorf("MOEX returned more than one traded primary board for %s", prices.FundUnitISIN)
		}
		primary = &candidate
	}

	if primary == nil {
		return board{}, fmt.Errorf("MOEX returned no traded primary board for %s", prices.FundUnitISIN)
	}

	return *primary, nil
}

func (p *Provider) fetchFundUnitQuote(ctx context.Context, currentBoard board) (*prices.SourceQuote, error) {
	requestURL, errURL := url.Parse(
		p.baseURL + "/iss/engines/stock/markets/shares/boards/" + url.PathEscape(currentBoard.ID) +
			"/securities/" + url.PathEscape(prices.FundUnitISIN) + "/securities.json",
	)
	if errURL != nil {
		return nil, fmt.Errorf("build MOEX fund unit quote URL: %w", errURL)
	}

	query := requestURL.Query()
	query.Set("iss.meta", "off")
	query.Set("iss.only", "marketdata,securities")
	query.Set("marketdata.columns", "LAST,TRADEDATE,UPDATETIME")
	query.Set("securities.columns", "PREVPRICE,PREVDATE")
	requestURL.RawQuery = query.Encode()

	var payload issQuoteResponse
	if err := p.fetchJSON(ctx, requestURL, &payload); err != nil {
		return nil, fmt.Errorf("request MOEX fund unit quote: %w", err)
	}

	if quote, ok := currentQuote(payload.MarketData, currentBoard.Currency); ok {
		return quote, nil
	}
	if quote, ok := previousQuote(payload.Securities, currentBoard.Currency); ok {
		return quote, nil
	}

	return nil, fmt.Errorf("MOEX fund unit quote does not contain a usable price")
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

func currentQuote(block issBlock, currency string) (*prices.SourceQuote, bool) {
	row, ok := firstRow(block)
	if !ok {
		return nil, false
	}

	unitValue, ok := positiveDecimal(row["LAST"])
	if !ok {
		return nil, false
	}

	tradeDate, okDate := stringValue(row["TRADEDATE"])
	updateTime, okTime := stringValue(row["UPDATETIME"])
	if !okDate || !okTime {
		return nil, false
	}

	pricedAt, err := time.ParseInLocation("2006-01-02 15:04:05", tradeDate+" "+updateTime, moscowLocation)
	if err != nil {
		return nil, false
	}

	return &prices.SourceQuote{
		UnitValue: unitValue,
		Currency:  currency,
		PricedAt:  pricedAt.UTC(),
		Source:    "last",
	}, true
}

func previousQuote(block issBlock, currency string) (*prices.SourceQuote, bool) {
	row, ok := firstRow(block)
	if !ok {
		return nil, false
	}

	unitValue, ok := positiveDecimal(row["PREVPRICE"])
	if !ok {
		return nil, false
	}

	previousDate, okDate := stringValue(row["PREVDATE"])
	if !okDate {
		return nil, false
	}

	pricedAt, err := time.ParseInLocation("2006-01-02", previousDate, moscowLocation)
	if err != nil {
		return nil, false
	}

	return &prices.SourceQuote{
		UnitValue: unitValue,
		Currency:  currency,
		PricedAt:  pricedAt.UTC(),
		Source:    "previous",
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

	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}

	number, ok := new(big.Rat).SetString(text)
	if !ok || number.Sign() <= 0 {
		return "", false
	}

	return canonicalDecimal(text), true
}

func canonicalDecimal(value string) string {
	text := strings.TrimSpace(value)
	text = strings.TrimPrefix(text, "+")

	parts := strings.Split(text, ".")
	if len(parts) != 2 {
		return text
	}

	integer := strings.TrimLeft(parts[0], "0")
	if integer == "" {
		integer = "0"
	}
	fraction := strings.TrimRight(parts[1], "0")
	if fraction == "" {
		return integer
	}
	return integer + "." + fraction
}

func stringValue(value any) (string, bool) {
	text, ok := value.(string)
	text = strings.TrimSpace(text)
	return text, ok && text != ""
}

func normalizeCurrency(value string) (string, bool) {
	currency := strings.ToUpper(strings.TrimSpace(value))
	if currency == "SUR" {
		currency = "RUB"
	}
	if len(currency) != 3 {
		return "", false
	}
	for _, char := range currency {
		if char < 'A' || char > 'Z' {
			return "", false
		}
	}
	return currency, true
}
