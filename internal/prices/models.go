package prices

import (
	"context"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
)

const (
	ProviderMOEX = "moex"

	FundUnitAssetType = "fund_unit"
	FundUnitISIN      = "RU000A101NK4"
	FundUnitName      = "Фонд первичных размещений"
)

type QuoteSource interface {
	FetchFundUnitQuote(ctx context.Context) (*SourceQuote, error)
}

type SourceQuote struct {
	UnitValue string
	Currency  string
	PricedAt  time.Time
	Source    string
}

type SyncResult struct {
	Changed bool
	Stale   bool
	Source  string
	Price   appstate.InstrumentPrice
}
