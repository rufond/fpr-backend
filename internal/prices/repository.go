package prices

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
	"github.com/rufond/fpr-backend/internal/decimal"
)

const pricePointRetention = 48 * time.Hour

type Repository struct {
	db *pgxpool.Pool
}

type priceQueryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type storedInstrument struct {
	ID        int64  `db:"id"`
	AssetType string `db:"asset_type"`
}

type storedPriceSource struct {
	ID             int64  `db:"id"`
	ProviderSymbol string `db:"provider_symbol"`
	Enabled        bool   `db:"enabled"`
}

type storedCurrentPrice struct {
	UnitValue string    `db:"unit_value"`
	Currency  string    `db:"currency"`
	PricedAt  time.Time `db:"priced_at"`
	FetchedAt time.Time `db:"fetched_at"`
}

type storedDailyPrice struct {
	PriceDate time.Time `db:"price_date"`
	UnitValue string    `db:"unit_value"`
	Currency  string    `db:"currency"`
}

type storedDailyPriceState struct {
	PriceSourceID int64 `db:"price_source_id"`
	InstrumentID  int64 `db:"instrument_id"`

	AssetType string `db:"asset_type"`
	ISIN      string `db:"isin"`
	Name      string `db:"name"`

	Provider       string `db:"provider"`
	ProviderSymbol string `db:"provider_symbol"`

	PriceDate time.Time `db:"price_date"`
	UnitValue string    `db:"unit_value"`
	Currency  string    `db:"currency"`
}

