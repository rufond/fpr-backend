package prices

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/currency"
	"github.com/rufond/fpr-backend/internal/dateonly"
	"github.com/rufond/fpr-backend/internal/decimal"
)

var fundUnitHistoryStartDate = time.Date(2020, time.February, 5, 0, 0, 0, 0, time.UTC)

type serviceRepository interface {
	EnsureFundUnitMOEXSource(ctx context.Context) error
	LoadState(ctx context.Context) (*appstate.PriceState, error)
	YahooPriceSources(ctx context.Context) ([]yahooPriceSource, error)
	ApplyFundUnitMOEXQuote(
		ctx context.Context,
		quote SourceQuote,
		fetchedAt time.Time,
	) (changed bool, stale bool, price appstate.InstrumentPrice, err error)
	ApplyYahooQuotes(ctx context.Context, items []yahooQuoteToApply, fetchedAt time.Time) (yahooApplyResult, error)
	ApplyFundUnitMOEXDailyPrices(
		ctx context.Context,
		items []SourceDailyPrice,
	) (inserted int, updated int, state *appstate.PriceState, err error)
}

type Service struct {
	repository serviceRepository
	source     Source
	yahoo      YahooSource
	state      *appstate.Manager
	now        func() time.Time
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

	if err := s.repository.EnsureFundUnitMOEXSource(ctx); err != nil {
		return err
	}

	priceState, err := s.repository.LoadState(ctx)
	if err != nil {
		return err
	}

	cutoff := s.now().UTC().Add(-pricePointRetention)
	for priceSourceID, series := range priceState.Points {
		series.Items = retainedPricePoints(series.Items, cutoff)
		if len(series.Items) == 0 {
			delete(priceState.Points, priceSourceID)
			continue
		}
		priceState.Points[priceSourceID] = series
	}

	return s.state.Update(func(current *appstate.State) (*appstate.State, error) {
		if current == nil {
			return nil, fmt.Errorf("application state is not initialized")
		}

		next := new(*current)
		next.Prices = priceState
		return next, nil
	})
}

