package prices

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
)

type fakeSource struct {
	quote    SourceQuote
	quoteErr error

	daily     []SourceDailyPrice
	dailyErr  error
	dailyFrom time.Time
}

func (s *fakeSource) FetchFundUnitQuote(context.Context) (SourceQuote, error) {
	return s.quote, s.quoteErr
}

func (s *fakeSource) FetchFundUnitDailyPrices(_ context.Context, from time.Time) ([]SourceDailyPrice, error) {
	s.dailyFrom = from

	return s.daily, s.dailyErr
}

type fakeYahooSource struct {
	result  YahooSourceResult
	err     error
	symbols []string
	onFetch func()

	resolveResult YahooSymbolResolutionResult
	resolveErr    error
	resolveISINs  []string
}

func (s *fakeYahooSource) FetchPrices(_ context.Context, symbols []string) (YahooSourceResult, error) {
	s.symbols = slices.Clone(symbols)
	if s.onFetch != nil {
		s.onFetch()
	}

	return s.result, s.err
}

func (s *fakeYahooSource) ResolveSymbols(_ context.Context, isins []string) (YahooSymbolResolutionResult, error) {
	s.resolveISINs = slices.Clone(isins)

	return s.resolveResult, s.resolveErr
}

type fakePriceRepository struct {
	ensureID    int64
	ensureErr   error
	ensureCalls int
	loadState   *appstate.PriceState
	loadErr     error

	applyChanged bool
	applyStale   bool
	applyPrice   appstate.InstrumentPrice
	applyErr     error

	dailyInserted int
	dailyUpdated  int
	dailyState    *appstate.PriceState
	dailyErr      error
	dailyItems    []SourceDailyPrice

	yahooSources   []yahooPriceSource
	yahooSourceErr error
	yahooApplied   []yahooQuoteToApply
	yahooResult    yahooApplyResult
	yahooApplyErr  error

	yahooMappedIDs       map[int64]struct{}
	yahooMappedErr       error
	yahooCreatedMappings []yahooSourceMapping
	yahooCreateCount     int
	yahooCreateErr       error
}

func (r *fakePriceRepository) EnsureFundUnitMOEXSource(context.Context) (int64, error) {
	r.ensureCalls++

	return r.ensureID, r.ensureErr
}

func (r *fakePriceRepository) LoadState(context.Context) (*appstate.PriceState, error) {
	return r.loadState, r.loadErr
}

func (r *fakePriceRepository) YahooPriceSources(context.Context) ([]yahooPriceSource, error) {
	return r.yahooSources, r.yahooSourceErr
}

func (r *fakePriceRepository) YahooMappedInstrumentIDs(context.Context) (map[int64]struct{}, error) {
	return r.yahooMappedIDs, r.yahooMappedErr
}

func (r *fakePriceRepository) CreateYahooPriceSources(_ context.Context, items []yahooSourceMapping) (int, error) {
	r.yahooCreatedMappings = slices.Clone(items)

	return r.yahooCreateCount, r.yahooCreateErr
}

func (r *fakePriceRepository) ApplyFundUnitMOEXQuote(
	context.Context,
	int64,
	SourceQuote,
	time.Time,
) (bool, bool, appstate.InstrumentPrice, error) {
	return r.applyChanged, r.applyStale, r.applyPrice, r.applyErr
}

func (r *fakePriceRepository) ApplyYahooQuotes(
	_ context.Context,
	items []yahooQuoteToApply,
	_ time.Time,
) (yahooApplyResult, error) {
	r.yahooApplied = slices.Clone(items)

	return r.yahooResult, r.yahooApplyErr
}

