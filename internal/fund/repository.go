package fund

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xloss/go-builder"
)

type StoredDailyValue struct {
	AsOfDate               time.Time `db:"as_of_date"`
	CalculatedUnitValueUSD string    `db:"calculated_unit_value_usd"`
	NAVUSD                 string    `db:"nav_usd"`
}

type DailyValueChanges struct {
	Insert []DailyValue
	Update []DailyValue
}

type Repository struct {
	db *pgxpool.Pool
}

type storedInstrumentIdentity struct {
	ID        int64  `db:"id"`
	AssetType string `db:"asset_type"`
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) HasSnapshots(ctx context.Context) (bool, error) {
	table := builder.NewTable("fund_snapshots")
	query := builder.NewSelect()
	query.From(table)
	query.Column(builder.ColumnName{Table: table, Name: "id"})
	query.Limit(1)

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return false, fmt.Errorf("build fund snapshots existence query: %w", errBuild)
	}

	rows, errQuery := r.db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return false, fmt.Errorf("query fund snapshots existence: %w", errQuery)
	}
	defer rows.Close()

	_, errCollect := pgx.CollectOneRow(rows, pgx.RowTo[int64])
	if errCollect != nil {
		if errors.Is(errCollect, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("collect fund snapshots existence: %w", errCollect)
	}

	return true, nil
}

func (r *Repository) LoadDailyValues(ctx context.Context) (map[string]StoredDailyValue, error) {
	table := builder.NewTable("fund_daily_values")
	query := builder.NewSelect()
	query.From(table)
	query.Column(
		builder.ColumnName{Table: table, Name: "as_of_date"},
		builder.ColumnName{Table: table, Name: "calculated_unit_value_usd"},
		builder.ColumnName{Table: table, Name: "nav_usd"},
	)
	query.Order(builder.Order{Table: table, Column: "as_of_date"})

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build fund daily values query: %w", errBuild)
	}

	rows, errQuery := r.db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return nil, fmt.Errorf("query fund daily values: %w", errQuery)
	}
	defer rows.Close()

	items, errCollect := pgx.CollectRows(rows, pgx.RowToStructByNameLax[StoredDailyValue])
	if errCollect != nil {
		return nil, fmt.Errorf("collect fund daily values: %w", errCollect)
	}

	result := make(map[string]StoredDailyValue, len(items))
	for _, item := range items {
		item.AsOfDate = dateOnlyUTC(item.AsOfDate)
		result[item.AsOfDate.Format(time.DateOnly)] = item
	}

	return result, nil
}

