package fund

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xloss/go-builder"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/dateonly"
)

var ErrFundStateNotFound = errors.New("fund state not found")

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

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type storedInstrumentIdentity struct {
	ID        int64  `db:"id"`
	AssetType string `db:"asset_type"`
}

type storedSnapshot struct {
	ID         int64     `db:"id"`
	AsOfDate   time.Time `db:"as_of_date"`
	ObservedAt time.Time `db:"observed_at"`
	SourceHash string    `db:"source_hash"`

	CalculatedUnitValueUSD string `db:"calculated_unit_value_usd"`
	NAVUSD                 string `db:"nav_usd"`
}

type storedSnapshotAsset struct {
	ID    int64 `db:"id"`
	RowNo int   `db:"row_no"`

	SourceName string `db:"source_name"`
	SourceType string `db:"source_type"`

	InstrumentID   *int64  `db:"instrument_id"`
	InstrumentType *string `db:"instrument_type"`
	ISIN           *string `db:"isin"`
	InstrumentName *string `db:"instrument_name"`
	Issuer         *string `db:"issuer"`
	Ticker         *string `db:"ticker"`

	Currency *string `db:"currency"`
	Quantity *string `db:"quantity"`

	AssetSharePercent    string `db:"asset_share_percent"`
	AssetShareUpperBound bool   `db:"asset_share_upper_bound"`
}

type storedSnapshotCategory struct {
	RowNo int `db:"row_no"`

	SourceName        string `db:"source_name"`
	AssetSharePercent string `db:"asset_share_percent"`
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) LoadDailyValues(ctx context.Context) (map[string]StoredDailyValue, error) {
	items, err := loadDailyValues(ctx, r.db)
	if err != nil {
		return nil, err
	}

	result := make(map[string]StoredDailyValue, len(items))
	for _, item := range items {
		result[item.AsOfDate.Format(time.DateOnly)] = item
	}

	return result, nil
}

func (r *Repository) LoadFundState(ctx context.Context) (*appstate.FundState, error) {
	tx, errBegin := r.db.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if errBegin != nil {
		return nil, fmt.Errorf("begin load fund state transaction: %w", errBegin)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	state, errState := loadFundState(ctx, tx)
	if errState != nil {
		return nil, errState
	}

	if errCommit := tx.Commit(ctx); errCommit != nil {
		return nil, fmt.Errorf("commit load fund state transaction: %w", errCommit)
	}

	return state, nil
}

func (r *Repository) ApplyManagementCompanySync(
	ctx context.Context,
	changes DailyValueChanges,
	snapshot SourceSnapshot,
	sourceHash string,
	observedAt time.Time,
) (bool, *appstate.FundState, error) {
	tx, errBegin := r.db.Begin(ctx)
	if errBegin != nil {
		return false, nil, fmt.Errorf("begin management company sync transaction: %w", errBegin)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if errChanges := applyDailyValueChanges(ctx, tx, changes); errChanges != nil {
		return false, nil, errChanges
	}

	snapshotID, created, errSnapshot := insertSnapshot(ctx, tx, snapshot, sourceHash, observedAt)
	if errSnapshot != nil {
		return false, nil, errSnapshot
	}

	if created {
		if errAssets := r.insertSnapshotAssets(ctx, tx, snapshotID, snapshot.Assets); errAssets != nil {
			return false, nil, errAssets
		}

		if errCategories := insertSnapshotCategories(ctx, tx, snapshotID, snapshot.Categories); errCategories != nil {
			return false, nil, errCategories
		}
	}

	changed := created || len(changes.Insert) != 0 || len(changes.Update) != 0
	var state *appstate.FundState
	if changed {
		var errState error
		state, errState = loadFundState(ctx, tx)
		if errState != nil {
			return false, nil, fmt.Errorf("build fund state after management company sync: %w", errState)
		}
	}

	if errCommit := tx.Commit(ctx); errCommit != nil {
		return false, nil, fmt.Errorf("commit management company sync transaction: %w", errCommit)
	}

	return created, state, nil
}

func loadFundState(ctx context.Context, db queryer) (*appstate.FundState, error) {
	snapshot, errSnapshot := loadLatestSnapshot(ctx, db)
	if errSnapshot != nil {
		return nil, errSnapshot
	}

	assets, errAssets := loadSnapshotAssets(ctx, db, snapshot.ID)
	if errAssets != nil {
		return nil, errAssets
	}

	categories, errCategories := loadSnapshotCategories(ctx, db, snapshot.ID)
	if errCategories != nil {
		return nil, errCategories
	}

	dailyValues, errDailyValues := loadDailyValues(ctx, db)
	if errDailyValues != nil {
		return nil, errDailyValues
	}

	state := &appstate.FundState{
		Snapshot: appstate.FundSnapshot{
			ID:                     snapshot.ID,
			AsOfDate:               dateonly.UTC(snapshot.AsOfDate),
			ObservedAt:             snapshot.ObservedAt.UTC(),
			SourceHash:             snapshot.SourceHash,
			CalculatedUnitValueUSD: snapshot.CalculatedUnitValueUSD,
			NAVUSD:                 snapshot.NAVUSD,
			Assets:                 assets,
			Categories:             categories,
		},
		DailyValues: make([]appstate.FundDailyValue, 0, len(dailyValues)),
	}

	for _, item := range dailyValues {
		state.DailyValues = append(state.DailyValues, appstate.FundDailyValue{
			AsOfDate:               item.AsOfDate,
			CalculatedUnitValueUSD: item.CalculatedUnitValueUSD,
			NAVUSD:                 item.NAVUSD,
		})
	}

	return state, nil
}

func loadDailyValues(ctx context.Context, db queryer) ([]StoredDailyValue, error) {
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

	rows, errQuery := db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return nil, fmt.Errorf("query fund daily values: %w", errQuery)
	}
	defer rows.Close()

	items, errCollect := pgx.CollectRows(rows, pgx.RowToStructByNameLax[StoredDailyValue])
	if errCollect != nil {
		return nil, fmt.Errorf("collect fund daily values: %w", errCollect)
	}

	for index := range items {
		items[index].AsOfDate = dateonly.UTC(items[index].AsOfDate)
	}

	return items, nil
}

func loadLatestSnapshot(ctx context.Context, db queryer) (*storedSnapshot, error) {
	table := builder.NewTable("fund_snapshots")
	query := builder.NewSelect()
	query.From(table)
	query.Column(
		builder.ColumnName{Table: table, Name: "id"},
		builder.ColumnName{Table: table, Name: "as_of_date"},
		builder.ColumnName{Table: table, Name: "observed_at"},
		builder.ColumnName{Table: table, Name: "source_hash"},
		builder.ColumnName{Table: table, Name: "calculated_unit_value_usd"},
		builder.ColumnName{Table: table, Name: "nav_usd"},
	)
	query.Order(
		builder.Order{Table: table, Column: "as_of_date", Desc: true},
		builder.Order{Table: table, Column: "observed_at", Desc: true},
		builder.Order{Table: table, Column: "id", Desc: true},
	)
	query.Limit(1)

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build latest fund snapshot query: %w", errBuild)
	}

	rows, errQuery := db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return nil, fmt.Errorf("query latest fund snapshot: %w", errQuery)
	}
	defer rows.Close()

	result, errCollect := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByNameLax[storedSnapshot])
	if errCollect != nil {
		if errors.Is(errCollect, pgx.ErrNoRows) {
			return nil, ErrFundStateNotFound
		}
		return nil, fmt.Errorf("collect latest fund snapshot: %w", errCollect)
	}

	return result, nil
}

