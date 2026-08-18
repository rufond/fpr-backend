package realtime

import (
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
)

const (
	ScopeDiagnostics      = "diagnostics"
	ScopeFundHistory      = "fund_history"
	ScopeFundState        = "fund_state"
	ScopeFXRates          = "fx_rates"
	ScopeInstrumentPrices = "instrument_prices"
	ScopeLiveValuation    = "live_valuation"
	ScopeScheduler        = "scheduler"
)

type InstrumentPriceDelta struct {
	InstrumentID int64     `json:"instrument_id"`
	UnitValue    string    `json:"unit_value"`
	Currency     string    `json:"currency"`
	PricedAt     time.Time `json:"priced_at"`
}

type FXRateDelta struct {
	BaseCurrency  string    `json:"base_currency"`
	QuoteCurrency string    `json:"quote_currency"`
	Rate          string    `json:"rate"`
	PricedAt      time.Time `json:"priced_at"`
}

type LiveValuationDelta struct {
	ObservedAt time.Time `json:"observed_at"`

	EstimatedNAVUSD                 string  `json:"estimated_nav_usd"`
	EstimatedCalculatedUnitValueUSD string  `json:"estimated_calculated_unit_value_usd"`
	EstimatedCalculatedUnitValueRUB *string `json:"estimated_calculated_unit_value_rub"`
	PremiumDiscountPercent          *string `json:"premium_discount_percent"`
	LiveDeltaUSD                    string  `json:"live_delta_usd"`
	LiveCoveragePercent             string  `json:"live_coverage_percent"`
}

type Update struct {
	Scopes           []string
	InstrumentIDs    []int64
	InstrumentPrices []InstrumentPriceDelta
	FXRates          []FXRateDelta
	LiveValuation    *LiveValuationDelta
}

type Publisher interface {
	Publish(update Update)
}

type DiscardPublisher struct{}

func (DiscardPublisher) Publish(Update) {}

func LiveValuationUpdate(live appstate.FundLiveValuation) Update {
	delta := &LiveValuationDelta{
		ObservedAt:                      live.ObservedAt,
		EstimatedNAVUSD:                 live.EstimatedNAVUSD,
		EstimatedCalculatedUnitValueUSD: live.EstimatedCalculatedUnitValueUSD,
		LiveDeltaUSD:                    live.LiveDeltaUSD,
		LiveCoveragePercent:             live.LiveCoveragePercent,
	}

	if live.EstimatedCalculatedUnitValueRUB != "" {
		delta.EstimatedCalculatedUnitValueRUB = new(live.EstimatedCalculatedUnitValueRUB)
	}
	if live.PremiumDiscountPercent != "" {
		delta.PremiumDiscountPercent = new(live.PremiumDiscountPercent)
	}

	return Update{
		Scopes:        []string{ScopeLiveValuation},
		LiveValuation: delta,
	}
}
