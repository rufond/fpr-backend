package fx

import (
	"context"
	"fmt"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
)

type serviceRepository interface {
	LoadState(ctx context.Context) (*appstate.FXState, error)
	ApplyRate(
		ctx context.Context,
		rate SourceRate,
		fetchedAt time.Time,
	) (changed bool, stale bool, state *appstate.FXState, current appstate.FXRate, err error)
}

type Service struct {
	repository serviceRepository
	source     Source
	state      *appstate.Manager
	now        func() time.Time
}

func NewService(repository serviceRepository, source Source, state *appstate.Manager) *Service {
	return &Service{
		repository: repository,
		source:     source,
		state:      state,
		now:        time.Now,
	}
}

func (s *Service) Start(ctx context.Context) error {
	if s.repository == nil {
		return fmt.Errorf("FX repository is not configured")
	}
	if s.state == nil {
		return fmt.Errorf("application state manager is not configured")
	}

	fxState, err := s.repository.LoadState(ctx)
	if err != nil {
		return err
	}

	return s.state.Update(func(current *appstate.State) (*appstate.State, error) {
		next := new(*current)
		next.FX = fxState
		return next, nil
	})
}

func (s *Service) SyncUSDRUB(ctx context.Context) (*SyncResult, error) {
	rate, errFetch := s.source.FetchUSDRUB(ctx)
	if errFetch != nil {
		return nil, fmt.Errorf("fetch USD/RUB rate: %w", errFetch)
	}

	result := &SyncResult{Source: rate.Source}
	fetchedAt := s.now().UTC()

	errUpdate := s.state.Update(func(current *appstate.State) (*appstate.State, error) {
		changed, stale, fxState, currentRate, errApply := s.repository.ApplyRate(ctx, rate, fetchedAt)
		if errApply != nil {
			return nil, errApply
		}

		result.Changed = changed
		result.Stale = stale
		result.Rate = currentRate

		if !changed {
			return current, nil
		}
		if fxState == nil {
			return nil, fmt.Errorf("FX rate update returned no FX state")
		}

		next := new(*current)
		next.FX = fxState

		return next, nil
	})
	if errUpdate != nil {
		return nil, errUpdate
	}

	return result, nil
}
