package schedulerjobs

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/rufond/fpr-backend/internal/fx"
	"github.com/rufond/fpr-backend/internal/realtime"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

const JobMOEXUSDRUBSync = "moex_usd_rub_sync"

type moexUSDRUBSyncService interface {
	SyncUSDRUB(ctx context.Context) (*fx.SyncResult, error)
}

func MOEXUSDRUBSync(service moexUSDRUBSyncService, valuation liveValuationRefresher, publisher realtime.Publisher) scheduler.JobFunc {
	if publisher == nil {
		publisher = realtime.DiscardPublisher{}
	}

	return func(ctx context.Context, logger zerolog.Logger) (*scheduler.JobResult, error) {
		result, err := service.SyncUSDRUB(ctx)
		if err != nil {
			return nil, err
		}

		summary := map[string]any{
			"changed":   result.Changed,
			"stale":     result.Stale,
			"source":    result.Source,
			"rate":      result.Rate.Rate,
			"priced_at": result.Rate.PricedAt,
		}

		if !result.Changed {
			logger.Debug().Interface("summary", summary).Msg("MOEX USD/RUB sync noop")
			return scheduler.JobNoop(summary), nil
		}

		publisher.Publish(realtime.Update{
			Scopes: []string{realtime.ScopeFXRates},
			FXRates: []realtime.FXRateDelta{
				{
					BaseCurrency:  result.Rate.BaseCurrency,
					QuoteCurrency: result.Rate.QuoteCurrency,
					Rate:          result.Rate.Rate,
					PricedAt:      result.Rate.PricedAt,
				},
			},
		})

		liveValuationChanged, errValuation := refreshLiveValuation(ctx, valuation, publisher)
		summary["live_valuation_changed"] = liveValuationChanged
		if errValuation != nil {
			summary["live_valuation_error"] = errValuation.Error()
			logger.Error().Err(errValuation).Msg("refresh live valuation after MOEX USD/RUB sync")
		} else {
			summary["live_valuation_error"] = ""
		}

		logger.Debug().Interface("summary", summary).Msg("MOEX USD/RUB sync completed")
		return scheduler.JobCompleted(summary), nil
	}
}
