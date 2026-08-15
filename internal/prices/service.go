package prices

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
)

type serviceRepository interface {
	EnsureFundUnitMOEXSource(ctx context.Context) error
	LoadState(ctx context.Context) (*appstate.PriceState, error)
	ApplyFundUnitMOEXQuote(
		ctx context.Context,
		quote SourceQuote,
		fetchedAt time.Time,
	) (changed bool, stale bool, state *appstate.PriceState, price appstate.InstrumentPrice, err error)
}

type Service struct {
	repository serviceRepository
	source     QuoteSource
	state      *appstate.Manager
	now        func() time.Time
}

func NewService(repository serviceRepository, source QuoteSource, state *appstate.Manager) *Service {
	return &Service{
		repository: repository,
		source:     source,
		state:      state,
		now:        time.Now,
	}
}

func (s *Service) Start(ctx context.Context) error {
	if s.repository == nil {
		return fmt.Errorf("price repository is not configured")
	}
	if s.state == nil {
		return fmt.Errorf("application state manager is not configured")
	}

	if err := s.repository.EnsureFundUnitMOEXSource(ctx); err != nil {
		return err
	}

	priceState, err := s.repository.LoadState(ctx)
	if err != nil {
		return err
	}

	return s.state.Update(func(current *appstate.State) (*appstate.State, error) {
		if current == nil {
			return nil, fmt.Errorf("application state is not initialized")
		}

		next := &appstate.State{}
		*next = *current
		next.Prices = priceState
		return next, nil
	})
}

func (s *Service) SyncFundUnitMOEX(ctx context.Context) (*SyncResult, error) {
	if s.repository == nil {
		return nil, fmt.Errorf("price repository is not configured")
	}
	if s.source == nil {
		return nil, fmt.Errorf("MOEX source is not configured")
	}
	if s.state == nil {
		return nil, fmt.Errorf("application state manager is not configured")
	}

	quote, errFetch := s.source.FetchFundUnitQuote(ctx)
	if errFetch != nil {
		return nil, fmt.Errorf("fetch MOEX fund unit quote: %w", errFetch)
	}
	if quote == nil {
		return nil, fmt.Errorf("MOEX fund unit quote is nil")
	}
	if errValidate := validateFundUnitMOEXQuote(*quote); errValidate != nil {
		return nil, errValidate
	}

	result := &SyncResult{Source: quote.Source}
	fetchedAt := s.now().UTC()

	errUpdate := s.state.Update(func(current *appstate.State) (*appstate.State, error) {
		if current == nil {
			return nil, fmt.Errorf("application state is not initialized")
		}

		changed, stale, priceState, price, errApply := s.repository.ApplyFundUnitMOEXQuote(ctx, *quote, fetchedAt)
		if errApply != nil {
			return nil, errApply
		}

		result.Changed = changed
		result.Stale = stale
		result.Price = price

		if !changed {
			return current, nil
		}
		if priceState == nil {
			return nil, fmt.Errorf("MOEX quote update returned no price state")
		}

		next := &appstate.State{}
		*next = *current
		next.Prices = priceState
		return next, nil
	})
	if errUpdate != nil {
		return nil, errUpdate
	}

	return result, nil
}

func validateFundUnitMOEXQuote(quote SourceQuote) error {
	value, ok := new(big.Rat).SetString(strings.TrimSpace(quote.UnitValue))
	if !ok || value.Sign() <= 0 {
		return fmt.Errorf("MOEX fund unit quote has invalid unit value %q", quote.UnitValue)
	}
	currency := strings.TrimSpace(quote.Currency)
	if len(currency) != 3 {
		return fmt.Errorf("MOEX fund unit quote has invalid currency %q", quote.Currency)
	}
	for _, char := range currency {
		if char < 'A' || char > 'Z' {
			return fmt.Errorf("MOEX fund unit quote has invalid currency %q", quote.Currency)
		}
	}
	if quote.PricedAt.IsZero() {
		return fmt.Errorf("MOEX fund unit quote has zero priced_at")
	}
	if strings.TrimSpace(quote.Source) == "" {
		return fmt.Errorf("MOEX fund unit quote has empty source")
	}

	return nil
}
