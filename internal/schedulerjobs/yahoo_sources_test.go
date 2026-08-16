package schedulerjobs

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"

	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

type fakeYahooSourcesDiscoveryService struct {
	result *prices.YahooSourceDiscoveryResult
	err    error
}

func (s fakeYahooSourcesDiscoveryService) DiscoverYahooSources(context.Context) (*prices.YahooSourceDiscoveryResult, error) {
	return s.result, s.err
}

func TestYahooSourcesDiscoveryCompletedWhenMappingsCreated(t *testing.T) {
	t.Parallel()

	job := YahooSourcesDiscovery(fakeYahooSourcesDiscoveryService{result: &prices.YahooSourceDiscoveryResult{
		CandidateInstruments: 3,
		ExistingSources:      1,
		RequestedISINs:       2,
		ResolvedISINs:        2,
		CreatedSources:       2,
	}})

	result, err := job(context.Background(), zerolog.Nop())
	if err != nil {
		t.Fatalf("job() error = %v", err)
	}
	if result == nil || result.Status != scheduler.RunStatusCompleted {
		t.Fatalf("result = %#v", result)
	}
}

func TestYahooSourcesDiscoveryNoopWhenNothingCreated(t *testing.T) {
	t.Parallel()

	job := YahooSourcesDiscovery(fakeYahooSourcesDiscoveryService{result: &prices.YahooSourceDiscoveryResult{
		CandidateInstruments: 2,
		RequestedISINs:       2,
		MissingISINs:         []string{"KZ1C00001122", "KZ0000000001"},
	}})

	result, err := job(context.Background(), zerolog.Nop())
	if err != nil {
		t.Fatalf("job() error = %v", err)
	}
	if result == nil || result.Status != scheduler.RunStatusNoop {
		t.Fatalf("result = %#v", result)
	}
}

func TestYahooSourcesDiscoveryReturnsServiceError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("Yahoo unavailable")
	job := YahooSourcesDiscovery(fakeYahooSourcesDiscoveryService{err: wantErr})

	if _, err := job(context.Background(), zerolog.Nop()); !errors.Is(err, wantErr) {
		t.Fatalf("job() error = %v, want %v", err, wantErr)
	}
}
