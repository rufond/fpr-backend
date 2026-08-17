package moex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rufond/fpr-backend/internal/fx"
	"github.com/rufond/fpr-backend/internal/providers"
)

func TestFetchFundUnitQuoteResolvesPrimaryBoardAndUsesLastPrice(t *testing.T) {
	t.Parallel()

	var boardRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("User-Agent"); got != providers.UserAgent {
			t.Fatalf("User-Agent = %q, want %q", got, providers.UserAgent)
		}

		switch request.URL.Path {
		case "/iss/securities/RU000A101NK4.json":
			boardRequests.Add(1)
			if request.URL.Query().Get("iss.only") != "boards" {
				t.Fatalf("iss.only = %q", request.URL.Query().Get("iss.only"))
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{
				"boards":{"columns":["boardid","engine","market","is_traded","is_primary","currencyid"],"data":[
					["OLD1","stock","shares",1,0,"SUR"],
					["NEW1","stock","shares",1,1,"SUR"]
				]}
			}`))
		case "/iss/engines/stock/markets/shares/boards/NEW1/securities/RU000A101NK4/securities.json":
			if request.URL.Query().Get("iss.only") != "marketdata,securities" {
				t.Fatalf("iss.only = %q", request.URL.Query().Get("iss.only"))
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{
				"marketdata":{"columns":["UPDATETIME","LAST","TRADEDATE"],"data":[["18:42:31",3210.5000,"2026-08-14"]]},
				"securities":{"columns":["PREVDATE","PREVPRICE"],"data":[["2026-08-13",3180.0]]}
			}`))
		default:
			t.Fatalf("unexpected path = %q", request.URL.Path)
		}
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	provider.now = func() time.Time { return time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC) }

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
	if boardRequests.Load() != 1 {
		t.Fatalf("board requests = %d, want 1", boardRequests.Load())
	}
}

func TestFetchFundUnitQuoteCachesBoardForTradingDay(t *testing.T) {
	t.Parallel()

	var boardRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/iss/securities/RU000A101NK4.json":
			boardRequests.Add(1)
			_, _ = writer.Write([]byte(`{"boards":{"columns":["boardid","engine","market","is_traded","is_primary","currencyid"],"data":[["DYN1","stock","shares",1,1,"RUB"]]}}`))
		case "/iss/engines/stock/markets/shares/boards/DYN1/securities/RU000A101NK4/securities.json":
			_, _ = writer.Write([]byte(`{
				"marketdata":{"columns":["LAST","TRADEDATE","UPDATETIME"],"data":[[3200,"2026-08-15","10:00:00"]]},
				"securities":{"columns":["PREVPRICE","PREVDATE"],"data":[[3190,"2026-08-14"]]}
			}`))
		default:
			t.Fatalf("unexpected path = %q", request.URL.Path)
		}
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	provider.now = func() time.Time { return time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC) }

	for range 2 {
		if _, err := provider.FetchFundUnitQuote(context.Background()); err != nil {
			t.Fatalf("FetchFundUnitQuote() error = %v", err)
		}
	}

	if boardRequests.Load() != 1 {
		t.Fatalf("board requests = %d, want 1", boardRequests.Load())
	}
}

func TestFetchFundUnitQuoteRefreshesBoardAfterQuoteFailure(t *testing.T) {
	t.Parallel()

	var boardRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/iss/securities/RU000A101NK4.json":
			requestNumber := boardRequests.Add(1)
			boardID := "OLD1"
			if requestNumber > 1 {
				boardID = "NEW1"
			}
			_, _ = writer.Write([]byte(`{"boards":{"columns":["boardid","engine","market","is_traded","is_primary","currencyid"],"data":[["` + boardID + `","stock","shares",1,1,"RUB"]]}}`))
		case "/iss/engines/stock/markets/shares/boards/OLD1/securities/RU000A101NK4/securities.json":
			writer.WriteHeader(http.StatusNotFound)
		case "/iss/engines/stock/markets/shares/boards/NEW1/securities/RU000A101NK4/securities.json":
			_, _ = writer.Write([]byte(`{
				"marketdata":{"columns":["LAST","TRADEDATE","UPDATETIME"],"data":[[3210,"2026-08-15","10:01:00"]]},
				"securities":{"columns":["PREVPRICE","PREVDATE"],"data":[[3200,"2026-08-14"]]}
			}`))
		default:
			t.Fatalf("unexpected path = %q", request.URL.Path)
		}
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	provider.now = func() time.Time { return time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC) }

	quote, err := provider.FetchFundUnitQuote(context.Background())
	if err != nil {
		t.Fatalf("FetchFundUnitQuote() error = %v", err)
	}
	if quote.UnitValue != "3210" {
		t.Fatalf("quote = %#v", quote)
	}
	if boardRequests.Load() != 2 {
		t.Fatalf("board requests = %d, want 2", boardRequests.Load())
	}
}

