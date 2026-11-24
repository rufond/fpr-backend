package kase

import (
	"strings"
	"testing"
	"time"
)

func TestParseQuoteUsesLastTradeAndPreviousTradingDay(t *testing.T) {
	page := `<table><thead><tr><th>Date/Period</th><th>Bid</th><th>Offer</th><th>Last</th><th>m. KZT</th></tr></thead>
 <tbody><tr><td>24.11.26 (17:31)</td><td>35925</td><td>36025</td><td>36&nbsp;000,00</td><td>11,9</td></tr>
 <tr><td>23.11.26</td><td>35926</td><td>36000</td><td>35 925,00</td><td>25,2</td></tr>
 <tr><td>Last 52 weeks</td><td>36000</td><td>35000</td><td>10 000</td><td>0</td></tr></tbody></table>`
	quote, err := parseQuote(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	if quote.UnitValue != "36000.00" || quote.PreviousClose != "35925.00" || quote.Currency != "KZT" ||
		quote.PricedAt.Format(time.RFC3339) != "2026-11-24T12:31:00Z" {
		t.Fatalf("unexpected quote %#v", quote)
	}
}