func (s *Service) SyncFundUnitMOEX(ctx context.Context) (*SyncResult, error) {
	if s.repository == nil {
		return nil, fmt.Errorf("price repository is not configured")
	}
	if s.source == nil {
		return nil, fmt.Errorf("MOEX source is not configured")
	}
	if s.state == nil {
		return nil, fmt.Errorf("application state manager is not configured")
	}

	quote, errFetch := s.source.FetchFundUnitQuote(ctx)
	if errFetch != nil {
		return nil, fmt.Errorf("fetch MOEX fund unit quote: %w", errFetch)
	}
	if quote == nil {
		return nil, fmt.Errorf("MOEX fund unit quote is nil")
	}

	if errValidate := validateSourceQuote(*quote); errValidate != nil {
		return nil, fmt.Errorf("invalid MOEX fund unit quote: %w", errValidate)
	}

	result := &SyncResult{Source: quote.Source}
	fetchedAt := s.now().UTC()

	errUpdate := s.state.Update(func(current *appstate.State) (*appstate.State, error) {
		if current == nil {
			return nil, fmt.Errorf("application state is not initialized")
		}

		changed, stale, price, errApply := s.repository.ApplyFundUnitMOEXQuote(ctx, *quote, fetchedAt)
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

func (s *Service) SyncYahooPrices(ctx context.Context) (*YahooSyncResult, error) {
	if s.repository == nil {
		return nil, fmt.Errorf("price repository is not configured")
	}

	if s.yahoo == nil {
		return nil, fmt.Errorf("Yahoo source is not configured")
	}

	if s.state == nil {
		return nil, fmt.Errorf("application state manager is not configured")
	}

	current := s.state.Load()
	if current == nil || current.Fund == nil {
		return nil, fmt.Errorf("fund state is not initialized")
	}

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
		if strings.TrimSpace(source.ProviderSymbol) == "" {
			return nil, fmt.Errorf("Yahoo price source %d has empty provider symbol", source.PriceSourceID)
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

	if fetchResult == nil {
		return nil, fmt.Errorf("Yahoo fetch result is nil")
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

		sourceQuote := SourceQuote{
			UnitValue: quote.UnitValue,
			Currency:  quote.Currency,
			PricedAt:  quote.PricedAt,
			Source:    ProviderYahoo,
		}
		if errValidate := validateSourceQuote(sourceQuote); errValidate != nil {
			return nil, fmt.Errorf("invalid normalized Yahoo quote for price source %d: %w", source.PriceSourceID, errValidate)
		}

		items = append(items, yahooQuoteToApply{
			PriceSourceID: source.PriceSourceID,
			InstrumentID:  source.InstrumentID,
			Quote:         sourceQuote,
		})
	}

	if len(items) == 0 {
		return result, nil
	}

	fetchedAt := s.now().UTC()
	errUpdate := s.state.Update(func(currentState *appstate.State) (*appstate.State, error) {
		if currentState == nil || currentState.Fund == nil {
			return nil, fmt.Errorf("fund state is not initialized")
		}

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
	if s.repository == nil {
		return nil, fmt.Errorf("price repository is not configured")
	}
	if s.source == nil {
		return nil, fmt.Errorf("MOEX source is not configured")
	}
	if s.state == nil {
		return nil, fmt.Errorf("application state manager is not configured")
	}

	current := s.state.Load()
	if current == nil {
		return nil, fmt.Errorf("application state is not initialized")
	}

	from := fundUnitMOEXHistoryFrom(current.Prices)
	items, errFetch := s.source.FetchFundUnitDailyPrices(ctx, from)
	if errFetch != nil {
		return nil, fmt.Errorf("fetch MOEX fund unit daily prices: %w", errFetch)
	}

	normalized, errNormalize := normalizeFundUnitMOEXDailyPrices(items)
	if errNormalize != nil {
		return nil, errNormalize
	}

	result := &DailySyncResult{FromDate: from}
	if len(normalized) != 0 {
		result.ToDate = normalized[len(normalized)-1].PriceDate
	}

	errUpdate := s.state.Update(func(currentState *appstate.State) (*appstate.State, error) {
		if currentState == nil {
			return nil, fmt.Errorf("application state is not initialized")
		}

		inserted, updated, priceState, errApply := s.repository.ApplyFundUnitMOEXDailyPrices(ctx, normalized)
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
	if state == nil || state.Fund == nil {
		return nil
	}

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
	latest := time.Time{}

	if state != nil {
		for _, series := range state.DailyPrices {
			if series.ISIN != FundUnitISIN || series.Provider != ProviderMOEX {
				continue
			}

			for _, item := range series.Items {
				priceDate := dateonly.UTC(item.PriceDate)
				if priceDate.After(latest) {
					latest = priceDate
				}
			}
		}
	}

	if latest.IsZero() {
		return fundUnitHistoryStartDate
	}

	if latest.Before(fundUnitHistoryStartDate) {
		return fundUnitHistoryStartDate
	}

	return latest
}

func normalizeFundUnitMOEXDailyPrices(items []SourceDailyPrice) ([]SourceDailyPrice, error) {
	result := make([]SourceDailyPrice, 0, len(items))
	seen := make(map[string]struct{}, len(items))

	for _, item := range items {
		item.PriceDate = dateonly.UTC(item.PriceDate)
		if item.PriceDate.IsZero() {
			return nil, fmt.Errorf("MOEX fund unit daily price has zero price date")
		}

		value, ok := decimal.Parse(item.UnitValue)
		if !ok || value.Sign() <= 0 {
			return nil, fmt.Errorf("MOEX fund unit daily price has invalid unit value %q", item.UnitValue)
		}

		currencyCode := strings.TrimSpace(item.Currency)
		if !currency.ValidCode(currencyCode) {
			return nil, fmt.Errorf("MOEX fund unit daily price has invalid currency %q", item.Currency)
		}

		key := item.PriceDate.Format(time.DateOnly)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("MOEX fund unit daily prices contain duplicate date %s", key)
		}
		seen[key] = struct{}{}

		item.UnitValue = strings.TrimSpace(item.UnitValue)
		item.Currency = currencyCode
		result = append(result, item)
	}

	slices.SortFunc(result, func(left, right SourceDailyPrice) int {
		return left.PriceDate.Compare(right.PriceDate)
	})

	return result, nil
}

func validateSourceQuote(quote SourceQuote) error {
	value, ok := decimal.Parse(quote.UnitValue)
	if !ok || value.Sign() <= 0 {
		return fmt.Errorf("invalid unit value %q", quote.UnitValue)
	}

	if !currency.ValidCode(strings.TrimSpace(quote.Currency)) {
		return fmt.Errorf("invalid currency %q", quote.Currency)
	}

	if quote.PricedAt.IsZero() {
		return fmt.Errorf("zero priced_at")
	}

	if strings.TrimSpace(quote.Source) == "" {
		return fmt.Errorf("empty source")
	}

	return nil
}
