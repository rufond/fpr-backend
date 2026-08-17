package schedulerjobs

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/realtime"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

const JobMOEXSecurityPricesSync = "moex_security_prices_sync"

type moexSecurityPricesSyncService interface {
	SyncMOEXSecurityPrices(ctx context.Context) (*prices.MOEXSecuritySyncResult, error)
}

func MOEXSecurityPricesSync(service moexSecurityPricesSyncService, publisher realtime.Publisher) scheduler.JobFunc {
	if publisher == nil {
		publisher = realtime.DiscardPublisher{}
	}

	return func(ctx context.Context, logger zerolog.Logger) (*scheduler.JobResult, error) {
		result, err := service.SyncMOEXSecurityPrices(ctx)
		if err != nil {
			return nil, err
		}

		for _, issue := range result.Issues {
			logger.Warn().Str("symbol", issue.Symbol).Str("error", issue.Error).Msg("MOEX security quote skipped")
		}

		if result.CompositionSkippedSources != 0 {
			logger.Info().Int("sources", result.CompositionSkippedSources).Msg("MOEX security quotes skipped after composition changed during fetch")
		}

		summary := map[string]any{
			"expected_sources":            result.ExpectedSources,
			"requested_symbols":           result.RequestedSymbols,
			"changed_sources":             len(result.ChangedPrices),
			"unchanged_sources":           result.UnchangedSources,
			"stale_sources":               result.StaleSources,
			"failed_sources":              result.FailedSources,
			"composition_skipped_sources": result.CompositionSkippedSources,
		}

		if !result.Changed() {
			logger.Debug().Interface("summary", summary).Msg("MOEX security prices sync noop")
			return scheduler.JobNoop(summary), nil
		}

		deltas := make([]realtime.InstrumentPriceDelta, 0, len(result.ChangedPrices))
		for _, price := range result.ChangedPrices {
			deltas = append(deltas, realtime.InstrumentPriceDelta{
				InstrumentID: price.InstrumentID,
				UnitValue:    price.UnitValue,
				Currency:     price.Currency,
				PricedAt:     price.PricedAt,
			})
		}

		publisher.Publish(realtime.Update{
			Scopes:           []string{realtime.ScopeInstrumentPrices},
			InstrumentPrices: deltas,
		})

		logger.Info().Interface("summary", summary).Msg("MOEX security prices sync completed")

		return scheduler.JobCompleted(summary), nil
	}
}
