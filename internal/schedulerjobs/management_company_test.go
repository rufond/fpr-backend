package schedulerjobs

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/rufond/fpr-backend/internal/fund"
	"github.com/rufond/fpr-backend/internal/realtime"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

type fakeManagementCompanySyncService struct {
	result *fund.SyncResult
	err    error
}

func (f *fakeManagementCompanySyncService) SyncManagementCompany(context.Context) (*fund.SyncResult, error) {
	return f.result, f.err
}

type fakeRealtimePublisher struct {
	updates []realtime.Update
}

func (f *fakeRealtimePublisher) Publish(update realtime.Update) {
	f.updates = append(f.updates, update)
}

func TestManagementCompanySyncReturnsNoop(t *testing.T) {
	t.Parallel()

	service := &fakeManagementCompanySyncService{
		result: &fund.SyncResult{SourceHash: "hash"},
	}
	publisher := &fakeRealtimePublisher{}

	result, err := ManagementCompanySync(service, publisher)(context.Background(), zerolog.Nop())
	if err != nil {
		t.Fatalf("ManagementCompanySync() error = %v", err)
	}

	if result.Status != scheduler.RunStatusNoop {
		t.Fatalf("status = %q, want %q", result.Status, scheduler.RunStatusNoop)
	}

	if result.Summary["source_hash"] != "hash" {
		t.Fatalf("source_hash = %#v, want hash", result.Summary["source_hash"])
	}

	if len(publisher.updates) != 0 {
		t.Fatalf("realtime updates = %#v, want none", publisher.updates)
	}
}

func TestManagementCompanySyncReturnsCompleted(t *testing.T) {
	t.Parallel()

	service := &fakeManagementCompanySyncService{
		result: &fund.SyncResult{
			SourceHash:      "hash",
			HistoryInserted: 2,
			HistoryUpdated:  1,
			HistoryConflicts: []fund.HistoryConflict{
				{AsOfDate: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)},
			},
			SnapshotCreated: true,
		},
	}
	publisher := &fakeRealtimePublisher{}

	result, err := ManagementCompanySync(service, publisher)(context.Background(), zerolog.Nop())
	if err != nil {
		t.Fatalf("ManagementCompanySync() error = %v", err)
	}

	if result.Status != scheduler.RunStatusCompleted {
		t.Fatalf("status = %q, want %q", result.Status, scheduler.RunStatusCompleted)
	}

	if result.Summary["history_inserted"] != 2 {
		t.Fatalf("history_inserted = %#v, want 2", result.Summary["history_inserted"])
	}
	if result.Summary["history_conflicts"] != 1 {
		t.Fatalf("history_conflicts = %#v, want 1", result.Summary["history_conflicts"])
	}
	if result.Summary["snapshot_created"] != true {
		t.Fatalf("snapshot_created = %#v, want true", result.Summary["snapshot_created"])
	}

	if len(publisher.updates) != 1 {
		t.Fatalf("realtime updates len = %d, want 1", len(publisher.updates))
	}
	if !slices.Equal(publisher.updates[0].Scopes, []string{realtime.ScopeFundState, realtime.ScopeFundHistory}) {
		t.Fatalf("realtime scopes = %#v", publisher.updates[0].Scopes)
	}
}

func TestManagementCompanySyncPublishesHistoryOnly(t *testing.T) {
	t.Parallel()

	service := &fakeManagementCompanySyncService{
		result: &fund.SyncResult{
			SourceHash:      "hash",
			HistoryInserted: 1,
		},
	}
	publisher := &fakeRealtimePublisher{}

	_, err := ManagementCompanySync(service, publisher)(context.Background(), zerolog.Nop())
	if err != nil {
		t.Fatalf("ManagementCompanySync() error = %v", err)
	}

	if len(publisher.updates) != 1 || !slices.Equal(publisher.updates[0].Scopes, []string{realtime.ScopeFundHistory}) {
		t.Fatalf("realtime updates = %#v", publisher.updates)
	}
}

func TestManagementCompanySyncDoesNotPublishFixedHistoryConflict(t *testing.T) {
	t.Parallel()

	service := &fakeManagementCompanySyncService{
		result: &fund.SyncResult{
			SourceHash: "hash",
			HistoryConflicts: []fund.HistoryConflict{
				{AsOfDate: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)},
			},
		},
	}
	publisher := &fakeRealtimePublisher{}

	_, err := ManagementCompanySync(service, publisher)(context.Background(), zerolog.Nop())
	if err != nil {
		t.Fatalf("ManagementCompanySync() error = %v", err)
	}

	if len(publisher.updates) != 0 {
		t.Fatalf("realtime updates = %#v, want none", publisher.updates)
	}
}

func TestManagementCompanySyncReturnsError(t *testing.T) {
	t.Parallel()

	expected := errors.New("source unavailable")
	service := &fakeManagementCompanySyncService{err: expected}
	publisher := &fakeRealtimePublisher{}

	result, err := ManagementCompanySync(service, publisher)(context.Background(), zerolog.Nop())
	if !errors.Is(err, expected) {
		t.Fatalf("ManagementCompanySync() error = %v, want %v", err, expected)
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
	if len(publisher.updates) != 0 {
		t.Fatalf("realtime updates = %#v, want none", publisher.updates)
	}
}