func loadSnapshotAssets(ctx context.Context, db queryer, snapshotID int64) ([]appstate.FundAsset, error) {
	assetsTable := builder.NewTable("fund_snapshot_assets")
	instrumentsTable := builder.NewTable("instruments")

	query := builder.NewSelect()
	query.From(assetsTable)
	query.LeftJoin(instrumentsTable, builder.OnEq{Table1: assetsTable, Column1: "instrument_id", Table2: instrumentsTable, Column2: "id"})
	query.Column(
		builder.ColumnName{Table: assetsTable, Name: "id"},
		builder.ColumnName{Table: assetsTable, Name: "row_no"},
		builder.ColumnName{Table: assetsTable, Name: "source_name"},
		builder.ColumnName{Table: assetsTable, Name: "source_type"},
		builder.ColumnName{Table: assetsTable, Name: "instrument_id"},
		builder.ColumnName{Table: instrumentsTable, Name: "asset_type", Alias: "instrument_type"},
		builder.ColumnName{Table: instrumentsTable, Name: "isin"},
		builder.ColumnName{Table: instrumentsTable, Name: "name", Alias: "instrument_name"},
		builder.ColumnName{Table: instrumentsTable, Name: "issuer"},
		builder.ColumnName{Table: instrumentsTable, Name: "ticker"},
		builder.ColumnName{Table: assetsTable, Name: "currency"},
		builder.ColumnName{Table: assetsTable, Name: "quantity"},
		builder.ColumnName{Table: assetsTable, Name: "asset_share_percent"},
		builder.ColumnName{Table: assetsTable, Name: "asset_share_upper_bound"},
	)
	query.Where(builder.WhereEq{Table: assetsTable, Column: "snapshot_id", Value: snapshotID})
	query.Order(builder.Order{Table: assetsTable, Column: "row_no"})

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build fund snapshot assets query: %w", errBuild)
	}

	rows, errQuery := db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return nil, fmt.Errorf("query fund snapshot assets: %w", errQuery)
	}
	defer rows.Close()

	stored, errCollect := pgx.CollectRows(rows, pgx.RowToStructByNameLax[storedSnapshotAsset])
	if errCollect != nil {
		return nil, fmt.Errorf("collect fund snapshot assets: %w", errCollect)
	}

	result := make([]appstate.FundAsset, 0, len(stored))
	for _, item := range stored {
		result = append(result, appstate.FundAsset{
			ID:                   item.ID,
			RowNo:                item.RowNo,
			SourceName:           item.SourceName,
			SourceType:           item.SourceType,
			InstrumentID:         item.InstrumentID,
			InstrumentType:       stringValue(item.InstrumentType),
			ISIN:                 stringValue(item.ISIN),
			InstrumentName:       stringValue(item.InstrumentName),
			Issuer:               stringValue(item.Issuer),
			Ticker:               stringValue(item.Ticker),
			Currency:             stringValue(item.Currency),
			Quantity:             stringValue(item.Quantity),
			AssetSharePercent:    item.AssetSharePercent,
			AssetShareUpperBound: item.AssetShareUpperBound,
		})
	}

	return result, nil
}

