package fx

import (
	"context"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
)

const ProviderMOEX = "moex"

type Source interface {
	FetchUSDRUB(ctx context.Context) (SourceRate, error)
}

type SourceRate struct {
	Provider      string
	BaseCurrency  string
	QuoteCurrency string
	Rate          string
	PricedAt      time.Time
	Source        string
}

type SyncResult struct {
	Changed bool
	Stale   bool
	Source  string
	Rate    appstate.FXRate
}
