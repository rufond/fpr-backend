package yahoo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

const (
	defaultBatchSize    = 20
	defaultTimeout      = 30 * time.Second
	defaultRequestDelay = time.Second

	yahooSparkURL   = "https://query1.finance.yahoo.com/v7/finance/spark"
	maxResponseSize = 16 << 20
	chromeUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"
)

type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
	CloseIdleConnections()
}

type Provider struct {
	client       httpClient
	batchSize    int
	requestDelay time.Duration
}

type quote struct {
	Symbol            string
	Currency          string
	Price             string
	RegularMarketTime time.Time
}

type fetchResult struct {
	RequestedSymbols int
	ReturnedSymbols  int
	Batches          int

	Quotes map[string]quote

	Missing    []string
	Unexpected []string
	Duplicates []string
}

type decimalNumber struct {
	Text string
}

func (n *decimalNumber) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*n = decimalNumber{}

		return nil
	}

	if strings.HasPrefix(raw, "\"") {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}

		raw = strings.TrimSpace(text)
	}

	n.Text = raw

	return nil
}

type sparkEnvelope struct {
	Spark sparkPayload `json:"spark"`
}

type sparkPayload struct {
	Result []sparkResult   `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type sparkResult struct {
	Symbol   string          `json:"symbol"`
	Response []sparkResponse `json:"response"`
}

type sparkResponse struct {
	Meta sparkMeta `json:"meta"`
}

type sparkMeta struct {
	Currency           string        `json:"currency"`
	RegularMarketTime  int64         `json:"regularMarketTime"`
	RegularMarketPrice decimalNumber `json:"regularMarketPrice"`
}

func NewProvider() (*Provider, error) {
	client, err := tlsclient.NewHttpClient(
		tlsclient.NewNoopLogger(),
		tlsclient.WithTimeoutMilliseconds(int(defaultTimeout.Milliseconds())),
		tlsclient.WithClientProfile(profiles.Chrome_146),
		tlsclient.WithDisableHttp3(),
	)
	if err != nil {
		return nil, fmt.Errorf("create Yahoo TLS client: %w", err)
	}

	return newProvider(client, defaultBatchSize, defaultRequestDelay), nil
}

func newProvider(client httpClient, batchSize int, requestDelay time.Duration) *Provider {
	if batchSize < 1 {
		batchSize = defaultBatchSize
	}

	return &Provider{client: client, batchSize: batchSize, requestDelay: requestDelay}
}

func (p *Provider) Close() {
	if p == nil || p.client == nil {
		return
	}

	p.client.CloseIdleConnections()
}

func (p *Provider) fetch(ctx context.Context, symbols []string) (fetchResult, error) {
	result := fetchResult{
		RequestedSymbols: len(symbols),
		Quotes:           make(map[string]quote, len(symbols)),
	}

	requested := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		requested[symbol] = struct{}{}
	}

	duplicates := map[string]struct{}{}
	unexpected := map[string]struct{}{}

	batchCount := (len(symbols) + p.batchSize - 1) / p.batchSize
	batchIndex := 0
	for batch := range slices.Chunk(symbols, p.batchSize) {
		items, returned, errFetch := p.fetchBatch(ctx, batch)
		if errFetch != nil {
			return fetchResult{}, fmt.Errorf("fetch Yahoo batch %d/%d: %w", batchIndex+1, batchCount, errFetch)
		}

		result.Batches++
		result.ReturnedSymbols += returned

		for _, item := range items {
			symbol := normalizeSymbol(item.Symbol)
			if symbol == "" {
				continue
			}

			if _, expected := requested[symbol]; !expected {
				unexpected[symbol] = struct{}{}
				continue
			}

			if _, exists := result.Quotes[symbol]; exists {
				duplicates[symbol] = struct{}{}
				continue
			}

			item.Symbol = symbol
			result.Quotes[symbol] = item
		}

		batchIndex++
		if batchIndex < batchCount {
			if errDelay := p.waitRequestDelay(ctx); errDelay != nil {
				return fetchResult{}, errDelay
			}
		}
	}

	for _, symbol := range symbols {
		if _, exists := result.Quotes[symbol]; !exists {
			result.Missing = append(result.Missing, symbol)
		}
	}

	result.Unexpected = slices.Sorted(maps.Keys(unexpected))
	result.Duplicates = slices.Sorted(maps.Keys(duplicates))

	return result, nil
}

func (p *Provider) fetchBatch(ctx context.Context, symbols []string) ([]quote, int, error) {
	requestURL := buildURL(symbols)
	req, errRequest := http.NewRequest(http.MethodGet, requestURL, nil)
	if errRequest != nil {
		return nil, 0, fmt.Errorf("create request: %w", errRequest)
	}

	req = req.WithContext(ctx)
	req.Header = browserHeaders()

	resp, errDo := p.client.Do(req)
	if errDo != nil {
		return nil, 0, fmt.Errorf("request: %w", errDo)
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, errRead := readResponseBody(resp.Body)
	if errRead != nil {
		return nil, 0, fmt.Errorf("read response: %w", errRead)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, responsePreview(body))
	}

	var envelope sparkEnvelope

	if errDecode := json.Unmarshal(body, &envelope); errDecode != nil {
		return nil, 0, fmt.Errorf("decode Yahoo JSON: %w", errDecode)
	}

	if len(envelope.Spark.Error) > 0 && string(envelope.Spark.Error) != "null" {
		return nil, 0, fmt.Errorf("Yahoo spark error: %s", strings.TrimSpace(string(envelope.Spark.Error)))
	}

	items := make([]quote, 0, len(envelope.Spark.Result))

	for _, item := range envelope.Spark.Result {
		quoteObject := quote{Symbol: item.Symbol}

		if len(item.Response) > 0 {
			meta := item.Response[0].Meta

			quoteObject.Currency = strings.TrimSpace(meta.Currency)
			quoteObject.Price = meta.RegularMarketPrice.Text

			if meta.RegularMarketTime > 0 {
				quoteObject.RegularMarketTime = time.Unix(meta.RegularMarketTime, 0).UTC()
			}
		}

		items = append(items, quoteObject)
	}

	return items, len(envelope.Spark.Result), nil
}

func buildURL(symbols []string) string {
	query := url.Values{}
	query.Set("symbols", strings.Join(symbols, ","))
	query.Set("range", "5m")
	query.Set("includeTimestamps", "false")
	query.Set("corsDomain", "finance.yahoo.com")
	query.Set(".tsrc", "finance")

	return yahooSparkURL + "?" + query.Encode()
}

func browserHeaders() http.Header {
	return http.Header{
		"sec-ch-ua":          {`"Not_A Brand";v="99", "Chromium";v="146", "Google Chrome";v="146"`},
		"sec-ch-ua-mobile":   {"?0"},
		"sec-ch-ua-platform": {`"Linux"`},
		"user-agent":         {chromeUserAgent},
		"accept":             {"*/*"},
		"accept-language":    {"ru-RU,ru;q=0.9,es;q=0.8,en;q=0.7,en-US;q=0.6"},
		"accept-encoding":    {"gzip, deflate, br, zstd"},
		"referer":            {"https://finance.yahoo.com/"},
		"origin":             {"https://finance.yahoo.com"},
		"sec-fetch-dest":     {"empty"},
		"sec-fetch-mode":     {"cors"},
		"sec-fetch-site":     {"same-site"},
		"priority":           {"u=1, i"},
		http.HeaderOrderKey: {
			"sec-ch-ua",
			"sec-ch-ua-mobile",
			"sec-ch-ua-platform",
			"user-agent",
			"accept",
			"accept-language",
			"accept-encoding",
			"referer",
			"origin",
			"sec-fetch-dest",
			"sec-fetch-mode",
			"sec-fetch-site",
			"priority",
		},
	}
}

func normalizeSymbol(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func (p *Provider) waitRequestDelay(ctx context.Context) error {
	if p.requestDelay <= 0 {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(p.requestDelay):
		return nil
	}
}

func readResponseBody(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxResponseSize+1))
	if err != nil {
		return nil, err
	}

	if len(data) > maxResponseSize {
		return nil, fmt.Errorf("response exceeds %d bytes", maxResponseSize)
	}

	return data, nil
}

func responsePreview(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) <= 300 {
		return text
	}

	return text[:300] + "..."
}