type storedPriceState struct {
	PriceSourceID int64 `db:"price_source_id"`
	InstrumentID  int64 `db:"instrument_id"`

	AssetType string `db:"asset_type"`
	ISIN      string `db:"isin"`
	Name      string `db:"name"`

	Provider       string `db:"provider"`
	ProviderSymbol string `db:"provider_symbol"`

	UnitValue string    `db:"unit_value"`
	Currency  string    `db:"currency"`
	PricedAt  time.Time `db:"priced_at"`
	FetchedAt time.Time `db:"fetched_at"`
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) EnsureFundUnitMOEXSource(ctx context.Context) error {
	tx, errBegin := r.db.Begin(ctx)
	if errBegin != nil {
		return fmt.Errorf("begin ensure fund unit MOEX price source transaction: %w", errBegin)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, errEnsure := ensureFundUnitMOEXSource(ctx, tx); errEnsure != nil {
		return errEnsure
	}

	if errCommit := tx.Commit(ctx); errCommit != nil {
		return fmt.Errorf("commit ensure fund unit MOEX price source transaction: %w", errCommit)
	}

	return nil
}

func (r *Repository) LoadState(ctx context.Context) (*appstate.PriceState, error) {
	return loadPriceState(ctx, r.db)
}

func (r *Repository) ApplyFundUnitMOEXQuote(
	ctx context.Context,
	quote SourceQuote,
	fetchedAt time.Time,
) (bool, bool, *appstate.PriceState, appstate.InstrumentPrice, error) {
	tx, errBegin := r.db.Begin(ctx)
	if errBegin != nil {
		return false, false, nil, appstate.InstrumentPrice{}, fmt.Errorf("begin MOEX fund unit quote transaction: %w", errBegin)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	priceSourceID, errSource := ensureFundUnitMOEXSource(ctx, tx)
	if errSource != nil {
		return false, false, nil, appstate.InstrumentPrice{}, errSource
	}

	stored, errStored := loadCurrentPrice(ctx, tx, priceSourceID)
	if errStored != nil && !errors.Is(errStored, pgx.ErrNoRows) {
		return false, false, nil, appstate.InstrumentPrice{}, errStored
	}

	if stored != nil && quote.PricedAt.Before(stored.PricedAt) {
		price, errPrice := loadInstrumentPrice(ctx, tx, priceSourceID)
		if errPrice != nil {
			return false, false, nil, appstate.InstrumentPrice{}, errPrice
		}
		if errCommit := tx.Commit(ctx); errCommit != nil {
			return false, false, nil, appstate.InstrumentPrice{}, fmt.Errorf("commit stale MOEX fund unit quote transaction: %w", errCommit)
		}
		return false, true, nil, price, nil
	}

	if stored != nil &&
		decimal.Equal(stored.UnitValue, quote.UnitValue) &&
		stored.Currency == quote.Currency &&
		stored.PricedAt.Equal(quote.PricedAt) {
		price, errPrice := loadInstrumentPrice(ctx, tx, priceSourceID)
		if errPrice != nil {
			return false, false, nil, appstate.InstrumentPrice{}, errPrice
		}
		if errCommit := tx.Commit(ctx); errCommit != nil {
			return false, false, nil, appstate.InstrumentPrice{}, fmt.Errorf("commit unchanged MOEX fund unit quote transaction: %w", errCommit)
		}
		return false, false, nil, price, nil
	}

	if errPersist := persistCurrentPrice(ctx, tx, priceSourceID, quote, fetchedAt, stored != nil); errPersist != nil {
		return false, false, nil, appstate.InstrumentPrice{}, errPersist
	}
	if errPoint := insertPricePoint(ctx, tx, priceSourceID, quote, fetchedAt); errPoint != nil {
		return false, false, nil, appstate.InstrumentPrice{}, errPoint
	}
	if errCleanup := deleteOldPricePoints(ctx, tx, priceSourceID, fetchedAt.Add(-pricePointRetention)); errCleanup != nil {
		return false, false, nil, appstate.InstrumentPrice{}, errCleanup
	}

	state, errState := loadCurrentPriceStateSnapshot(ctx, tx)
	if errState != nil {
		return false, false, nil, appstate.InstrumentPrice{}, fmt.Errorf("build current price state after MOEX fund unit quote: %w", errState)
	}

	price, ok := state.Sources[priceSourceID]
	if !ok {
		return false, false, nil, appstate.InstrumentPrice{}, fmt.Errorf("MOEX fund unit price is missing from rebuilt state")
	}

	if errCommit := tx.Commit(ctx); errCommit != nil {
		return false, false, nil, appstate.InstrumentPrice{}, fmt.Errorf("commit MOEX fund unit quote transaction: %w", errCommit)
	}

	return true, false, state, price, nil
}

func (r *Repository) ApplyFundUnitMOEXDailyPrices(
	ctx context.Context,
	items []SourceDailyPrice,
) (int, int, *appstate.PriceState, error) {
	tx, errBegin := r.db.Begin(ctx)
	if errBegin != nil {
		return 0, 0, nil, fmt.Errorf("begin MOEX fund unit daily prices transaction: %w", errBegin)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	priceSourceID, errSource := ensureFundUnitMOEXSource(ctx, tx)
	if errSource != nil {
		return 0, 0, nil, errSource
	}

	stored, errStored := loadDailyPrices(ctx, tx, priceSourceID)
	if errStored != nil {
		return 0, 0, nil, errStored
	}

	byDate := make(map[string]storedDailyPrice, len(stored))
	for _, item := range stored {
		item.PriceDate = dateonly.UTC(item.PriceDate)
		byDate[item.PriceDate.Format(time.DateOnly)] = item
	}

	inserted := 0
	updated := 0

	for _, item := range items {
		priceDate := dateonly.UTC(item.PriceDate)
		key := priceDate.Format(time.DateOnly)
		previous, exists := byDate[key]

		if !exists {
			if errInsert := insertDailyPrice(ctx, tx, priceSourceID, item); errInsert != nil {
				return 0, 0, nil, errInsert
			}
			inserted++
			continue
		}

		if decimal.Equal(previous.UnitValue, item.UnitValue) && previous.Currency == item.Currency {
			continue
		}

		if errUpdate := updateDailyPrice(ctx, tx, priceSourceID, item); errUpdate != nil {
			return 0, 0, nil, errUpdate
		}
		updated++
	}

	if inserted == 0 && updated == 0 {
		if errCommit := tx.Commit(ctx); errCommit != nil {
			return 0, 0, nil, fmt.Errorf("commit unchanged MOEX fund unit daily prices transaction: %w", errCommit)
		}
		return 0, 0, nil, nil
	}

	state, errState := loadPriceState(ctx, tx)
	if errState != nil {
		return 0, 0, nil, fmt.Errorf("build price state after MOEX fund unit daily prices: %w", errState)
	}

	if errCommit := tx.Commit(ctx); errCommit != nil {
		return 0, 0, nil, fmt.Errorf("commit MOEX fund unit daily prices transaction: %w", errCommit)
	}

	return inserted, updated, state, nil
}

func ensureFundUnitMOEXSource(ctx context.Context, tx pgx.Tx) (int64, error) {
	instrumentID, errInstrument := ensureFundUnitInstrument(ctx, tx)
	if errInstrument != nil {
		return 0, errInstrument
	}

	table := builder.NewTable("instrument_price_sources")
	query := builder.NewSelect()
	query.From(table)
	query.Column(
		builder.ColumnName{Table: table, Name: "id"},
		builder.ColumnName{Table: table, Name: "provider_symbol"},
		builder.ColumnName{Table: table, Name: "enabled"},
	)
	query.Where(builder.WhereAnd{List: []builder.Where{
		builder.WhereEq{Table: table, Column: "instrument_id", Value: instrumentID},
		builder.WhereEq{Table: table, Column: "provider", Value: ProviderMOEX},
	}})
	query.Limit(1)

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return 0, fmt.Errorf("build fund unit MOEX price source query: %w", errBuild)
	}

	rows, errQuery := tx.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return 0, fmt.Errorf("query fund unit MOEX price source: %w", errQuery)
	}

	stored, errCollect := pgx.CollectOneRow(rows, pgx.RowToStructByNameLax[storedPriceSource])
	if errCollect == nil {
		if !stored.Enabled {
			return 0, fmt.Errorf("fund unit MOEX price source is disabled")
		}
		if stored.ProviderSymbol != FundUnitISIN {
			update := builder.NewUpdate(table)
			update.Set("provider_symbol", FundUnitISIN)
			update.SetNow("updated_at")
			update.Where(builder.WhereEq{Table: table, Column: "id", Value: stored.ID})

			sqlUpdate, bindsUpdate, errBuildUpdate := update.Get()
			if errBuildUpdate != nil {
				return 0, fmt.Errorf("build normalize fund unit MOEX price source query: %w", errBuildUpdate)
			}
			if _, errExecUpdate := tx.Exec(ctx, sqlUpdate, pgx.NamedArgs(bindsUpdate)); errExecUpdate != nil {
				return 0, fmt.Errorf("normalize fund unit MOEX price source: %w", errExecUpdate)
			}
		}

		return stored.ID, nil
	}
	if !errors.Is(errCollect, pgx.ErrNoRows) {
		return 0, fmt.Errorf("collect fund unit MOEX price source: %w", errCollect)
	}

	insert := builder.NewInsert(table)
	insert.Value("instrument_id", instrumentID)
	insert.Value("provider", ProviderMOEX)
	insert.Value("provider_symbol", FundUnitISIN)
	insert.Value("enabled", true)
	insert.Return(builder.ColumnName{Table: table, Name: "id"})

	sqlInsert, bindsInsert, errBuildInsert := insert.Get()
	if errBuildInsert != nil {
		return 0, fmt.Errorf("build insert fund unit MOEX price source: %w", errBuildInsert)
	}

	rowsInsert, errQueryInsert := tx.Query(ctx, sqlInsert, pgx.NamedArgs(bindsInsert))
	if errQueryInsert != nil {
		return 0, fmt.Errorf("insert fund unit MOEX price source: %w", errQueryInsert)
	}

	created, errCollectInsert := pgx.CollectOneRow(rowsInsert, pgx.RowTo[int64])
	if errCollectInsert != nil {
		return 0, fmt.Errorf("collect inserted fund unit MOEX price source: %w", errCollectInsert)
	}

	return created, nil
}

func ensureFundUnitInstrument(ctx context.Context, tx pgx.Tx) (int64, error) {
	table := builder.NewTable("instruments")
	query := builder.NewSelect()
	query.From(table)
	query.Column(
		builder.ColumnName{Table: table, Name: "id"},
		builder.ColumnName{Table: table, Name: "asset_type"},
	)
	query.Where(builder.WhereEq{Table: table, Column: "isin", Value: FundUnitISIN})
	query.Limit(1)

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return 0, fmt.Errorf("build fund unit instrument query: %w", errBuild)
	}

	rows, errQuery := tx.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return 0, fmt.Errorf("query fund unit instrument: %w", errQuery)
	}

	stored, errCollect := pgx.CollectOneRow(rows, pgx.RowToStructByNameLax[storedInstrument])
	if errCollect == nil {
		if stored.AssetType != FundUnitAssetType {
			return 0, fmt.Errorf("fund unit ISIN %s belongs to asset type %s", FundUnitISIN, stored.AssetType)
		}
		return stored.ID, nil
	}
	if !errors.Is(errCollect, pgx.ErrNoRows) {
		return 0, fmt.Errorf("collect fund unit instrument: %w", errCollect)
	}

	insert := builder.NewInsert(table)
	insert.Value("asset_type", FundUnitAssetType)
	insert.Value("isin", FundUnitISIN)
	insert.Value("name", FundUnitName)
	insert.Return(builder.ColumnName{Table: table, Name: "id"})

	sqlInsert, bindsInsert, errBuildInsert := insert.Get()
	if errBuildInsert != nil {
		return 0, fmt.Errorf("build insert fund unit instrument: %w", errBuildInsert)
	}

	rowsInsert, errQueryInsert := tx.Query(ctx, sqlInsert, pgx.NamedArgs(bindsInsert))
	if errQueryInsert != nil {
		return 0, fmt.Errorf("insert fund unit instrument: %w", errQueryInsert)
	}

	created, errCollectInsert := pgx.CollectOneRow(rowsInsert, pgx.RowTo[int64])
	if errCollectInsert != nil {
		return 0, fmt.Errorf("collect inserted fund unit instrument: %w", errCollectInsert)
	}

	return created, nil
}

