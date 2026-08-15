package prices

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
)

type fakeSource struct {
	quote    *SourceQuote
	quoteErr error

	daily     []SourceDailyPrice
	dailyErr  error
	dailyFrom time.Time
}

func (s *fakeSource) FetchFundUnitQuote(context.Context) (*SourceQuote, error) {
	return s.quote, s.quoteErr
}

func (s *fakeSource) FetchFundUnitDailyPrices(_ context.Context, from time.Time) ([]SourceDailyPrice, error) {
	s.dailyFrom = from
	return s.daily, s.dailyErr
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

	dailyInserted int
	dailyUpdated  int
	dailyState    *appstate.PriceState
	dailyErr      error
	dailyItems    []SourceDailyPrice
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

func (r *fakePriceRepository) ApplyFundUnitMOEXDailyPrices(
	_ context.Context,
	items []SourceDailyPrice,
) (int, int, *appstate.PriceState, error) {
	r.dailyItems = append([]SourceDailyPrice(nil), items...)
	return r.dailyInserted, r.dailyUpdated, r.dailyState, r.dailyErr
}

func TestServiceStartLoadsPricesIntoExistingApplicationState(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	if err := manager.Initialize(&appstate.State{Fund: &appstate.FundState{}}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	priceState := &appstate.PriceState{
		Sources: map[int64]appstate.InstrumentPrice{
			7: {InstrumentID: 7, ISIN: FundUnitISIN, UnitValue: "3200", Currency: "RUB"},
		},
		DailyPrices: map[int64]appstate.InstrumentDailyPriceSeries{
			7: {
				PriceSourceID: 7,
				ISIN:          FundUnitISIN,
				Provider:      ProviderMOEX,
				Items: []appstate.InstrumentDailyPrice{
					{PriceDate: time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC), UnitValue: "3200", Currency: "RUB"},
				},
			},
		},
	}
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
	dailyPrices := map[int64]appstate.InstrumentDailyPriceSeries{
		7: {PriceSourceID: 7, ISIN: FundUnitISIN, Provider: ProviderMOEX},
	}
	initialPrices := &appstate.PriceState{
		Sources:     map[int64]appstate.InstrumentPrice{},
		DailyPrices: dailyPrices,
	}
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
	source := &fakeSource{quote: &SourceQuote{
		UnitValue: "3210.5",
		Currency:  "RUB",
		PricedAt:  pricedAt,
		Source:    "last",
	}}
	service := NewService(repository, source, manager)

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
	if len(nextPrices.DailyPrices) != 1 || nextPrices.DailyPrices[7].PriceSourceID != 7 {
		t.Fatalf("daily prices were not preserved: %#v", nextPrices.DailyPrices)
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
	service := NewService(repository, &fakeSource{quote: &SourceQuote{
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
	service := NewService(repository, &fakeSource{quote: &SourceQuote{
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
	service := NewService(repository, &fakeSource{quote: &SourceQuote{
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
	service := NewService(repository, &fakeSource{quote: &SourceQuote{
		UnitValue: "31.8",
		Currency:  "USD",
		PricedAt:  time.Now().UTC(),
		Source:    "previous",
	}}, manager)

	if _, err := service.SyncFundUnitMOEX(context.Background()); err != nil {
		t.Fatalf("SyncFundUnitMOEX() error = %v", err)
	}
}

func TestSyncFundUnitMOEXHistoryBackfillsFromFundRegistrationDate(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	initialPrices := &appstate.PriceState{
		Sources:     map[int64]appstate.InstrumentPrice{},
		DailyPrices: map[int64]appstate.InstrumentDailyPriceSeries{},
	}
	if err := manager.Initialize(&appstate.State{Fund: &appstate.FundState{}, Prices: initialPrices}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	daily := []SourceDailyPrice{
		{PriceDate: time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC), UnitValue: "3180", Currency: "RUB"},
		{PriceDate: time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC), UnitValue: "3200", Currency: "RUB"},
	}
	source := &fakeSource{daily: daily}
	nextPrices := &appstate.PriceState{DailyPrices: map[int64]appstate.InstrumentDailyPriceSeries{7: {PriceSourceID: 7, ISIN: FundUnitISIN, Provider: ProviderMOEX}}}
	repository := &fakePriceRepository{dailyInserted: 2, dailyState: nextPrices}
	service := NewService(repository, source, manager)

	result, err := service.SyncFundUnitMOEXHistory(context.Background())
	if err != nil {
		t.Fatalf("SyncFundUnitMOEXHistory() error = %v", err)
	}
	if !source.dailyFrom.Equal(fundUnitHistoryStartDate) {
		t.Fatalf("from = %s, want %s", source.dailyFrom, fundUnitHistoryStartDate)
	}
	if result.Inserted != 2 || result.Updated != 0 || !result.ToDate.Equal(daily[1].PriceDate) {
		t.Fatalf("result = %#v", result)
	}
	if manager.Load().Prices != nextPrices {
		t.Fatalf("Prices = %#v, want %#v", manager.Load().Prices, nextPrices)
	}
	if len(repository.dailyItems) != 2 {
		t.Fatalf("daily items = %#v", repository.dailyItems)
	}
}

func TestSyncFundUnitMOEXHistoryRefetchesLatestStoredDateInclusively(t *testing.T) {
	t.Parallel()

	latest := time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC)
	manager := appstate.NewManager()
	initial := &appstate.State{
		Fund: &appstate.FundState{},
		Prices: &appstate.PriceState{DailyPrices: map[int64]appstate.InstrumentDailyPriceSeries{
			7: {
				PriceSourceID: 7,
				ISIN:          FundUnitISIN,
				Provider:      ProviderMOEX,
				Items: []appstate.InstrumentDailyPrice{
					{PriceDate: latest, UnitValue: "3200", Currency: "RUB"},
				},
			},
		}},
	}
	if err := manager.Initialize(initial); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	source := &fakeSource{daily: []SourceDailyPrice{}}
	service := NewService(&fakePriceRepository{}, source, manager)

	result, err := service.SyncFundUnitMOEXHistory(context.Background())
	if err != nil {
		t.Fatalf("SyncFundUnitMOEXHistory() error = %v", err)
	}

	wantFrom := latest
	if !source.dailyFrom.Equal(wantFrom) {
		t.Fatalf("from = %s, want %s", source.dailyFrom, wantFrom)
	}
	if result.Changed() {
		t.Fatalf("result = %#v", result)
	}
	if manager.Load() != initial {
		t.Fatal("noop replaced application state pointer")
	}
}

func TestSyncFundUnitMOEXHistoryKeepsRAMOnRepositoryError(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	initial := &appstate.State{Fund: &appstate.FundState{}, Prices: &appstate.PriceState{}}
	if err := manager.Initialize(initial); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	source := &fakeSource{daily: []SourceDailyPrice{{
		PriceDate: time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC),
		UnitValue: "3200",
		Currency:  "RUB",
	}}}
	repository := &fakePriceRepository{dailyErr: errors.New("write failed")}
	service := NewService(repository, source, manager)

	if _, err := service.SyncFundUnitMOEXHistory(context.Background()); err == nil {
		t.Fatal("SyncFundUnitMOEXHistory() error = nil")
	}
	if manager.Load() != initial {
		t.Fatal("RAM state changed after repository error")
	}
}

func TestSyncFundUnitMOEXHistoryRejectsInvalidDailyPriceBeforeRepository(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	if err := manager.Initialize(&appstate.State{Fund: &appstate.FundState{}}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	source := &fakeSource{daily: []SourceDailyPrice{{
		PriceDate: time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC),
		UnitValue: "0",
		Currency:  "RUB",
	}}}
	repository := &fakePriceRepository{}
	service := NewService(repository, source, manager)

	if _, err := service.SyncFundUnitMOEXHistory(context.Background()); err == nil {
		t.Fatal("SyncFundUnitMOEXHistory() error = nil")
	}
	if repository.dailyItems != nil {
		t.Fatalf("repository received invalid items: %#v", repository.dailyItems)
	}
}
