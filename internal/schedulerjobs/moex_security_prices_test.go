package schedulerjobs

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/realtime"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

type fakeMOEXSecurityPricesSyncService struct {
	result *prices.MOEXSecuritySyncResult
	err    error
}

func (s fakeMOEXSecurityPricesSyncService) SyncMOEXSecurityPrices(context.Context) (*prices.MOEXSecuritySyncResult, error) {
	return s.result, s.err
}

func TestMOEXSecurityPricesSyncPublishesPriceDeltas(t *testing.T) {
	t.Parallel()

	pricedAt := time.Date(2026, time.August, 16, 18, 42, 31, 0, time.UTC)
	publisher := &captureRealtimePublisher{}
	job := MOEXSecurityPricesSync(fakeMOEXSecurityPricesSyncService{result: &prices.MOEXSecuritySyncResult{
		ExpectedSources:  1,
		RequestedSymbols: 1,
		ChangedPrices: []appstate.InstrumentPrice{
			{InstrumentID: 51, UnitValue: "85.75", Currency: "RUB", PricedAt: pricedAt},
		},
	}}, fakeLiveValuationRefresher{}, publisher)

	result, err := job(context.Background(), zerolog.Nop())
	if err != nil {
		t.Fatalf("job() error = %v", err)
	}
	if result == nil || result.Status != scheduler.RunStatusCompleted {
		t.Fatalf("result = %#v", result)
	}
	if len(publisher.updates) != 1 || len(publisher.updates[0].InstrumentPrices) != 1 {
		t.Fatalf("updates = %#v", publisher.updates)
	}
	if publisher.updates[0].Scopes[0] != realtime.ScopeInstrumentPrices || publisher.updates[0].InstrumentPrices[0].InstrumentID != 51 {
		t.Fatalf("update = %#v", publisher.updates[0])
	}
}
