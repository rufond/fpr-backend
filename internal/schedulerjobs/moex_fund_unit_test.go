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

type fakeMOEXFundUnitSyncService struct {
	result *prices.SyncResult
	err    error
}

func (s fakeMOEXFundUnitSyncService) SyncFundUnitMOEX(context.Context) (*prices.SyncResult, error) {
	return s.result, s.err
}

type captureRealtimePublisher struct {
	updates []realtime.Update
}

func (p *captureRealtimePublisher) Publish(update realtime.Update) {
	p.updates = append(p.updates, update)
}

func TestMOEXFundUnitSyncPublishesPriceDelta(t *testing.T) {
	t.Parallel()

	pricedAt := time.Date(2026, time.August, 14, 15, 42, 31, 0, time.UTC)
	publisher := &captureRealtimePublisher{}
	job := MOEXFundUnitSync(fakeMOEXFundUnitSyncService{result: &prices.SyncResult{
		Changed: true,
		Source:  "last",
		Price: appstate.InstrumentPrice{
			InstrumentID: 7,
			UnitValue:    "3210.5",
			Currency:     "RUB",
			PricedAt:     pricedAt,
		},
	}}, fakeLiveValuationRefresher{
		changed: true,
		live: appstate.FundLiveValuation{
			EstimatedCalculatedUnitValueRUB: "3250",
			PremiumDiscountPercent:          "-1.215384615384615385",
		},
	}, publisher)

	result, err := job(context.Background(), zerolog.Nop())
	if err != nil {
		t.Fatalf("job() error = %v", err)
	}
	if result == nil || result.Status != scheduler.RunStatusCompleted {
		t.Fatalf("result = %#v", result)
	}
	if len(publisher.updates) != 2 {
		t.Fatalf("updates len = %d, want 2", len(publisher.updates))
	}

	update := publisher.updates[0]
	if len(update.Scopes) != 1 || update.Scopes[0] != realtime.ScopeInstrumentPrices {
		t.Fatalf("scopes = %#v", update.Scopes)
	}
	if len(update.InstrumentPrices) != 1 || update.InstrumentPrices[0].InstrumentID != 7 || update.InstrumentPrices[0].UnitValue != "3210.5" {
		t.Fatalf("instrument prices = %#v", update.InstrumentPrices)
	}

	liveUpdate := publisher.updates[1]
	if len(liveUpdate.Scopes) != 1 || liveUpdate.Scopes[0] != realtime.ScopeLiveValuation ||
		liveUpdate.LiveValuation == nil || liveUpdate.LiveValuation.PremiumDiscountPercent == nil {
		t.Fatalf("live valuation update = %#v", liveUpdate)
	}
	if result.Summary["live_valuation_changed"] != true || result.Summary["live_valuation_error"] != "" {
		t.Fatalf("summary = %#v", result.Summary)
	}
}

func TestMOEXFundUnitSyncNoopDoesNotPublish(t *testing.T) {
	t.Parallel()

	publisher := &captureRealtimePublisher{}
	job := MOEXFundUnitSync(fakeMOEXFundUnitSyncService{result: &prices.SyncResult{
		Changed: false,
		Stale:   true,
		Source:  "previous",
		Price: appstate.InstrumentPrice{
			InstrumentID: 7,
			UnitValue:    "3180",
			Currency:     "RUB",
			PricedAt:     time.Date(2026, time.August, 13, 21, 0, 0, 0, time.UTC),
		},
	}}, fakeLiveValuationRefresher{changed: true}, publisher)

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

func TestMOEXFundUnitSyncCompletesWhenLiveValuationFails(t *testing.T) {
	t.Parallel()

	publisher := &captureRealtimePublisher{}
	valuationErr := errors.New("valuation unavailable")
	job := MOEXFundUnitSync(fakeMOEXFundUnitSyncService{result: &prices.SyncResult{
		Changed: true,
		Source:  "last",
		Price: appstate.InstrumentPrice{
			InstrumentID: 7,
			UnitValue:    "3210.5",
			Currency:     "RUB",
			PricedAt:     time.Date(2026, time.August, 14, 15, 42, 31, 0, time.UTC),
		},
	}}, fakeLiveValuationRefresher{err: valuationErr}, publisher)

	result, err := job(context.Background(), zerolog.Nop())
	if err != nil {
		t.Fatalf("job() error = %v", err)
	}
	if result == nil || result.Status != scheduler.RunStatusCompleted {
		t.Fatalf("result = %#v", result)
	}
	if result.Summary["live_valuation_changed"] != false || result.Summary["live_valuation_error"] != valuationErr.Error() {
		t.Fatalf("summary = %#v", result.Summary)
	}
	if len(publisher.updates) != 1 || len(publisher.updates[0].InstrumentPrices) != 1 {
		t.Fatalf("updates = %#v", publisher.updates)
	}
}

func TestMOEXFundUnitSyncReturnsServiceError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("moex unavailable")
	job := MOEXFundUnitSync(fakeMOEXFundUnitSyncService{err: wantErr}, fakeLiveValuationRefresher{}, nil)

	if _, err := job(context.Background(), zerolog.Nop()); !errors.Is(err, wantErr) {
		t.Fatalf("job() error = %v, want %v", err, wantErr)
	}
}
