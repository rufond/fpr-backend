package fund

import (
	"strings"
	"testing"
	"time"
)

func TestValidateSourcePage(t *testing.T) {
	t.Parallel()

	page := &SourcePage{
		Snapshot: testSnapshot(),
		History: []DailyValue{
			{
				AsOfDate:               time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC),
				CalculatedUnitValueUSD: "30.60",
				NAVUSD:                 "483764001.84",
			},
			{
				AsOfDate:               time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC),
				CalculatedUnitValueUSD: "31.18",
				NAVUSD:                 "492986650",
			},
		},
	}

	if err := ValidateSourcePage(page); err != nil {
		t.Fatalf("ValidateSourcePage() error = %v", err)
	}
}

func TestValidateSourcePageRejectsInvalidISINThroughValidationRule(t *testing.T) {
	t.Parallel()

	page := &SourcePage{
		Snapshot: testSnapshot(),
		History: []DailyValue{
			{
				AsOfDate:               time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC),
				CalculatedUnitValueUSD: "31.18",
				NAVUSD:                 "492986650",
			},
		},
	}
	page.Snapshot.Assets[0].ISIN = "KZ1C00001123"

	err := ValidateSourcePage(page)
	if err == nil {
		t.Fatal("ValidateSourcePage() error = nil, want invalid ISIN")
	}
	if !strings.Contains(err.Error(), "failed isin validation") {
		t.Fatalf("ValidateSourcePage() error = %q, want ISIN rule error", err)
	}
}

func TestValidateSourcePageRejectsSecurityWithoutQuantity(t *testing.T) {
	t.Parallel()

	page := &SourcePage{
		Snapshot: testSnapshot(),
		History: []DailyValue{
			{
				AsOfDate:               time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC),
				CalculatedUnitValueUSD: "31.18",
				NAVUSD:                 "492986650",
			},
		},
	}
	page.Snapshot.Assets[0].Quantity = ""

	if err := ValidateSourcePage(page); err == nil {
		t.Fatal("ValidateSourcePage() error = nil, want missing security quantity error")
	}
}

func TestValidateSourcePageRejectsHistoryThatDoesNotEndAtCurrentSnapshot(t *testing.T) {
	t.Parallel()

	page := &SourcePage{
		Snapshot: testSnapshot(),
		History: []DailyValue{
			{
				AsOfDate:               time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC),
				CalculatedUnitValueUSD: "30.60",
				NAVUSD:                 "483764001.84",
			},
		},
	}

	if err := ValidateSourcePage(page); err == nil {
		t.Fatal("ValidateSourcePage() error = nil, want current/history mismatch")
	}
}
