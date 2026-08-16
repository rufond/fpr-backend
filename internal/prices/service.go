package prices

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/dateonly"
)

var fundUnitHistoryStartDate = time.Date(2020, time.February, 5, 0, 0, 0, 0, time.UTC)

type serviceRepository interface {
	EnsureFundUnitMOEXSource(ctx context.Context) (int64, error)
	LoadState(ctx context.Context) (*appstate.PriceState, error)
	YahooPriceSources(ctx context.Context) ([]yahooPriceSource, error)
	YahooMappedInstrumentIDs(ctx context.Context) (map[int64]struct{}, error)
	CreateYahooPriceSources(ctx context.Context, items []yahooSourceMapping) (int, error)
	ApplyFundUnitMOEXQuote(
		ctx context.Context,
		priceSourceID int64,
		quote SourceQuote,
		fetchedAt time.Time,
	) (changed bool, stale bool, price appstate.InstrumentPrice, err error)
	ApplyYahooQuotes(ctx context.Context, items []yahooQuoteToApply, fetchedAt time.Time) (yahooApplyResult, error)
	ApplyFundUnitMOEXDailyPrices(
		ctx context.Context,
		priceSourceID int64,
		items []SourceDailyPrice,
	) (inserted int, updated int, state *appstate.PriceState, err error)
}

type Service struct {
	repository           serviceRepository
	source               Source
	yahoo                YahooSource
	state                *appstate.Manager
	now                  func() time.Time
	fundUnitMOEXSourceID int64
}

func NewService(repository serviceRepository, source Source, yahooSource YahooSource, state *appstate.Manager) *Service {
	return &Service{
		repository: repository,
		source:     source,
		yahoo:      yahooSource,
		state:      state,
		now:        time.Now,
	}
}

func (s *Service) Start(ctx context.Context) error {
	if s.repository == nil {
		return fmt.Errorf("price repository is not configured")
	}
	if s.state == nil {
		return fmt.Errorf("application state manager is not configured")
	}

	priceSourceID, errSource := s.repository.EnsureFundUnitMOEXSource(ctx)
	if errSource != nil {
		return errSource
	}

	s.fundUnitMOEXSourceID = priceSourceID

	priceState, err := s.repository.LoadState(ctx)
	if err != nil {
		return err
	}

	cutoff := s.now().UTC().Add(-pricePointRetention)
	for pricePointSourceID, series := range priceState.Points {
		series.Items = retainedPricePoints(series.Items, cutoff)
		if len(series.Items) == 0 {
			delete(priceState.Points, pricePointSourceID)
			continue
		}

		priceState.Points[pricePointSourceID] = series
	}

	return s.state.Update(func(current *appstate.State) (*appstate.State, error) {
		next := new(*current)
		next.Prices = priceState

		return next, nil
	})
}

func (s *Service) SyncFundUnitMOEX(ctx context.Context) (*SyncResult, error) {
	quote, errFetch := s.source.FetchFundUnitQuote(ctx)
	if errFetch != nil {
		return nil, fmt.Errorf("fetch MOEX fund unit quote: %w", errFetch)
	}

	result := &SyncResult{Source: quote.Source}
	fetchedAt := s.now().UTC()

	errUpdate := s.state.Update(func(current *appstate.State) (*appstate.State, error) {
		changed, stale, price, errApply := s.repository.ApplyFundUnitMOEXQuote(ctx, s.fundUnitMOEXSourceID, quote, fetchedAt)
		if errApply != nil {
			return nil, errApply
		}

		result.Changed = changed
		result.Stale = stale
		result.Price = price

		if !changed {
			return current, nil
		}

		next := new(*current)
		next.Prices = priceStateWithCurrentUpdates(current.Prices, []appstate.InstrumentPrice{price}, fetchedAt)

		return next, nil
	})
	if errUpdate != nil {
		return nil, errUpdate
	}

	return result, nil
}

