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

type Source interface {
	FetchFundUnitQuote(ctx context.Context) (*SourceQuote, error)
	FetchFundUnitDailyPrices(ctx context.Context, from time.Time) ([]SourceDailyPrice, error)
}

type SourceQuote struct {
	UnitValue string
	Currency  string
	PricedAt  time.Time
	Source    string
}

type SourceDailyPrice struct {
	PriceDate time.Time
	UnitValue string
	Currency  string
}

type SyncResult struct {
	Changed bool
	Stale   bool
	Source  string
	Price   appstate.InstrumentPrice
}

type DailySyncResult struct {
	Inserted int
	Updated  int
	FromDate time.Time
	ToDate   time.Time
}

func (r DailySyncResult) Changed() bool {
	return r.Inserted != 0 || r.Updated != 0
}