func loadCurrentPrice(ctx context.Context, db priceQueryer, priceSourceID int64) (*storedCurrentPrice, error) {
	table := builder.NewTable("instrument_prices")
	query := builder.NewSelect()
	query.From(table)
	query.Column(
		builder.ColumnName{Table: table, Name: "unit_value"},
		builder.ColumnName{Table: table, Name: "currency"},
		builder.ColumnName{Table: table, Name: "priced_at"},
		builder.ColumnName{Table: table, Name: "fetched_at"},
	)
	query.Where(builder.WhereEq{Table: table, Column: "price_source_id", Value: priceSourceID})
	query.Limit(1)

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build current instrument price query: %w", errBuild)
	}

	rows, errQuery := db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return nil, fmt.Errorf("query current instrument price: %w", errQuery)
	}
	defer rows.Close()

	stored, errCollect := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByNameLax[storedCurrentPrice])
	if errCollect != nil {
		return nil, errCollect
	}

	stored.PricedAt = stored.PricedAt.UTC()
	stored.FetchedAt = stored.FetchedAt.UTC()
	return stored, nil
}

func persistCurrentPrice(
	ctx context.Context,
	tx pgx.Tx,
	priceSourceID int64,
	quote SourceQuote,
	fetchedAt time.Time,
	exists bool,
) error {
	table := builder.NewTable("instrument_prices")

	if !exists {
		query := builder.NewInsert(table)
		query.Value("price_source_id", priceSourceID)
		query.Value("unit_value", quote.UnitValue)
		query.Value("currency", quote.Currency)
		query.Value("priced_at", quote.PricedAt.UTC())
		query.Value("fetched_at", fetchedAt.UTC())

		sql, binds, errBuild := query.Get()
		if errBuild != nil {
			return fmt.Errorf("build insert current instrument price: %w", errBuild)
		}
		if _, errExec := tx.Exec(ctx, sql, pgx.NamedArgs(binds)); errExec != nil {
			return fmt.Errorf("insert current instrument price: %w", errExec)
		}
		return nil
	}

	query := builder.NewUpdate(table)
	query.Set("unit_value", quote.UnitValue)
	query.Set("currency", quote.Currency)
	query.Set("priced_at", quote.PricedAt.UTC())
	query.Set("fetched_at", fetchedAt.UTC())
	query.Where(builder.WhereEq{Table: table, Column: "price_source_id", Value: priceSourceID})

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return fmt.Errorf("build update current instrument price: %w", errBuild)
	}
	if _, errExec := tx.Exec(ctx, sql, pgx.NamedArgs(binds)); errExec != nil {
		return fmt.Errorf("update current instrument price: %w", errExec)
	}

	return nil
}

