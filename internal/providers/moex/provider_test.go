package moex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchFundUnitQuoteUsesLastPrice(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/iss/engines/stock/markets/shares/boards/TQBR/securities/RU000A101NK4/securities.json" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.URL.Query().Get("iss.only") != "marketdata,securities" {
			t.Fatalf("iss.only = %q", request.URL.Query().Get("iss.only"))
		}

		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"marketdata":{"columns":["UPDATETIME","LAST","TRADEDATE"],"data":[["18:42:31",3210.5000,"2026-08-14"]]},
			"securities":{"columns":["PREVDATE","PREVPRICE"],"data":[["2026-08-13",3180.0]]}
		}`))
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	quote, err := provider.FetchFundUnitQuote(context.Background())
	if err != nil {
		t.Fatalf("FetchFundUnitQuote() error = %v", err)
	}

	if quote.UnitValue != "3210.5" || quote.Currency != "RUB" || quote.Source != "last" {
		t.Fatalf("quote = %#v", quote)
	}

	want := time.Date(2026, time.August, 14, 15, 42, 31, 0, time.UTC)
	if !quote.PricedAt.Equal(want) {
		t.Fatalf("PricedAt = %s, want %s", quote.PricedAt, want)
	}
}

func TestFetchFundUnitQuoteFallsBackToPreviousPrice(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"marketdata":{"columns":["LAST","TRADEDATE","UPDATETIME"],"data":[[null,"2026-08-15","10:00:00"]]},
			"securities":{"columns":["PREVPRICE","PREVDATE"],"data":[[3180.0,"2026-08-14"]]}
		}`))
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	quote, err := provider.FetchFundUnitQuote(context.Background())
	if err != nil {
		t.Fatalf("FetchFundUnitQuote() error = %v", err)
	}

	if quote.UnitValue != "3180" || quote.Source != "previous" {
		t.Fatalf("quote = %#v", quote)
	}

	want := time.Date(2026, time.August, 13, 21, 0, 0, 0, time.UTC)
	if !quote.PricedAt.Equal(want) {
		t.Fatalf("PricedAt = %s, want %s", quote.PricedAt, want)
	}
}

func TestFetchFundUnitQuoteRejectsMissingPrice(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"marketdata":{"columns":["LAST","TRADEDATE","UPDATETIME"],"data":[[null,"2026-08-15","10:00:00"]]},
			"securities":{"columns":["PREVPRICE","PREVDATE"],"data":[[null,"2026-08-14"]]}
		}`))
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	if _, err := provider.FetchFundUnitQuote(context.Background()); err == nil {
		t.Fatal("FetchFundUnitQuote() error = nil")
	}
}

func TestFetchFundUnitQuoteRejectsHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	if _, err := provider.FetchFundUnitQuote(context.Background()); err == nil {
		t.Fatal("FetchFundUnitQuote() error = nil")
	}
}

func TestFetchFundUnitQuoteRejectsOversizedResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(strings.Repeat(" ", maxResponseSize+1)))
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	if _, err := provider.FetchFundUnitQuote(context.Background()); err == nil {
		t.Fatal("FetchFundUnitQuote() error = nil")
	}
}
