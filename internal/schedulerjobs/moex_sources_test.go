package schedulerjobs

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"

	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

type fakeMOEXSourcesDiscoveryService struct {
	result *prices.MOEXSourceDiscoveryResult
	err    error
}

func (s fakeMOEXSourcesDiscoveryService) DiscoverMOEXSources(context.Context) (*prices.MOEXSourceDiscoveryResult, error) {
	return s.result, s.err
}

func TestMOEXSourcesDiscoveryCompletedWhenMappingsCreated(t *testing.T) {
	t.Parallel()

	job := MOEXSourcesDiscovery(fakeMOEXSourcesDiscoveryService{result: &prices.MOEXSourceDiscoveryResult{
		CandidateInstruments: 3,
		RequestedISINs:       3,
		ResolvedISINs:        1,
		CreatedSources:       1,
	}})

	result, err := job(context.Background(), zerolog.Nop())
	if err != nil {
		t.Fatalf("job() error = %v", err)
	}
	if result == nil || result.Status != scheduler.RunStatusCompleted {
		t.Fatalf("result = %#v", result)
	}
}

func TestMOEXSourcesDiscoveryNoopWhenNothingCreated(t *testing.T) {
	t.Parallel()

	job := MOEXSourcesDiscovery(fakeMOEXSourcesDiscoveryService{result: &prices.MOEXSourceDiscoveryResult{
		CandidateInstruments: 2,
		RequestedISINs:       2,
		MissingISINs:         []string{"KZ1C00001122", "US00449L1026"},
	}})

	result, err := job(context.Background(), zerolog.Nop())
	if err != nil {
		t.Fatalf("job() error = %v", err)
	}
	if result == nil || result.Status != scheduler.RunStatusNoop {
		t.Fatalf("result = %#v", result)
	}
}

func TestMOEXSourcesDiscoveryReturnsServiceError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("MOEX unavailable")
	job := MOEXSourcesDiscovery(fakeMOEXSourcesDiscoveryService{err: wantErr})

	if _, err := job(context.Background(), zerolog.Nop()); !errors.Is(err, wantErr) {
		t.Fatalf("job() error = %v, want %v", err, wantErr)
	}
}
