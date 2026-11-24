package nasdaq

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseQuoteUsesExchangeTimeAndDailyChange(t *testing.T) {
	var payload quotePayload
	err := json.Unmarshal([]byte(`{"data":{"symbol":"FRHC","assetClass":"STOCKS","primaryData":{
 "lastSalePrice":"$164.06","netChange":"-0.19","lastTradeTimestamp":"Nov 24, 2026 9:50 AM ET"
 }}}`), &payload)
	if err != nil {
		t.Fatal(err)
	}
	provider := NewProvider()
	quote, err := provider.parseQuote("FRHC", payload)
	if err != nil {
		t.Fatal(err)
	}
	if quote.PreviousClose != "164.25000000" || quote.UnitValue != "164.06" ||
		quote.Currency != "USD" || quote.PricedAt.Format(time.RFC3339) != "2026-11-24T14:50:00Z" {
		t.Fatalf("unexpected quote %#v", quote)
	}
	if _, err := provider.parseQuote("S", payload); err == nil {
		t.Fatal("accepted a quote for a different ticker")
	}
}
