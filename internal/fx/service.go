package fx

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/currency"
	"github.com/rufond/fpr-backend/internal/decimal"
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
		if current == nil {
			return nil, fmt.Errorf("application state is not initialized")
		}

		next := new(*current)
		next.FX = fxState
		return next, nil
	})
}

func (s *Service) SyncUSDRUB(ctx context.Context) (*SyncResult, error) {
	if s.repository == nil {
		return nil, fmt.Errorf("FX repository is not configured")
	}
	if s.source == nil {
		return nil, fmt.Errorf("FX source is not configured")
	}
	if s.state == nil {
		return nil, fmt.Errorf("application state manager is not configured")
	}

	rate, errFetch := s.source.FetchRate(ctx, currency.USD, currency.RUB)
	if errFetch != nil {
		return nil, fmt.Errorf("fetch USD/RUB rate: %w", errFetch)
	}
	if rate == nil {
		return nil, fmt.Errorf("USD/RUB rate is nil")
	}

	rate.Provider = strings.TrimSpace(rate.Provider)
	rate.BaseCurrency = strings.ToUpper(strings.TrimSpace(rate.BaseCurrency))
	rate.QuoteCurrency = strings.ToUpper(strings.TrimSpace(rate.QuoteCurrency))
	rate.Rate = strings.TrimSpace(rate.Rate)
	rate.Source = strings.TrimSpace(rate.Source)
	rate.PricedAt = rate.PricedAt.UTC()

	if rate.Provider == "" {
		return nil, fmt.Errorf("FX source returned empty provider")
	}
	if rate.BaseCurrency != currency.USD || rate.QuoteCurrency != currency.RUB {
		return nil, fmt.Errorf("FX source returned pair %s/%s, want USD/RUB", rate.BaseCurrency, rate.QuoteCurrency)
	}
	if !currency.ValidCode(rate.BaseCurrency) || !currency.ValidCode(rate.QuoteCurrency) {
		return nil, fmt.Errorf("FX source returned invalid currency pair %s/%s", rate.BaseCurrency, rate.QuoteCurrency)
	}
	value, ok := decimal.Parse(rate.Rate)
	if !ok || value.Sign() <= 0 {
		return nil, fmt.Errorf("FX source returned invalid USD/RUB rate %q", rate.Rate)
	}
	if rate.PricedAt.IsZero() {
		return nil, fmt.Errorf("FX source returned zero priced_at")
	}
	if rate.Source == "" {
		return nil, fmt.Errorf("FX source returned empty source")
	}

	result := &SyncResult{Source: rate.Source}
	fetchedAt := s.now().UTC()

	errUpdate := s.state.Update(func(current *appstate.State) (*appstate.State, error) {
		if current == nil {
			return nil, fmt.Errorf("application state is not initialized")
		}

		changed, stale, fxState, currentRate, errApply := s.repository.ApplyRate(ctx, *rate, fetchedAt)
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
