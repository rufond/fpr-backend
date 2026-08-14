package schedulerjobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/rufond/fpr-backend/internal/fund"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

type fakeManagementCompanySyncService struct {
	result *fund.SyncResult
	err    error
}

func (f *fakeManagementCompanySyncService) SyncManagementCompany(context.Context) (*fund.SyncResult, error) {
	return f.result, f.err
}

func TestManagementCompanySyncReturnsNoop(t *testing.T) {
	t.Parallel()

	service := &fakeManagementCompanySyncService{
		result: &fund.SyncResult{SourceHash: "hash"},
	}

	result, err := ManagementCompanySync(service)(context.Background(), zerolog.Nop())
	if err != nil {
		t.Fatalf("ManagementCompanySync() error = %v", err)
	}

	if result.Status != scheduler.RunStatusNoop {
		t.Fatalf("status = %q, want %q", result.Status, scheduler.RunStatusNoop)
	}

	if result.Summary["source_hash"] != "hash" {
		t.Fatalf("source_hash = %#v, want hash", result.Summary["source_hash"])
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

	result, err := ManagementCompanySync(service)(context.Background(), zerolog.Nop())
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
}

func TestManagementCompanySyncReturnsError(t *testing.T) {
	t.Parallel()

	expected := errors.New("source unavailable")
	service := &fakeManagementCompanySyncService{err: expected}

	result, err := ManagementCompanySync(service)(context.Background(), zerolog.Nop())
	if !errors.Is(err, expected) {
		t.Fatalf("ManagementCompanySync() error = %v, want %v", err, expected)
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
}