func TestFetchFundUnitQuoteFallsBackToPreviousPriceAndBoardCurrency(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/iss/securities/RU000A101NK4.json":
			_, _ = writer.Write([]byte(`{"boards":{"columns":["boardid","engine","market","is_traded","is_primary","currencyid"],"data":[["USD1","stock","shares",1,1,"USD"]]}}`))
		case "/iss/engines/stock/markets/shares/boards/USD1/securities/RU000A101NK4/securities.json":
			_, _ = writer.Write([]byte(`{
				"marketdata":{"columns":["LAST","TRADEDATE","UPDATETIME"],"data":[[null,"2026-08-15","10:00:00"]]},
				"securities":{"columns":["PREVPRICE","PREVDATE"],"data":[[31.80,"2026-08-14"]]}
			}`))
		default:
			t.Fatalf("unexpected path = %q", request.URL.Path)
		}
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	quote, err := provider.FetchFundUnitQuote(context.Background())
	if err != nil {
		t.Fatalf("FetchFundUnitQuote() error = %v", err)
	}

	if quote.UnitValue != "31.8" || quote.Currency != "USD" || quote.Source != "previous" {
		t.Fatalf("quote = %#v", quote)
	}

	want := time.Date(2026, time.August, 13, 21, 0, 0, 0, time.UTC)
	if !quote.PricedAt.Equal(want) {
		t.Fatalf("PricedAt = %s, want %s", quote.PricedAt, want)
	}
}

func TestFetchFundUnitQuoteUsesOnlyTradedBoardWithoutPrimary(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/iss/securities/RU000A101NK4.json":
			_, _ = writer.Write([]byte(`{"boards":{"columns":["boardid","engine","market","is_traded","is_primary","currencyid"],"data":[["ONLY1","stock","shares",1,0,"RUB"]]}}`))
		case "/iss/engines/stock/markets/shares/boards/ONLY1/securities/RU000A101NK4/securities.json":
			_, _ = writer.Write([]byte(`{"marketdata":{"columns":["LAST","TRADEDATE","TIME"],"data":[[3210.5,"2026-08-14","18:42:31"]]},"securities":{"columns":["PREVPRICE","PREVDATE"],"data":[]}}`))
		default:
			t.Fatalf("unexpected path = %q", request.URL.Path)
		}
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	if _, err := provider.FetchFundUnitQuote(context.Background()); err != nil {
		t.Fatalf("FetchFundUnitQuote() error = %v", err)
	}
}

