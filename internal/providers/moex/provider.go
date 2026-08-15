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
	"strings"
	"time"

	"github.com/rufond/fpr-backend/internal/prices"
)

const (
	defaultBaseURL  = "https://iss.moex.com"
	requestTimeout  = 15 * time.Second
	maxResponseSize = 1 << 20
)

var moscowLocation = time.FixedZone("MSK", 3*60*60)

type Provider struct {
	baseURL string
	client  *http.Client
}

type issBlock struct {
	Columns []string `json:"columns"`
	Data    [][]any  `json:"data"`
}

type issResponse struct {
	MarketData issBlock `json:"marketdata"`
	Securities issBlock `json:"securities"`
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
	}
}

func (p *Provider) FetchFundUnitQuote(ctx context.Context) (*prices.SourceQuote, error) {
	requestURL, errURL := url.Parse(p.baseURL + "/iss/engines/stock/markets/shares/boards/TQBR/securities/" + prices.FundUnitISIN + "/securities.json")
	if errURL != nil {
		return nil, fmt.Errorf("build MOEX fund unit URL: %w", errURL)
	}

	query := requestURL.Query()
	query.Set("iss.meta", "off")
	query.Set("iss.only", "marketdata,securities")
	query.Set("marketdata.columns", "LAST,TRADEDATE,UPDATETIME")
	query.Set("securities.columns", "PREVPRICE,PREVDATE")
	requestURL.RawQuery = query.Encode()

	request, errRequest := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if errRequest != nil {
		return nil, fmt.Errorf("create MOEX fund unit request: %w", errRequest)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cache-Control", "no-cache")
	request.Header.Set("User-Agent", "FPR/1.0")

	response, errDo := p.client.Do(request)
	if errDo != nil {
		return nil, fmt.Errorf("request MOEX fund unit quote: %w", errDo)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MOEX fund unit quote returned HTTP %d", response.StatusCode)
	}

	body, errRead := io.ReadAll(io.LimitReader(response.Body, maxResponseSize+1))
	if errRead != nil {
		return nil, fmt.Errorf("read MOEX fund unit quote: %w", errRead)
	}
	if len(body) > maxResponseSize {
		return nil, fmt.Errorf("MOEX fund unit quote response exceeds %d bytes", maxResponseSize)
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var payload issResponse
	if errDecode := decoder.Decode(&payload); errDecode != nil {
		return nil, fmt.Errorf("decode MOEX fund unit quote: %w", errDecode)
	}

	if quote, ok := currentQuote(payload.MarketData); ok {
		return quote, nil
	}
	if quote, ok := previousQuote(payload.Securities); ok {
		return quote, nil
	}

	return nil, fmt.Errorf("MOEX fund unit quote does not contain a usable price")
}

func currentQuote(block issBlock) (*prices.SourceQuote, bool) {
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
		Currency:  "RUB",
		PricedAt:  pricedAt.UTC(),
		Source:    "last",
	}, true
}

func previousQuote(block issBlock) (*prices.SourceQuote, bool) {
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
		Currency:  "RUB",
		PricedAt:  pricedAt.UTC(),
		Source:    "previous",
	}, true
}

func firstRow(block issBlock) (map[string]any, bool) {
	if len(block.Columns) == 0 || len(block.Data) == 0 {
		return nil, false
	}
	if len(block.Data[0]) != len(block.Columns) {
		return nil, false
	}

	result := make(map[string]any, len(block.Columns))
	for index, column := range block.Columns {
		result[column] = block.Data[0][index]
	}
	return result, true
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
