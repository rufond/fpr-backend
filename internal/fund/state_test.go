package fund

import (
	"errors"
	"testing"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/prices"
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
	quote, exists := result.Market.AssetPrices[instrumentID]
	if len(result.Market.AssetPrices) != 1 || !exists || quote.UnitValue != "30000" || quote.Currency != "KZT" || quote.Provider != "yahoo" {
		t.Fatalf("asset prices = %#v", result.Market.AssetPrices)
	}
	if result.OfficialSnapshot.UnitCount == nil || result.OfficialSnapshot.UnitCount.AsOfDate != "2026-07-31" ||
		result.OfficialSnapshot.UnitCount.UnitsOutstanding != "15700000" {
		t.Fatalf("unit count = %#v", result.OfficialSnapshot.UnitCount)
	}
	if result.Market.USDRUB == nil || result.Market.USDRUB.Rate != "79.125" {
		t.Fatalf("USD/RUB = %#v", result.Market.USDRUB)
	}
	if result.Market.UnitPriceUSD != "40.57503949" {
		t.Fatalf("market unit price in USD = %q", result.Market.UnitPriceUSD)
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
	if len(result.UnitMarketPrices) != 3 {
		t.Fatalf("unit market prices len = %d, want 3", len(result.UnitMarketPrices))
	}
	if result.UnitMarketPrices[0].AsOfDate != "2026-08-10" || result.UnitMarketPrices[2].AsOfDate != "2026-08-12" {
		t.Fatalf("unit market prices = %#v", result.UnitMarketPrices)
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
	if len(result.UnitMarketPrices) != 2 {
		t.Fatalf("unit market prices len = %d, want 2", len(result.UnitMarketPrices))
	}
	if result.UnitMarketPrices[0].AsOfDate != "2026-08-11" || result.UnitMarketPrices[1].AsOfDate != "2026-08-12" {
		t.Fatalf("unit market prices = %#v", result.UnitMarketPrices)
	}
}

func TestServiceMarketHistoryReturnsFullRetainedSeries(t *testing.T) {
	t.Parallel()

	service := NewService(nil, nil, testStateManager(t, 15))
	result, err := service.MarketHistory(nil)
	if err != nil {
		t.Fatalf("MarketHistory() error = %v", err)
	}

	if len(result.UnitPrices) != 3 {
		t.Fatalf("unit prices len = %d, want 3", len(result.UnitPrices))
	}
	if result.UnitPrices[0].UnitValue != "3190" || result.UnitPrices[2].UnitValue != "3210.5" {
		t.Fatalf("unit prices = %#v", result.UnitPrices)
	}
}

func TestServiceMarketHistoryFiltersFromTimeInclusively(t *testing.T) {
	t.Parallel()

	service := NewService(nil, nil, testStateManager(t, 15))
	from := time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC)

	result, err := service.MarketHistory(&from)
	if err != nil {
		t.Fatalf("MarketHistory() error = %v", err)
	}
	if len(result.UnitPrices) != 2 {
		t.Fatalf("unit prices len = %d, want 2", len(result.UnitPrices))
	}
	if !result.UnitPrices[0].PricedAt.Equal(from) || result.UnitPrices[1].UnitValue != "3210.5" {
		t.Fatalf("unit prices = %#v", result.UnitPrices)
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
	marketHistory, errMarketHistory := service.MarketHistory(nil)
	if errMarketHistory != nil {
		t.Fatalf("MarketHistory() error = %v", errMarketHistory)
	}

	state.OfficialSnapshot.Assets[0].SourceType = "changed"
	state.OfficialSnapshot.Categories[0].SourceName = "changed"
	history.DailyValues[0].NAVUSD = "1"
	history.UnitMarketPrices[0].UnitValue = "1"
	marketHistory.UnitPrices[0].UnitValue = "1"

	stateAgain, errStateAgain := service.State()
	if errStateAgain != nil {
		t.Fatalf("second State() error = %v", errStateAgain)
	}
	historyAgain, errHistoryAgain := service.History(nil)
	if errHistoryAgain != nil {
		t.Fatalf("second History() error = %v", errHistoryAgain)
	}
	marketHistoryAgain, errMarketHistoryAgain := service.MarketHistory(nil)
	if errMarketHistoryAgain != nil {
		t.Fatalf("second MarketHistory() error = %v", errMarketHistoryAgain)
	}

	if stateAgain.OfficialSnapshot.Assets[0].SourceType != "Акции" ||
		stateAgain.OfficialSnapshot.Categories[0].SourceName != "Акции" ||
		historyAgain.DailyValues[0].NAVUSD != "470000000.00" ||
		historyAgain.UnitMarketPrices[0].UnitValue != "3100" ||
		marketHistoryAgain.UnitPrices[0].UnitValue != "3190" {
		t.Fatalf("public result mutated RAM state: state=%#v history=%#v market_history=%#v", stateAgain, historyAgain, marketHistoryAgain)
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

	_, errMarketHistory := service.MarketHistory(nil)
	if !errors.Is(errMarketHistory, ErrStateUnavailable) {
		t.Fatalf("MarketHistory() error = %v, want ErrStateUnavailable", errMarketHistory)
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
				UnitCount: &appstate.FundUnitCount{
					AsOfDate:         time.Date(2026, time.July, 31, 0, 0, 0, 0, time.UTC),
					UnitsOutstanding: "15700000",
					SourceURL:        "https://example.test/report.pdf",
				},
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
		FX: &appstate.FXState{
			Rates: map[appstate.FXPair]appstate.FXRate{
				{BaseCurrency: "USD", QuoteCurrency: "RUB"}: {
					BaseCurrency:  "USD",
					QuoteCurrency: "RUB",
					Provider:      "moex",
					Rate:          "79.125",
					PricedAt:      time.Date(2026, time.August, 14, 15, 42, 31, 0, time.UTC),
				},
			},
		},
		Prices: &appstate.PriceState{
			Sources: map[int64]appstate.InstrumentPrice{
				102: {
					PriceSourceID: 102,
					InstrumentID:  instrumentID,
					Provider:      "yahoo",
					UnitValue:     "30000",
					Currency:      "KZT",
				},
				103: {
					PriceSourceID: 103,
					InstrumentID:  instrumentID + 1,
					Provider:      "moex",
					UnitValue:     "160",
					Currency:      "RUB",
				},
				101: {
					PriceSourceID:  101,
					InstrumentID:   99,
					AssetType:      "fund_unit",
					ISIN:           "RU000A101NK4",
					Name:           "Фонд первичных размещений",
					Provider:       "moex",
					ProviderSymbol: "RU000A101NK4",
					UnitValue:      "3210.5",
					Currency:       "RUB",
					PricedAt:       time.Date(2026, time.August, 14, 15, 42, 31, 0, time.UTC),
				},
			},
			DailyPrices: map[int64]appstate.InstrumentDailyPriceSeries{
				101: {
					PriceSourceID:  101,
					InstrumentID:   99,
					AssetType:      "fund_unit",
					ISIN:           "RU000A101NK4",
					Name:           "Фонд первичных размещений",
					Provider:       "moex",
					ProviderSymbol: "RU000A101NK4",
					Items: []appstate.InstrumentDailyPrice{
						{PriceDate: time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC), UnitValue: "3100", Currency: "RUB"},
						{PriceDate: time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC), UnitValue: "3150", Currency: "RUB"},
						{PriceDate: time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC), UnitValue: "3200", Currency: "RUB"},
					},
				},
			},
			Points: map[int64]appstate.InstrumentPricePointSeries{
				101: {
					PriceSourceID:  101,
					InstrumentID:   99,
					AssetType:      "fund_unit",
					ISIN:           "RU000A101NK4",
					Name:           "Фонд первичных размещений",
					Provider:       "moex",
					ProviderSymbol: "RU000A101NK4",
					Items: []appstate.InstrumentPricePoint{
						{UnitValue: "3190", Currency: "RUB", PricedAt: time.Date(2026, time.August, 14, 14, 30, 0, 0, time.UTC), ObservedAt: time.Date(2026, time.August, 14, 14, 30, 1, 0, time.UTC)},
						{UnitValue: "3200", Currency: "RUB", PricedAt: time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC), ObservedAt: time.Date(2026, time.August, 14, 15, 0, 1, 0, time.UTC)},
						{UnitValue: "3210.5", Currency: "RUB", PricedAt: time.Date(2026, time.August, 14, 15, 42, 31, 0, time.UTC), ObservedAt: time.Date(2026, time.August, 14, 15, 42, 35, 0, time.UTC)},
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	return manager
}

func TestServiceStateIncludesLiveValuationFromRAM(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.August, 17, 18, 30, 0, 0, time.UTC)
	manager := appstate.NewManager()
	if err := manager.Initialize(&appstate.State{
		Fund: &appstate.FundState{Snapshot: appstate.FundSnapshot{
			ID:                     21,
			AsOfDate:               time.Date(2026, time.August, 16, 0, 0, 0, 0, time.UTC),
			CalculatedUnitValueUSD: "31.18",
			NAVUSD:                 "492986650",
		}},
		Valuation: &appstate.ValuationState{
			SnapshotID: 21,
			Current: appstate.FundLiveValuation{
				SnapshotID:                      21,
				ObservedAt:                      observedAt,
				EstimatedNAVUSD:                 "493100000.25",
				EstimatedCalculatedUnitValueUSD: "31.187169",
				EstimatedCalculatedUnitValueRUB: "2472.95689",
				PremiumDiscountPercent:          "-1.25",
				LiveDeltaUSD:                    "113350.25",
				LiveCoveragePercent:             "74.5",
			},
		},
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	result, err := NewService(nil, nil, manager).State()
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if result.Market.LiveValuation == nil {
		t.Fatal("LiveValuation = nil")
	}
	if result.Market.LiveValuation.EstimatedNAVUSD != "493100000.25" ||
		result.Market.LiveValuation.LiveDeltaUSD != "113350.25" ||
		result.Market.LiveValuation.LiveCoveragePercent != "74.5" {
		t.Fatalf("LiveValuation = %#v", result.Market.LiveValuation)
	}
	if result.Market.LiveValuation.EstimatedCalculatedUnitValueRUB == nil ||
		*result.Market.LiveValuation.EstimatedCalculatedUnitValueRUB != "2472.95689" ||
		result.Market.LiveValuation.PremiumDiscountPercent == nil ||
		*result.Market.LiveValuation.PremiumDiscountPercent != "-1.25" {
		t.Fatalf("market comparison = %#v", result.Market.LiveValuation)
	}
}

func TestServiceStateHidesLiveValuationForPreviousSnapshot(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	if err := manager.Initialize(&appstate.State{
		Fund: &appstate.FundState{Snapshot: appstate.FundSnapshot{
			ID:                     22,
			AsOfDate:               time.Date(2026, time.August, 17, 0, 0, 0, 0, time.UTC),
			CalculatedUnitValueUSD: "31.18",
			NAVUSD:                 "492986650",
		}},
		Valuation: &appstate.ValuationState{
			SnapshotID: 21,
			Current:    appstate.FundLiveValuation{SnapshotID: 21},
		},
	}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	result, err := NewService(nil, nil, manager).State()
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}
	if result.Market.LiveValuation != nil {
		t.Fatalf("LiveValuation = %#v, want nil", result.Market.LiveValuation)
	}
}

func TestInstrumentMarketHistoryUsesOnlyCurrentSourceAndSelectedWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 24, 15, 0, 0, 0, time.UTC)
	isin := "KZ1C00001122"
	price := func(amount, currency string, before time.Duration) appstate.InstrumentPricePoint {
		return appstate.InstrumentPricePoint{UnitValue: amount, Currency: currency, PricedAt: now.Add(-before)}
	}
	manager := appstate.NewManager()
	err := manager.Initialize(&appstate.State{
		Fund: &appstate.FundState{},
		Prices: &appstate.PriceState{
			Sources: map[int64]appstate.InstrumentPrice{2: {ISIN: isin, Provider: "kase"}},
			Points: map[int64]appstate.InstrumentPricePointSeries{
				1: {ISIN: isin, Provider: "yahoo", Items: []appstate.InstrumentPricePoint{price("917", "USD", time.Hour)}},
				2: {ISIN: isin, Provider: "kase", Items: []appstate.InstrumentPricePoint{
					price("10200", "KZT", 0),
					price("8000", "KZT", 25*time.Hour),
					price("10000", "KZT", 24*time.Hour),
					price("10250", "KZT", 0),
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := NewService(nil, nil, manager).InstrumentMarketHistory(isin, new(now.Add(-24*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.UnitPrices) != 2 || len(result.LiveValues) != 0 ||
		result.UnitChange != "250.00000000" || result.UnitChangePercent != "2.500000" {
		t.Fatalf("instrument history = %#v", result)
	}
}

func TestStateUsesWebsiteCheckpointsForShareChange(t *testing.T) {
	current := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	previous := current.AddDate(0, 0, -5)
	snapshot := &appstate.FundState{
		Snapshot: appstate.FundSnapshot{
			AsOfDate: current, NAVUSD: "1000000",
			Assets: []appstate.FundAsset{
				{ISIN: "KZ1C00001122", Quantity: "2000", AssetSharePercent: "8.4"},
				{ISIN: "US3563901046", Quantity: "1000", AssetSharePercent: "0.01", AssetShareUpperBound: true},
			},
		},
		CompositionHistory: []appstate.FundComposition{
			{AsOfDate: previous, Holdings: []appstate.FundHolding{
				{ISIN: "KZ1C00001122", AssetSharePercent: "7.25", Rows: 1},
				{ISIN: "US3563901046", AssetSharePercent: "0.1", Rows: 1},
			}},
			{AsOfDate: current, Holdings: []appstate.FundHolding{
				{ISIN: "KZ1C00001122", AssetSharePercent: "8.4", Rows: 1},
			}},
		},
	}
	result := buildStateResult(&appstate.State{Fund: snapshot}, current, nil)
	asset := result.OfficialSnapshot.Assets[0]
	if asset.DisclosureUnitEstimateUSD != "42.00000000" || asset.ShareChangePP != "1.1500" || asset.PreviousShareDate != "2026-08-26" {
		t.Fatalf("unexpected asset %#v", asset)
	}
	bounded := result.OfficialSnapshot.Assets[1]
	if bounded.DisclosureUnitEstimateUSD != "" || bounded.ShareChangePP != "" {
		t.Fatalf("rounded upper bound treated as exact: %#v", bounded)
	}
}

func TestStateMarketPriceOmitsPriorSessionDayChange(t *testing.T) {
	// 20:00 UTC is still the same trading date on the Moscow Exchange at 20:50 UTC.
	quote := appstate.InstrumentPrice{Provider: prices.ProviderMOEX, UnitValue: "103", PreviousClose: "100", PricedAt: time.Date(2026, 8, 31, 20, 0, 0, 0, time.UTC)}
	daily := appstate.InstrumentDailyPriceSeries{}
	if result := stateMarketPrice(quote, daily, "2", quote.PricedAt.Add(50*time.Minute)); result.DayChange == nil || result.DayChange.Total != "6.00000000" {
		t.Fatalf("current trading day change = %#v", result.DayChange)
	}
	if result := stateMarketPrice(quote, daily, "2", quote.PricedAt.Add(time.Hour)); result.DayChange != nil {
		t.Fatalf("prior trading day change = %#v", result.DayChange)
	}
}

func TestStateMarketPriceShowsImportedQuoteProvider(t *testing.T) {
	pricedAt := time.Date(2026, 9, 1, 20, 0, 1, 0, time.UTC)
	quote := appstate.InstrumentPrice{
		Provider:         prices.ProviderNasdaq,
		ImportedProvider: prices.ProviderYahoo,
		ProviderSymbol:   "FRHC",
		UnitValue:        "171.87",
		PreviousClose:    "170",
		PricedAt:         pricedAt,
	}
	result := stateMarketPrice(quote, appstate.InstrumentDailyPriceSeries{}, "2", pricedAt.Add(time.Minute))
	if result.Provider != prices.ProviderYahoo || !result.Archived || !result.PricedAt.Equal(pricedAt) || result.DayChange != nil {
		t.Fatalf("unexpected imported market price: %#v", result)
	}
}