func loadSnapshotCategories(ctx context.Context, db queryer, snapshotID int64) ([]appstate.FundCategory, error) {
	table := builder.NewTable("fund_snapshot_categories")
	query := builder.NewSelect()
	query.From(table)
	query.Column(
		builder.ColumnName{Table: table, Name: "row_no"},
		builder.ColumnName{Table: table, Name: "source_name"},
		builder.ColumnName{Table: table, Name: "asset_share_percent"},
	)
	query.Where(builder.WhereEq{Table: table, Column: "snapshot_id", Value: snapshotID})
	query.Order(builder.Order{Table: table, Column: "row_no"})

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build fund snapshot categories query: %w", errBuild)
	}

	rows, errQuery := db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return nil, fmt.Errorf("query fund snapshot categories: %w", errQuery)
	}
	defer rows.Close()

	stored, errCollect := pgx.CollectRows(rows, pgx.RowToStructByNameLax[storedSnapshotCategory])
	if errCollect != nil {
		return nil, fmt.Errorf("collect fund snapshot categories: %w", errCollect)
	}

	result := make([]appstate.FundCategory, 0, len(stored))
	for _, item := range stored {
		result = append(result, appstate.FundCategory{
			RowNo:             item.RowNo,
			SourceName:        item.SourceName,
			AssetSharePercent: item.AssetSharePercent,
		})
	}

	return result, nil
}

func applyDailyValueChanges(ctx context.Context, tx pgx.Tx, changes DailyValueChanges) error {
	table := builder.NewTable("fund_daily_values")

	for _, item := range changes.Insert {
		query := builder.NewInsert(table)
		query.Value("as_of_date", dateonly.UTC(item.AsOfDate))
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
		query.Where(builder.WhereEq{Table: table, Column: "as_of_date", Value: dateonly.UTC(item.AsOfDate)})

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

	return nil
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
	query.Value("as_of_date", dateonly.UTC(snapshot.AsOfDate))
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

func (r *Repository) insertSnapshotAssets(ctx context.Context, tx pgx.Tx, snapshotID int64, assets []SourceAsset) error {
	table := builder.NewTable("fund_snapshot_assets")

	for index, asset := range assets {
		var instrumentID any
		if asset.IsSecurity() {
			id, errResolve := r.resolveInstrument(ctx, tx, asset)
			if errResolve != nil {
				return fmt.Errorf("resolve snapshot asset %d instrument: %w", index+1, errResolve)
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

		query := builder.NewInsert(table)
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
			return fmt.Errorf("build insert fund snapshot asset %d: %w", index+1, errBuild)
		}
		if _, errExec := tx.Exec(ctx, sql, pgx.NamedArgs(binds)); errExec != nil {
			return fmt.Errorf("insert fund snapshot asset %d: %w", index+1, errExec)
		}
	}

	return nil
}

func insertSnapshotCategories(ctx context.Context, tx pgx.Tx, snapshotID int64, categories []SourceCategory) error {
	table := builder.NewTable("fund_snapshot_categories")

	for index, category := range categories {
		query := builder.NewInsert(table)
		query.Value("snapshot_id", snapshotID)
		query.Value("row_no", index+1)
		query.Value("source_name", category.SourceName)
		query.Value("asset_share_percent", category.AssetSharePercent)

		sql, binds, errBuild := query.Get()
		if errBuild != nil {
			return fmt.Errorf("build insert fund snapshot category %d: %w", index+1, errBuild)
		}
		if _, errExec := tx.Exec(ctx, sql, pgx.NamedArgs(binds)); errExec != nil {
			return fmt.Errorf("insert fund snapshot category %d: %w", index+1, errExec)
		}
	}

	return nil
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
			return 0, fmt.Errorf("instrument %s has asset_type %q, source requires %q", asset.ISIN, identity.AssetType, asset.Kind)
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

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
