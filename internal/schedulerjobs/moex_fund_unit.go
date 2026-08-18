package schedulerjobs

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/realtime"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

const JobMOEXFundUnitSync = "moex_fund_unit_sync"

type moexFundUnitSyncService interface {
	SyncFundUnitMOEX(ctx context.Context) (*prices.SyncResult, error)
}

func MOEXFundUnitSync(service moexFundUnitSyncService, valuation liveValuationRefresher, publisher realtime.Publisher) scheduler.JobFunc {
	if publisher == nil {
		publisher = realtime.DiscardPublisher{}
	}

	return func(ctx context.Context, logger zerolog.Logger) (*scheduler.JobResult, error) {
		result, err := service.SyncFundUnitMOEX(ctx)
		if err != nil {
			return nil, err
		}

		summary := map[string]any{
			"changed":    result.Changed,
			"stale":      result.Stale,
			"source":     result.Source,
			"unit_value": result.Price.UnitValue,
			"currency":   result.Price.Currency,
			"priced_at":  result.Price.PricedAt,
		}

		if !result.Changed {
			logger.Debug().Interface("summary", summary).Msg("MOEX fund unit sync noop")
			return scheduler.JobNoop(summary), nil
		}

		publisher.Publish(realtime.Update{
			Scopes: []string{realtime.ScopeInstrumentPrices},
			InstrumentPrices: []realtime.InstrumentPriceDelta{
				{
					InstrumentID: result.Price.InstrumentID,
					UnitValue:    result.Price.UnitValue,
					Currency:     result.Price.Currency,
					PricedAt:     result.Price.PricedAt,
				},
			},
		})

		liveValuationChanged, errValuation := refreshLiveValuation(ctx, valuation, publisher)
		summary["live_valuation_changed"] = liveValuationChanged
		if errValuation != nil {
			summary["live_valuation_error"] = errValuation.Error()
			logger.Error().Err(errValuation).Msg("refresh live valuation after MOEX fund unit sync")
		} else {
			summary["live_valuation_error"] = ""
		}

		logger.Debug().Interface("summary", summary).Msg("MOEX fund unit sync completed")
		return scheduler.JobCompleted(summary), nil
	}
}
