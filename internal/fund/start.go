package fund

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

func (m *Module) Start(ctx context.Context) error {
	result, err := m.Service.EnsureInitialized(ctx)
	if err != nil {
		return fmt.Errorf("initialize fund data: %w", err)
	}

	if result == nil {
		return nil
	}

	for _, conflict := range result.HistoryConflicts {
		log.Warn().
			Str("date", conflict.AsOfDate.Format(time.DateOnly)).
			Str("stored_calculated_unit_value_usd", conflict.StoredCalculatedUnitValueUSD).
			Str("source_calculated_unit_value_usd", conflict.SourceCalculatedUnitValueUSD).
			Str("stored_nav_usd", conflict.StoredNAVUSD).
			Str("source_nav_usd", conflict.SourceNAVUSD).
			Msg("management company historical value differs from fixed history")
	}

	log.Info().
		Int("history_inserted", result.HistoryInserted).
		Int("history_updated", result.HistoryUpdated).
		Int("history_conflicts", len(result.HistoryConflicts)).
		Bool("snapshot_created", result.SnapshotCreated).
		Str("source_hash", result.SourceHash).
		Msg("initial management company sync completed")

	return nil
}
