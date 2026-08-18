package diagnostics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

type schedulerJobsStub struct {
	items []scheduler.JobInfo
}

func (s schedulerJobsStub) Jobs() []scheduler.JobInfo {
	return s.items
}

type schedulerRunsStub struct {
	items map[string]*scheduler.JobRun
	err   error
}

func (s schedulerRunsStub) LatestFinishedRun(_ context.Context, jobKey string) (*scheduler.JobRun, error) {
	if s.err != nil {
		return nil, s.err
	}

	return s.items[jobKey], nil
}

type priceSourcesStub struct {
	result *prices.AdminPriceSourcesResult
	err    error
}

func (s priceSourcesStub) AdminPriceSources(context.Context) (*prices.AdminPriceSourcesResult, error) {
	return s.result, s.err
}

func TestListReturnsNoIssuesForHealthyCurrentState(t *testing.T) {
	state := appstate.NewManager()
	instrumentID := int64(10)
	assetID := int64(20)
	priceSourceID := int64(30)
	fundUnitPriceSourceID := int64(31)
	now := time.Date(2026, time.August, 18, 20, 0, 0, 0, time.UTC)

	errInitialize := state.Initialize(&appstate.State{
		Fund: &appstate.FundState{
			Snapshot: appstate.FundSnapshot{
				ID: 1,
				Assets: []appstate.FundAsset{
					{
						ID:             assetID,
						InstrumentID:   &instrumentID,
						InstrumentType: "equity",
						ISIN:           "US3563901046",
						InstrumentName: "Freedom Holding",
					},
				},
			},
		},
		Prices: &appstate.PriceState{
			Sources: map[int64]appstate.InstrumentPrice{
				priceSourceID: {
					PriceSourceID:  priceSourceID,
					InstrumentID:   instrumentID,
					AssetType:      "equity",
					ISIN:           "US3563901046",
					Provider:       prices.ProviderYahoo,
					ProviderSymbol: "FRHC",
					UnitValue:      "150",
					Currency:       "USD",
					PricedAt:       now,
					FetchedAt:      now,
				},
				fundUnitPriceSourceID: {
					PriceSourceID:  fundUnitPriceSourceID,
					AssetType:      prices.FundUnitAssetType,
					ISIN:           prices.FundUnitISIN,
					Provider:       prices.ProviderMOEX,
					ProviderSymbol: prices.FundUnitISIN,
					UnitValue:      "5640",
					Currency:       "RUB",
					PricedAt:       now,
					FetchedAt:      now,
				},
			},
		},
		FX: &appstate.FXState{
			Rates: map[appstate.FXPair]appstate.FXRate{
				{BaseCurrency: "USD", QuoteCurrency: "RUB"}: {
					BaseCurrency:  "USD",
					QuoteCurrency: "RUB",
					Provider:      prices.ProviderMOEX,
					Rate:          "80",
					PricedAt:      now,
					FetchedAt:     now,
				},
			},
		},
		Valuation: &appstate.ValuationState{
			SnapshotID: 1,
			Baselines: map[int64]appstate.FundAssetPriceBaseline{
				assetID: {
					AssetID:       assetID,
					PriceSourceID: priceSourceID,
				},
			},
			Current: appstate.FundLiveValuation{
				SnapshotID:                      1,
				ObservedAt:                      now,
				EstimatedNAVUSD:                 "1000",
				EstimatedCalculatedUnitValueUSD: "10",
				LiveCoveragePercent:             "100",
			},
		},
	})
	if errInitialize != nil {
		t.Fatalf("initialize state: %v", errInitialize)
	}

	service := NewService(
		state,
		schedulerJobsStub{items: []scheduler.JobInfo{{Key: "prices", Name: "Prices"}}},
		schedulerRunsStub{items: map[string]*scheduler.JobRun{
			"prices": {
				ID:      1,
				JobKey:  "prices",
				Status:  scheduler.RunStatusCompleted,
				Summary: map[string]any{},
			},
		}},
		priceSourcesStub{result: &prices.AdminPriceSourcesResult{
			Items: []prices.AdminPriceSourceInstrument{
				{
					InstrumentID: instrumentID,
					AssetType:    "equity",
					ISIN:         "US3563901046",
					Name:         "Freedom Holding",
					Sources: []prices.AdminPriceSource{
						{
							ID:             priceSourceID,
							Provider:       prices.ProviderYahoo,
							ProviderSymbol: "FRHC",
							Enabled:        true,
						},
					},
				},
			},
		}},
	)

	result, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("list diagnostics: %v", err)
	}

	if result.Total != 0 || result.Errors != 0 || result.Warnings != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Items == nil {
		t.Fatal("items must be an empty array, not nil")
	}
}

