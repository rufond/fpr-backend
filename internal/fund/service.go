package fund

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/dateonly"
	"github.com/rufond/fpr-backend/internal/decimal"
)

const historyCorrectionWindowDays = 7

type ManagementCompanySource interface {
	FetchPage(ctx context.Context) (*SourcePage, error)
}

type serviceRepository interface {
	LoadFundState(ctx context.Context) (*appstate.FundState, error)
	LoadDailyValues(ctx context.Context) (map[string]StoredDailyValue, error)
	ApplyManagementCompanySync(
		ctx context.Context,
		changes DailyValueChanges,
		snapshot SourceSnapshot,
		sourceHash string,
		observedAt time.Time,
	) (bool, *appstate.FundState, error)
}

type Service struct {
	repository serviceRepository
	source     ManagementCompanySource
	state      *appstate.Manager
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

func NewService(repository serviceRepository, source ManagementCompanySource, state *appstate.Manager) *Service {
	return &Service{
		repository: repository,
		source:     source,
		state:      state,
		now:        time.Now,
	}
}

// EnsureInitialized loads durable fund state into RAM. The external source is
// needed only when PostgreSQL does not contain an official snapshot yet.
func (s *Service) EnsureInitialized(ctx context.Context) (*SyncResult, error) {
	if s.state == nil {
		return nil, fmt.Errorf("application state manager is not configured")
	}

	fundState, errState := s.repository.LoadFundState(ctx)
	if errState == nil {
		if errInitialize := s.state.Initialize(&appstate.State{Fund: fundState}); errInitialize != nil {
			return nil, fmt.Errorf("initialize application state: %w", errInitialize)
		}
		return nil, nil
	}
	if !errors.Is(errState, ErrFundStateNotFound) {
		return nil, errState
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
	if s.state == nil {
		return nil, fmt.Errorf("application state manager is not configured")
	}

	page, err := s.source.FetchPage(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch management company page: %w", err)
	}
	if errValidate := ValidateSourcePage(page); errValidate != nil {
		return nil, fmt.Errorf("validate management company page: %w", errValidate)
	}

	sourceHash, err := SnapshotSourceHash(page.Snapshot)
	if err != nil {
		return nil, fmt.Errorf("calculate management company snapshot source hash: %w", err)
	}

	result := &SyncResult{SourceHash: sourceHash}

	errUpdate := s.state.Update(func(current *appstate.State) (*appstate.State, error) {
		existingHistory, errHistory := s.repository.LoadDailyValues(ctx)
		if errHistory != nil {
			return nil, errHistory
		}

		changes, conflicts := planDailyValueChanges(page.History, existingHistory)
		observedAt := s.now().UTC()

		snapshotCreated, fundState, errApply := s.repository.ApplyManagementCompanySync(ctx, changes, page.Snapshot, sourceHash, observedAt)
		if errApply != nil {
			return nil, errApply
		}

		result.HistoryInserted = len(changes.Insert)
		result.HistoryUpdated = len(changes.Update)
		result.HistoryConflicts = conflicts
		result.SnapshotCreated = snapshotCreated

		if len(changes.Insert) == 0 && len(changes.Update) == 0 && !snapshotCreated {
			if current == nil {
				return nil, fmt.Errorf("management company sync persisted no state")
			}
			return current, nil
		}
		if fundState == nil {
			return nil, fmt.Errorf("management company sync returned no fund state after changes")
		}

		next := &appstate.State{}
		if current != nil {
			next = new(*current)
		}
		next.Fund = fundState

		return next, nil
	})
	if errUpdate != nil {
		return nil, errUpdate
	}

	return result, nil
}

func planDailyValueChanges(
	source []DailyValue,
	existing map[string]StoredDailyValue,
) (DailyValueChanges, []HistoryConflict) {
	if len(source) == 0 {
		return DailyValueChanges{}, nil
	}

	latestSourceDate := dateonly.UTC(source[len(source)-1].AsOfDate)
	mutableFrom := latestSourceDate.AddDate(0, 0, -historyCorrectionWindowDays)

	changes := DailyValueChanges{
		Insert: make([]DailyValue, 0),
		Update: make([]DailyValue, 0),
	}
	conflicts := make([]HistoryConflict, 0)

	for _, item := range source {
		item.AsOfDate = dateonly.UTC(item.AsOfDate)
		key := item.AsOfDate.Format(time.DateOnly)

		stored, exists := existing[key]
		if !exists {
			changes.Insert = append(changes.Insert, item)
			continue
		}

		if decimal.Equal(stored.CalculatedUnitValueUSD, item.CalculatedUnitValueUSD) &&
			decimal.Equal(stored.NAVUSD, item.NAVUSD) {
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