func (r *fakePriceRepository) ApplyFundUnitMOEXDailyPrices(
	_ context.Context,
	_ int64,
	items []SourceDailyPrice,
) (int, int, *appstate.PriceState, error) {
	r.dailyItems = slices.Clone(items)

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
		Points: map[int64]appstate.InstrumentPricePointSeries{
			7: {
				PriceSourceID: 7,
				ISIN:          FundUnitISIN,
				Provider:      ProviderMOEX,
				Items: []appstate.InstrumentPricePoint{
					{UnitValue: "3190", Currency: "RUB", PricedAt: time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC), ObservedAt: time.Date(2026, time.August, 12, 10, 0, 5, 0, time.UTC)},
					{UnitValue: "3200", Currency: "RUB", PricedAt: time.Date(2026, time.August, 14, 10, 0, 0, 0, time.UTC), ObservedAt: time.Date(2026, time.August, 14, 10, 0, 5, 0, time.UTC)},
				},
			},
		},
	}
	repository := &fakePriceRepository{ensureID: 7, loadState: priceState}
	service := NewService(repository, nil, nil, manager)
	service.now = func() time.Time { return time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC) }

	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	current := manager.Load()
	if current == nil || current.Fund == nil || current.Prices != priceState {
		t.Fatalf("state = %#v", current)
	}
	if len(current.Prices.Points[7].Items) != 1 || current.Prices.Points[7].Items[0].UnitValue != "3200" {
		t.Fatalf("retained price points = %#v", current.Prices.Points[7].Items)
	}
	if service.fundUnitMOEXSourceID != 7 || repository.ensureCalls != 1 {
		t.Fatalf("fund unit source cache = %d, ensure calls = %d", service.fundUnitMOEXSourceID, repository.ensureCalls)
	}
}

