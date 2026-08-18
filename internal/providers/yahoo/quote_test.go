package yahoo

import (
	"testing"
	"time"
)

func TestNormalizeCurrentQuoteUsesCanonicalCurrencyCode(t *testing.T) {
	t.Parallel()

	marketTime := time.Date(2026, time.August, 3, 15, 30, 0, 0, time.UTC)
	result, err := normalizeCurrentQuote(quote{
		Symbol:            "TEST",
		Currency:          "USD",
		Price:             "12.6320",
		RegularMarketTime: marketTime,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.UnitValue != "12.632" || result.Currency != "USD" {
		t.Fatalf("result = %#v", result)
	}
	if !result.PricedAt.Equal(marketTime) {
		t.Fatalf("PricedAt = %s, want %s", result.PricedAt, marketTime)
	}
}

func TestNormalizeCurrentQuoteRejectsProviderSpecificNonCurrencyUnits(t *testing.T) {
	t.Parallel()

	marketTime := time.Date(2026, time.August, 3, 15, 30, 0, 0, time.UTC)
	for _, code := range []string{"GBp", "GBX", "ILA"} {
		t.Run(code, func(t *testing.T) {
			if _, err := normalizeCurrentQuote(quote{
				Symbol:            "TEST",
				Currency:          code,
				Price:             "1234",
				RegularMarketTime: marketTime,
			}); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestNormalizeCurrentQuoteKeepsExactDecimalWithoutFloat64(t *testing.T) {
	t.Parallel()

	result, err := normalizeCurrentQuote(quote{
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

	result, err := normalizeCurrentQuote(quote{
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
	tests := []quote{
		{Symbol: "TEST", Currency: "", Price: "1", RegularMarketTime: marketTime},
		{Symbol: "TEST", Currency: "US", Price: "1", RegularMarketTime: marketTime},
		{Symbol: "TEST", Currency: "USD", Price: "N/A", RegularMarketTime: marketTime},
		{Symbol: "TEST", Currency: "USD", Price: "0", RegularMarketTime: marketTime},
		{Symbol: "TEST", Currency: "USD", Price: "-1", RegularMarketTime: marketTime},
		{Symbol: "TEST", Currency: "USD", Price: "1"},
	}

	for index, quote := range tests {
		if _, err := normalizeCurrentQuote(quote); err == nil {
			t.Fatalf("case %d: expected error", index)
		}
	}
}
