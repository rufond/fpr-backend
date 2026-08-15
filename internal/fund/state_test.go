package fund

import (
	"errors"
	"testing"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
)

func TestServiceStateBuildsCurrentReadModelFromRAM(t *testing.T) {
	t.Parallel()

	instrumentID := int64(15)
	manager := testStateManager(t, instrumentID)
	service := NewService(nil, nil, manager)

	result, err := service.State()
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}

	if result.OfficialSnapshot.AsOfDate != "2026-08-12" {
		t.Fatalf("AsOfDate = %q, want 2026-08-12", result.OfficialSnapshot.AsOfDate)
	}
	if result.OfficialSnapshot.CalculatedUnitValueUSD != "31.18" || result.OfficialSnapshot.NAVUSD != "492986650.00" {
		t.Fatalf("official values = %#v", result.OfficialSnapshot)
	}
	if len(result.OfficialSnapshot.Assets) != 2 {
		t.Fatalf("assets len = %d, want 2", len(result.OfficialSnapshot.Assets))
	}
	if result.OfficialSnapshot.Assets[0].Instrument == nil || result.OfficialSnapshot.Assets[0].Instrument.ISIN != "KZ1C00001122" {
		t.Fatalf("security instrument = %#v", result.OfficialSnapshot.Assets[0].Instrument)
	}
	if result.OfficialSnapshot.Assets[0].Quantity == nil || *result.OfficialSnapshot.Assets[0].Quantity != "584986" {
		t.Fatalf("security quantity = %#v", result.OfficialSnapshot.Assets[0].Quantity)
	}
	if result.OfficialSnapshot.Assets[1].Instrument != nil || result.OfficialSnapshot.Assets[1].Quantity != nil {
		t.Fatalf("cash asset optional fields = %#v", result.OfficialSnapshot.Assets[1])
	}
	if result.Market.UnitPrice == nil {
		t.Fatal("market unit price is nil")
	}
	if result.Market.UnitPrice.InstrumentID != 99 || result.Market.UnitPrice.UnitValue != "3210.5" || result.Market.UnitPrice.Currency != "RUB" {
		t.Fatalf("market unit price = %#v", result.Market.UnitPrice)
	}
}

func TestServiceHistoryReturnsFullHistory(t *testing.T) {
	t.Parallel()

	manager := testStateManager(t, 15)
	service := NewService(nil, nil, manager)

	result, err := service.History(nil)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}

	if len(result.DailyValues) != 3 {
		t.Fatalf("daily values len = %d, want 3", len(result.DailyValues))
	}
	if result.DailyValues[0].AsOfDate != "2026-08-10" || result.DailyValues[2].AsOfDate != "2026-08-12" {
		t.Fatalf("daily values = %#v", result.DailyValues)
	}
}

func TestServiceHistoryFiltersFromDateInclusively(t *testing.T) {
	t.Parallel()

	manager := testStateManager(t, 15)
	service := NewService(nil, nil, manager)
	from := time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC)

	result, err := service.History(&from)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}

	if len(result.DailyValues) != 2 {
		t.Fatalf("daily values len = %d, want 2", len(result.DailyValues))
	}
	if result.DailyValues[0].AsOfDate != "2026-08-11" || result.DailyValues[1].AsOfDate != "2026-08-12" {
		t.Fatalf("daily values = %#v", result.DailyValues)
	}
}

