package portfolio

import (
	"slices"
	"testing"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
)

func TestArchiveUsesDisclosureIntervalsAndRestartsComparisonAfterReentry(t *testing.T) {
	t.Parallel()

	const isin = "US22266M1045"
	const other = "US22266T1097"
	const websiteOnly = "US3563901046"
	const website = "https://example.test/official"
	holding := Position{ISIN: isin, Name: "Coursera Inc.", Kind: "equity", Quantity: "10", Value: "100"}
	control := Position{ISIN: other, Name: "Coupang Inc.", Kind: "equity", Quantity: "4", Value: "40"}
	report := func(date, currency string, positions ...Position) Report {
		return Report{
			AsOfDate: date, Currency: currency, SourceURL: "https://example.test/" + date, TotalAssets: "200", NAV: "180", Units: "10",
			Positions: positions,
		}
	}

	higher := holding
	higher.Quantity = "12"
	higher.Value = "120"
	a := NewArchive([]Report{
		report("2023-10-01", "USD", holding, control),
		report("2023-11-01", "USD", higher, control),
		report("2024-02-01", "USD", control),
		report("2024-04-01", "USD", holding, control),
		report("2024-05-01", "RUB", holding, control),
	}, nil)
	state := &appstate.State{Fund: &appstate.FundState{CompositionHistory: []appstate.FundComposition{
		{AsOfDate: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC), Holdings: []appstate.FundHolding{
			{ISIN: other, Name: control.Name, Kind: control.Kind, Quantity: control.Quantity},
			{ISIN: websiteOnly, Name: "Freedom Holding", Kind: "equity", Quantity: "2"},
		}},
	}}}
	results := a.Detail(isin, state, website)
	if len(results.Changes) != 4 {
		t.Fatalf("changes = %#v", results.Changes)
	}
	removed := results.Changes[1]
	if removed.Type != "removed" || removed.FromDate != "2023-11-01" || removed.ToDate != "2024-02-01" || removed.Before != "12" {
		t.Fatalf("removal interval = %#v", removed)
	}
	if detached := results.Changes[3]; detached.ToSource != website || detached.ToDate != "2024-06-01" {
		t.Fatalf("daily disclosure change = %#v", detached)
	}
	starts := a.Index(state, website).PositionStarts
	if got := starts[isin]; got != (PositionStart{"2024-04-01", "2024-02-01", "https://example.test/2024-04-01"}) {
		t.Errorf("position start after re-entry = %#v", got)
	}
	if got := starts[other]; got != (PositionStart{"2023-10-01", "", "https://example.test/2023-10-01"}) {
		t.Errorf("position start from the earliest report = %#v", got)
	}
	if got := starts[websiteOnly]; got != (PositionStart{"2024-06-01", "2024-05-01", website}) {
		t.Errorf("position start from the fund website = %#v", got)
	}
	for _, d := range []string{"2024-04-01", "2024-05-01"} {
		snapshot, ok := a.Snapshot(d)
		if !ok {
			t.Fatal("snapshot not found")
		}
		position := snapshot.Rows[slices.IndexFunc(snapshot.Rows, func(row Holding) bool { return row.ISIN == isin })]
		if position.ValueChange != "" || position.UnitChangePercent != "" {
			t.Fatalf("unexpected monetary comparison at %s: %#v", d, position)
		}
		if d == "2024-04-01" && position.Previous != nil {
			t.Fatalf("unexpected previous holding after re-entry: %#v", position.Previous)
		}
	}
}

func TestClosedPositionsUseLatestRemovalAndExcludeCurrentHoldings(t *testing.T) {
	t.Parallel()

	first := Report{
		AsOfDate: "2026-09-30", SourceURL: "https://example.test/september", Currency: "USD", TotalAssets: "1000", NAV: "950",
		Positions: []Position{
			{ISIN: "US22266M1045", Name: "Coursera", Kind: "equity", Quantity: "5", Value: "50"},
			{ISIN: "US22266T1097", Name: "Coupang", Kind: "equity", Quantity: "8", Value: "80"},
		},
	}
	second := Report{
		AsOfDate: "2026-10-30", SourceURL: "https://example.test/october", Currency: "USD", TotalAssets: "1000", NAV: "950",
		Positions: []Position{},
	}
	legacy := []IPO{
		{ISIN: "US22266T1097", Ticker: "CPNG", ClosedInLegacy: new(true)},
		{ISIN: "US82489W1071", Name: "Shoals", Ticker: "SHLS", Quantity: "80", ClosedInLegacy: new(true)},
	}
	state := &appstate.State{Fund: &appstate.FundState{Snapshot: appstate.FundSnapshot{
		Assets: []appstate.FundAsset{{ISIN: "US22266T1097"}},
	}}}
	a := NewArchive([]Report{first, second}, legacy)

	items := a.Closed(state, "https://example.test/official")
	if len(items) != 2 {
		t.Fatalf("closed positions = %#v", items)
	}
	if dated := items[0]; dated.ISIN != "US22266M1045" || dated.LastSeen != "2026-09-30" ||
		dated.FirstAbsent != "2026-10-30" || dated.Quantity != "5" {
		t.Fatalf("dated closure = %#v", dated)
	}
	if undated := items[1]; undated.ISIN != "US82489W1071" || undated.LastSeen != "" ||
		undated.FirstAbsent != "" || undated.Ticker != "SHLS" {
		t.Fatalf("undated closure = %#v", undated)
	}
}

func TestClosingReferenceCalculatesReturnFromLastValuation(t *testing.T) {
	isin := "US75340L1044"
	archive := NewArchive([]Report{{
		AsOfDate: "2026-10-30", Currency: "USD", SourceURL: "https://example.com/report.pdf", TotalAssets: "1000", NAV: "1000",
		Positions: []Position{{ISIN: isin, Kind: "equity", Name: "Rapid Micro", Quantity: "10", Value: "60"}},
	}}, []IPO{{ISIN: isin, EntryPriceUSD: "20"}})
	closed := ClosedPosition{ISIN: isin, Name: "Rapid Micro", Kind: "equity", LastSeen: "2026-10-30", Quantity: "20"}
	archive.addClosingReference(&closed)
	if closed.Exit == nil || closed.Exit.Basis != "disclosure" || closed.Exit.Return == nil ||
		closed.Exit.Return.Total != "-140.00000000" || closed.Exit.Return.Percent != "-70.000000" {
		t.Fatalf("unexpected archive reference %#v", closed.Exit)
	}
}