func insertPricePoint(ctx context.Context, tx pgx.Tx, priceSourceID int64, quote SourceQuote, observedAt time.Time) error {
	table := builder.NewTable("instrument_price_points")
	query := builder.NewInsert(table)
	query.Value("price_source_id", priceSourceID)
	query.Value("unit_value", quote.UnitValue)
	query.Value("currency", quote.Currency)
	query.Value("priced_at", quote.PricedAt.UTC())
	query.Value("observed_at", observedAt.UTC())

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return fmt.Errorf("build insert instrument price point: %w", errBuild)
	}
	if _, errExec := tx.Exec(ctx, sql, pgx.NamedArgs(binds)); errExec != nil {
		return fmt.Errorf("insert instrument price point: %w", errExec)
	}

	return nil
}

func deleteOldPricePoints(ctx context.Context, tx pgx.Tx, priceSourceID int64, before time.Time) error {
	table := builder.NewTable("instrument_price_points")
	query := builder.NewDelete(table)
	query.Where(builder.WhereAnd{List: []builder.Where{
		builder.WhereEq{Table: table, Column: "price_source_id", Value: priceSourceID},
		builder.WhereLess{Table: table, Column: "observed_at", Value: before.UTC()},
	}})

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return fmt.Errorf("build old instrument price points delete: %w", errBuild)
	}
	if _, errExec := tx.Exec(ctx, sql, pgx.NamedArgs(binds)); errExec != nil {
		return fmt.Errorf("delete old instrument price points: %w", errExec)
	}

	return nil
}