func TestFetchFundUnitQuoteRejectsAmbiguousTradedBoardsWithoutPrimary(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"boards":{"columns":["boardid","engine","market","is_traded","is_primary","currencyid"],"data":[["ONE","stock","shares",1,0,"RUB"],["TWO","stock","shares",1,0,"RUB"]]}}`))
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
		_, _ = writer.Write([]byte(strings.Repeat(" ", maxResponseSize+1)))
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	if _, err := provider.FetchFundUnitQuote(context.Background()); err == nil {
		t.Fatal("FetchFundUnitQuote() error = nil")
	}
}

func TestFetchFundUnitDailyPricesUsesBoardlessCandlesForCompletedMoscowDays(t *testing.T) {
	t.Parallel()

	var candleRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("User-Agent"); got != providers.UserAgent {
			t.Fatalf("User-Agent = %q, want %q", got, providers.UserAgent)
		}

		switch request.URL.Path {
		case "/iss/securities/RU000A101NK4.json":
			_, _ = writer.Write([]byte(`{"boards":{"columns":["boardid","engine","market","is_traded","is_primary","currencyid"],"data":[["CURRENT","stock","shares",1,1,"SUR"]]}}`))
		case "/iss/engines/stock/markets/shares/securities/RU000A101NK4/candles.json":
			candleRequests.Add(1)
			query := request.URL.Query()
			if query.Get("interval") != "24" || query.Get("from") != "2026-08-01" || query.Get("till") != "2026-08-14" {
				t.Fatalf("query = %q", request.URL.RawQuery)
			}
			if strings.Contains(request.URL.Path, "/boards/") {
				t.Fatalf("daily candles path is bound to board: %q", request.URL.Path)
			}

			switch query.Get("start") {
			case "0":
				_, _ = writer.Write([]byte(`{
					"candles":{"columns":["close","begin"],"data":[
						[3180.0000,"2026-08-13 00:00:00"],
						[3200.5000,"2026-08-14 00:00:00"]
					]}
				}`))
			case "2":
				_, _ = writer.Write([]byte(`{"candles":{"columns":["close","begin"],"data":[]}}`))
			default:
				t.Fatalf("unexpected start = %q", query.Get("start"))
			}
		default:
			t.Fatalf("unexpected path = %q", request.URL.Path)
		}
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	provider.now = func() time.Time { return time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC) }

	items, err := provider.FetchFundUnitDailyPrices(
		context.Background(),
		time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("FetchFundUnitDailyPrices() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %#v", items)
	}
	if items[0].PriceDate.Format(time.DateOnly) != "2026-08-13" || items[0].UnitValue != "3180" || items[0].Currency != "RUB" {
		t.Fatalf("first item = %#v", items[0])
	}
	if items[1].PriceDate.Format(time.DateOnly) != "2026-08-14" || items[1].UnitValue != "3200.5" || items[1].Currency != "RUB" {
		t.Fatalf("second item = %#v", items[1])
	}
	if candleRequests.Load() != 2 {
		t.Fatalf("candle requests = %d, want 2", candleRequests.Load())
	}
}

func TestFetchFundUnitDailyPricesDoesNotRequestCurrentMoscowDay(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	provider.now = func() time.Time { return time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC) }

	items, err := provider.FetchFundUnitDailyPrices(
		context.Background(),
		time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("FetchFundUnitDailyPrices() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %#v", items)
	}
	if requests.Load() != 0 {
		t.Fatalf("requests = %d, want 0", requests.Load())
	}
}

func TestFetchRateUSDRUBUsesCETS(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("User-Agent"); got != providers.UserAgent {
			t.Fatalf("User-Agent = %q, want %q", got, providers.UserAgent)
		}

		if request.URL.Path != "/iss/engines/currency/markets/selt/boards/CETS/securities/USD000UTSTOM/securities.json" {
			t.Fatalf("unexpected path = %q", request.URL.Path)
		}

		_, _ = writer.Write([]byte(`{
			"marketdata":{"columns":["LAST","TIME","SYSTIME"],"data":[[79.1250,"18:42:31","2026-08-15 18:42:40"]]},
			"securities":{"columns":["PREVPRICE","PREVDATE"],"data":[[78.95,"2026-08-14"]]}
		}`))
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	rate, err := provider.FetchUSDRUB(context.Background())
	if err != nil {
		t.Fatalf("FetchUSDRUB() error = %v", err)
	}

	if rate.Provider != fx.ProviderMOEX || rate.BaseCurrency != "USD" || rate.QuoteCurrency != "RUB" || rate.Rate != "79.125" || rate.Source != "last" {
		t.Fatalf("rate = %#v", rate)
	}

	want := time.Date(2026, time.August, 15, 15, 42, 31, 0, time.UTC)
	if !rate.PricedAt.Equal(want) {
		t.Fatalf("PricedAt = %s, want %s", rate.PricedAt, want)
	}
}

func TestResolveSecuritySymbolsUsesExactTradedShareISIN(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/iss/securities.json" {
			t.Fatalf("unexpected path = %q", request.URL.Path)
		}
		if request.URL.Query().Get("securities.columns") != "secid,isin,is_traded,group" {
			t.Fatalf("securities.columns = %q", request.URL.Query().Get("securities.columns"))
		}

		switch request.URL.Query().Get("q") {
		case "RU000A0JQ9P9":
			_, _ = writer.Write([]byte(`{
				"securities":{"columns":["secid","isin","is_traded","group"],"data":[
					["SPBE","RU000A0JQ9P9",1,"stock_shares"],
					["NOISE","RU000A000000",1,"stock_shares"]
				]}
			}`))
		case "KZ1C00001122":
			_, _ = writer.Write([]byte(`{
				"securities":{"columns":["secid","isin","is_traded","group"],"data":[
					["KMGZ","KZ1C00001122",0,"stock_shares"]
				]}
			}`))
		case "US00449L1026":
			_, _ = writer.Write([]byte(`{"securities":{"columns":["secid","isin","is_traded","group"],"data":[]}}`))
		default:
			t.Fatalf("unexpected q = %q", request.URL.Query().Get("q"))
		}
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	result, err := provider.ResolveSecuritySymbols(context.Background(), []string{
		"RU000A0JQ9P9",
		"KZ1C00001122",
		"US00449L1026",
	})
	if err != nil {
		t.Fatalf("ResolveSecuritySymbols() error = %v", err)
	}

	if result.RequestedISINs != 3 || len(result.SymbolsByISIN) != 1 || result.SymbolsByISIN["RU000A0JQ9P9"] != "SPBE" {
		t.Fatalf("result = %#v", result)
	}
	if len(result.MissingISINs) != 2 || result.MissingISINs[0] != "KZ1C00001122" || result.MissingISINs[1] != "US00449L1026" {
		t.Fatalf("missing ISINs = %#v", result.MissingISINs)
	}
}

func TestFetchSecurityPricesUsesDynamicBoard(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/iss/securities/SPBE.json":
			_, _ = writer.Write([]byte(`{"boards":{"columns":["boardid","engine","market","is_traded","is_primary","currencyid"],"data":[["TQBR","stock","shares",1,1,"RUB"]]}}`))
		case "/iss/engines/stock/markets/shares/boards/TQBR/securities/SPBE/securities.json":
			_, _ = writer.Write([]byte(`{
				"marketdata":{"columns":["LAST","TRADEDATE","TIME"],"data":[[85.75,"2026-08-14","18:42:31"]]},
				"securities":{"columns":["PREVPRICE","PREVDATE"],"data":[[84.5,"2026-08-13"]]}
			}`))
		default:
			t.Fatalf("unexpected path = %q", request.URL.Path)
		}
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	result, err := provider.FetchSecurityPrices(context.Background(), []string{"SPBE"})
	if err != nil {
		t.Fatalf("FetchSecurityPrices() error = %v", err)
	}

	quote, exists := result.QuotesBySymbol["SPBE"]
	if !exists || quote.UnitValue != "85.75" || quote.Currency != "RUB" || quote.Source != "last" {
		t.Fatalf("quote = %#v", quote)
	}
	if len(result.Issues) != 0 {
		t.Fatalf("issues = %#v", result.Issues)
	}
}

func TestFetchSecurityDailyPricesUsesDailyCloseNotAfterOfficialDate(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/iss/securities/SPBE.json":
			_, _ = writer.Write([]byte(`{"boards":{"columns":["boardid","engine","market","is_traded","is_primary","currencyid"],"data":[["TQBR","stock","shares",1,1,"RUB"]]}}`))
		case "/iss/engines/stock/markets/shares/boards/TQBR/securities/SPBE/candles.json":
			if request.URL.Query().Get("interval") != "24" || request.URL.Query().Get("till") != "2026-08-14" {
				t.Fatalf("query = %#v", request.URL.Query())
			}
			_, _ = writer.Write([]byte(`{
				"candles":{"columns":["close","begin","end"],"data":[
					[84.5,"2026-08-13 10:00:00","2026-08-13 18:45:00"],
					[85.75,"2026-08-14 10:00:00","2026-08-14 18:45:00"]
				]}
			}`))
		default:
			t.Fatalf("unexpected path = %q", request.URL.Path)
		}
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	till := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	result, err := provider.FetchSecurityDailyPrices(context.Background(), []string{"SPBE"}, till)
	if err != nil {
		t.Fatalf("FetchSecurityDailyPrices() error = %v", err)
	}

	price, exists := result.PricesBySymbol["SPBE"]
	if !exists || price.UnitValue != "85.75" || price.Currency != "RUB" || !price.PriceDate.Equal(till) {
		t.Fatalf("price = %#v", price)
	}
	wantPricedAt := time.Date(2026, time.August, 14, 15, 45, 0, 0, time.UTC)
	if !price.PricedAt.Equal(wantPricedAt) {
		t.Fatalf("PricedAt = %s, want %s", price.PricedAt, wantPricedAt)
	}
}

func TestFetchUSDRUBAtUsesCETSDailyCloseWithoutBoardDiscovery(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/iss/engines/currency/markets/selt/boards/CETS/securities/USD000UTSTOM/candles.json" {
			t.Fatalf("unexpected path = %q", request.URL.Path)
		}
		_, _ = writer.Write([]byte(`{
			"candles":{"columns":["close","begin","end"],"data":[
				[78.95,"2026-08-13 10:00:00","2026-08-13 23:49:00"],
				[79.125,"2026-08-14 10:00:00","2026-08-14 23:49:00"]
			]}
		}`))
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client())
	rate, exists, err := provider.FetchUSDRUBAt(
		context.Background(),
		time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("FetchUSDRUBAt() error = %v", err)
	}
	if !exists || rate.Provider != fx.ProviderMOEX || rate.Rate != "79.125" || rate.Source != "close" {
		t.Fatalf("rate = %#v, exists = %v", rate, exists)
	}
	wantPricedAt := time.Date(2026, time.August, 14, 20, 49, 0, 0, time.UTC)
	if !rate.PricedAt.Equal(wantPricedAt) {
		t.Fatalf("PricedAt = %s, want %s", rate.PricedAt, wantPricedAt)
	}
}
