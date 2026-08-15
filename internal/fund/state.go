package fund

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/prices"
)

var ErrStateUnavailable = errors.New("fund state unavailable")

func (s *Service) State() (*StateResult, error) {
	state, err := s.currentState()
	if err != nil {
		return nil, err
	}

	return buildStateResult(state), nil
}

func (s *Service) History(from *time.Time) (*HistoryResult, error) {
	state, err := s.currentState()
	if err != nil {
		return nil, err
	}

	return buildHistoryResult(state, from), nil
}

func (s *Service) currentState() (*appstate.State, error) {
	if s.state == nil {
		return nil, fmt.Errorf("application state manager is not configured")
	}

	current := s.state.Load()
	if current == nil || current.Fund == nil {
		return nil, ErrStateUnavailable
	}

	return current, nil
}

func buildStateResult(state *appstate.State) *StateResult {
	fundState := state.Fund
	result := &StateResult{
		OfficialSnapshot: StateOfficialSnapshot{
			AsOfDate:               fundState.Snapshot.AsOfDate.Format(time.DateOnly),
			ObservedAt:             fundState.Snapshot.ObservedAt,
			CalculatedUnitValueUSD: fundState.Snapshot.CalculatedUnitValueUSD,
			NAVUSD:                 fundState.Snapshot.NAVUSD,
			Assets:                 make([]StateAsset, 0, len(fundState.Snapshot.Assets)),
			Categories:             make([]StateCategory, 0, len(fundState.Snapshot.Categories)),
		},
		Market: StateMarket{
			UnitPrice: stateFundUnitPrice(state.Prices),
		},
	}

	for _, item := range fundState.Snapshot.Assets {
		result.OfficialSnapshot.Assets = append(result.OfficialSnapshot.Assets, stateAsset(item))
	}

	for _, item := range fundState.Snapshot.Categories {
		result.OfficialSnapshot.Categories = append(result.OfficialSnapshot.Categories, StateCategory{
			RowNo:             item.RowNo,
			SourceName:        item.SourceName,
			AssetSharePercent: item.AssetSharePercent,
		})
	}

	return result
}

func buildHistoryResult(state *appstate.State, from *time.Time) *HistoryResult {
	fundState := state.Fund
	start := 0
	if from != nil {
		start, _ = slices.BinarySearchFunc(fundState.DailyValues, *from, func(item appstate.FundDailyValue, target time.Time) int {
			return item.AsOfDate.Compare(target)
		})
	}

	result := &HistoryResult{
		DailyValues:      make([]StateDailyValue, 0, len(fundState.DailyValues)-start),
		UnitMarketPrices: []StateDailyMarketPrice{},
	}

	for _, item := range fundState.DailyValues[start:] {
		result.DailyValues = append(result.DailyValues, StateDailyValue{
			AsOfDate:               item.AsOfDate.Format(time.DateOnly),
			CalculatedUnitValueUSD: item.CalculatedUnitValueUSD,
			NAVUSD:                 item.NAVUSD,
		})
	}

	series := stateFundUnitDailyPrices(state.Prices)
	marketStart := 0
	if from != nil {
		marketStart, _ = slices.BinarySearchFunc(series, *from, func(item appstate.InstrumentDailyPrice, target time.Time) int {
			return item.PriceDate.Compare(target)
		})
	}

	result.UnitMarketPrices = make([]StateDailyMarketPrice, 0, len(series)-marketStart)
	for _, item := range series[marketStart:] {
		result.UnitMarketPrices = append(result.UnitMarketPrices, StateDailyMarketPrice{
			AsOfDate:  item.PriceDate.Format(time.DateOnly),
			UnitValue: item.UnitValue,
			Currency:  item.Currency,
		})
	}

	return result
}

func stateFundUnitDailyPrices(state *appstate.PriceState) []appstate.InstrumentDailyPrice {
	if state == nil {
		return nil
	}

	for _, series := range state.DailyPrices {
		if series.ISIN == prices.FundUnitISIN && series.Provider == prices.ProviderMOEX {
			return series.Items
		}
	}

	return nil
}

func stateFundUnitPrice(state *appstate.PriceState) *StateMarketPrice {
	if state == nil {
		return nil
	}

	for _, item := range state.Sources {
		if item.ISIN != prices.FundUnitISIN || item.Provider != prices.ProviderMOEX {
			continue
		}

		return &StateMarketPrice{
			InstrumentID: item.InstrumentID,
			UnitValue:    item.UnitValue,
			Currency:     item.Currency,
			PricedAt:     item.PricedAt,
		}
	}

	return nil
}

func stateAsset(item appstate.FundAsset) StateAsset {
	result := StateAsset{
		RowNo:                item.RowNo,
		SourceName:           item.SourceName,
		SourceType:           item.SourceType,
		AssetSharePercent:    item.AssetSharePercent,
		AssetShareUpperBound: item.AssetShareUpperBound,
	}

	if item.Currency != "" {
		result.Currency = new(item.Currency)
	}
	if item.Quantity != "" {
		result.Quantity = new(item.Quantity)
	}

	if item.InstrumentID != nil {
		result.Instrument = &StateInstrument{
			ID:        *item.InstrumentID,
			AssetType: item.InstrumentType,
			ISIN:      item.ISIN,
			Name:      item.InstrumentName,
			Issuer:    item.Issuer,
			Ticker:    item.Ticker,
		}
	}

	return result
}
