package fund

import (
	"errors"
	"fmt"
	"sort"
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
	state, err := s.currentFundState()
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

func (s *Service) currentFundState() (*appstate.FundState, error) {
	current, err := s.currentState()
	if err != nil {
		return nil, err
	}

	return current.Fund, nil
}

func buildStateResult(state *appstate.State) *StateResult {
	fundState := state.Fund
	result := &StateResult{
		OfficialSnapshot: StateOfficialSnapshot{
			AsOfDate:               fundState.Snapshot.AsOfDate.Format("2006-01-02"),
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

func buildHistoryResult(state *appstate.FundState, from *time.Time) *HistoryResult {
	start := 0
	if from != nil {
		start = sort.Search(len(state.DailyValues), func(index int) bool {
			return !state.DailyValues[index].AsOfDate.Before(*from)
		})
	}

	result := &HistoryResult{
		DailyValues: make([]StateDailyValue, 0, len(state.DailyValues)-start),
	}

	for _, item := range state.DailyValues[start:] {
		result.DailyValues = append(result.DailyValues, StateDailyValue{
			AsOfDate:               item.AsOfDate.Format("2006-01-02"),
			CalculatedUnitValueUSD: item.CalculatedUnitValueUSD,
			NAVUSD:                 item.NAVUSD,
		})
	}

	return result
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
		Currency:             optionalString(item.Currency),
		Quantity:             optionalString(item.Quantity),
		AssetSharePercent:    item.AssetSharePercent,
		AssetShareUpperBound: item.AssetShareUpperBound,
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

func optionalString(value string) *string {
	if value == "" {
		return nil
	}

	result := value
	return &result
}