func (r *Repository) ApplyDailyValueChanges(ctx context.Context, changes DailyValueChanges) error {
	if len(changes.Insert) == 0 && len(changes.Update) == 0 {
		return nil
	}

	tx, errBegin := r.db.Begin(ctx)
	if errBegin != nil {
		return fmt.Errorf("begin fund daily values transaction: %w", errBegin)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	table := builder.NewTable("fund_daily_values")

	for _, item := range changes.Insert {
		query := builder.NewInsert(table)
		query.Value("as_of_date", dateOnlyUTC(item.AsOfDate))
		query.Value("calculated_unit_value_usd", item.CalculatedUnitValueUSD)
		query.Value("nav_usd", item.NAVUSD)

		sql, binds, errBuild := query.Get()
		if errBuild != nil {
			return fmt.Errorf("build insert fund daily value %s: %w", item.AsOfDate.Format(time.DateOnly), errBuild)
		}
		if _, errExec := tx.Exec(ctx, sql, pgx.NamedArgs(binds)); errExec != nil {
			return fmt.Errorf("insert fund daily value %s: %w", item.AsOfDate.Format(time.DateOnly), errExec)
		}
	}

	for _, item := range changes.Update {
		query := builder.NewUpdate(table)
		query.Set("calculated_unit_value_usd", item.CalculatedUnitValueUSD)
		query.Set("nav_usd", item.NAVUSD)
		query.SetNow("updated_at")
		query.Where(builder.WhereEq{Table: table, Column: "as_of_date", Value: dateOnlyUTC(item.AsOfDate)})

		sql, binds, errBuild := query.Get()
		if errBuild != nil {
			return fmt.Errorf("build update fund daily value %s: %w", item.AsOfDate.Format(time.DateOnly), errBuild)
		}
		tag, errExec := tx.Exec(ctx, sql, pgx.NamedArgs(binds))
		if errExec != nil {
			return fmt.Errorf("update fund daily value %s: %w", item.AsOfDate.Format(time.DateOnly), errExec)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("update fund daily value %s: row disappeared", item.AsOfDate.Format(time.DateOnly))
		}
	}

	if errCommit := tx.Commit(ctx); errCommit != nil {
		return fmt.Errorf("commit fund daily values transaction: %w", errCommit)
	}

	return nil
}

func (r *Repository) ApplySnapshot(
	ctx context.Context,
	snapshot SourceSnapshot,
	sourceHash string,
	observedAt time.Time,
) (bool, error) {
	tx, errBegin := r.db.Begin(ctx)
	if errBegin != nil {
		return false, fmt.Errorf("begin fund snapshot transaction: %w", errBegin)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	snapshotID, created, errSnapshot := insertSnapshot(ctx, tx, snapshot, sourceHash, observedAt)
	if errSnapshot != nil {
		return false, errSnapshot
	}
	if !created {
		return false, nil
	}

	assetsTable := builder.NewTable("fund_snapshot_assets")
	for index, asset := range snapshot.Assets {
		var instrumentID any
		if asset.IsSecurity() {
			id, errResolve := r.resolveInstrument(ctx, tx, asset)
			if errResolve != nil {
				return false, fmt.Errorf("resolve snapshot asset %d instrument: %w", index+1, errResolve)
			}
			instrumentID = id
		}

		var currency any
		if asset.Currency != "" {
			currency = asset.Currency
		}

		var quantity any
		if asset.Quantity != "" {
			quantity = asset.Quantity
		}

		query := builder.NewInsert(assetsTable)
		query.Value("snapshot_id", snapshotID)
		query.Value("row_no", index+1)
		query.Value("source_name", asset.SourceName)
		query.Value("source_type", asset.SourceType)
		query.Value("instrument_id", instrumentID)
		query.Value("currency", currency)
		query.Value("quantity", quantity)
		query.Value("asset_share_percent", asset.AssetSharePercent)
		query.Value("asset_share_upper_bound", asset.AssetShareUpperBound)

		sql, binds, errBuild := query.Get()
		if errBuild != nil {
			return false, fmt.Errorf("build insert fund snapshot asset %d: %w", index+1, errBuild)
		}
		if _, errExec := tx.Exec(ctx, sql, pgx.NamedArgs(binds)); errExec != nil {
			return false, fmt.Errorf("insert fund snapshot asset %d: %w", index+1, errExec)
		}
	}

	categoriesTable := builder.NewTable("fund_snapshot_categories")
	for index, category := range snapshot.Categories {
		query := builder.NewInsert(categoriesTable)
		query.Value("snapshot_id", snapshotID)
		query.Value("row_no", index+1)
		query.Value("source_name", category.SourceName)
		query.Value("asset_share_percent", category.AssetSharePercent)

		sql, binds, errBuild := query.Get()
		if errBuild != nil {
			return false, fmt.Errorf("build insert fund snapshot category %d: %w", index+1, errBuild)
		}
		if _, errExec := tx.Exec(ctx, sql, pgx.NamedArgs(binds)); errExec != nil {
			return false, fmt.Errorf("insert fund snapshot category %d: %w", index+1, errExec)
		}
	}

	if errCommit := tx.Commit(ctx); errCommit != nil {
		return false, fmt.Errorf("commit fund snapshot transaction: %w", errCommit)
	}

	return true, nil
}

func insertSnapshot(
	ctx context.Context,
	tx pgx.Tx,
	snapshot SourceSnapshot,
	sourceHash string,
	observedAt time.Time,
) (int64, bool, error) {
	table := builder.NewTable("fund_snapshots")
	query := builder.NewInsert(table)
	query.Value("as_of_date", dateOnlyUTC(snapshot.AsOfDate))
	query.Value("observed_at", observedAt.UTC())
	query.Value("source_hash", sourceHash)
	query.Value("calculated_unit_value_usd", snapshot.CalculatedUnitValueUSD)
	query.Value("nav_usd", snapshot.NAVUSD)
	query.OnConflictDoNothing("as_of_date", "source_hash")
	query.Return(builder.ColumnName{Table: table, Name: "id"})

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return 0, false, fmt.Errorf("build insert fund snapshot: %w", errBuild)
	}

	rows, errQuery := tx.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return 0, false, fmt.Errorf("insert fund snapshot: %w", errQuery)
	}
	defer rows.Close()

	snapshotID, errCollect := pgx.CollectOneRow(rows, pgx.RowTo[int64])
	if errCollect != nil {
		if errors.Is(errCollect, pgx.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("collect inserted fund snapshot id: %w", errCollect)
	}

	return snapshotID, true, nil
}

func (r *Repository) resolveInstrument(ctx context.Context, tx pgx.Tx, asset SourceAsset) (int64, error) {
	table := builder.NewTable("instruments")
	query := builder.NewSelect()
	query.From(table)
	query.Column(
		builder.ColumnName{Table: table, Name: "id"},
		builder.ColumnName{Table: table, Name: "asset_type"},
	)
	query.Where(builder.WhereEq{Table: table, Column: "isin", Value: asset.ISIN})
	query.Limit(1)

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return 0, fmt.Errorf("build find instrument %s: %w", asset.ISIN, errBuild)
	}

	rows, errQuery := tx.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return 0, fmt.Errorf("find instrument %s: %w", asset.ISIN, errQuery)
	}

	identity, errCollect := pgx.CollectOneRow(rows, pgx.RowToStructByNameLax[storedInstrumentIdentity])
	rows.Close()
	if errCollect == nil {
		if identity.AssetType != asset.Kind {
			return 0, fmt.Errorf(
				"instrument %s has asset_type %q, source requires %q",
				asset.ISIN,
				identity.AssetType,
				asset.Kind,
			)
		}
		return identity.ID, nil
	}
	if !errors.Is(errCollect, pgx.ErrNoRows) {
		return 0, fmt.Errorf("collect instrument %s: %w", asset.ISIN, errCollect)
	}

	insert := builder.NewInsert(table)
	insert.Value("asset_type", asset.Kind)
	insert.Value("isin", asset.ISIN)
	insert.Value("name", asset.SourceName)
	insert.Return(builder.ColumnName{Table: table, Name: "id"})

	insertSQL, insertBinds, errBuildInsert := insert.Get()
	if errBuildInsert != nil {
		return 0, fmt.Errorf("build create instrument %s: %w", asset.ISIN, errBuildInsert)
	}

	insertRows, errInsert := tx.Query(ctx, insertSQL, pgx.NamedArgs(insertBinds))
	if errInsert != nil {
		return 0, fmt.Errorf("create instrument %s: %w", asset.ISIN, errInsert)
	}
	defer insertRows.Close()

	id, errCollectID := pgx.CollectOneRow(insertRows, pgx.RowTo[int64])
	if errCollectID != nil {
		return 0, fmt.Errorf("collect created instrument %s id: %w", asset.ISIN, errCollectID)
	}

	return id, nil
}

func dateOnlyUTC(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}
