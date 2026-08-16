package realtime

import "time"

const (
	ScopeDiagnostics      = "diagnostics"
	ScopeFundHistory      = "fund_history"
	ScopeFundState        = "fund_state"
	ScopeFXRates          = "fx_rates"
	ScopeInstrumentPrices = "instrument_prices"
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

type Update struct {
	Scopes           []string
	InstrumentIDs    []int64
	InstrumentPrices []InstrumentPriceDelta
	FXRates          []FXRateDelta
}

type Publisher interface {
	Publish(update Update)
}

type DiscardPublisher struct{}

func (DiscardPublisher) Publish(Update) {}
