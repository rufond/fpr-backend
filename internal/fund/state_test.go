package fund

import (
	"errors"
	"testing"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
)

func TestServiceStateBuildsPublicReadModelFromRAM(t *testing.T) {
	t.Parallel()

	instrumentID := int64(15)
	manager := appstate.NewManager()
	errInitialize := manager.Initialize(&appstate.State{Fund: &appstate.FundState{
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
	}})
	if errInitialize != nil {
		t.Fatalf("Initialize() error = %v", errInitialize)
	}

	service := NewService(nil, nil, manager)
	result, err := service.State()
	if err != nil {
		t.Fatalf("State() error = %v", err)
	}

	if result.Fund.UnitISIN != "RU000A101NK4" {
		t.Fatalf("UnitISIN = %q, want RU000A101NK4", result.Fund.UnitISIN)
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
	if len(result.DailyValues) != 2 || result.DailyValues[0].AsOfDate != "2026-08-11" {
		t.Fatalf("daily values = %#v", result.DailyValues)
	}
}

func TestServiceStateDoesNotExposeMutableRAMSlices(t *testing.T) {
	t.Parallel()

	manager := appstate.NewManager()
	if err := manager.Initialize(&appstate.State{Fund: &appstate.FundState{
		Snapshot: appstate.FundSnapshot{
			AsOfDate:               time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC),
			ObservedAt:             time.Date(2026, time.August, 13, 16, 20, 0, 0, time.UTC),
			CalculatedUnitValueUSD: "31.18",
			NAVUSD:                 "492986650.00",
			Assets: []appstate.FundAsset{
				{RowNo: 1, SourceType: "cash", AssetSharePercent: "1.00"},
			},
			Categories: []appstate.FundCategory{
				{RowNo: 1, SourceName: "cash", AssetSharePercent: "1.00"},
			},
		},
		DailyValues: []appstate.FundDailyValue{
			{
				AsOfDate:               time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC),
				CalculatedUnitValueUSD: "31.18",
				NAVUSD:                 "492986650.00",
			},
		},
	}}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	service := NewService(nil, nil, manager)
	first, errFirst := service.State()
	if errFirst != nil {
		t.Fatalf("first State() error = %v", errFirst)
	}

	first.OfficialSnapshot.Assets[0].SourceType = "changed"
	first.OfficialSnapshot.Categories[0].SourceName = "changed"
	first.DailyValues[0].NAVUSD = "1"

	second, errSecond := service.State()
	if errSecond != nil {
		t.Fatalf("second State() error = %v", errSecond)
	}

	if second.OfficialSnapshot.Assets[0].SourceType != "cash" ||
		second.OfficialSnapshot.Categories[0].SourceName != "cash" ||
		second.DailyValues[0].NAVUSD != "492986650.00" {
		t.Fatalf("public result mutated RAM state: %#v", second)
	}
}

func TestServiceStateUnavailable(t *testing.T) {
	t.Parallel()

	service := NewService(nil, nil, appstate.NewManager())
	_, err := service.State()
	if !errors.Is(err, ErrStateUnavailable) {
		t.Fatalf("State() error = %v, want ErrStateUnavailable", err)
	}
}
