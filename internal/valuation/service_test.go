package valuation

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/currency"
	"github.com/rufond/fpr-backend/internal/fx"
	"github.com/rufond/fpr-backend/internal/prices"
)

type fakeRepository struct {
	baselines map[int64]baseline
	points    []valuePoint
	loadErr   error
	applyErr  error

	appliedBaselines []baseline
	appliedPoint     valuePoint
	appliedCutoff    time.Time
}

func (r *fakeRepository) LoadBaselines(context.Context, int64) (map[int64]baseline, error) {
	if r.loadErr != nil {
		return nil, r.loadErr
	}

	return r.baselines, nil
}

func (r *fakeRepository) LoadValuePoints(context.Context, int64, time.Time) ([]valuePoint, error) {
	if r.loadErr != nil {
		return nil, r.loadErr
	}

	return slices.Clone(r.points), nil
}

func (r *fakeRepository) ApplyRefresh(_ context.Context, baselines []baseline, point valuePoint, cutoff time.Time) error {
	if r.applyErr != nil {
		return r.applyErr
	}

	r.appliedBaselines = slices.Clone(baselines)
	r.appliedPoint = point
	r.appliedCutoff = cutoff
	return nil
}

type fakeHistoricalPrices struct {
	result  *prices.HistoricalPricesResult
	err     error
	sources []appstate.InstrumentPrice
	till    time.Time
}

func (s *fakeHistoricalPrices) HistoricalPricesAt(_ context.Context, sources []appstate.InstrumentPrice, till time.Time) (*prices.HistoricalPricesResult, error) {
	s.sources = slices.Clone(sources)
	s.till = till
	return s.result, s.err
}

type fakeHistoricalFX struct {
	rate   fx.SourceRate
	exists bool
	err    error
	tills  []time.Time
}

func (s *fakeHistoricalFX) HistoricalUSDRUB(_ context.Context, till time.Time) (fx.SourceRate, bool, error) {
	s.tills = append(s.tills, till)
	return s.rate, s.exists, s.err
}

func TestStartBuildsBaselineFromHistoricalCloseAtOfficialDate(t *testing.T) {
	t.Parallel()

	snapshotDate := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	manager := appstate.NewManager()
	initial := valuationTestState(snapshotDate)
	if err := manager.Initialize(initial); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	historical := &fakeHistoricalPrices{result: &prices.HistoricalPricesResult{
		PricesBySource: map[int64]prices.SourceDailyPrice{
			10: {
				PriceDate: snapshotDate,
				UnitValue: "100",
				Currency:  currency.USD,
				PricedAt:  time.Date(2026, time.August, 14, 20, 0, 0, 0, time.UTC),
			},
			20: {
				PriceDate: snapshotDate,
				UnitValue: "80",
				Currency:  currency.RUB,
				PricedAt:  time.Date(2026, time.August, 14, 15, 45, 0, 0, time.UTC),
			},
			30: {
				PriceDate: snapshotDate,
				UnitValue: "18000",
				Currency:  "KZT",
				PricedAt:  time.Date(2026, time.August, 14, 10, 0, 0, 0, time.UTC),
			},
		},
	}}
	historicalFX := &fakeHistoricalFX{
		rate: fx.SourceRate{
			Provider:      fx.ProviderMOEX,
			BaseCurrency:  currency.USD,
			QuoteCurrency: currency.RUB,
			Rate:          "80",
			PricedAt:      time.Date(2026, time.August, 14, 20, 49, 0, 0, time.UTC),
			Source:        "close",
		},
		exists: true,
	}
	repository := &fakeRepository{baselines: map[int64]baseline{}}
	service := NewService(repository, manager, historical, historicalFX)
	service.now = func() time.Time { return time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC) }

	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	current := manager.Load()
	if current.Valuation == nil {
		t.Fatal("Valuation = nil")
	}
	if len(current.Valuation.Baselines) != 2 {
		t.Fatalf("baselines = %#v", current.Valuation.Baselines)
	}
	if current.Valuation.Baselines[1].UnitValue != "100" || current.Valuation.Baselines[2].UnitValue != "80" {
		t.Fatalf("baseline values = %#v", current.Valuation.Baselines)
	}
	if _, exists := current.Valuation.Baselines[3]; exists {
		t.Fatalf("KZT baseline must not exist without KZT/USD FX: %#v", current.Valuation.Baselines[3])
	}

	live := current.Valuation.Current
	if live.EstimatedNAVUSD != "1021" || live.EstimatedCalculatedUnitValueUSD != "10.21" || live.LiveDeltaUSD != "21" || live.LiveCoveragePercent != "30" {
		t.Fatalf("live = %#v", live)
	}
	if len(repository.appliedBaselines) != 2 {
		t.Fatalf("applied baselines = %#v", repository.appliedBaselines)
	}
	if !historical.till.Equal(snapshotDate) {
		t.Fatalf("historical till = %s, want %s", historical.till, snapshotDate)
	}
	if len(historical.sources) != 2 {
		t.Fatalf("historical sources = %#v, want only USD/RUB sources", historical.sources)
	}
	if len(historicalFX.tills) != 1 || !historicalFX.tills[0].Equal(snapshotDate) {
		t.Fatalf("historical FX tills = %#v", historicalFX.tills)
	}
}

