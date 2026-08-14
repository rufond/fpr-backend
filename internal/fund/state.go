package fund

import (
	"errors"
	"fmt"

	"github.com/rufond/fpr-backend/internal/appstate"
)

var ErrStateUnavailable = errors.New("fund state unavailable")

var stateFundInfo = StateFundInfo{
	Name:              "Закрытый паевой инвестиционный фонд рыночных финансовых инструментов «Фонд первичных размещений»",
	ShortName:         "Фонд первичных размещений",
	RulesNumber:       "3964",
	UnitISIN:          "RU000A101NK4",
	ManagementCompany: "ООО «Управляющая компания «Восток-Запад»»",
}

func (s *Service) State() (*StateResult, error) {
	if s.state == nil {
		return nil, fmt.Errorf("application state manager is not configured")
	}

	current := s.state.Load()
	if current == nil || current.Fund == nil {
		return nil, ErrStateUnavailable
	}

	return buildStateResult(current.Fund), nil
}

func buildStateResult(state *appstate.FundState) *StateResult {
	result := &StateResult{
		Fund: stateFundInfo,
		OfficialSnapshot: StateOfficialSnapshot{
			AsOfDate:               state.Snapshot.AsOfDate.Format("2006-01-02"),
			ObservedAt:             state.Snapshot.ObservedAt,
			CalculatedUnitValueUSD: state.Snapshot.CalculatedUnitValueUSD,
			NAVUSD:                 state.Snapshot.NAVUSD,
			Assets:                 make([]StateAsset, 0, len(state.Snapshot.Assets)),
			Categories:             make([]StateCategory, 0, len(state.Snapshot.Categories)),
		},
		DailyValues: make([]StateDailyValue, 0, len(state.DailyValues)),
	}

	for _, item := range state.Snapshot.Assets {
		result.OfficialSnapshot.Assets = append(result.OfficialSnapshot.Assets, stateAsset(item))
	}

	for _, item := range state.Snapshot.Categories {
		result.OfficialSnapshot.Categories = append(result.OfficialSnapshot.Categories, StateCategory{
			RowNo:             item.RowNo,
			SourceName:        item.SourceName,
			AssetSharePercent: item.AssetSharePercent,
		})
	}

	for _, item := range state.DailyValues {
		result.DailyValues = append(result.DailyValues, StateDailyValue{
			AsOfDate:               item.AsOfDate.Format("2006-01-02"),
			CalculatedUnitValueUSD: item.CalculatedUnitValueUSD,
			NAVUSD:                 item.NAVUSD,
		})
	}

	return result
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
