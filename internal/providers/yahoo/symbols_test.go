package yahoo

import (
	"context"
	"testing"

	http "github.com/bogdanfinn/fhttp"
)

func TestResolveSymbolsUsesYahooSearchByISIN(t *testing.T) {
	t.Parallel()

	client := &fakeHTTPClient{responses: append([]*http.Response(nil),
		jsonResponse(`{"quotes":[{"symbol":"frhc","quoteType":"EQUITY"}]}`),
		jsonResponse(`{"quotes":[]}`),
	)}

	provider := newProvider(client, 20, 0)
	result, err := provider.ResolveSymbols(context.Background(), []string{
		"US3563901046",
		"KZ1C00001122",
	})
	if err != nil {
		t.Fatalf("ResolveSymbols() error = %v", err)
	}

	if result.RequestedISINs != 2 {
		t.Fatalf("RequestedISINs = %d, want 2", result.RequestedISINs)
	}
	if result.SymbolsByISIN["US3563901046"] != "FRHC" {
		t.Fatalf("SymbolsByISIN = %#v", result.SymbolsByISIN)
	}
	if len(result.MissingISINs) != 1 || result.MissingISINs[0] != "KZ1C00001122" {
		t.Fatalf("MissingISINs = %#v", result.MissingISINs)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests len = %d, want 2", len(client.requests))
	}

	request := client.requests[0]
	if request.URL.Scheme != "https" || request.URL.Host != "query2.finance.yahoo.com" || request.URL.Path != "/v1/finance/search" {
		t.Fatalf("request URL = %q", request.URL.String())
	}
	query := request.URL.Query()
	if query.Get("q") != "US3563901046" ||
		query.Get("quotesCount") != "1" ||
		query.Get("enableFuzzyQuery") != "false" ||
		query.Get("newsCount") != "0" ||
		query.Get("listsCount") != "0" ||
		query.Get("recommendedCount") != "0" {
		t.Fatalf("query = %#v", query)
	}
	if values := request.Header["user-agent"]; len(values) != 1 || values[0] != chromeUserAgent {
		t.Fatalf("user-agent headers = %#v", request.Header)
	}
}
