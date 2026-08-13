package managementcompany

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rufond/fpr-backend/internal/fund"
)

func TestParsePage(t *testing.T) {
	t.Parallel()

	body := readFixture(t)
	page, err := ParsePage(body)
	if err != nil {
		t.Fatalf("ParsePage() error = %v", err)
	}

	wantDate := time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC)
	if !page.Snapshot.AsOfDate.Equal(wantDate) {
		t.Fatalf("snapshot date = %s, want %s", page.Snapshot.AsOfDate, wantDate)
	}
	if page.Snapshot.CalculatedUnitValueUSD != "31.18" || page.Snapshot.NAVUSD != "492986650" {
		t.Fatalf("current values = %s / %s", page.Snapshot.CalculatedUnitValueUSD, page.Snapshot.NAVUSD)
	}
	if page.Snapshot.DeclaredAssetCount != 5 || len(page.Snapshot.Assets) != 5 {
		t.Fatalf("assets = declared %d parsed %d", page.Snapshot.DeclaredAssetCount, len(page.Snapshot.Assets))
	}
	if len(page.Snapshot.Categories) != 5 {
		t.Fatalf("categories = %d, want 5", len(page.Snapshot.Categories))
	}

	equity := page.Snapshot.Assets[0]
	if equity.Kind != fund.AssetKindEquity || equity.ISIN != "KZ1C00001122" || equity.Quantity != "584986" || equity.AssetSharePercent != "8.76" {
		t.Fatalf("equity = %#v", equity)
	}

	claim := page.Snapshot.Assets[2]
	if claim.Kind != fund.AssetKindClaim || claim.SourceName != "" || claim.IsSecurity() {
		t.Fatalf("claim = %#v", claim)
	}

	cash := page.Snapshot.Assets[3]
	if cash.Kind != fund.AssetKindBankCash || cash.Currency != "USD" || cash.AssetSharePercent != "0.01" || !cash.AssetShareUpperBound {
		t.Fatalf("cash = %#v", cash)
	}

	dr := page.Snapshot.Assets[4]
	if dr.Kind != fund.AssetKindDepositaryReceipt || !dr.IsSecurity() || dr.Quantity != "264799" || !dr.AssetShareUpperBound {
		t.Fatalf("depositary receipt = %#v", dr)
	}

	if got := page.Snapshot.Categories[0]; got.SourceName != "Облигации" || got.AssetSharePercent != "75.1254" {
		t.Fatalf("first category = %#v", got)
	}

	if len(page.History) != 3 {
		t.Fatalf("history points = %d, want 3", len(page.History))
	}
	if got := page.History[0]; !got.AsOfDate.Equal(time.Date(2020, time.May, 29, 0, 0, 0, 0, time.UTC)) || got.CalculatedUnitValueUSD != "10.32" || got.NAVUSD != "2634639.49" {
		t.Fatalf("first history point = %#v", got)
	}
}

func TestParseSnapshotRejectsDateMismatch(t *testing.T) {
	t.Parallel()

	body := strings.Replace(string(readFixture(t)), "Данные по состоянию на 12.08.2026", "Данные по состоянию на 11.08.2026", 1)
	_, err := ParseSnapshot([]byte(body))
	if err == nil || !strings.Contains(err.Error(), "differs from composition date") {
		t.Fatalf("ParseSnapshot() error = %v, want date mismatch", err)
	}
}

func TestParseSnapshotRejectsUnknownSourceType(t *testing.T) {
	t.Parallel()

	body := strings.Replace(string(readFixture(t)), "(Акции)</div>", "(Неизвестный тип)</div>", 1)
	_, err := ParseSnapshot([]byte(body))
	if err == nil || !strings.Contains(err.Error(), "unknown management company asset source type") {
		t.Fatalf("ParseSnapshot() error = %v, want unknown type", err)
	}
}

func TestParseSnapshotRejectsSecurityWithoutQuantity(t *testing.T) {
	t.Parallel()

	body := strings.Replace(string(readFixture(t)), `<div class="cell right desktop">584986</div>`, `<div class="cell right desktop"></div>`, 1)
	_, err := ParseSnapshot([]byte(body))
	if err == nil || !strings.Contains(err.Error(), "has no quantity") {
		t.Fatalf("ParseSnapshot() error = %v, want missing quantity", err)
	}
}

func TestParseHistoryRejectsDifferentDateSets(t *testing.T) {
	t.Parallel()

	body := strings.Replace(string(readFixture(t)), `[new Date(2026, 7, 11), 483764001.84],`, "", 1)
	_, err := ParseHistory([]byte(body))
	if err == nil || !strings.Contains(err.Error(), "shareRows has 3 points, chaRows has 2") {
		t.Fatalf("ParseHistory() error = %v, want series mismatch", err)
	}
}

func TestCanonicalDecimal(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"492 986 650.00": "492986650",
		"00031.1800":     "31.18",
		"0.0100":         "0.01",
		"-0.000":         "0",
		"1,2500":         "1.25",
	}
	for input, want := range tests {
		got, err := canonicalDecimal(input)
		if err != nil {
			t.Fatalf("canonicalDecimal(%q) error = %v", input, err)
		}
		if got != want {
			t.Fatalf("canonicalDecimal(%q) = %q, want %q", input, got, want)
		}
	}
}

func readFixture(t *testing.T) []byte {
	t.Helper()

	body, err := os.ReadFile("testdata/fund_page.html")
	if err != nil {
		t.Fatal(err)
	}
	return body
}
