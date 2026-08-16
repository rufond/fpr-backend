package schedulerjobs

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

const JobYahooSourcesDiscovery = "yahoo_sources_discovery"

type yahooSourcesDiscoveryService interface {
	DiscoverYahooSources(ctx context.Context) (*prices.YahooSourceDiscoveryResult, error)
}

func YahooSourcesDiscovery(service yahooSourcesDiscoveryService) scheduler.JobFunc {
	return func(ctx context.Context, logger zerolog.Logger) (*scheduler.JobResult, error) {
		result, err := service.DiscoverYahooSources(ctx)
		if err != nil {
			return nil, err
		}

		for _, isin := range result.MissingISINs {
			logger.Warn().Str("isin", isin).Msg("Yahoo symbol not found")
		}

		summary := map[string]any{
			"candidate_instruments": result.CandidateInstruments,
			"existing_sources":      result.ExistingSources,
			"requested_isins":       result.RequestedISINs,
			"resolved_isins":        result.ResolvedISINs,
			"created_sources":       result.CreatedSources,
			"missing_isins":         len(result.MissingISINs),
		}

		if result.CreatedSources == 0 {
			logger.Debug().Interface("summary", summary).Msg("Yahoo source discovery noop")

			return scheduler.JobNoop(summary), nil
		}

		logger.Info().Interface("summary", summary).Msg("Yahoo source discovery completed")

		return scheduler.JobCompleted(summary), nil
	}
}
