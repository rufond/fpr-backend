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

func MOEXFundUnitSync(service moexFundUnitSyncService, publisher realtime.Publisher) scheduler.JobFunc {
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

		logger.Debug().Interface("summary", summary).Msg("MOEX fund unit sync completed")
		return scheduler.JobCompleted(summary), nil
	}
}
