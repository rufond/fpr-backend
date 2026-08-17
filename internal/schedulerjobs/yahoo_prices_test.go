package schedulerjobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/realtime"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

type fakeYahooPricesSyncService struct {
	result *prices.YahooSyncResult
	err    error
}

func (s fakeYahooPricesSyncService) SyncYahooPrices(context.Context) (*prices.YahooSyncResult, error) {
	return s.result, s.err
}

func TestYahooPricesSyncPublishesPriceDeltas(t *testing.T) {
	t.Parallel()

	pricedAt := time.Date(2026, time.August, 16, 15, 42, 31, 0, time.UTC)
	publisher := &captureRealtimePublisher{}
	job := YahooPricesSync(fakeYahooPricesSyncService{result: &prices.YahooSyncResult{
		ExpectedSources:  2,
		RequestedSymbols: 2,
		ReturnedSymbols:  2,
		Batches:          1,
		ChangedPrices: []appstate.InstrumentPrice{
			{InstrumentID: 42, UnitValue: "126.32", Currency: "GBP", PricedAt: pricedAt},
			{InstrumentID: 43, UnitValue: "81.5", Currency: "USD", PricedAt: pricedAt},
		},
	}}, fakeLiveValuationRefresher{}, publisher)

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
	if len(update.Scopes) != 1 || update.Scopes[0] != realtime.ScopeInstrumentPrices {
		t.Fatalf("scopes = %#v", update.Scopes)
	}
	if len(update.InstrumentPrices) != 2 || update.InstrumentPrices[0].InstrumentID != 42 || update.InstrumentPrices[1].InstrumentID != 43 {
		t.Fatalf("instrument prices = %#v", update.InstrumentPrices)
	}
}

func TestYahooPricesSyncNoopDoesNotPublish(t *testing.T) {
	t.Parallel()

	publisher := &captureRealtimePublisher{}
	job := YahooPricesSync(fakeYahooPricesSyncService{result: &prices.YahooSyncResult{
		ExpectedSources:  2,
		RequestedSymbols: 2,
		ReturnedSymbols:  1,
		MissingSources:   1,
		MissingSymbols:   []string{"BBB"},
		UnchangedSources: 1,
	}}, fakeLiveValuationRefresher{}, publisher)

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

func TestYahooPricesSyncReturnsServiceError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("Yahoo unavailable")
	job := YahooPricesSync(fakeYahooPricesSyncService{err: wantErr}, fakeLiveValuationRefresher{}, nil)

	if _, err := job(context.Background(), zerolog.Nop()); !errors.Is(err, wantErr) {
		t.Fatalf("job() error = %v, want %v", err, wantErr)
	}
}
