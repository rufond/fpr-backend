package schedulerjobs

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/realtime"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

const JobYahooPricesSync = "yahoo_prices_sync"

type yahooPricesSyncService interface {
	SyncYahooPrices(ctx context.Context) (*prices.YahooSyncResult, error)
}

func YahooPricesSync(service yahooPricesSyncService, valuation liveValuationRefresher, publisher realtime.Publisher) scheduler.JobFunc {
	if publisher == nil {
		publisher = realtime.DiscardPublisher{}
	}

	return func(ctx context.Context, logger zerolog.Logger) (*scheduler.JobResult, error) {
		result, err := service.SyncYahooPrices(ctx)
		if err != nil {
			return nil, err
		}

		for _, symbol := range result.MissingSymbols {
			logger.Warn().Str("symbol", symbol).Msg("Yahoo symbol missing from response")
		}
		for _, symbol := range result.UnexpectedSymbols {
			logger.Warn().Str("symbol", symbol).Msg("Yahoo returned unexpected symbol")
		}
		for _, symbol := range result.DuplicateSymbols {
			logger.Warn().Str("symbol", symbol).Msg("Yahoo returned duplicate symbol")
		}
		for _, issue := range result.InvalidQuotes {
			logger.Warn().Str("symbol", issue.Symbol).Str("error", issue.Error).Msg("Yahoo quote skipped")
		}

		if result.CompositionSkippedSources != 0 {
			logger.Info().Int("sources", result.CompositionSkippedSources).Msg("Yahoo quotes skipped after composition changed during fetch")
		}

		summary := map[string]any{
			"expected_sources":            result.ExpectedSources,
			"requested_symbols":           result.RequestedSymbols,
			"returned_symbols":            result.ReturnedSymbols,
			"batches":                     result.Batches,
			"changed_sources":             len(result.ChangedPrices),
			"unchanged_sources":           result.UnchangedSources,
			"stale_sources":               result.StaleSources,
			"missing_sources":             result.MissingSources,
			"invalid_sources":             result.InvalidSources,
			"composition_skipped_sources": result.CompositionSkippedSources,
			"missing_symbols":             len(result.MissingSymbols),
			"unexpected_symbols":          len(result.UnexpectedSymbols),
			"duplicate_symbols":           len(result.DuplicateSymbols),
		}

		if !result.Changed() {
			logger.Debug().Interface("summary", summary).Msg("Yahoo prices sync noop")
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

		liveValuationChanged, errValuation := refreshLiveValuation(ctx, valuation, publisher)
		summary["live_valuation_changed"] = liveValuationChanged
		if errValuation != nil {
			summary["live_valuation_error"] = errValuation.Error()
			logger.Error().Err(errValuation).Msg("refresh live valuation after Yahoo prices sync")
		} else {
			summary["live_valuation_error"] = ""
		}

		logger.Debug().Interface("summary", summary).Msg("Yahoo prices sync completed")

		return scheduler.JobCompleted(summary), nil
	}
}
