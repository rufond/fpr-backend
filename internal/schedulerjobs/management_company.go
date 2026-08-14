package schedulerjobs

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/rufond/fpr-backend/internal/fund"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

const JobManagementCompanySync = "management_company_sync"

type managementCompanySyncService interface {
	SyncManagementCompany(ctx context.Context) (*fund.SyncResult, error)
}

func ManagementCompanySync(service managementCompanySyncService) scheduler.JobFunc {
	return func(ctx context.Context, logger zerolog.Logger) (*scheduler.JobResult, error) {
		result, err := service.SyncManagementCompany(ctx)
		if err != nil {
			return nil, err
		}

		for _, conflict := range result.HistoryConflicts {
			logger.Warn().
				Str("date", conflict.AsOfDate.Format("2006-01-02")).
				Str("stored_calculated_unit_value_usd", conflict.StoredCalculatedUnitValueUSD).
				Str("source_calculated_unit_value_usd", conflict.SourceCalculatedUnitValueUSD).
				Str("stored_nav_usd", conflict.StoredNAVUSD).
				Str("source_nav_usd", conflict.SourceNAVUSD).
				Msg("management company historical value differs from fixed history")
		}

		summary := map[string]any{
			"source_hash":       result.SourceHash,
			"history_inserted":  result.HistoryInserted,
			"history_updated":   result.HistoryUpdated,
			"history_conflicts": len(result.HistoryConflicts),
			"snapshot_created":  result.SnapshotCreated,
		}

		if result.HistoryInserted == 0 &&
			result.HistoryUpdated == 0 &&
			len(result.HistoryConflicts) == 0 &&
			!result.SnapshotCreated {
			logger.Debug().Interface("summary", summary).Msg("management company sync noop")
			return scheduler.JobNoop(summary), nil
		}

		logger.Info().Interface("summary", summary).Msg("management company sync completed")

		return scheduler.JobCompleted(summary), nil
	}
}
