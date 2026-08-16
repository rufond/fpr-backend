package yahoo

import (
	"context"
	"testing"
	"time"
)

func TestFetchPricesNormalizesQuotesAndKeepsInvalidPerSymbol(t *testing.T) {
	t.Parallel()

	client := &fakeHTTPClient{}
	client.responses = append(client.responses, jsonResponse(`{
		"spark": {
			"result": [
				{"symbol": "AZN.L", "response": [{"meta": {
					"currency": "GBp",
					"symbol": "AZN.L",
					"regularMarketTime": 1785771000,
					"regularMarketPrice": 12632.0
				}}]},
				{"symbol": "BAD", "response": [{"meta": {
					"currency": "USD",
					"symbol": "BAD",
					"regularMarketTime": 1785771000,
					"regularMarketPrice": "N/A"
				}}]}
			],
			"error": null
		}
	}`))

	provider := newProvider(client, 20, 0)
	result, err := provider.FetchPrices(context.Background(), []string{" azn.l ", "bad"})
	if err != nil {
		t.Fatalf("FetchPrices() error = %v", err)
	}

	quote, exists := result.QuotesByRequest[" azn.l "]
	if !exists {
		t.Fatalf("quotes = %#v", result.QuotesByRequest)
	}
	if quote.UnitValue != "126.32" || quote.Currency != "GBP" || !quote.PricedAt.Equal(time.Unix(1785771000, 0).UTC()) {
		t.Fatalf("quote = %#v", quote)
	}
	if len(result.Invalid) != 1 || result.Invalid[0].Symbol != "BAD" {
		t.Fatalf("invalid = %#v", result.Invalid)
	}
	if _, exists := result.QuotesByRequest["bad"]; exists {
		t.Fatalf("invalid quote was published: %#v", result.QuotesByRequest["bad"])
	}
}
