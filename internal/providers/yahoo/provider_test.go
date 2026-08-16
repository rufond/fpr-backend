package yahoo

import (
	"context"
	"io"
	"strings"
	"testing"

	http "github.com/bogdanfinn/fhttp"
)

type fakeHTTPClient struct {
	responses []*http.Response
	requests  []*http.Request
}

func (f *fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	f.requests = append(f.requests, req)
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

func (f *fakeHTTPClient) CloseIdleConnections() {}

func TestProviderFetchParsesCompactSparkResponse(t *testing.T) {
	t.Parallel()

	client := &fakeHTTPClient{responses: []*http.Response{jsonResponse(`{
		"spark": {
			"result": [{
				"symbol": "AZN.L",
				"response": [{"meta": {
					"currency": "GBp",
					"symbol": "AZN.L",
					"exchangeName": "LSE",
					"instrumentType": "EQUITY",
					"regularMarketTime": 1785771000,
					"regularMarketPrice": 12632.0,
					"previousClose": 12720.0,
					"exchangeTimezoneName": "Europe/London"
				}}]
			}],
			"error": null
		}
	}`)}}

	provider := newProvider(client, 20, 0)
	result, err := provider.fetch(context.Background(), []string{"AZN.L"})
	if err != nil {
		t.Fatal(err)
	}

	quote := result.Quotes["AZN.L"]
	if quote.Price != "12632.0" || quote.Currency != "GBp" {
		t.Fatalf("quote = %#v", quote)
	}
	if quote.RegularMarketTime.IsZero() {
		t.Fatalf("quote metadata = %#v", quote)
	}
	if len(result.Missing) != 0 || len(result.Unexpected) != 0 || len(result.Duplicates) != 0 {
		t.Fatalf("coverage = %#v", result)
	}

	request := client.requests[0]
	if !strings.Contains(request.URL.RawQuery, "symbols=AZN.L") || !strings.Contains(request.URL.RawQuery, "range=5m") {
		t.Fatalf("query = %q", request.URL.RawQuery)
	}
	if values := request.Header["user-agent"]; len(values) != 1 || values[0] != chromeUserAgent {
		t.Fatalf("user-agent headers = %#v", request.Header)
	}
	if values := request.Header["priority"]; len(values) != 1 || values[0] != "u=1, i" {
		t.Fatalf("priority headers = %#v", request.Header)
	}
}

func TestProviderFetchReportsMissingUnexpectedAndDuplicateSymbols(t *testing.T) {
	t.Parallel()

	client := &fakeHTTPClient{responses: []*http.Response{jsonResponse(`{
		"spark": {
			"result": [
				{"symbol": "AAPL", "response": [{"meta": {"symbol": "AAPL", "regularMarketPrice": 1, "regularMarketTime": 1785771000}}]},
				{"symbol": "AAPL", "response": [{"meta": {"symbol": "AAPL", "regularMarketPrice": 2, "regularMarketTime": 1785771001}}]},
				{"symbol": "MSFT", "response": [{"meta": {"symbol": "MSFT", "regularMarketPrice": 3, "regularMarketTime": 1785771002}}]}
			],
			"error": null
		}
	}`)}}

	provider := newProvider(client, 20, 0)
	result, err := provider.fetch(context.Background(), []string{"AAPL", "KO"})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Missing) != 1 || result.Missing[0] != "KO" {
		t.Fatalf("missing = %#v", result.Missing)
	}
	if len(result.Unexpected) != 1 || result.Unexpected[0] != "MSFT" {
		t.Fatalf("unexpected = %#v", result.Unexpected)
	}
	if len(result.Duplicates) != 1 || result.Duplicates[0] != "AAPL" {
		t.Fatalf("duplicates = %#v", result.Duplicates)
	}
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Proto:      "HTTP/2.0",
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    &http.Request{},
	}
}

func TestProviderFetchKeepsInvalidPerSymbolPriceForServiceValidation(t *testing.T) {
	t.Parallel()

	client := &fakeHTTPClient{responses: []*http.Response{jsonResponse(`{
		"spark": {
			"result": [{"symbol": "BAD", "response": [{"meta": {
				"symbol": "BAD",
				"currency": "USD",
				"regularMarketTime": 1785771000,
				"regularMarketPrice": "N/A"
			}}]}],
			"error": null
		}
	}`)}}

	provider := newProvider(client, 20, 0)
	result, err := provider.fetch(context.Background(), []string{"BAD"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Quotes["BAD"].Price != "N/A" {
		t.Fatalf("quote = %#v", result.Quotes["BAD"])
	}
}