func TestServiceStateAndHistoryDoNotExposeMutableRAMSlices(t *testing.T) {
	t.Parallel()

	manager := testStateManager(t, 15)
	service := NewService(nil, nil, manager)

	state, errState := service.State()
	if errState != nil {
		t.Fatalf("State() error = %v", errState)
	}
	history, errHistory := service.History(nil)
	if errHistory != nil {
		t.Fatalf("History() error = %v", errHistory)
	}

	state.OfficialSnapshot.Assets[0].SourceType = "changed"
	state.OfficialSnapshot.Categories[0].SourceName = "changed"
	history.DailyValues[0].NAVUSD = "1"

	stateAgain, errStateAgain := service.State()
	if errStateAgain != nil {
		t.Fatalf("second State() error = %v", errStateAgain)
	}
	historyAgain, errHistoryAgain := service.History(nil)
	if errHistoryAgain != nil {
		t.Fatalf("second History() error = %v", errHistoryAgain)
	}

	if stateAgain.OfficialSnapshot.Assets[0].SourceType != "Акции" ||
		stateAgain.OfficialSnapshot.Categories[0].SourceName != "Акции" ||
		historyAgain.DailyValues[0].NAVUSD != "470000000.00" {
		t.Fatalf("public result mutated RAM state: state=%#v history=%#v", stateAgain, historyAgain)
	}
}

func TestServiceStateUnavailable(t *testing.T) {
	t.Parallel()

	service := NewService(nil, nil, appstate.NewManager())
	_, errState := service.State()
	if !errors.Is(errState, ErrStateUnavailable) {
		t.Fatalf("State() error = %v, want ErrStateUnavailable", errState)
	}

	_, errHistory := service.History(nil)
	if !errors.Is(errHistory, ErrStateUnavailable) {
		t.Fatalf("History() error = %v, want ErrStateUnavailable", errHistory)
	}
}

func testStateManager(t *testing.T, instrumentID int64) *appstate.Manager {
	t.Helper()

	manager := appstate.NewManager()
	if err := manager.Initialize(&appstate.State{
		Fund: &appstate.FundState{
			Snapshot: appstate.FundSnapshot{
				ID:                     21,
				AsOfDate:               time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC),
				ObservedAt:             time.Date(2026, time.August, 13, 16, 20, 0, 0, time.UTC),
				SourceHash:             "ignored-by-public-api",
				CalculatedUnitValueUSD: "31.18",
				NAVUSD:                 "492986650.00",
				Assets: []appstate.FundAsset{
					{
						ID:                   1,
						RowNo:                1,
						SourceName:           "АО НК КазМунайГаз",
						SourceType:           "Акции",
						InstrumentID:         &instrumentID,
						InstrumentType:       AssetKindEquity,
						ISIN:                 "KZ1C00001122",
						InstrumentName:       "АО НК КазМунайГаз",
						Ticker:               "KMGZ",
						Currency:             "KZT",
						Quantity:             "584986",
						AssetSharePercent:    "10.25",
						AssetShareUpperBound: false,
					},
					{
						ID:                   2,
						RowNo:                2,
						SourceName:           "",
						SourceType:           "Денежные средства в кредитных организациях",
						AssetSharePercent:    "4.50",
						AssetShareUpperBound: false,
					},
				},
				Categories: []appstate.FundCategory{
					{RowNo: 1, SourceName: "Акции", AssetSharePercent: "20.50"},
				},
			},
			DailyValues: []appstate.FundDailyValue{
				{
					AsOfDate:               time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC),
					CalculatedUnitValueUSD: "29.80",
					NAVUSD:                 "470000000.00",
				},
				{
					AsOfDate:               time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC),
					CalculatedUnitValueUSD: "30.60",
					NAVUSD:                 "483764001.84",
				},
				{
					AsOfDate:               time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC),
					CalculatedUnitValueUSD: "31.18",
					NAVUSD:                 "492986650.00",
				},
			},
		},
		Prices: &appstate.PriceState{
			Sources: map[int64]appstate.InstrumentPrice{
				101: {
					PriceSourceID:  101,
					InstrumentID:   99,
					AssetType:      "fund_unit",
					ISIN:           "RU000A101NK4",
					Name:           "Фонд первичных размещений",
					Provider:       "moex",
					ProviderSymbol: "TQBR:RU000A101NK4",
					UnitValue:      "3210.5",
					Currency:       "RUB",
					PricedAt:       time.Date(2026, time.August, 14, 15, 42, 31, 0, time.UTC),
				},
			},
		},
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	return manager
}
