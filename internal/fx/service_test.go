package fx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/currency"
)

type fakeSource struct {
	rate SourceRate
	err  error

	historicalRate   SourceRate
	historicalExists bool
	historicalErr    error
}

func (s fakeSource) FetchUSDRUB(context.Context) (SourceRate, error) {
	return s.rate, s.err
}

func (s fakeSource) FetchUSDRUBAt(_ context.Context, _ time.Time) (SourceRate, bool, error) {
	return s.historicalRate, s.historicalExists, s.historicalErr
}

type fakeRepository struct {
	loadState *appstate.FXState
	loadErr   error

	changed bool
	stale   bool
	state   *appstate.FXState
	rate    appstate.FXRate
	err     error
}

func (r fakeRepository) LoadState(context.Context) (*appstate.FXState, error) {
	return r.loadState, r.loadErr
}

func (r fakeRepository) ApplyRate(
	context.Context,
	SourceRate,
	time.Time,
) (bool, bool, *appstate.FXState, appstate.FXRate, error) {
	return r.changed, r.stale, r.state, r.rate, r.err
}

func TestServiceStartLoadsFXIntoExistingApplicationState(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	fundState := &appstate.FundState{}
	priceState := &appstate.PriceState{}
	if err := manager.Initialize(&appstate.State{Fund: fundState, Prices: priceState}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	fxState := &appstate.FXState{Rates: map[appstate.FXPair]appstate.FXRate{
		{BaseCurrency: currency.USD, QuoteCurrency: currency.RUB}: {
			BaseCurrency:  currency.USD,
			QuoteCurrency: currency.RUB,
			Provider:      ProviderMOEX,
			Rate:          "79.125",
		},
	}}
	service := NewService(fakeRepository{loadState: fxState}, nil, manager)

	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	current := manager.Load()
	if current == nil || current.Fund != fundState || current.Prices != priceState || current.FX != fxState {
		t.Fatalf("state = %#v", current)
	}
}

func TestSyncUSDRUBPublishesRAMOnlyAfterRepositorySuccess(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	initial := &appstate.State{Fund: &appstate.FundState{}, Prices: &appstate.PriceState{}, FX: &appstate.FXState{}}
	if err := manager.Initialize(initial); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	pricedAt := time.Date(2026, time.August, 15, 15, 42, 31, 0, time.UTC)
	currentRate := appstate.FXRate{
		BaseCurrency:  currency.USD,
		QuoteCurrency: currency.RUB,
		Provider:      ProviderMOEX,
		Rate:          "79.125",
		PricedAt:      pricedAt,
	}
	nextFX := &appstate.FXState{Rates: map[appstate.FXPair]appstate.FXRate{
		{BaseCurrency: currency.USD, QuoteCurrency: currency.RUB}: currentRate,
	}}
	service := NewService(
		fakeRepository{changed: true, state: nextFX, rate: currentRate},
		fakeSource{rate: SourceRate{
			Provider:      ProviderMOEX,
			BaseCurrency:  currency.USD,
			QuoteCurrency: currency.RUB,
			Rate:          "79.1250",
			PricedAt:      pricedAt,
			Source:        "last",
		}},
		manager,
	)

	result, err := service.SyncUSDRUB(context.Background())
	if err != nil {
		t.Fatalf("SyncUSDRUB() error = %v", err)
	}
	if !result.Changed || result.Stale || result.Rate.Rate != "79.125" {
		t.Fatalf("result = %#v", result)
	}

	current := manager.Load()
	if current == initial || current.Fund != initial.Fund || current.Prices != initial.Prices || current.FX != nextFX {
		t.Fatalf("state = %#v", current)
	}
}

func TestSyncUSDRUBNoopKeepsApplicationStatePointer(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	initial := &appstate.State{Fund: &appstate.FundState{}, FX: &appstate.FXState{}}
	if err := manager.Initialize(initial); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	pricedAt := time.Date(2026, time.August, 15, 15, 42, 31, 0, time.UTC)
	service := NewService(
		fakeRepository{rate: appstate.FXRate{
			BaseCurrency:  currency.USD,
			QuoteCurrency: currency.RUB,
			Provider:      ProviderMOEX,
			Rate:          "79.125",
			PricedAt:      pricedAt,
		}},
		fakeSource{rate: SourceRate{
			Provider:      ProviderMOEX,
			BaseCurrency:  currency.USD,
			QuoteCurrency: currency.RUB,
			Rate:          "79.125",
			PricedAt:      pricedAt,
			Source:        "last",
		}},
		manager,
	)

	result, err := service.SyncUSDRUB(context.Background())
	if err != nil {
		t.Fatalf("SyncUSDRUB() error = %v", err)
	}
	if result.Changed || result.Stale {
		t.Fatalf("result = %#v", result)
	}
	if manager.Load() != initial {
		t.Fatal("noop replaced application state pointer")
	}
}

func TestSyncUSDRUBKeepsRAMOnRepositoryError(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	initial := &appstate.State{Fund: &appstate.FundState{}, FX: &appstate.FXState{}}
	if err := manager.Initialize(initial); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	service := NewService(
		fakeRepository{err: errors.New("write failed")},
		fakeSource{rate: SourceRate{
			Provider:      ProviderMOEX,
			BaseCurrency:  currency.USD,
			QuoteCurrency: currency.RUB,
			Rate:          "79.125",
			PricedAt:      time.Date(2026, time.August, 15, 15, 42, 31, 0, time.UTC),
			Source:        "last",
		}},
		manager,
	)

	if _, err := service.SyncUSDRUB(context.Background()); err == nil {
		t.Fatal("SyncUSDRUB() error = nil")
	}
	if manager.Load() != initial {
		t.Fatal("failed update replaced application state pointer")
	}
}

func TestHistoricalUSDRUBReturnsHistoricalSourceRate(t *testing.T) {
	t.Parallel()

	pricedAt := time.Date(2026, time.August, 14, 20, 49, 0, 0, time.UTC)
	service := NewService(nil, fakeSource{
		historicalRate: SourceRate{
			Provider:      ProviderMOEX,
			BaseCurrency:  currency.USD,
			QuoteCurrency: currency.RUB,
			Rate:          "79.125",
			PricedAt:      pricedAt,
			Source:        "close",
		},
		historicalExists: true,
	}, nil)

	rate, exists, err := service.HistoricalUSDRUB(
		context.Background(),
		time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("HistoricalUSDRUB() error = %v", err)
	}
	if !exists || rate.Rate != "79.125" || !rate.PricedAt.Equal(pricedAt) {
		t.Fatalf("rate = %#v, exists = %v", rate, exists)
	}
}
