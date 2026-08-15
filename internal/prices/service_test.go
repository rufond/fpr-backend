package prices

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
)

type fakeQuoteSource struct {
	quote *SourceQuote
	err   error
}

func (s fakeQuoteSource) FetchFundUnitQuote(context.Context) (*SourceQuote, error) {
	return s.quote, s.err
}

type fakePriceRepository struct {
	ensureErr error
	loadState *appstate.PriceState
	loadErr   error

	applyChanged bool
	applyStale   bool
	applyState   *appstate.PriceState
	applyPrice   appstate.InstrumentPrice
	applyErr     error
}

func (r *fakePriceRepository) EnsureFundUnitMOEXSource(context.Context) error {
	return r.ensureErr
}

func (r *fakePriceRepository) LoadState(context.Context) (*appstate.PriceState, error) {
	return r.loadState, r.loadErr
}

func (r *fakePriceRepository) ApplyFundUnitMOEXQuote(
	context.Context,
	SourceQuote,
	time.Time,
) (bool, bool, *appstate.PriceState, appstate.InstrumentPrice, error) {
	return r.applyChanged, r.applyStale, r.applyState, r.applyPrice, r.applyErr
}

func TestServiceStartLoadsPricesIntoExistingApplicationState(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	if err := manager.Initialize(&appstate.State{Fund: &appstate.FundState{}}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	priceState := &appstate.PriceState{Sources: map[int64]appstate.InstrumentPrice{
		7: {InstrumentID: 7, ISIN: FundUnitISIN, UnitValue: "3200", Currency: "RUB"},
	}}
	repository := &fakePriceRepository{loadState: priceState}
	service := NewService(repository, nil, manager)

	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	current := manager.Load()
	if current == nil || current.Fund == nil || current.Prices != priceState {
		t.Fatalf("state = %#v", current)
	}
}

func TestSyncFundUnitMOEXPublishesRAMOnlyAfterRepositorySuccess(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	initialPrices := &appstate.PriceState{Sources: map[int64]appstate.InstrumentPrice{}}
	if err := manager.Initialize(&appstate.State{Fund: &appstate.FundState{}, Prices: initialPrices}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	pricedAt := time.Date(2026, time.August, 14, 15, 42, 31, 0, time.UTC)
	price := appstate.InstrumentPrice{
		InstrumentID: 7,
		ISIN:         FundUnitISIN,
		Provider:     ProviderMOEX,
		UnitValue:    "3210.5",
		Currency:     "RUB",
		PricedAt:     pricedAt,
	}
	nextPrices := &appstate.PriceState{Sources: map[int64]appstate.InstrumentPrice{7: price}}
	repository := &fakePriceRepository{
		applyChanged: true,
		applyState:   nextPrices,
		applyPrice:   price,
	}
	service := NewService(repository, fakeQuoteSource{quote: &SourceQuote{
		UnitValue: "3210.5",
		Currency:  "RUB",
		PricedAt:  pricedAt,
		Source:    "last",
	}}, manager)

	result, err := service.SyncFundUnitMOEX(context.Background())
	if err != nil {
		t.Fatalf("SyncFundUnitMOEX() error = %v", err)
	}
	if !result.Changed || result.Price.InstrumentID != 7 {
		t.Fatalf("result = %#v", result)
	}
	if manager.Load().Prices != nextPrices {
		t.Fatalf("Prices = %#v, want %#v", manager.Load().Prices, nextPrices)
	}
}

func TestSyncFundUnitMOEXKeepsRAMOnRepositoryError(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	initialPrices := &appstate.PriceState{Sources: map[int64]appstate.InstrumentPrice{}}
	if err := manager.Initialize(&appstate.State{Fund: &appstate.FundState{}, Prices: initialPrices}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	repository := &fakePriceRepository{applyErr: errors.New("write failed")}
	service := NewService(repository, fakeQuoteSource{quote: &SourceQuote{
		UnitValue: "3210.5",
		Currency:  "RUB",
		PricedAt:  time.Now().UTC(),
		Source:    "last",
	}}, manager)

	if _, err := service.SyncFundUnitMOEX(context.Background()); err == nil {
		t.Fatal("SyncFundUnitMOEX() error = nil")
	}
	if manager.Load().Prices != initialPrices {
		t.Fatal("RAM state changed after repository error")
	}
}

func TestSyncFundUnitMOEXNoopKeepsCurrentStatePointer(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	initial := &appstate.State{
		Fund:   &appstate.FundState{},
		Prices: &appstate.PriceState{Sources: map[int64]appstate.InstrumentPrice{}},
	}
	if err := manager.Initialize(initial); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	repository := &fakePriceRepository{applyPrice: appstate.InstrumentPrice{InstrumentID: 7}}
	service := NewService(repository, fakeQuoteSource{quote: &SourceQuote{
		UnitValue: "3210.5",
		Currency:  "RUB",
		PricedAt:  time.Now().UTC(),
		Source:    "last",
	}}, manager)

	result, err := service.SyncFundUnitMOEX(context.Background())
	if err != nil {
		t.Fatalf("SyncFundUnitMOEX() error = %v", err)
	}
	if result.Changed {
		t.Fatalf("result = %#v", result)
	}
	if manager.Load() != initial {
		t.Fatal("noop replaced application state pointer")
	}
}

func TestSyncFundUnitMOEXRejectsInvalidQuoteBeforeRepository(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	if err := manager.Initialize(&appstate.State{Fund: &appstate.FundState{}}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	repository := &fakePriceRepository{}
	service := NewService(repository, fakeQuoteSource{quote: &SourceQuote{
		UnitValue: "0",
		Currency:  "RUB",
		PricedAt:  time.Now().UTC(),
		Source:    "last",
	}}, manager)

	if _, err := service.SyncFundUnitMOEX(context.Background()); err == nil {
		t.Fatal("SyncFundUnitMOEX() error = nil")
	}
}

func TestSyncFundUnitMOEXAcceptsProviderCurrency(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	initial := &appstate.State{Fund: &appstate.FundState{}, Prices: &appstate.PriceState{Sources: map[int64]appstate.InstrumentPrice{}}}
	if err := manager.Initialize(initial); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	repository := &fakePriceRepository{applyPrice: appstate.InstrumentPrice{InstrumentID: 7, Currency: "USD"}}
	service := NewService(repository, fakeQuoteSource{quote: &SourceQuote{
		UnitValue: "31.8",
		Currency:  "USD",
		PricedAt:  time.Now().UTC(),
		Source:    "previous",
	}}, manager)

	if _, err := service.SyncFundUnitMOEX(context.Background()); err != nil {
		t.Fatalf("SyncFundUnitMOEX() error = %v", err)
	}
}
