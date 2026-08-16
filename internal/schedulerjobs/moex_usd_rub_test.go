package schedulerjobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/currency"
	"github.com/rufond/fpr-backend/internal/fx"
	"github.com/rufond/fpr-backend/internal/realtime"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

type fakeMOEXUSDRUBSyncService struct {
	result *fx.SyncResult
	err    error
}

func (s fakeMOEXUSDRUBSyncService) SyncUSDRUB(context.Context) (*fx.SyncResult, error) {
	return s.result, s.err
}

func TestMOEXUSDRUBSyncPublishesFXDelta(t *testing.T) {
	t.Parallel()

	pricedAt := time.Date(2026, time.August, 15, 15, 42, 31, 0, time.UTC)
	publisher := &captureRealtimePublisher{}
	job := MOEXUSDRUBSync(fakeMOEXUSDRUBSyncService{result: &fx.SyncResult{
		Changed: true,
		Source:  "last",
		Rate: appstate.FXRate{
			BaseCurrency:  currency.USD,
			QuoteCurrency: currency.RUB,
			Provider:      fx.ProviderMOEX,
			Rate:          "79.125",
			PricedAt:      pricedAt,
		},
	}}, publisher)

	result, err := job(context.Background(), zerolog.Nop())
	if err != nil {
		t.Fatalf("job() error = %v", err)
	}
	if result == nil || result.Status != scheduler.RunStatusCompleted {
		t.Fatalf("result = %#v", result)
	}
	if len(publisher.updates) != 1 {
		t.Fatalf("updates len = %d, want 1", len(publisher.updates))
	}

	update := publisher.updates[0]
	if len(update.Scopes) != 1 || update.Scopes[0] != realtime.ScopeFXRates {
		t.Fatalf("scopes = %#v", update.Scopes)
	}
	if len(update.FXRates) != 1 || update.FXRates[0].BaseCurrency != "USD" || update.FXRates[0].QuoteCurrency != "RUB" || update.FXRates[0].Rate != "79.125" {
		t.Fatalf("FX rates = %#v", update.FXRates)
	}
}

func TestMOEXUSDRUBSyncNoopDoesNotPublish(t *testing.T) {
	t.Parallel()

	publisher := &captureRealtimePublisher{}
	job := MOEXUSDRUBSync(fakeMOEXUSDRUBSyncService{result: &fx.SyncResult{
		Stale:  true,
		Source: "previous",
		Rate: appstate.FXRate{
			BaseCurrency:  currency.USD,
			QuoteCurrency: currency.RUB,
			Provider:      fx.ProviderMOEX,
			Rate:          "79",
			PricedAt:      time.Date(2026, time.August, 14, 21, 0, 0, 0, time.UTC),
		},
	}}, publisher)

	result, err := job(context.Background(), zerolog.Nop())
	if err != nil {
		t.Fatalf("job() error = %v", err)
	}
	if result == nil || result.Status != scheduler.RunStatusNoop {
		t.Fatalf("result = %#v", result)
	}
	if len(publisher.updates) != 0 {
		t.Fatalf("updates = %#v", publisher.updates)
	}
}

func TestMOEXUSDRUBSyncReturnsServiceError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("moex unavailable")
	job := MOEXUSDRUBSync(fakeMOEXUSDRUBSyncService{err: wantErr}, nil)

	if _, err := job(context.Background(), zerolog.Nop()); !errors.Is(err, wantErr) {
		t.Fatalf("job() error = %v, want %v", err, wantErr)
	}
}