func TestStartDoesNotUseCurrentQuoteWhenHistoricalBaselineIsMissing(t *testing.T) {
	t.Parallel()

	snapshotDate := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	manager := appstate.NewManager()
	initial := valuationTestState(snapshotDate)
	if err := manager.Initialize(initial); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	historical := &fakeHistoricalPrices{result: &prices.HistoricalPricesResult{
		PricesBySource:   map[int64]prices.SourceDailyPrice{},
		MissingSourceIDs: []int64{10, 20, 30},
	}}
	repository := &fakeRepository{baselines: map[int64]baseline{}}
	service := NewService(repository, manager, historical, &fakeHistoricalFX{})
	service.now = func() time.Time { return time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC) }

	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	current := manager.Load()
	if len(current.Valuation.Baselines) != 0 {
		t.Fatalf("baselines = %#v, want none", current.Valuation.Baselines)
	}
	live := current.Valuation.Current
	if live.EstimatedNAVUSD != "1000" || live.EstimatedCalculatedUnitValueUSD != "10" || live.LiveDeltaUSD != "0" || live.LiveCoveragePercent != "0" {
		t.Fatalf("live = %#v", live)
	}
	if len(repository.appliedBaselines) != 0 {
		t.Fatalf("current quotes were persisted as baseline: %#v", repository.appliedBaselines)
	}
}

func TestStartKeepsRAMUnchangedWhenValuationPersistenceFails(t *testing.T) {
	t.Parallel()

	snapshotDate := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	manager := appstate.NewManager()
	initial := valuationTestState(snapshotDate)
	if err := manager.Initialize(initial); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	historical := &fakeHistoricalPrices{result: &prices.HistoricalPricesResult{
		PricesBySource: map[int64]prices.SourceDailyPrice{
			10: {PriceDate: snapshotDate, UnitValue: "100", Currency: currency.USD, PricedAt: snapshotDate.Add(20 * time.Hour)},
		},
	}}
	wantErr := errors.New("write failed")
	service := NewService(&fakeRepository{baselines: map[int64]baseline{}, applyErr: wantErr}, manager, historical, &fakeHistoricalFX{})

	if err := service.Start(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Start() error = %v, want %v", err, wantErr)
	}
	if manager.Load() != initial {
		t.Fatal("RAM state changed after valuation persistence error")
	}
}

func valuationTestState(snapshotDate time.Time) *appstate.State {
	instrument1 := int64(101)
	instrument2 := int64(102)
	instrument3 := int64(103)

	return &appstate.State{
		Fund: &appstate.FundState{Snapshot: appstate.FundSnapshot{
			ID:                     50,
			AsOfDate:               snapshotDate,
			CalculatedUnitValueUSD: "10",
			NAVUSD:                 "1000",
			Assets: []appstate.FundAsset{
				{ID: 1, InstrumentID: &instrument1, Quantity: "2", AssetSharePercent: "20"},
				{ID: 2, InstrumentID: &instrument2, Quantity: "10", AssetSharePercent: "10"},
				{ID: 3, InstrumentID: &instrument3, Quantity: "1", AssetSharePercent: "5"},
			},
		}},
		Prices: &appstate.PriceState{Sources: map[int64]appstate.InstrumentPrice{
			10: {
				PriceSourceID:  10,
				InstrumentID:   instrument1,
				Provider:       prices.ProviderYahoo,
				ProviderSymbol: "AAA",
				UnitValue:      "110",
				Currency:       currency.USD,
				PricedAt:       time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC),
			},
			20: {
				PriceSourceID:  20,
				InstrumentID:   instrument2,
				Provider:       prices.ProviderMOEX,
				ProviderSymbol: "SPBE",
				UnitValue:      "88",
				Currency:       currency.RUB,
				PricedAt:       time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC),
			},
			30: {
				PriceSourceID:  30,
				InstrumentID:   instrument3,
				Provider:       prices.ProviderKASE,
				ProviderSymbol: "KMGZ",
				UnitValue:      "20000",
				Currency:       "KZT",
				PricedAt:       time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC),
			},
		}},
		FX: &appstate.FXState{Rates: map[appstate.FXPair]appstate.FXRate{
			{BaseCurrency: currency.USD, QuoteCurrency: currency.RUB}: {
				BaseCurrency:  currency.USD,
				QuoteCurrency: currency.RUB,
				Provider:      fx.ProviderMOEX,
				Rate:          "80",
			},
		}},
	}
}
