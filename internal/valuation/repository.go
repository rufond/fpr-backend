package valuation

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xloss/go-builder"
)

type Repository struct {
	db *pgxpool.Pool
}

type storedBaseline struct {
	AssetID        int64     `db:"asset_id"`
	PriceSourceID  int64     `db:"price_source_id"`
	Provider       string    `db:"provider"`
	ProviderSymbol string    `db:"provider_symbol"`
	UnitValue      string    `db:"unit_value"`
	Currency       string    `db:"currency"`
	PricedAt       time.Time `db:"priced_at"`
	FXRateToUSD    string    `db:"fx_rate_to_usd"`
	FXProvider     string    `db:"fx_provider"`
	FXPricedAt     time.Time `db:"fx_priced_at"`
	MarketValueUSD string    `db:"market_value_usd"`
}

type storedValuePoint struct {
	SnapshotID                      int64     `db:"snapshot_id"`
	ObservedAt                      time.Time `db:"observed_at"`
	EstimatedNAVUSD                 string    `db:"estimated_nav_usd"`
	EstimatedCalculatedUnitValueUSD string    `db:"estimated_calculated_unit_value_usd"`
	LiveDeltaUSD                    string    `db:"live_delta_usd"`
	LiveCoveragePercent             string    `db:"live_coverage_percent"`
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) LoadBaselines(ctx context.Context, snapshotID int64) (map[int64]baseline, error) {
	baselines := builder.NewTable("fund_snapshot_price_baselines")
	assets := builder.NewTable("fund_snapshot_assets")
	sources := builder.NewTable("instrument_price_sources")

	query := builder.NewSelect()
	query.From(baselines)
	query.LeftJoin(assets, builder.OnEq{Table1: baselines, Column1: "asset_id", Table2: assets, Column2: "id"})
	query.LeftJoin(sources, builder.OnEq{Table1: baselines, Column1: "price_source_id", Table2: sources, Column2: "id"})
	query.Column(
		builder.ColumnName{Table: baselines, Name: "asset_id"},
		builder.ColumnName{Table: baselines, Name: "price_source_id"},
		builder.ColumnName{Table: sources, Name: "provider"},
		builder.ColumnName{Table: sources, Name: "provider_symbol"},
		builder.ColumnName{Table: baselines, Name: "unit_value"},
		builder.ColumnName{Table: baselines, Name: "currency"},
		builder.ColumnName{Table: baselines, Name: "priced_at"},
		builder.ColumnName{Table: baselines, Name: "fx_rate_to_usd"},
		builder.ColumnName{Table: baselines, Name: "fx_provider"},
		builder.ColumnName{Table: baselines, Name: "fx_priced_at"},
		builder.ColumnName{Table: baselines, Name: "market_value_usd"},
	)
	query.Where(builder.WhereEq{Table: assets, Column: "snapshot_id", Value: snapshotID})
	query.Order(builder.Order{Table: baselines, Column: "asset_id"})

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build fund baseline query: %w", errBuild)
	}

	rows, errQuery := r.db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return nil, fmt.Errorf("query fund baselines: %w", errQuery)
	}
	defer rows.Close()

	stored, errCollect := pgx.CollectRows(rows, pgx.RowToStructByNameLax[storedBaseline])
	if errCollect != nil {
		return nil, fmt.Errorf("collect fund baselines: %w", errCollect)
	}

	result := make(map[int64]baseline, len(stored))
	for _, item := range stored {
		result[item.AssetID] = baseline{
			AssetID:        item.AssetID,
			PriceSourceID:  item.PriceSourceID,
			Provider:       item.Provider,
			ProviderSymbol: item.ProviderSymbol,
			UnitValue:      item.UnitValue,
			Currency:       item.Currency,
			PricedAt:       item.PricedAt.UTC(),
			FXRateToUSD:    item.FXRateToUSD,
			FXProvider:     item.FXProvider,
			FXPricedAt:     item.FXPricedAt.UTC(),
			MarketValueUSD: item.MarketValueUSD,
		}
	}

	return result, nil
}

func (r *Repository) LoadValuePoints(ctx context.Context, snapshotID int64, cutoff time.Time) ([]valuePoint, error) {
	table := builder.NewTable("fund_value_points")
	query := builder.NewSelect()
	query.From(table)
	query.Column(
		builder.ColumnName{Table: table, Name: "snapshot_id"},
		builder.ColumnName{Table: table, Name: "observed_at"},
		builder.ColumnName{Table: table, Name: "estimated_nav_usd"},
		builder.ColumnName{Table: table, Name: "estimated_calculated_unit_value_usd"},
		builder.ColumnName{Table: table, Name: "live_delta_usd"},
		builder.ColumnName{Table: table, Name: "live_coverage_percent"},
	)
	query.Where(builder.WhereAnd{List: []builder.Where{
		builder.WhereEq{Table: table, Column: "snapshot_id", Value: snapshotID},
		builder.WhereMoreEq{Table: table, Column: "observed_at", Value: cutoff.UTC()},
	}})
	query.Order(builder.Order{Table: table, Column: "observed_at"})

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build fund value points query: %w", errBuild)
	}

	rows, errQuery := r.db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return nil, fmt.Errorf("query fund value points: %w", errQuery)
	}
	defer rows.Close()

	stored, errCollect := pgx.CollectRows(rows, pgx.RowToStructByNameLax[storedValuePoint])
	if errCollect != nil {
		return nil, fmt.Errorf("collect fund value points: %w", errCollect)
	}

	result := make([]valuePoint, 0, len(stored))
	for _, item := range stored {
		result = append(result, valuePoint{
			SnapshotID:                      item.SnapshotID,
			ObservedAt:                      item.ObservedAt.UTC(),
			EstimatedNAVUSD:                 item.EstimatedNAVUSD,
			EstimatedCalculatedUnitValueUSD: item.EstimatedCalculatedUnitValueUSD,
			LiveDeltaUSD:                    item.LiveDeltaUSD,
			LiveCoveragePercent:             item.LiveCoveragePercent,
		})
	}

	return result, nil
}

