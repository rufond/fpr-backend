package fund

import (
	"context"
	"fmt"
	"time"
)

const historyCorrectionWindowDays = 7

type ManagementCompanySource interface {
	FetchPage(ctx context.Context) (*SourcePage, error)
}

type serviceRepository interface {
	HasSnapshots(ctx context.Context) (bool, error)
	LoadDailyValues(ctx context.Context) (map[string]StoredDailyValue, error)
	ApplyDailyValueChanges(ctx context.Context, changes DailyValueChanges) error
	ApplySnapshot(ctx context.Context, snapshot SourceSnapshot, sourceHash string, observedAt time.Time) (bool, error)
}

type Service struct {
	repository serviceRepository
	source     ManagementCompanySource
	now        func() time.Time
}

type SyncResult struct {
	SourceHash string

	HistoryInserted  int
	HistoryUpdated   int
	HistoryConflicts []HistoryConflict

	SnapshotCreated bool
}

type HistoryConflict struct {
	AsOfDate time.Time

	StoredCalculatedUnitValueUSD string
	StoredNAVUSD                 string

	SourceCalculatedUnitValueUSD string
	SourceNAVUSD                 string
}

func NewService(repository serviceRepository, source ManagementCompanySource) *Service {
	return &Service{
		repository: repository,
		source:     source,
		now:        time.Now,
	}
}

// EnsureInitialized performs the external management-company sync only when the
// database does not contain any official snapshot yet. An already initialized
// application must remain able to start while the external source is unavailable.
func (s *Service) EnsureInitialized(ctx context.Context) (*SyncResult, error) {
	hasSnapshots, err := s.repository.HasSnapshots(ctx)
	if err != nil {
		return nil, err
	}
	if hasSnapshots {
		return nil, nil
	}

	result, err := s.SyncManagementCompany(ctx)
	if err != nil {
		return nil, fmt.Errorf("initial management company sync: %w", err)
	}
	if !result.SnapshotCreated {
		return nil, fmt.Errorf("initial management company sync did not create a snapshot")
	}

	return result, nil
}

func (s *Service) SyncManagementCompany(ctx context.Context) (*SyncResult, error) {
	if s.source == nil {
		return nil, fmt.Errorf("management company source is not configured")
	}

	page, err := s.source.FetchPage(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch management company page: %w", err)
	}
	if errValidate := ValidateSourcePage(page); errValidate != nil {
		return nil, fmt.Errorf("validate management company page: %w", errValidate)
	}

	existingHistory, err := s.repository.LoadDailyValues(ctx)
	if err != nil {
		return nil, err
	}

	changes, conflicts := planDailyValueChanges(page.History, existingHistory)
	if errApply := s.repository.ApplyDailyValueChanges(ctx, changes); errApply != nil {
		return nil, errApply
	}

	sourceHash, err := SnapshotSourceHash(page.Snapshot)
	if err != nil {
		return nil, fmt.Errorf("calculate management company snapshot source hash: %w", err)
	}

	snapshotCreated, err := s.repository.ApplySnapshot(
		ctx,
		page.Snapshot,
		sourceHash,
		s.now().UTC(),
	)
	if err != nil {
		return nil, err
	}

	return &SyncResult{
		SourceHash:       sourceHash,
		HistoryInserted:  len(changes.Insert),
		HistoryUpdated:   len(changes.Update),
		HistoryConflicts: conflicts,
		SnapshotCreated:  snapshotCreated,
	}, nil
}

func planDailyValueChanges(
	source []DailyValue,
	existing map[string]StoredDailyValue,
) (DailyValueChanges, []HistoryConflict) {
	if len(source) == 0 {
		return DailyValueChanges{}, nil
	}

	latestSourceDate := dateOnlyUTC(source[len(source)-1].AsOfDate)
	mutableFrom := latestSourceDate.AddDate(0, 0, -historyCorrectionWindowDays)

	changes := DailyValueChanges{
		Insert: make([]DailyValue, 0),
		Update: make([]DailyValue, 0),
	}
	conflicts := make([]HistoryConflict, 0)

	for _, item := range source {
		item.AsOfDate = dateOnlyUTC(item.AsOfDate)
		key := item.AsOfDate.Format(time.DateOnly)

		stored, exists := existing[key]
		if !exists {
			changes.Insert = append(changes.Insert, item)
			continue
		}

		if decimalEqual(stored.CalculatedUnitValueUSD, item.CalculatedUnitValueUSD) &&
			decimalEqual(stored.NAVUSD, item.NAVUSD) {
			continue
		}

		if item.AsOfDate.Before(mutableFrom) {
			conflicts = append(conflicts, HistoryConflict{
				AsOfDate: item.AsOfDate,

				StoredCalculatedUnitValueUSD: stored.CalculatedUnitValueUSD,
				StoredNAVUSD:                 stored.NAVUSD,

				SourceCalculatedUnitValueUSD: item.CalculatedUnitValueUSD,
				SourceNAVUSD:                 item.NAVUSD,
			})
			continue
		}

		changes.Update = append(changes.Update, item)
	}

	return changes, conflicts
}
