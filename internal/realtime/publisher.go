package realtime

import "time"

const (
	ScopeDiagnostics      = "diagnostics"
	ScopeFundHistory      = "fund_history"
	ScopeFundState        = "fund_state"
	ScopeInstrumentPrices = "instrument_prices"
	ScopeScheduler        = "scheduler"
)

type InstrumentPriceDelta struct {
	InstrumentID int64     `json:"instrument_id"`
	UnitValue    string    `json:"unit_value"`
	Currency     string    `json:"currency"`
	PricedAt     time.Time `json:"priced_at"`
}

type Update struct {
	Scopes           []string
	InstrumentIDs    []int64
	InstrumentPrices []InstrumentPriceDelta
}

type Publisher interface {
	Publish(update Update)
}

type DiscardPublisher struct{}

func (DiscardPublisher) Publish(Update) {}