func (r *Repository) ApplyRefresh(ctx context.Context, baselineChanges []baseline, point valuePoint, cutoff time.Time) error {
	tx, errBegin := r.db.Begin(ctx)
	if errBegin != nil {
		return fmt.Errorf("begin live valuation transaction: %w", errBegin)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	baselineTable := builder.NewTable("fund_snapshot_price_baselines")
	for _, item := range baselineChanges {
		update := builder.NewUpdate(baselineTable)
		update.Set("price_source_id", item.PriceSourceID)
		update.Set("unit_value", item.UnitValue)
		update.Set("currency", item.Currency)
		update.Set("priced_at", item.PricedAt.UTC())
		update.Set("fx_rate_to_usd", item.FXRateToUSD)
		update.Set("fx_provider", item.FXProvider)
		update.Set("fx_priced_at", item.FXPricedAt.UTC())
		update.Set("market_value_usd", item.MarketValueUSD)
		update.Set("created_at", point.ObservedAt.UTC())
		update.Where(builder.WhereEq{Table: baselineTable, Column: "asset_id", Value: item.AssetID})

		sqlUpdate, bindsUpdate, errBuildUpdate := update.Get()
		if errBuildUpdate != nil {
			return fmt.Errorf("build fund baseline update: %w", errBuildUpdate)
		}
		command, errExecUpdate := tx.Exec(ctx, sqlUpdate, pgx.NamedArgs(bindsUpdate))
		if errExecUpdate != nil {
			return fmt.Errorf("update fund baseline: %w", errExecUpdate)
		}
		if command.RowsAffected() != 0 {
			continue
		}

		insert := builder.NewInsert(baselineTable)
		insert.Value("asset_id", item.AssetID)
		insert.Value("price_source_id", item.PriceSourceID)
		insert.Value("unit_value", item.UnitValue)
		insert.Value("currency", item.Currency)
		insert.Value("priced_at", item.PricedAt.UTC())
		insert.Value("fx_rate_to_usd", item.FXRateToUSD)
		insert.Value("fx_provider", item.FXProvider)
		insert.Value("fx_priced_at", item.FXPricedAt.UTC())
		insert.Value("market_value_usd", item.MarketValueUSD)
		insert.Value("created_at", point.ObservedAt.UTC())

		sqlInsert, bindsInsert, errBuildInsert := insert.Get()
		if errBuildInsert != nil {
			return fmt.Errorf("build fund baseline insert: %w", errBuildInsert)
		}
		if _, errExec := tx.Exec(ctx, sqlInsert, pgx.NamedArgs(bindsInsert)); errExec != nil {
			return fmt.Errorf("insert fund baseline: %w", errExec)
		}
	}

	pointTable := builder.NewTable("fund_value_points")
	insertPoint := builder.NewInsert(pointTable)
	insertPoint.Value("snapshot_id", point.SnapshotID)
	insertPoint.Value("observed_at", point.ObservedAt.UTC())
	insertPoint.Value("estimated_nav_usd", point.EstimatedNAVUSD)
	insertPoint.Value("estimated_calculated_unit_value_usd", point.EstimatedCalculatedUnitValueUSD)
	insertPoint.Value("live_delta_usd", point.LiveDeltaUSD)
	insertPoint.Value("live_coverage_percent", point.LiveCoveragePercent)

	sqlPoint, bindsPoint, errBuildPoint := insertPoint.Get()
	if errBuildPoint != nil {
		return fmt.Errorf("build fund value point insert: %w", errBuildPoint)
	}
	if _, errExec := tx.Exec(ctx, sqlPoint, pgx.NamedArgs(bindsPoint)); errExec != nil {
		return fmt.Errorf("insert fund value point: %w", errExec)
	}

	deleteOld := builder.NewDelete(pointTable)
	deleteOld.Where(builder.WhereLess{Table: pointTable, Column: "observed_at", Value: cutoff.UTC()})

	sqlDelete, bindsDelete, errBuildDelete := deleteOld.Get()
	if errBuildDelete != nil {
		return fmt.Errorf("build old fund value points delete: %w", errBuildDelete)
	}
	if _, errExec := tx.Exec(ctx, sqlDelete, pgx.NamedArgs(bindsDelete)); errExec != nil {
		return fmt.Errorf("delete old fund value points: %w", errExec)
	}

	if errCommit := tx.Commit(ctx); errCommit != nil {
		return fmt.Errorf("commit live valuation transaction: %w", errCommit)
	}

	return nil
}
