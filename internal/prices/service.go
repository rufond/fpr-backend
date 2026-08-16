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
	ApplyFundUnitMOEXQuote(
		ctx context.Context,
		quote SourceQuote,
		fetchedAt time.Time,
	) (changed bool, stale bool, price appstate.InstrumentPrice, err error)
	ApplyFundUnitMOEXDailyPrices(
		ctx context.Context,
		items []SourceDailyPrice,
	) (inserted int, updated int, state *appstate.PriceState, err error)
}

type Service struct {
	repository serviceRepository
	source     Source
	state      *appstate.Manager
	now        func() time.Time
}

func NewService(repository serviceRepository, source Source, state *appstate.Manager) *Service {
	return &Service{
		repository: repository,
		source:     source,
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
	if errValidate := validateFundUnitMOEXQuote(*quote); errValidate != nil {
		return nil, errValidate
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

		priceState := &appstate.PriceState{
			Sources: map[int64]appstate.InstrumentPrice{},
			Points:  map[int64]appstate.InstrumentPricePointSeries{},
		}
		if current.Prices != nil {
			priceState.Sources = maps.Clone(current.Prices.Sources)
			priceState.DailyPrices = current.Prices.DailyPrices
			priceState.Points = maps.Clone(current.Prices.Points)
		}
		if priceState.Sources == nil {
			priceState.Sources = map[int64]appstate.InstrumentPrice{}
		}
		if priceState.Points == nil {
			priceState.Points = map[int64]appstate.InstrumentPricePointSeries{}
		}

		priceState.Sources[price.PriceSourceID] = price

		series, exists := priceState.Points[price.PriceSourceID]
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
			ObservedAt: fetchedAt,
		})
		series.Items = retainedPricePoints(series.Items, fetchedAt.Add(-pricePointRetention))
		priceState.Points[price.PriceSourceID] = series

		next := new(*current)
		next.Prices = priceState
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

func validateFundUnitMOEXQuote(quote SourceQuote) error {
	value, ok := decimal.Parse(quote.UnitValue)
	if !ok || value.Sign() <= 0 {
		return fmt.Errorf("MOEX fund unit quote has invalid unit value %q", quote.UnitValue)
	}
	if !currency.ValidCode(strings.TrimSpace(quote.Currency)) {
		return fmt.Errorf("MOEX fund unit quote has invalid currency %q", quote.Currency)
	}
	if quote.PricedAt.IsZero() {
		return fmt.Errorf("MOEX fund unit quote has zero priced_at")
	}
	if strings.TrimSpace(quote.Source) == "" {
		return fmt.Errorf("MOEX fund unit quote has empty source")
	}

	return nil
}
