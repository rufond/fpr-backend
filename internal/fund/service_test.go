package fund

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
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

type fakeFundStateResult struct {
	state *appstate.FundState
	err   error
}

type fakeServiceRepository struct {
	fundStateResults []fakeFundStateResult
	fundStateLoads   int

	existing       map[string]StoredDailyValue
	loadDailyErr   error
	appliedChanges DailyValueChanges

	appliedSnapshot SourceSnapshot
	appliedHash     string
	appliedObserved time.Time

	snapshotCreated  bool
	appliedFundState *appstate.FundState
	applyErr         error
}

func (f *fakeServiceRepository) LoadFundState(_ context.Context) (*appstate.FundState, error) {
	if f.fundStateLoads >= len(f.fundStateResults) {
		return nil, errors.New("unexpected LoadFundState call")
	}

	result := f.fundStateResults[f.fundStateLoads]
	f.fundStateLoads++

	return result.state, result.err
}

func (f *fakeServiceRepository) LoadDailyValues(_ context.Context) (map[string]StoredDailyValue, error) {
	if f.loadDailyErr != nil {
		return nil, f.loadDailyErr
	}
	return f.existing, nil
}

func (f *fakeServiceRepository) ApplyManagementCompanySync(
	_ context.Context,
	changes DailyValueChanges,
	snapshot SourceSnapshot,
	sourceHash string,
	observedAt time.Time,
) (bool, *appstate.FundState, error) {
	f.appliedChanges = changes
	f.appliedSnapshot = snapshot
	f.appliedHash = sourceHash
	f.appliedObserved = observedAt

	if f.applyErr != nil {
		return false, nil, f.applyErr
	}

	return f.snapshotCreated, f.appliedFundState, nil
}

func TestEnsureInitializedLoadsPersistentStateWithoutExternalSource(t *testing.T) {
	t.Parallel()

	fundState := testFundState(10)
	repository := &fakeServiceRepository{
		fundStateResults: []fakeFundStateResult{{state: fundState}},
	}
	source := &fakeManagementCompanySource{}
	state := appstate.NewManager()
	service := NewService(repository, source, state)

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

	current := state.Load()
	if current == nil || current.Fund != fundState {
		t.Fatalf("RAM fund state = %#v, want %#v", current, fundState)
	}
}

func TestEnsureInitializedSyncsWhenPersistentStateIsEmpty(t *testing.T) {
	t.Parallel()

	page := testSourcePageForSync()
	fundState := testFundState(11)
	repository := &fakeServiceRepository{
		fundStateResults: []fakeFundStateResult{{err: ErrFundStateNotFound}},
		existing:         map[string]StoredDailyValue{},
		snapshotCreated:  true,
		appliedFundState: fundState,
	}
	source := &fakeManagementCompanySource{page: page}
	state := appstate.NewManager()
	service := NewService(repository, source, state)

	result, err := service.EnsureInitialized(context.Background())
	if err != nil {
		t.Fatalf("EnsureInitialized() error = %v", err)
	}
	if result == nil || !result.SnapshotCreated {
		t.Fatalf("EnsureInitialized() result = %#v, want created snapshot", result)
	}
	if source.fetchCount != 1 {
		t.Fatalf("source fetch count = %d, want 1", source.fetchCount)
	}

	current := state.Load()
	if current == nil || current.Fund != fundState {
		t.Fatalf("RAM fund state = %#v, want %#v", current, fundState)
	}
}

func TestEnsureInitializedDoesNotPublishRAMStateWhenInitialSyncFails(t *testing.T) {
	t.Parallel()

	persistErr := errors.New("persist failed")
	repository := &fakeServiceRepository{
		fundStateResults: []fakeFundStateResult{{err: ErrFundStateNotFound}},
		existing:         map[string]StoredDailyValue{},
		applyErr:         persistErr,
	}
	state := appstate.NewManager()
	service := NewService(repository, &fakeManagementCompanySource{page: testSourcePageForSync()}, state)

	_, err := service.EnsureInitialized(context.Background())
	if !errors.Is(err, persistErr) {
		t.Fatalf("EnsureInitialized() error = %v, want %v", err, persistErr)
	}
	if state.Load() != nil {
		t.Fatal("RAM state initialized after failed persistence")
	}
}

