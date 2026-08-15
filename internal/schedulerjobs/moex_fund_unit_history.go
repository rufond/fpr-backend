package schedulerjobs

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/realtime"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

const JobMOEXFundUnitHistorySync = "moex_fund_unit_history_sync"

type moexFundUnitHistorySyncService interface {
	SyncFundUnitMOEXHistory(ctx context.Context) (*prices.DailySyncResult, error)
}

func MOEXFundUnitHistorySync(service moexFundUnitHistorySyncService, publisher realtime.Publisher) scheduler.JobFunc {
	if publisher == nil {
		publisher = realtime.DiscardPublisher{}
	}

	return func(ctx context.Context, logger zerolog.Logger) (*scheduler.JobResult, error) {
		result, err := service.SyncFundUnitMOEXHistory(ctx)
		if err != nil {
			return nil, err
		}

		summary := map[string]any{
			"inserted": result.Inserted,
			"updated":  result.Updated,
			"from":     result.FromDate,
			"to":       result.ToDate,
		}

		if !result.Changed() {
			logger.Debug().Interface("summary", summary).Msg("MOEX fund unit history sync noop")
			return scheduler.JobNoop(summary), nil
		}

		publisher.Publish(realtime.Update{Scopes: []string{realtime.ScopeFundHistory}})

		logger.Info().Interface("summary", summary).Msg("MOEX fund unit history sync completed")
		return scheduler.JobCompleted(summary), nil
	}
}