func (s *Service) DiscoverYahooSources(ctx context.Context) (*YahooSourceDiscoveryResult, error) {
	current := s.state.Load()

	mappedInstrumentIDs, errMapped := s.repository.YahooMappedInstrumentIDs(ctx)
	if errMapped != nil {
		return nil, errMapped
	}

	type candidate struct {
		instrumentID int64
		isin         string
	}

	result := &YahooSourceDiscoveryResult{}
	candidates := make([]candidate, 0)
	seenInstrumentIDs := make(map[int64]struct{})

	for _, asset := range current.Fund.Snapshot.Assets {
		if asset.InstrumentID == nil {
			continue
		}

		switch asset.InstrumentType {
		case "equity", "depositary_receipt":
		default:
			continue
		}

		instrumentID := *asset.InstrumentID
		if _, seen := seenInstrumentIDs[instrumentID]; seen {
			continue
		}
		seenInstrumentIDs[instrumentID] = struct{}{}
		result.CandidateInstruments++

		if _, mapped := mappedInstrumentIDs[instrumentID]; mapped {
			result.ExistingSources++
			continue
		}

		candidates = append(candidates, candidate{
			instrumentID: instrumentID,
			isin:         asset.ISIN,
		})
	}

	if len(candidates) == 0 {
		return result, nil
	}

	isins := make([]string, 0, len(candidates))
	for _, item := range candidates {
		isins = append(isins, item.isin)
	}

	resolved, errResolve := s.yahoo.ResolveSymbols(ctx, isins)
	if errResolve != nil {
		return nil, fmt.Errorf("resolve Yahoo symbols: %w", errResolve)
	}

	result.RequestedISINs = resolved.RequestedISINs
	result.ResolvedISINs = len(resolved.SymbolsByISIN)
	result.MissingISINs = slices.Clone(resolved.MissingISINs)

	mappings := make([]yahooSourceMapping, 0, len(resolved.SymbolsByISIN))
	for _, item := range candidates {
		symbol, exists := resolved.SymbolsByISIN[item.isin]
		if !exists {
			continue
		}

		mappings = append(mappings, yahooSourceMapping{
			InstrumentID:   item.instrumentID,
			ProviderSymbol: symbol,
		})
	}

	if len(mappings) == 0 {
		return result, nil
	}

	created, errCreate := s.repository.CreateYahooPriceSources(ctx, mappings)
	if errCreate != nil {
		return nil, errCreate
	}
	result.CreatedSources = created

	return result, nil
}

func (s *Service) SyncYahooPrices(ctx context.Context) (*YahooSyncResult, error) {
	current := s.state.Load()

	priceSources, errSources := s.repository.YahooPriceSources(ctx)
	if errSources != nil {
		return nil, errSources
	}

	currentInstrumentIDs := currentFundInstrumentIDs(current)
	activeSources := make([]yahooPriceSource, 0, len(priceSources))
	symbols := make([]string, 0, len(priceSources))

	for _, source := range priceSources {
		if _, currentInstrument := currentInstrumentIDs[source.InstrumentID]; !currentInstrument {
			continue
		}

		activeSources = append(activeSources, source)
		symbols = append(symbols, source.ProviderSymbol)
	}

	result := &YahooSyncResult{ExpectedSources: len(activeSources)}
	if len(activeSources) == 0 {
		return result, nil
	}

	fetchResult, errFetch := s.yahoo.FetchPrices(ctx, symbols)
	if errFetch != nil {
		return nil, fmt.Errorf("fetch Yahoo prices: %w", errFetch)
	}

	result.RequestedSymbols = fetchResult.RequestedSymbols
	result.ReturnedSymbols = fetchResult.ReturnedSymbols
	result.Batches = fetchResult.Batches
	result.MissingSources = fetchResult.MissingRequests
	result.InvalidSources = fetchResult.InvalidRequests
	result.MissingSymbols = slices.Clone(fetchResult.Missing)
	result.UnexpectedSymbols = slices.Clone(fetchResult.Unexpected)
	result.DuplicateSymbols = slices.Clone(fetchResult.Duplicates)
	result.InvalidQuotes = slices.Clone(fetchResult.Invalid)

	items := make([]yahooQuoteToApply, 0, len(activeSources))
	for _, source := range activeSources {
		quote, exists := fetchResult.QuotesByRequest[source.ProviderSymbol]
		if !exists {
			continue
		}

		items = append(items, yahooQuoteToApply{
			PriceSourceID: source.PriceSourceID,
			InstrumentID:  source.InstrumentID,
			Quote: SourceQuote{
				UnitValue: quote.UnitValue,
				Currency:  quote.Currency,
				PricedAt:  quote.PricedAt,
				Source:    ProviderYahoo,
			},
		})
	}

	if len(items) == 0 {
		return result, nil
	}

	fetchedAt := s.now().UTC()
	errUpdate := s.state.Update(func(currentState *appstate.State) (*appstate.State, error) {
		activeInstrumentIDs := currentFundInstrumentIDs(currentState)
		activeItems := make([]yahooQuoteToApply, 0, len(items))
		for _, item := range items {
			if _, active := activeInstrumentIDs[item.InstrumentID]; active {
				activeItems = append(activeItems, item)
			} else {
				result.CompositionSkippedSources++
			}
		}
		if len(activeItems) == 0 {
			return currentState, nil
		}

		applied, errApply := s.repository.ApplyYahooQuotes(ctx, activeItems, fetchedAt)
		if errApply != nil {
			return nil, errApply
		}

		result.ChangedPrices = applied.ChangedPrices
		result.UnchangedSources = applied.Unchanged
		result.StaleSources = applied.Stale

		if len(applied.ChangedPrices) == 0 {
			return currentState, nil
		}

		next := new(*currentState)
		next.Prices = priceStateWithCurrentUpdates(currentState.Prices, applied.ChangedPrices, fetchedAt)

		return next, nil
	})
	if errUpdate != nil {
		return nil, errUpdate
	}

	return result, nil
}

