package schedulerjobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/realtime"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

type fakeMOEXFundUnitHistorySyncService struct {
	result *prices.DailySyncResult
	err    error
}

func (s fakeMOEXFundUnitHistorySyncService) SyncFundUnitMOEXHistory(context.Context) (*prices.DailySyncResult, error) {
	return s.result, s.err
}

func TestMOEXFundUnitHistorySyncPublishesHistoryInvalidation(t *testing.T) {
	t.Parallel()

	publisher := &captureRealtimePublisher{}
	job := MOEXFundUnitHistorySync(fakeMOEXFundUnitHistorySyncService{result: &prices.DailySyncResult{
		Inserted: 2,
		Updated:  1,
		FromDate: time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC),
		ToDate:   time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC),
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
	if len(publisher.updates[0].Scopes) != 1 || publisher.updates[0].Scopes[0] != realtime.ScopeFundHistory {
		t.Fatalf("scopes = %#v", publisher.updates[0].Scopes)
	}
}

func TestMOEXFundUnitHistorySyncNoopDoesNotPublish(t *testing.T) {
	t.Parallel()

	publisher := &captureRealtimePublisher{}
	job := MOEXFundUnitHistorySync(fakeMOEXFundUnitHistorySyncService{result: &prices.DailySyncResult{
		FromDate: time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC),
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

func TestMOEXFundUnitHistorySyncReturnsServiceError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("moex unavailable")
	job := MOEXFundUnitHistorySync(fakeMOEXFundUnitHistorySyncService{err: wantErr}, nil)

	if _, err := job(context.Background(), zerolog.Nop()); !errors.Is(err, wantErr) {
		t.Fatalf("job() error = %v, want %v", err, wantErr)
	}
}
