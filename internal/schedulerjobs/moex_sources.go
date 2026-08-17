package schedulerjobs

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

const JobMOEXSourcesDiscovery = "moex_sources_discovery"

type moexSourcesDiscoveryService interface {
	DiscoverMOEXSources(ctx context.Context) (*prices.MOEXSourceDiscoveryResult, error)
}

func MOEXSourcesDiscovery(service moexSourcesDiscoveryService) scheduler.JobFunc {
	return func(ctx context.Context, logger zerolog.Logger) (*scheduler.JobResult, error) {
		result, err := service.DiscoverMOEXSources(ctx)
		if err != nil {
			return nil, err
		}

		for _, isin := range result.MissingISINs {
			logger.Debug().Str("isin", isin).Msg("MOEX symbol not found")
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
			logger.Debug().Interface("summary", summary).Msg("MOEX source discovery noop")

			return scheduler.JobNoop(summary), nil
		}

		logger.Info().Interface("summary", summary).Msg("MOEX source discovery completed")

		return scheduler.JobCompleted(summary), nil
	}
}
