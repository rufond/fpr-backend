package fund

import (
	"context"
	"testing"
	"time"
)

type fakeManagementCompanySource struct {
	page       *SourcePage
	err        error
	fetchCount int
}

func (f *fakeManagementCompanySource) FetchPage(_ context.Context) (*SourcePage, error) {
	f.fetchCount++
	return f.page, f.err
}

type fakeServiceRepository struct {
	hasSnapshots bool

	existing map[string]StoredDailyValue

	appliedChanges  DailyValueChanges
	appliedSnapshot SourceSnapshot
	appliedHash     string
	appliedObserved time.Time

	snapshotCreated bool
}

func (f *fakeServiceRepository) HasSnapshots(_ context.Context) (bool, error) {
	return f.hasSnapshots, nil
}

func (f *fakeServiceRepository) LoadDailyValues(_ context.Context) (map[string]StoredDailyValue, error) {
	return f.existing, nil
}

func (f *fakeServiceRepository) ApplyDailyValueChanges(_ context.Context, changes DailyValueChanges) error {
	f.appliedChanges = changes
	return nil
}

func (f *fakeServiceRepository) ApplySnapshot(
	_ context.Context,
	snapshot SourceSnapshot,
	sourceHash string,
	observedAt time.Time,
) (bool, error) {
	f.appliedSnapshot = snapshot
	f.appliedHash = sourceHash
	f.appliedObserved = observedAt
	return f.snapshotCreated, nil
}

func TestEnsureInitializedSkipsExternalSourceWhenSnapshotExists(t *testing.T) {
	t.Parallel()

	repository := &fakeServiceRepository{hasSnapshots: true}
	source := &fakeManagementCompanySource{}
	service := NewService(repository, source)

	result, err := service.EnsureInitialized(context.Background())
	if err != nil {
		t.Fatalf("EnsureInitialized() error = %v", err)
	}
	if result != nil {
		t.Fatalf("EnsureInitialized() result = %#v, want nil", result)
	}
	if source.fetchCount != 0 {
		t.Fatalf("source fetch count = %d, want 0", source.fetchCount)
	}
}

func TestSyncManagementCompanyPlansHistoryAndSnapshot(t *testing.T) {
	t.Parallel()

	page := testSourcePageForSync()
	repository := &fakeServiceRepository{
		existing: map[string]StoredDailyValue{
			"2026-08-01": {
				AsOfDate:               time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
				CalculatedUnitValueUSD: "29.50",
				NAVUSD:                 "470000000",
			},
			"2026-08-11": {
				AsOfDate:               time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC),
				CalculatedUnitValueUSD: "30.50",
				NAVUSD:                 "480000000",
			},
		},
		snapshotCreated: true,
	}
	source := &fakeManagementCompanySource{page: page}
	service := NewService(repository, source)
	service.now = func() time.Time {
		return time.Date(2026, time.August, 14, 1, 2, 3, 0, time.FixedZone("test", 2*60*60))
	}

	result, err := service.SyncManagementCompany(context.Background())
	if err != nil {
		t.Fatalf("SyncManagementCompany() error = %v", err)
	}
	if result.HistoryInserted != 1 {
		t.Fatalf("HistoryInserted = %d, want 1", result.HistoryInserted)
	}
	if result.HistoryUpdated != 1 {
		t.Fatalf("HistoryUpdated = %d, want 1", result.HistoryUpdated)
	}
	if len(result.HistoryConflicts) != 1 {
		t.Fatalf("HistoryConflicts = %d, want 1", len(result.HistoryConflicts))
	}
	if !result.SnapshotCreated {
		t.Fatal("SnapshotCreated = false, want true")
	}
	if result.SourceHash == "" || repository.appliedHash != result.SourceHash {
		t.Fatalf("source hash = %q repository = %q", result.SourceHash, repository.appliedHash)
	}

	if got := repository.appliedChanges.Insert[0].AsOfDate.Format(time.DateOnly); got != "2026-08-12" {
		t.Fatalf("inserted date = %s, want 2026-08-12", got)
	}
	if got := repository.appliedChanges.Update[0].AsOfDate.Format(time.DateOnly); got != "2026-08-11" {
		t.Fatalf("updated date = %s, want 2026-08-11", got)
	}
	if got := result.HistoryConflicts[0].AsOfDate.Format(time.DateOnly); got != "2026-08-01" {
		t.Fatalf("conflict date = %s, want 2026-08-01", got)
	}

	wantObserved := time.Date(2026, time.August, 13, 23, 2, 3, 0, time.UTC)
	if !repository.appliedObserved.Equal(wantObserved) {
		t.Fatalf("observed_at = %s, want %s", repository.appliedObserved, wantObserved)
	}
}

func TestPlanDailyValueChangesAllowsCorrectionExactlySevenDaysOld(t *testing.T) {
	t.Parallel()

	source := []DailyValue{
		{
			AsOfDate:               time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC),
			CalculatedUnitValueUSD: "30",
			NAVUSD:                 "100",
		},
		{
			AsOfDate:               time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC),
			CalculatedUnitValueUSD: "31",
			NAVUSD:                 "110",
		},
	}
	existing := map[string]StoredDailyValue{
		"2026-08-05": {
			AsOfDate:               time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC),
			CalculatedUnitValueUSD: "29",
			NAVUSD:                 "99",
		},
		"2026-08-12": {
			AsOfDate:               time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC),
			CalculatedUnitValueUSD: "31",
			NAVUSD:                 "110",
		},
	}

	changes, conflicts := planDailyValueChanges(source, existing)
	if len(changes.Update) != 1 || len(conflicts) != 0 {
		t.Fatalf("updates = %d conflicts = %d, want 1/0", len(changes.Update), len(conflicts))
	}
}

func TestPlanDailyValueChangesInsertsMissingDateOutsideMutableWindow(t *testing.T) {
	t.Parallel()

	source := []DailyValue{
		{
			AsOfDate:               time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
			CalculatedUnitValueUSD: "28.50",
			NAVUSD:                 "450000000",
		},
		{
			AsOfDate:               time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC),
			CalculatedUnitValueUSD: "31.18",
			NAVUSD:                 "492986650",
		},
	}
	existing := map[string]StoredDailyValue{
		"2026-08-12": {
			AsOfDate:               time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC),
			CalculatedUnitValueUSD: "31.18",
			NAVUSD:                 "492986650",
		},
	}

	changes, conflicts := planDailyValueChanges(source, existing)
	if len(changes.Insert) != 1 || len(changes.Update) != 0 || len(conflicts) != 0 {
		t.Fatalf(
			"insert/update/conflicts = %d/%d/%d, want 1/0/0",
			len(changes.Insert),
			len(changes.Update),
			len(conflicts),
		)
	}
	if got := changes.Insert[0].AsOfDate.Format(time.DateOnly); got != "2026-07-01" {
		t.Fatalf("inserted date = %s, want 2026-07-01", got)
	}
}

func testSourcePageForSync() *SourcePage {
	snapshot := testSnapshot()
	return &SourcePage{
		Snapshot: snapshot,
		History: []DailyValue{
			{
				AsOfDate:               time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
				CalculatedUnitValueUSD: "29.60",
				NAVUSD:                 "471000000",
			},
			{
				AsOfDate:               time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC),
				CalculatedUnitValueUSD: "30.60",
				NAVUSD:                 "483764001.84",
			},
			{
				AsOfDate:               time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC),
				CalculatedUnitValueUSD: "31.18",
				NAVUSD:                 "492986650",
			},
		},
	}
}