func TestListBuildsCurrentIssuesWithoutPersistentDiagnosticsState(t *testing.T) {
	state := appstate.NewManager()
	now := time.Date(2026, time.August, 18, 20, 0, 0, 0, time.UTC)

	missingSourceID := int64(10)
	disabledSourceID := int64(11)
	missingPriceID := int64(12)
	missingBaselineID := int64(13)

	assetMissingSource := int64(100)
	assetDisabledSource := int64(101)
	assetMissingPrice := int64(102)
	assetMissingBaseline := int64(103)

	errInitialize := state.Initialize(&appstate.State{
		Fund: &appstate.FundState{
			Snapshot: appstate.FundSnapshot{
				ID: 1,
				Assets: []appstate.FundAsset{
					{ID: assetMissingSource, InstrumentID: &missingSourceID, InstrumentType: "equity"},
					{ID: assetDisabledSource, InstrumentID: &disabledSourceID, InstrumentType: "depositary_receipt"},
					{ID: assetMissingPrice, InstrumentID: &missingPriceID, InstrumentType: "equity"},
					{ID: assetMissingBaseline, InstrumentID: &missingBaselineID, InstrumentType: "equity"},
				},
			},
		},
		Prices: &appstate.PriceState{
			Sources: map[int64]appstate.InstrumentPrice{
				203: {
					PriceSourceID:  203,
					InstrumentID:   missingBaselineID,
					AssetType:      "equity",
					ISIN:           "US3563901046",
					Provider:       prices.ProviderYahoo,
					ProviderSymbol: "FRHC",
					UnitValue:      "150",
					Currency:       "USD",
					PricedAt:       now,
					FetchedAt:      now,
				},
			},
		},
		FX: &appstate.FXState{Rates: map[appstate.FXPair]appstate.FXRate{}},
		Valuation: &appstate.ValuationState{
			SnapshotID: 1,
			Baselines:  map[int64]appstate.FundAssetPriceBaseline{},
			Current: appstate.FundLiveValuation{
				SnapshotID:                      1,
				ObservedAt:                      now,
				EstimatedNAVUSD:                 "1000",
				EstimatedCalculatedUnitValueUSD: "10",
				LiveCoveragePercent:             "42.5",
			},
		},
	})
	if errInitialize != nil {
		t.Fatalf("initialize state: %v", errInitialize)
	}

	finishedAt := now.Add(time.Minute)
	service := NewService(
		state,
		schedulerJobsStub{items: []scheduler.JobInfo{
			{Key: "management", Name: "Management company"},
			{Key: "yahoo", Name: "Yahoo"},
		}},
		schedulerRunsStub{items: map[string]*scheduler.JobRun{
			"management": {
				ID:         7,
				JobKey:     "management",
				RunSource:  scheduler.RunSourceSchedule,
				Status:     scheduler.RunStatusCompleted,
				Summary:    map[string]any{"history_conflicts": float64(2)},
				StartedAt:  now,
				FinishedAt: &finishedAt,
			},
			"yahoo": {
				ID:        8,
				JobKey:    "yahoo",
				RunSource: scheduler.RunSourceSchedule,
				Status:    scheduler.RunStatusFailed,
				Summary: map[string]any{
					"missing_sources":      float64(1),
					"live_valuation_error": "baseline unavailable",
				},
				Error:      "provider unavailable",
				StartedAt:  now,
				FinishedAt: &finishedAt,
			},
		}},
		priceSourcesStub{result: &prices.AdminPriceSourcesResult{
			Items: []prices.AdminPriceSourceInstrument{
				{
					InstrumentID: missingSourceID,
					AssetType:    "equity",
					ISIN:         "US0000000001",
					Name:         "Missing source",
					Sources:      []prices.AdminPriceSource{},
				},
				{
					InstrumentID: disabledSourceID,
					AssetType:    "depositary_receipt",
					ISIN:         "US0000000002",
					Name:         "Disabled source",
					Sources: []prices.AdminPriceSource{
						{ID: 201, Provider: prices.ProviderYahoo, ProviderSymbol: "DISABLED", Enabled: false},
					},
				},
				{
					InstrumentID: missingPriceID,
					AssetType:    "equity",
					ISIN:         "US0000000003",
					Name:         "Missing price",
					Sources: []prices.AdminPriceSource{
						{ID: 202, Provider: prices.ProviderYahoo, ProviderSymbol: "NOPRICE", Enabled: true},
					},
				},
				{
					InstrumentID: missingBaselineID,
					AssetType:    "equity",
					ISIN:         "US3563901046",
					Name:         "Missing baseline",
					Sources: []prices.AdminPriceSource{
						{ID: 203, Provider: prices.ProviderYahoo, ProviderSymbol: "FRHC", Enabled: true},
					},
				},
				{
					InstrumentID: 99,
					AssetType:    "bond",
					ISIN:         "KZ0000000001",
					Name:         "Unsupported bond path",
					Sources:      []prices.AdminPriceSource{},
				},
			},
		}},
	)

	result, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("list diagnostics: %v", err)
	}

	types := make(map[string]int)
	for _, issue := range result.Items {
		types[issue.Type]++
	}

	expectedTypes := []string{
		"job_failed",
		"fixed_history_conflict",
		"live_valuation_refresh_failed",
		"partial_price_sync",
		"missing_price_source",
		"price_source_disabled",
		"missing_current_price",
		"missing_price_baseline",
		"missing_usd_rub",
		"fund_unit_price_missing",
		"partial_live_coverage",
	}
	for _, issueType := range expectedTypes {
		if types[issueType] != 1 {
			t.Fatalf("issue %s count=%d, want 1; all=%v", issueType, types[issueType], types)
		}
	}

	if result.Total != len(expectedTypes) {
		t.Fatalf("total=%d, want %d", result.Total, len(expectedTypes))
	}
	if result.Errors != 2 {
		t.Fatalf("errors=%d, want 2", result.Errors)
	}
	if result.Warnings != len(expectedTypes)-2 {
		t.Fatalf("warnings=%d, want %d", result.Warnings, len(expectedTypes)-2)
	}
	if types["missing_price_source"] != 1 {
		t.Fatalf("bond without implemented price path must not create another source issue: %v", types)
	}
}