func (s *Service) SyncFundUnitMOEXHistory(ctx context.Context) (*DailySyncResult, error) {
	current := s.state.Load()
	from := fundUnitMOEXHistoryFrom(current.Prices)
	items, errFetch := s.source.FetchFundUnitDailyPrices(ctx, from)
	if errFetch != nil {
		return nil, fmt.Errorf("fetch MOEX fund unit daily prices: %w", errFetch)
	}

	result := &DailySyncResult{FromDate: from}
	if len(items) != 0 {
		result.ToDate = items[len(items)-1].PriceDate
	}

	errUpdate := s.state.Update(func(currentState *appstate.State) (*appstate.State, error) {
		inserted, updated, priceState, errApply := s.repository.ApplyFundUnitMOEXDailyPrices(ctx, s.fundUnitMOEXSourceID, items)
		if errApply != nil {
			return nil, errApply
		}

		result.Inserted = inserted
		result.Updated = updated

		if !result.Changed() {
			return currentState, nil
		}
		if priceState == nil {
			return nil, fmt.Errorf("MOEX daily price update returned no price state")
		}

		if currentState.Prices != nil {
			priceState.Points = currentState.Prices.Points
		}

		next := new(*currentState)
		next.Prices = priceState
		return next, nil
	})
	if errUpdate != nil {
		return nil, errUpdate
	}

	return result, nil
}

func currentFundInstrumentIDs(state *appstate.State) map[int64]struct{} {
	result := make(map[int64]struct{}, len(state.Fund.Snapshot.Assets))
	for _, asset := range state.Fund.Snapshot.Assets {
		if asset.InstrumentID != nil {
			result[*asset.InstrumentID] = struct{}{}
		}
	}

	return result
}

func priceStateWithCurrentUpdates(current *appstate.PriceState, prices []appstate.InstrumentPrice, observedAt time.Time) *appstate.PriceState {
	next := &appstate.PriceState{
		Sources: map[int64]appstate.InstrumentPrice{},
		Points:  map[int64]appstate.InstrumentPricePointSeries{},
	}
	if current != nil {
		next.Sources = maps.Clone(current.Sources)
		next.DailyPrices = current.DailyPrices
		next.Points = maps.Clone(current.Points)
	}
	if next.Sources == nil {
		next.Sources = map[int64]appstate.InstrumentPrice{}
	}
	if next.Points == nil {
		next.Points = map[int64]appstate.InstrumentPricePointSeries{}
	}

	for _, price := range prices {
		next.Sources[price.PriceSourceID] = price

		series, exists := next.Points[price.PriceSourceID]
		if !exists {
			series = appstate.InstrumentPricePointSeries{
				PriceSourceID:  price.PriceSourceID,
				InstrumentID:   price.InstrumentID,
				AssetType:      price.AssetType,
				ISIN:           price.ISIN,
				Name:           price.Name,
				Provider:       price.Provider,
				ProviderSymbol: price.ProviderSymbol,
			}
		} else {
			series.Items = slices.Clone(series.Items)
		}

		series.Items = append(series.Items, appstate.InstrumentPricePoint{
			UnitValue:  price.UnitValue,
			Currency:   price.Currency,
			PricedAt:   price.PricedAt,
			ObservedAt: observedAt,
		})
		series.Items = retainedPricePoints(series.Items, observedAt.Add(-pricePointRetention))

		next.Points[price.PriceSourceID] = series
	}

	return next
}

func retainedPricePoints(items []appstate.InstrumentPricePoint, cutoff time.Time) []appstate.InstrumentPricePoint {
	return slices.DeleteFunc(items, func(item appstate.InstrumentPricePoint) bool {
		return item.ObservedAt.Before(cutoff)
	})
}

func fundUnitMOEXHistoryFrom(state *appstate.PriceState) time.Time {
	if state == nil {
		return fundUnitHistoryStartDate
	}

	for _, series := range state.DailyPrices {
		if series.ISIN != FundUnitISIN || series.Provider != ProviderMOEX || len(series.Items) == 0 {
			continue
		}

		latest := dateonly.UTC(series.Items[len(series.Items)-1].PriceDate)
		if latest.Before(fundUnitHistoryStartDate) {
			return fundUnitHistoryStartDate
		}

		return latest
	}

	return fundUnitHistoryStartDate
}