func loadDailyPrices(ctx context.Context, db priceQueryer, priceSourceID int64) ([]storedDailyPrice, error) {
	table := builder.NewTable("instrument_daily_prices")
	query := builder.NewSelect()
	query.From(table)
	query.Column(
		builder.ColumnName{Table: table, Name: "price_date"},
		builder.ColumnName{Table: table, Name: "unit_value"},
		builder.ColumnName{Table: table, Name: "currency"},
	)
	query.Where(builder.WhereEq{Table: table, Column: "price_source_id", Value: priceSourceID})
	query.Order(builder.Order{Table: table, Column: "price_date"})

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build fund unit daily prices query: %w", errBuild)
	}

	rows, errQuery := db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return nil, fmt.Errorf("query fund unit daily prices: %w", errQuery)
	}
	defer rows.Close()

	items, errCollect := pgx.CollectRows(rows, pgx.RowToStructByNameLax[storedDailyPrice])
	if errCollect != nil {
		return nil, fmt.Errorf("collect fund unit daily prices: %w", errCollect)
	}

	for index := range items {
		items[index].PriceDate = dateonly.UTC(items[index].PriceDate)
	}

	return items, nil
}

func insertDailyPrice(ctx context.Context, tx pgx.Tx, priceSourceID int64, item SourceDailyPrice) error {
	table := builder.NewTable("instrument_daily_prices")
	query := builder.NewInsert(table)
	query.Value("price_source_id", priceSourceID)
	query.Value("price_date", dateonly.UTC(item.PriceDate))
	query.Value("unit_value", item.UnitValue)
	query.Value("currency", item.Currency)

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return fmt.Errorf("build insert instrument daily price: %w", errBuild)
	}
	if _, errExec := tx.Exec(ctx, sql, pgx.NamedArgs(binds)); errExec != nil {
		return fmt.Errorf("insert instrument daily price: %w", errExec)
	}

	return nil
}

func updateDailyPrice(ctx context.Context, tx pgx.Tx, priceSourceID int64, item SourceDailyPrice) error {
	table := builder.NewTable("instrument_daily_prices")
	query := builder.NewUpdate(table)
	query.Set("unit_value", item.UnitValue)
	query.Set("currency", item.Currency)
	query.SetNow("updated_at")
	query.Where(builder.WhereAnd{List: []builder.Where{
		builder.WhereEq{Table: table, Column: "price_source_id", Value: priceSourceID},
		builder.WhereEq{Table: table, Column: "price_date", Value: dateonly.UTC(item.PriceDate)},
	}})

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return fmt.Errorf("build update instrument daily price: %w", errBuild)
	}
	if _, errExec := tx.Exec(ctx, sql, pgx.NamedArgs(binds)); errExec != nil {
		return fmt.Errorf("update instrument daily price: %w", errExec)
	}

	return nil
}

