package schedulerjobs

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/realtime"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

const JobMOEXSecurityPricesSync = "moex_security_prices_sync"
const JobKASESecurityPricesSync = "kase_security_prices_sync"
const JobNasdaqSecurityPricesSync = "nasdaq_security_prices_sync"

type moexSecurityPricesSyncService interface {
	SyncMOEXSecurityPrices(ctx context.Context) (*prices.MOEXSecuritySyncResult, error)
}

func MOEXSecurityPricesSync(service moexSecurityPricesSyncService, valuation liveValuationRefresher, publisher realtime.Publisher) scheduler.JobFunc {
	return SecurityPricesSync("MOEX", service.SyncMOEXSecurityPrices, valuation, publisher)
}

func SecurityPricesSync(provider string, sync func(context.Context) (*prices.MOEXSecuritySyncResult, error), valuation liveValuationRefresher, publisher realtime.Publisher) scheduler.JobFunc {
	if publisher == nil {
		publisher = realtime.DiscardPublisher{}
	}

	return func(ctx context.Context, logger zerolog.Logger) (*scheduler.JobResult, error) {
		result, err := sync(ctx)
		if err != nil {
			return nil, err
		}

		for _, issue := range result.Issues {
			logger.Warn().Str("provider", provider).Str("symbol", issue.Symbol).Str("error", issue.Error).Msg("Security quote skipped")
		}

		if result.CompositionSkippedSources != 0 {
			logger.Info().Int("sources", result.CompositionSkippedSources).Str("provider", provider).Msg("Security quotes skipped after composition changed during fetch")
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
			logger.Debug().Interface("summary", summary).Str("provider", provider).Msg("Security prices sync noop")
			return scheduler.JobNoop(summary), nil
		}

		deltas := make([]realtime.InstrumentPriceDelta, 0, len(result.ChangedPrices))
		for _, price := range result.ChangedPrices {
			deltas = append(deltas, realtime.InstrumentPriceDelta{
				InstrumentID:  price.InstrumentID,
				PositionValue: result.PositionValues[price.InstrumentID],
				Provider:      price.Provider,
				Symbol:        price.ProviderSymbol,
				UnitValue:     price.UnitValue,
				Currency:      price.Currency,
				PricedAt:      price.PricedAt,
			})
		}

		publisher.Publish(realtime.Update{
			Scopes:           []string{realtime.ScopeInstrumentPrices},
			InstrumentPrices: deltas,
		})

		liveValuationChanged, errValuation := refreshLiveValuation(ctx, valuation, publisher)
		summary["live_valuation_changed"] = liveValuationChanged
		if errValuation != nil {
			summary["live_valuation_error"] = errValuation.Error()
			logger.Error().Err(errValuation).Str("provider", provider).Msg("Refresh live valuation after security prices sync")
		} else {
			summary["live_valuation_error"] = ""
		}

		logger.Debug().Interface("summary", summary).Str("provider", provider).Msg("Security prices sync completed")

		return scheduler.JobCompleted(summary), nil
	}
}
