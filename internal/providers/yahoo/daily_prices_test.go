package yahoo

import (
	"context"
	"testing"
	"time"

	http "github.com/bogdanfinn/fhttp"
)

func TestFetchDailyPricesUsesLatestCloseNotAfterOfficialDate(t *testing.T) {
	t.Parallel()

	client := &fakeHTTPClient{responses: []*http.Response{jsonResponse(`{
		"chart": {
			"result": [{
				"meta": {
					"currency": "USD",
					"exchangeTimezoneName": "UTC"
				},
				"timestamp": [1786608000, 1786694400, 1786780800],
				"indicators": {
					"quote": [{"close": [125, 126.32, 130]}]
				}
			}],
			"error": null
		}
	}`)}}

	provider := newProvider(client, 20, 0)
	till := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	result, err := provider.FetchDailyPrices(context.Background(), []string{"FRHC"}, till)
	if err != nil {
		t.Fatalf("FetchDailyPrices() error = %v", err)
	}

	price, exists := result.PricesBySymbol["FRHC"]
	if !exists {
		t.Fatalf("PricesBySymbol = %#v", result.PricesBySymbol)
	}
	if price.UnitValue != "126.32" || price.Currency != "USD" || !price.PriceDate.Equal(till) {
		t.Fatalf("price = %#v", price)
	}
	wantPricedAt := time.Unix(1786694400, 0).UTC()
	if !price.PricedAt.Equal(wantPricedAt) {
		t.Fatalf("PricedAt = %s, want %s", price.PricedAt, wantPricedAt)
	}

	request := client.requests[0]
	if request.URL.Path != "/v8/finance/chart/FRHC" {
		t.Fatalf("path = %q", request.URL.Path)
	}
	query := request.URL.Query()
	if query.Get("interval") != "1d" || query.Get("includePrePost") != "false" {
		t.Fatalf("query = %#v", query)
	}
	if values := request.Header["user-agent"]; len(values) != 1 || values[0] != chromeUserAgent {
		t.Fatalf("user-agent headers = %#v", request.Header)
	}
}