func loadCurrentPriceStateSnapshot(ctx context.Context, db priceQueryer) (*appstate.PriceState, error) {
	state := &appstate.PriceState{
		Sources: map[int64]appstate.InstrumentPrice{},
	}

	if errCurrent := loadCurrentPriceState(ctx, db, state); errCurrent != nil {
		return nil, errCurrent
	}

	return state, nil
}

func loadPriceState(ctx context.Context, db priceQueryer) (*appstate.PriceState, error) {
	state := &appstate.PriceState{
		Sources:     map[int64]appstate.InstrumentPrice{},
		DailyPrices: map[int64]appstate.InstrumentDailyPriceSeries{},
	}

	if errCurrent := loadCurrentPriceState(ctx, db, state); errCurrent != nil {
		return nil, errCurrent
	}
	if errDaily := loadDailyPriceState(ctx, db, state); errDaily != nil {
		return nil, errDaily
	}

	return state, nil
}

func loadCurrentPriceState(ctx context.Context, db priceQueryer, state *appstate.PriceState) error {
	instruments := builder.NewTable("instruments")
	sources := builder.NewTable("instrument_price_sources")
	prices := builder.NewTable("instrument_prices")

	query := builder.NewSelect()
	query.From(prices)
	query.LeftJoin(sources, builder.OnEq{Table1: prices, Column1: "price_source_id", Table2: sources, Column2: "id"})
	query.LeftJoin(instruments, builder.OnEq{Table1: sources, Column1: "instrument_id", Table2: instruments, Column2: "id"})
	query.Column(
		builder.ColumnName{Table: prices, Name: "price_source_id"},
		builder.ColumnName{Table: sources, Name: "instrument_id"},
		builder.ColumnName{Table: instruments, Name: "asset_type"},
		builder.ColumnName{Table: instruments, Name: "isin"},
		builder.ColumnName{Table: instruments, Name: "name"},
		builder.ColumnName{Table: sources, Name: "provider"},
		builder.ColumnName{Table: sources, Name: "provider_symbol"},
		builder.ColumnName{Table: prices, Name: "unit_value"},
		builder.ColumnName{Table: prices, Name: "currency"},
		builder.ColumnName{Table: prices, Name: "priced_at"},
		builder.ColumnName{Table: prices, Name: "fetched_at"},
	)
	query.Where(builder.WhereEq{Table: sources, Column: "enabled", Value: true})
	query.Order(builder.Order{Table: sources, Column: "instrument_id"})

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return fmt.Errorf("build current price state query: %w", errBuild)
	}

	rows, errQuery := db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return fmt.Errorf("query current price state: %w", errQuery)
	}
	defer rows.Close()

	stored, errCollect := pgx.CollectRows(rows, pgx.RowToStructByNameLax[storedPriceState])
	if errCollect != nil {
		return fmt.Errorf("collect current price state: %w", errCollect)
	}

	for _, item := range stored {
		state.Sources[item.PriceSourceID] = instrumentPrice(item)
	}

	return nil
}