func TestSyncManagementCompanyPersistsThenPublishesRAMState(t *testing.T) {
	t.Parallel()

	page := testSourcePageForSync()
	persistedState := testFundState(20)
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
		snapshotCreated:  true,
		appliedFundState: persistedState,
	}
	source := &fakeManagementCompanySource{page: page}
	state := appstate.NewManager()
	oldState := &appstate.State{Fund: testFundState(19)}
	if err := state.Initialize(oldState); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	service := NewService(repository, source, state)
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

	current := state.Load()
	if current == oldState {
		t.Fatal("RAM state pointer did not change after successful persistence")
	}
	if current == nil || current.Fund != persistedState {
		t.Fatalf("RAM fund state = %#v, want %#v", current, persistedState)
	}
}

func TestSyncManagementCompanyDoesNotPublishWhenPersistenceFails(t *testing.T) {
	t.Parallel()

	persistErr := errors.New("persist failed")
	repository := &fakeServiceRepository{
		existing: map[string]StoredDailyValue{},
		applyErr: persistErr,
	}
	state := appstate.NewManager()
	oldState := &appstate.State{Fund: testFundState(30)}
	if err := state.Initialize(oldState); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	service := NewService(repository, &fakeManagementCompanySource{page: testSourcePageForSync()}, state)
	_, err := service.SyncManagementCompany(context.Background())
	if !errors.Is(err, persistErr) {
		t.Fatalf("SyncManagementCompany() error = %v, want %v", err, persistErr)
	}
	if state.Load() != oldState {
		t.Fatal("RAM state changed after failed persistence")
	}
	if repository.fundStateLoads != 0 {
		t.Fatalf("LoadFundState calls = %d, want 0 after failed persistence", repository.fundStateLoads)
	}
}

func TestSyncManagementCompanyNoopKeepsCurrentRAMState(t *testing.T) {
	t.Parallel()

	page := testSourcePageForSync()
	existing := make(map[string]StoredDailyValue, len(page.History))
	for _, item := range page.History {
		existing[item.AsOfDate.Format(time.DateOnly)] = StoredDailyValue{
			AsOfDate:               item.AsOfDate,
			CalculatedUnitValueUSD: item.CalculatedUnitValueUSD,
			NAVUSD:                 item.NAVUSD,
		}
	}

	repository := &fakeServiceRepository{
		existing:        existing,
		snapshotCreated: false,
	}
	state := appstate.NewManager()
	oldState := &appstate.State{Fund: testFundState(40)}
	if err := state.Initialize(oldState); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	service := NewService(repository, &fakeManagementCompanySource{page: page}, state)
	result, err := service.SyncManagementCompany(context.Background())
	if err != nil {
		t.Fatalf("SyncManagementCompany() error = %v", err)
	}
	if result.HistoryInserted != 0 || result.HistoryUpdated != 0 || result.SnapshotCreated {
		t.Fatalf("unexpected sync changes: %#v", result)
	}
	if state.Load() != oldState {
		t.Fatal("RAM state pointer changed after noop sync")
	}
	if repository.fundStateLoads != 0 {
		t.Fatalf("LoadFundState calls = %d, want 0 after noop sync", repository.fundStateLoads)
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

func testFundState(snapshotID int64) *appstate.FundState {
	return &appstate.FundState{
		Snapshot: appstate.FundSnapshot{
			ID:                     snapshotID,
			AsOfDate:               time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC),
			ObservedAt:             time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC),
			SourceHash:             "test-hash",
			CalculatedUnitValueUSD: "31.18",
			NAVUSD:                 "492986650",
		},
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