func TestListReturnsSchedulerReadError(t *testing.T) {
	state := initializedMinimalState(t)

	service := NewService(
		state,
		schedulerJobsStub{items: []scheduler.JobInfo{{Key: "job"}}},
		schedulerRunsStub{err: errors.New("database unavailable")},
		priceSourcesStub{result: &prices.AdminPriceSourcesResult{}},
	)

	if _, err := service.List(context.Background()); err == nil {
		t.Fatal("expected scheduler read error")
	}
}

func TestListReturnsPriceSourceReadError(t *testing.T) {
	state := initializedMinimalState(t)

	service := NewService(
		state,
		schedulerJobsStub{},
		schedulerRunsStub{},
		priceSourcesStub{err: errors.New("database unavailable")},
	)

	if _, err := service.List(context.Background()); err == nil {
		t.Fatal("expected price source read error")
	}
}

func initializedMinimalState(t *testing.T) *appstate.Manager {
	t.Helper()

	state := appstate.NewManager()
	err := state.Initialize(&appstate.State{
		Fund:   &appstate.FundState{},
		Prices: &appstate.PriceState{Sources: map[int64]appstate.InstrumentPrice{}},
		FX: &appstate.FXState{
			Rates: map[appstate.FXPair]appstate.FXRate{
				{BaseCurrency: "USD", QuoteCurrency: "RUB"}: {},
			},
		},
		Valuation: &appstate.ValuationState{
			Baselines: map[int64]appstate.FundAssetPriceBaseline{},
			Current: appstate.FundLiveValuation{
				EstimatedNAVUSD:     "1",
				LiveCoveragePercent: "100",
			},
		},
	})
	if err != nil {
		t.Fatalf("initialize state: %v", err)
	}

	return state
}
