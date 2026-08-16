package yahoo

import (
	"testing"
	"time"
)

func TestNormalizeCurrentQuotePreservesYahooPenceMeaning(t *testing.T) {
	t.Parallel()

	marketTime := time.Date(2026, time.August, 3, 15, 30, 0, 0, time.UTC)
	result, err := NormalizeCurrentQuote(Quote{
		Symbol:            " azn.l ",
		Currency:          "GBp",
		Price:             "12632.0",
		RegularMarketTime: marketTime,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Symbol != "AZN.L" || result.UnitValue != "126.32" || result.Currency != "GBP" {
		t.Fatalf("result = %#v", result)
	}
	if !result.PricedAt.Equal(marketTime) {
		t.Fatalf("PricedAt = %s, want %s", result.PricedAt, marketTime)
	}
}

func TestNormalizeCurrentQuoteDoesNotTreatGBPAsPence(t *testing.T) {
	t.Parallel()

	result, err := NormalizeCurrentQuote(Quote{
		Symbol:            "VOD.L",
		Currency:          "GBP",
		Price:             "12.6320",
		RegularMarketTime: time.Date(2026, time.August, 3, 15, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.UnitValue != "12.632" || result.Currency != "GBP" {
		t.Fatalf("result = %#v", result)
	}
}

func TestNormalizeCurrentQuoteNormalizesGBXAndILA(t *testing.T) {
	t.Parallel()

	marketTime := time.Date(2026, time.August, 3, 15, 30, 0, 0, time.UTC)
	tests := []struct {
		currency string
		price    string
		want     string
		wantCurr string
	}{
		{currency: "GBX", price: "1234", want: "12.34", wantCurr: "GBP"},
		{currency: "ILA", price: "455.5", want: "4.555", wantCurr: "ILS"},
	}

	for _, test := range tests {
		t.Run(test.currency, func(t *testing.T) {
			result, err := NormalizeCurrentQuote(Quote{
				Symbol:            "TEST",
				Currency:          test.currency,
				Price:             test.price,
				RegularMarketTime: marketTime,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.UnitValue != test.want || result.Currency != test.wantCurr {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestNormalizeCurrentQuoteKeepsExactDecimalWithoutFloat64(t *testing.T) {
	t.Parallel()

	result, err := NormalizeCurrentQuote(Quote{
		Symbol:            "TEST",
		Currency:          "USD",
		Price:             "1.2300000000000000000000000001",
		RegularMarketTime: time.Date(2026, time.August, 3, 15, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.UnitValue != "1.2300000000000000000000000001" {
		t.Fatalf("UnitValue = %q", result.UnitValue)
	}
}

func TestNormalizeCurrentQuoteAcceptsExponentNotation(t *testing.T) {
	t.Parallel()

	result, err := NormalizeCurrentQuote(Quote{
		Symbol:            "TEST",
		Currency:          "USD",
		Price:             "1.234e2",
		RegularMarketTime: time.Date(2026, time.August, 3, 15, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.UnitValue != "123.4" {
		t.Fatalf("UnitValue = %q, want 123.4", result.UnitValue)
	}
}

func TestNormalizeCurrentQuoteRejectsInvalidFields(t *testing.T) {
	t.Parallel()

	marketTime := time.Date(2026, time.August, 3, 15, 30, 0, 0, time.UTC)
	tests := []Quote{
		{Symbol: "", Currency: "USD", Price: "1", RegularMarketTime: marketTime},
		{Symbol: "TEST", Currency: "", Price: "1", RegularMarketTime: marketTime},
		{Symbol: "TEST", Currency: "US", Price: "1", RegularMarketTime: marketTime},
		{Symbol: "TEST", Currency: "USD", Price: "N/A", RegularMarketTime: marketTime},
		{Symbol: "TEST", Currency: "USD", Price: "0", RegularMarketTime: marketTime},
		{Symbol: "TEST", Currency: "USD", Price: "-1", RegularMarketTime: marketTime},
		{Symbol: "TEST", Currency: "USD", Price: "1"},
	}

	for index, quote := range tests {
		if _, err := NormalizeCurrentQuote(quote); err == nil {
			t.Fatalf("case %d: expected error", index)
		}
	}
}

func TestNormalizePreviousCloseUsesExchangeLocalMarketDate(t *testing.T) {
	t.Parallel()

	result, err := NormalizePreviousClose(Quote{
		Symbol:               "AAPL",
		Currency:             "USD",
		PreviousClose:        "231.2500",
		RegularMarketTime:    time.Date(2026, time.August, 4, 0, 30, 0, 0, time.UTC),
		ExchangeTimezoneName: "America/New_York",
	})
	if err != nil {
		t.Fatal(err)
	}

	wantDate := time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC)
	if result == nil || result.UnitValue != "231.25" || result.Currency != "USD" || !result.PriceDate.Equal(wantDate) {
		t.Fatalf("result = %#v, want date %s", result, wantDate)
	}
}

func TestNormalizePreviousCloseKeepsCurrentQuoteIndependent(t *testing.T) {
	t.Parallel()

	quote := Quote{
		Symbol:               "AZN.L",
		Currency:             "GBp",
		Price:                "12632",
		PreviousClose:        "N/A",
		RegularMarketTime:    time.Date(2026, time.August, 3, 15, 30, 0, 0, time.UTC),
		ExchangeTimezoneName: "Europe/London",
	}

	current, errCurrent := NormalizeCurrentQuote(quote)
	if errCurrent != nil {
		t.Fatal(errCurrent)
	}
	if current.UnitValue != "126.32" {
		t.Fatalf("current = %#v", current)
	}

	if _, errPrevious := NormalizePreviousClose(quote); errPrevious == nil {
		t.Fatalf("expected previous close error")
	}
}

func TestNormalizePreviousCloseReturnsNilWhenYahooDidNotProvideIt(t *testing.T) {
	t.Parallel()

	result, err := NormalizePreviousClose(Quote{PreviousClose: "  "})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
}