func TestSyncFundUnitMOEXPublishesRAMOnlyAfterRepositorySuccess(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	dailyPrices := map[int64]appstate.InstrumentDailyPriceSeries{
		7: {PriceSourceID: 7, ISIN: FundUnitISIN, Provider: ProviderMOEX},
	}
	initialPoints := map[int64]appstate.InstrumentPricePointSeries{
		7: {
			PriceSourceID: 7,
			InstrumentID:  7,
			ISIN:          FundUnitISIN,
			Provider:      ProviderMOEX,
			Items: []appstate.InstrumentPricePoint{
				{UnitValue: "3200", Currency: "RUB", PricedAt: time.Date(2026, time.August, 14, 15, 40, 0, 0, time.UTC), ObservedAt: time.Date(2026, time.August, 14, 15, 40, 1, 0, time.UTC)},
			},
		},
	}
	initialPrices := &appstate.PriceState{
		Sources:     map[int64]appstate.InstrumentPrice{},
		DailyPrices: dailyPrices,
		Points:      initialPoints,
	}
	if err := manager.Initialize(&appstate.State{Fund: &appstate.FundState{}, Prices: initialPrices}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	pricedAt := time.Date(2026, time.August, 14, 15, 42, 31, 0, time.UTC)
	fetchedAt := time.Date(2026, time.August, 14, 15, 42, 35, 0, time.UTC)
	price := appstate.InstrumentPrice{
		PriceSourceID:  7,
		InstrumentID:   7,
		AssetType:      FundUnitAssetType,
		ISIN:           FundUnitISIN,
		Name:           FundUnitName,
		Provider:       ProviderMOEX,
		ProviderSymbol: FundUnitISIN,
		UnitValue:      "3210.5",
		Currency:       "RUB",
		PricedAt:       pricedAt,
	}
	repository := &fakePriceRepository{
		applyChanged: true,
		applyPrice:   price,
	}
	source := &fakeSource{quote: SourceQuote{
		UnitValue: "3210.5",
		Currency:  "RUB",
		PricedAt:  pricedAt,
		Source:    "last",
	}}
	service := NewService(repository, source, nil, manager)
	service.now = func() time.Time { return fetchedAt }

	result, err := service.SyncFundUnitMOEX(context.Background())
	if err != nil {
		t.Fatalf("SyncFundUnitMOEX() error = %v", err)
	}
	if !result.Changed || result.Price.InstrumentID != 7 {
		t.Fatalf("result = %#v", result)
	}

	current := manager.Load()
	if current.Prices == initialPrices {
		t.Fatal("changed quote kept old price-state pointer")
	}
	if current.Prices.Sources[7].UnitValue != "3210.5" {
		t.Fatalf("current price = %#v", current.Prices.Sources[7])
	}
	if current.Prices.DailyPrices[7].PriceSourceID != 7 {
		t.Fatalf("daily prices were not preserved: %#v", current.Prices.DailyPrices)
	}
	points := current.Prices.Points[7].Items
	if len(points) != 2 || points[1].UnitValue != "3210.5" || !points[1].ObservedAt.Equal(fetchedAt) {
		t.Fatalf("price points = %#v", points)
	}
	if len(initialPrices.Points[7].Items) != 1 {
		t.Fatalf("previous RAM state was mutated: %#v", initialPrices.Points[7].Items)
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

	service := NewService(repository, &fakeSource{quote: SourceQuote{
		UnitValue: "3210.5",
		Currency:  "RUB",
		PricedAt:  time.Now().UTC(),
		Source:    "last",
	}}, nil, manager)

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

	service := NewService(repository, &fakeSource{quote: SourceQuote{
		UnitValue: "3210.5",
		Currency:  "RUB",
		PricedAt:  time.Now().UTC(),
		Source:    "last",
	}}, nil, manager)

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

func TestSyncFundUnitMOEXAcceptsProviderCurrency(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	initial := &appstate.State{Fund: &appstate.FundState{}, Prices: &appstate.PriceState{Sources: map[int64]appstate.InstrumentPrice{}}}
	if err := manager.Initialize(initial); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	repository := &fakePriceRepository{applyPrice: appstate.InstrumentPrice{InstrumentID: 7, Currency: "USD"}}
	service := NewService(repository, &fakeSource{quote: SourceQuote{
		UnitValue: "31.8",
		Currency:  "USD",
		PricedAt:  time.Now().UTC(),
		Source:    "previous",
	}}, nil, manager)

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
	service := NewService(repository, source, nil, manager)

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
	service := NewService(&fakePriceRepository{}, source, nil, manager)

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
	service := NewService(repository, source, nil, manager)

	if _, err := service.SyncFundUnitMOEXHistory(context.Background()); err == nil {
		t.Fatal("SyncFundUnitMOEXHistory() error = nil")
	}
	if manager.Load() != initial {
		t.Fatal("RAM state changed after repository error")
	}
}

func TestDiscoverYahooSourcesCreatesMappingsForCurrentYahooEligibleInstruments(t *testing.T) {
	t.Parallel()

	equityID := int64(41)
	depositaryReceiptID := int64(42)
	bondID := int64(43)
	existingID := int64(44)

	manager := appstate.NewManager()
	if err := manager.Initialize(&appstate.State{Fund: &appstate.FundState{
		Snapshot: appstate.FundSnapshot{
			Assets: []appstate.FundAsset{
				{InstrumentID: &equityID, InstrumentType: "equity", ISIN: "US3563901046"},
				{InstrumentID: &depositaryReceiptID, InstrumentType: "depositary_receipt", ISIN: "US0000000001"},
				{InstrumentID: &bondID, InstrumentType: "bond", ISIN: "XS0000000001"},
				{InstrumentID: &existingID, InstrumentType: "equity", ISIN: "US0000000002"},
				{InstrumentID: &equityID, InstrumentType: "equity", ISIN: "US3563901046"},
			},
		},
	}}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	repository := &fakePriceRepository{
		yahooMappedIDs:   map[int64]struct{}{existingID: {}},
		yahooCreateCount: 2,
	}
	source := &fakeYahooSource{
		resolveResult: YahooSymbolResolutionResult{
			RequestedISINs: 2,
			SymbolsByISIN: map[string]string{
				"US3563901046": "FRHC",
				"US0000000001": "DR.TEST",
			},
		},
	}

	service := NewService(repository, nil, source, manager)

	result, err := service.DiscoverYahooSources(context.Background())
	if err != nil {
		t.Fatalf("DiscoverYahooSources() error = %v", err)
	}

	if result.CandidateInstruments != 3 ||
		result.ExistingSources != 1 ||
		result.RequestedISINs != 2 ||
		result.ResolvedISINs != 2 ||
		result.CreatedSources != 2 {
		t.Fatalf("result = %#v", result)
	}

	if len(source.resolveISINs) != 2 || source.resolveISINs[0] != "US3563901046" || source.resolveISINs[1] != "US0000000001" {
		t.Fatalf("resolved ISINs = %#v", source.resolveISINs)
	}

	if len(repository.yahooCreatedMappings) != 2 {
		t.Fatalf("created mappings = %#v", repository.yahooCreatedMappings)
	}
	if repository.yahooCreatedMappings[0].InstrumentID != equityID ||
		repository.yahooCreatedMappings[0].ProviderSymbol != "FRHC" ||
		repository.yahooCreatedMappings[1].InstrumentID != depositaryReceiptID ||
		repository.yahooCreatedMappings[1].ProviderSymbol != "DR.TEST" {
		t.Fatalf("created mappings = %#v", repository.yahooCreatedMappings)
	}
}

func TestDiscoverYahooSourcesKeepsMissingISINUnmapped(t *testing.T) {
	t.Parallel()

	instrumentID := int64(41)
	manager := appstate.NewManager()
	if err := manager.Initialize(&appstate.State{Fund: &appstate.FundState{
		Snapshot: appstate.FundSnapshot{
			Assets: []appstate.FundAsset{
				{InstrumentID: &instrumentID, InstrumentType: "equity", ISIN: "KZ1C00001122"},
			},
		},
	}}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	repository := &fakePriceRepository{}
	source := &fakeYahooSource{
		resolveResult: YahooSymbolResolutionResult{
			RequestedISINs: 1,
			SymbolsByISIN:  map[string]string{},
			MissingISINs:   []string{"KZ1C00001122"},
		},
	}

	service := NewService(repository, nil, source, manager)

	result, err := service.DiscoverYahooSources(context.Background())
	if err != nil {
		t.Fatalf("DiscoverYahooSources() error = %v", err)
	}

	if result.CreatedSources != 0 ||
		len(result.MissingISINs) != 1 ||
		result.MissingISINs[0] != "KZ1C00001122" {
		t.Fatalf("result = %#v", result)
	}

	if len(repository.yahooCreatedMappings) != 0 {
		t.Fatalf("created mappings = %#v", repository.yahooCreatedMappings)
	}
}

func TestSyncYahooPricesUpdatesOnlyCurrentCompositionSources(t *testing.T) {
	t.Parallel()

	instrumentID := int64(42)
	manager := appstate.NewManager()
	initialPrices := &appstate.PriceState{
		Sources: map[int64]appstate.InstrumentPrice{
			7: {
				PriceSourceID: 7,
				InstrumentID:  7,
				ISIN:          FundUnitISIN,
				Provider:      ProviderMOEX,
				UnitValue:     "729.5",
				Currency:      "RUB",
			},
		},
		DailyPrices: map[int64]appstate.InstrumentDailyPriceSeries{
			7: {PriceSourceID: 7, ISIN: FundUnitISIN, Provider: ProviderMOEX},
		},
		Points: map[int64]appstate.InstrumentPricePointSeries{},
	}
	if err := manager.Initialize(&appstate.State{
		Fund: &appstate.FundState{Snapshot: appstate.FundSnapshot{Assets: []appstate.FundAsset{
			{InstrumentID: &instrumentID},
		}}},
		Prices: initialPrices,
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	pricedAt := time.Date(2026, time.August, 16, 15, 10, 0, 0, time.UTC)
	fetchedAt := time.Date(2026, time.August, 16, 15, 10, 5, 0, time.UTC)
	repository := &fakePriceRepository{
		yahooSources: []yahooPriceSource{
			{PriceSourceID: 10, InstrumentID: 42, ProviderSymbol: " dre2.f "},
			{PriceSourceID: 11, InstrumentID: 99, ProviderSymbol: "OLD"},
		},
		yahooResult: yahooApplyResult{ChangedPrices: []appstate.InstrumentPrice{
			{
				PriceSourceID:  10,
				InstrumentID:   42,
				AssetType:      "equity",
				ISIN:           "DE000A3H2333",
				Name:           "D2C",
				Provider:       ProviderYahoo,
				ProviderSymbol: "DRE2.F",
				UnitValue:      "126.32",
				Currency:       "GBP",
				PricedAt:       pricedAt,
				FetchedAt:      fetchedAt,
			},
		}},
	}
	source := &fakeYahooSource{result: YahooSourceResult{
		RequestedSymbols: 1,
		ReturnedSymbols:  1,
		Batches:          1,
		QuotesByRequest: map[string]YahooSourceQuote{
			" dre2.f ": {
				Currency:  "GBP",
				UnitValue: "126.32",
				PricedAt:  pricedAt,
			},
		},
	}}
	service := NewService(repository, nil, source, manager)
	service.now = func() time.Time { return fetchedAt }

	result, err := service.SyncYahooPrices(context.Background())
	if err != nil {
		t.Fatalf("SyncYahooPrices() error = %v", err)
	}
	if len(source.symbols) != 1 || source.symbols[0] != " dre2.f " {
		t.Fatalf("fetched symbols = %#v, want configured Yahoo symbol", source.symbols)
	}
	if result.ExpectedSources != 1 || !result.Changed() || len(result.ChangedPrices) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if len(repository.yahooApplied) != 1 {
		t.Fatalf("applied Yahoo quotes = %#v", repository.yahooApplied)
	}
	applied := repository.yahooApplied[0]
	if applied.PriceSourceID != 10 || applied.InstrumentID != 42 || applied.Quote.UnitValue != "126.32" || applied.Quote.Currency != "GBP" || !applied.Quote.PricedAt.Equal(pricedAt) {
		t.Fatalf("applied Yahoo quote = %#v", applied)
	}

	current := manager.Load()
	if current.Prices == initialPrices {
		t.Fatal("changed Yahoo prices kept old price-state pointer")
	}
	if current.Prices.Sources[7].UnitValue != "729.5" {
		t.Fatalf("existing MOEX price was lost: %#v", current.Prices.Sources)
	}
	if current.Prices.Sources[10].UnitValue != "126.32" {
		t.Fatalf("Yahoo price = %#v", current.Prices.Sources[10])
	}
	if current.Prices.DailyPrices[7].PriceSourceID != 7 {
		t.Fatalf("daily prices were not preserved: %#v", current.Prices.DailyPrices)
	}
	points := current.Prices.Points[10].Items
	if len(points) != 1 || points[0].UnitValue != "126.32" || !points[0].ObservedAt.Equal(fetchedAt) {
		t.Fatalf("Yahoo price points = %#v", points)
	}
}

func TestSyncYahooPricesSkipsMissingAndInvalidQuotes(t *testing.T) {
	t.Parallel()

	firstID := int64(42)
	secondID := int64(43)
	manager := appstate.NewManager()
	initial := &appstate.State{
		Fund: &appstate.FundState{Snapshot: appstate.FundSnapshot{Assets: []appstate.FundAsset{
			{InstrumentID: &firstID},
			{InstrumentID: &secondID},
		}}},
		Prices: &appstate.PriceState{Sources: map[int64]appstate.InstrumentPrice{}},
	}
	if err := manager.Initialize(initial); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	repository := &fakePriceRepository{yahooSources: []yahooPriceSource{
		{PriceSourceID: 10, InstrumentID: 42, ProviderSymbol: "AAA"},
		{PriceSourceID: 11, InstrumentID: 43, ProviderSymbol: "BBB"},
	}}
	source := &fakeYahooSource{result: YahooSourceResult{
		RequestedSymbols: 2,
		ReturnedSymbols:  1,
		Batches:          1,
		QuotesByRequest:  map[string]YahooSourceQuote{},
		MissingRequests:  1,
		InvalidRequests:  1,
		Missing:          []string{"BBB"},
		Invalid:          []YahooQuoteIssue{{Symbol: "AAA", Error: "Yahoo price: value must be positive"}},
	}}
	service := NewService(repository, nil, source, manager)

	result, err := service.SyncYahooPrices(context.Background())
	if err != nil {
		t.Fatalf("SyncYahooPrices() error = %v", err)
	}
	if result.InvalidSources != 1 || result.MissingSources != 1 || len(result.InvalidQuotes) != 1 || len(result.MissingSymbols) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if repository.yahooApplied != nil {
		t.Fatalf("repository received skipped quotes: %#v", repository.yahooApplied)
	}
	if manager.Load() != initial {
		t.Fatal("skipped Yahoo quotes replaced application state")
	}
}

func TestSyncYahooPricesRechecksCompositionAfterFetch(t *testing.T) {
	t.Parallel()

	instrumentID := int64(42)
	manager := appstate.NewManager()
	initial := &appstate.State{
		Fund:   &appstate.FundState{Snapshot: appstate.FundSnapshot{Assets: []appstate.FundAsset{{InstrumentID: &instrumentID}}}},
		Prices: &appstate.PriceState{Sources: map[int64]appstate.InstrumentPrice{}},
	}
	if err := manager.Initialize(initial); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	repository := &fakePriceRepository{yahooSources: []yahooPriceSource{
		{PriceSourceID: 10, InstrumentID: 42, ProviderSymbol: "AAA"},
	}}
	pricedAt := time.Date(2026, time.August, 16, 15, 20, 0, 0, time.UTC)
	source := &fakeYahooSource{result: YahooSourceResult{
		RequestedSymbols: 1,
		ReturnedSymbols:  1,
		Batches:          1,
		QuotesByRequest: map[string]YahooSourceQuote{
			"AAA": {Currency: "USD", UnitValue: "10", PricedAt: pricedAt},
		},
	}}
	source.onFetch = func() {
		if err := manager.Update(func(current *appstate.State) (*appstate.State, error) {
			next := new(*current)
			next.Fund = &appstate.FundState{Snapshot: appstate.FundSnapshot{Assets: []appstate.FundAsset{}}}
			return next, nil
		}); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
	}

	service := NewService(repository, nil, source, manager)
	result, err := service.SyncYahooPrices(context.Background())
	if err != nil {
		t.Fatalf("SyncYahooPrices() error = %v", err)
	}
	if result.CompositionSkippedSources != 1 || result.Changed() {
		t.Fatalf("result = %#v", result)
	}
	if repository.yahooApplied != nil {
		t.Fatalf("repository received stale composition quote: %#v", repository.yahooApplied)
	}
	if len(manager.Load().Fund.Snapshot.Assets) != 0 {
		t.Fatalf("current fund state was reverted: %#v", manager.Load().Fund.Snapshot.Assets)
	}
}

func TestSyncYahooPricesKeepsRAMOnRepositoryError(t *testing.T) {
	t.Parallel()

	instrumentID := int64(42)
	manager := appstate.NewManager()
	initial := &appstate.State{
		Fund: &appstate.FundState{Snapshot: appstate.FundSnapshot{Assets: []appstate.FundAsset{
			{InstrumentID: &instrumentID},
		}}},
		Prices: &appstate.PriceState{Sources: map[int64]appstate.InstrumentPrice{}},
	}
	if err := manager.Initialize(initial); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	repository := &fakePriceRepository{
		yahooSources:  []yahooPriceSource{{PriceSourceID: 10, InstrumentID: 42, ProviderSymbol: "AAA"}},
		yahooApplyErr: errors.New("write failed"),
	}
	source := &fakeYahooSource{result: YahooSourceResult{
		RequestedSymbols: 1,
		ReturnedSymbols:  1,
		Batches:          1,
		QuotesByRequest: map[string]YahooSourceQuote{
			"AAA": {
				Currency:  "USD",
				UnitValue: "10",
				PricedAt:  time.Date(2026, time.August, 16, 15, 25, 0, 0, time.UTC),
			},
		},
	}}

	service := NewService(repository, nil, source, manager)
	if _, err := service.SyncYahooPrices(context.Background()); err == nil {
		t.Fatal("SyncYahooPrices() error = nil")
	}
	if manager.Load() != initial {
		t.Fatal("RAM state changed after Yahoo repository error")
	}
}