func loadDailyPriceState(ctx context.Context, db priceQueryer, state *appstate.PriceState) error {
	instruments := builder.NewTable("instruments")
	sources := builder.NewTable("instrument_price_sources")
	dailyPrices := builder.NewTable("instrument_daily_prices")

	query := builder.NewSelect()
	query.From(dailyPrices)
	query.LeftJoin(sources, builder.OnEq{Table1: dailyPrices, Column1: "price_source_id", Table2: sources, Column2: "id"})
	query.LeftJoin(instruments, builder.OnEq{Table1: sources, Column1: "instrument_id", Table2: instruments, Column2: "id"})
	query.Column(
		builder.ColumnName{Table: dailyPrices, Name: "price_source_id"},
		builder.ColumnName{Table: sources, Name: "instrument_id"},
		builder.ColumnName{Table: instruments, Name: "asset_type"},
		builder.ColumnName{Table: instruments, Name: "isin"},
		builder.ColumnName{Table: instruments, Name: "name"},
		builder.ColumnName{Table: sources, Name: "provider"},
		builder.ColumnName{Table: sources, Name: "provider_symbol"},
		builder.ColumnName{Table: dailyPrices, Name: "price_date"},
		builder.ColumnName{Table: dailyPrices, Name: "unit_value"},
		builder.ColumnName{Table: dailyPrices, Name: "currency"},
	)
	query.Where(builder.WhereEq{Table: sources, Column: "enabled", Value: true})
	query.Order(
		builder.Order{Table: dailyPrices, Column: "price_source_id"},
		builder.Order{Table: dailyPrices, Column: "price_date"},
	)

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return fmt.Errorf("build daily price state query: %w", errBuild)
	}

	rows, errQuery := db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return fmt.Errorf("query daily price state: %w", errQuery)
	}
	defer rows.Close()

	stored, errCollect := pgx.CollectRows(rows, pgx.RowToStructByNameLax[storedDailyPriceState])
	if errCollect != nil {
		return fmt.Errorf("collect daily price state: %w", errCollect)
	}

	for _, item := range stored {
		series, exists := state.DailyPrices[item.PriceSourceID]
		if !exists {
			series = appstate.InstrumentDailyPriceSeries{
				PriceSourceID:  item.PriceSourceID,
				InstrumentID:   item.InstrumentID,
				AssetType:      item.AssetType,
				ISIN:           item.ISIN,
				Name:           item.Name,
				Provider:       item.Provider,
				ProviderSymbol: item.ProviderSymbol,
				Items:          []appstate.InstrumentDailyPrice{},
			}
		}

		series.Items = append(series.Items, appstate.InstrumentDailyPrice{
			PriceDate: dateonly.UTC(item.PriceDate),
			UnitValue: item.UnitValue,
			Currency:  item.Currency,
		})
		state.DailyPrices[item.PriceSourceID] = series
	}

	return nil
}

func loadInstrumentPrice(ctx context.Context, db priceQueryer, priceSourceID int64) (appstate.InstrumentPrice, error) {
	instruments := builder.NewTable("instruments")
	sources := builder.NewTable("instrument_price_sources")
	prices := builder.NewTable("instrument_prices")

	query := builder.NewSelect()
	query.From(prices)
	query.LeftJoin(sources, builder.OnEq{Table1: prices, Column1: "price_source_id", Table2: sources, Column2: "id"})
	query.LeftJoin(instruments, builder.OnEq{Table1: sources, Column1: "instrument_id", Table2: instruments, Column2: "id"})
	query.Column(
		builder.ColumnName{Table: prices, Name: "price_source_id"},
		builder.ColumnName{Table: sources, Name: "instrument_id"},
		builder.ColumnName{Table: instruments, Name: "asset_type"},
		builder.ColumnName{Table: instruments, Name: "isin"},
		builder.ColumnName{Table: instruments, Name: "name"},
		builder.ColumnName{Table: sources, Name: "provider"},
		builder.ColumnName{Table: sources, Name: "provider_symbol"},
		builder.ColumnName{Table: prices, Name: "unit_value"},
		builder.ColumnName{Table: prices, Name: "currency"},
		builder.ColumnName{Table: prices, Name: "priced_at"},
		builder.ColumnName{Table: prices, Name: "fetched_at"},
	)
	query.Where(builder.WhereEq{Table: prices, Column: "price_source_id", Value: priceSourceID})
	query.Limit(1)

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return appstate.InstrumentPrice{}, fmt.Errorf("build instrument price query: %w", errBuild)
	}

	rows, errQuery := db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return appstate.InstrumentPrice{}, fmt.Errorf("query instrument price: %w", errQuery)
	}
	defer rows.Close()

	stored, errCollect := pgx.CollectOneRow(rows, pgx.RowToStructByNameLax[storedPriceState])
	if errCollect != nil {
		return appstate.InstrumentPrice{}, fmt.Errorf("collect instrument price: %w", errCollect)
	}

	return instrumentPrice(stored), nil
}

func instrumentPrice(item storedPriceState) appstate.InstrumentPrice {
	return appstate.InstrumentPrice{
		PriceSourceID:  item.PriceSourceID,
		InstrumentID:   item.InstrumentID,
		AssetType:      item.AssetType,
		ISIN:           item.ISIN,
		Name:           item.Name,
		Provider:       item.Provider,
		ProviderSymbol: item.ProviderSymbol,
		UnitValue:      item.UnitValue,
		Currency:       item.Currency,
		PricedAt:       item.PricedAt.UTC(),
		FetchedAt:      item.FetchedAt.UTC(),
	}
}
