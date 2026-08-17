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

type storedPricePointState struct {
	PriceSourceID int64 `db:"price_source_id"`
	InstrumentID  int64 `db:"instrument_id"`

	AssetType string `db:"asset_type"`
	ISIN      string `db:"isin"`
	Name      string `db:"name"`

	Provider       string `db:"provider"`
	ProviderSymbol string `db:"provider_symbol"`

	UnitValue  string    `db:"unit_value"`
	Currency   string    `db:"currency"`
	PricedAt   time.Time `db:"priced_at"`
	ObservedAt time.Time `db:"observed_at"`
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

func (r *Repository) EnsureFundUnitMOEXSource(ctx context.Context) (int64, error) {
	tx, errBegin := r.db.Begin(ctx)
	if errBegin != nil {
		return 0, fmt.Errorf("begin ensure fund unit MOEX price source transaction: %w", errBegin)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	priceSourceID, errEnsure := ensureFundUnitMOEXSource(ctx, tx)
	if errEnsure != nil {
		return 0, errEnsure
	}

	if errCommit := tx.Commit(ctx); errCommit != nil {
		return 0, fmt.Errorf("commit ensure fund unit MOEX price source transaction: %w", errCommit)
	}

	return priceSourceID, nil
}

func (r *Repository) LoadState(ctx context.Context) (*appstate.PriceState, error) {
	return loadPriceState(ctx, r.db)
}

func (r *Repository) PriceSources(ctx context.Context, provider string) ([]priceSource, error) {
	sources := builder.NewTable("instrument_price_sources")

	query := builder.NewSelect()
	query.From(sources)
	query.Column(
		builder.ColumnName{Table: sources, Name: "id", Alias: "price_source_id"},
		builder.ColumnName{Table: sources, Name: "instrument_id"},
		builder.ColumnName{Table: sources, Name: "provider_symbol"},
	)
	query.Where(builder.WhereAnd{List: []builder.Where{
		builder.WhereEq{Table: sources, Column: "provider", Value: provider},
		builder.WhereEq{Table: sources, Column: "enabled", Value: true},
	}})
	query.Order(builder.Order{Table: sources, Column: "instrument_id"})

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build price sources query: %w", errBuild)
	}

	rows, errQuery := r.db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return nil, fmt.Errorf("query price sources: %w", errQuery)
	}
	defer rows.Close()

	stored, errCollect := pgx.CollectRows(rows, pgx.RowToStructByNameLax[priceSource])
	if errCollect != nil {
		return nil, fmt.Errorf("collect price sources: %w", errCollect)
	}

	return stored, nil
}

func (r *Repository) MappedInstrumentIDs(ctx context.Context) (map[int64]struct{}, error) {
	sources := builder.NewTable("instrument_price_sources")

	query := builder.NewSelect()
	query.From(sources)
	query.Column(builder.ColumnName{Table: sources, Name: "instrument_id"})
	query.Order(builder.Order{Table: sources, Column: "instrument_id"})

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build mapped instruments query: %w", errBuild)
	}

	rows, errQuery := r.db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return nil, fmt.Errorf("query mapped instruments: %w", errQuery)
	}
	defer rows.Close()

	instrumentIDs, errCollect := pgx.CollectRows(rows, pgx.RowTo[int64])
	if errCollect != nil {
		return nil, fmt.Errorf("collect mapped instruments: %w", errCollect)
	}

	result := make(map[int64]struct{}, len(instrumentIDs))
	for _, instrumentID := range instrumentIDs {
		result[instrumentID] = struct{}{}
	}

	return result, nil
}

func (r *Repository) AllPriceSources(ctx context.Context) ([]storedPriceSource, error) {
	table := builder.NewTable("instrument_price_sources")

	query := builder.NewSelect()
	query.From(table)
	query.Column(
		builder.ColumnName{Table: table, Name: "id"},
		builder.ColumnName{Table: table, Name: "instrument_id"},
		builder.ColumnName{Table: table, Name: "provider"},
		builder.ColumnName{Table: table, Name: "provider_symbol"},
		builder.ColumnName{Table: table, Name: "enabled"},
	)
	query.Order(
		builder.Order{Table: table, Column: "instrument_id"},
		builder.Order{Table: table, Column: "provider"},
	)

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build all price sources query: %w", errBuild)
	}

	rows, errQuery := r.db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return nil, fmt.Errorf("query all price sources: %w", errQuery)
	}
	defer rows.Close()

	items, errCollect := pgx.CollectRows(rows, pgx.RowToStructByNameLax[storedPriceSource])
	if errCollect != nil {
		return nil, fmt.Errorf("collect all price sources: %w", errCollect)
	}

	return items, nil
}

func (r *Repository) SetPriceSource(
	ctx context.Context,
	instrumentID int64,
	provider string,
	providerSymbol string,
	enabled bool,
) (storedPriceSource, bool, *appstate.PriceState, error) {
	tx, errBegin := r.db.Begin(ctx)
	if errBegin != nil {
		return storedPriceSource{}, false, nil, fmt.Errorf("begin set price source transaction: %w", errBegin)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	table := builder.NewTable("instrument_price_sources")
	query := builder.NewSelect()
	query.From(table)
	query.Column(
		builder.ColumnName{Table: table, Name: "id"},
		builder.ColumnName{Table: table, Name: "instrument_id"},
		builder.ColumnName{Table: table, Name: "provider"},
		builder.ColumnName{Table: table, Name: "provider_symbol"},
		builder.ColumnName{Table: table, Name: "enabled"},
	)
	query.Where(builder.WhereEq{Table: table, Column: "instrument_id", Value: instrumentID})
	query.Order(builder.Order{Table: table, Column: "provider"})

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return storedPriceSource{}, false, nil, fmt.Errorf("build instrument price sources query: %w", errBuild)
	}

	rows, errQuery := tx.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return storedPriceSource{}, false, nil, fmt.Errorf("query instrument price sources: %w", errQuery)
	}

	sources, errCollect := pgx.CollectRows(rows, pgx.RowToStructByNameLax[storedPriceSource])
	if errCollect != nil {
		return storedPriceSource{}, false, nil, fmt.Errorf("collect instrument price sources: %w", errCollect)
	}

	var current *storedPriceSource
	changed := false
	for index := range sources {
		source := &sources[index]
		if source.Provider == provider {
			current = source
			if source.ProviderSymbol != providerSymbol || source.Enabled != enabled {
				changed = true
			}
			continue
		}

		if enabled && source.Enabled {
			changed = true

			update := builder.NewUpdate(table)
			update.Set("enabled", false)
			update.SetNow("updated_at")
			update.Where(builder.WhereEq{Table: table, Column: "id", Value: source.ID})

			sqlUpdate, bindsUpdate, errBuildUpdate := update.Get()
			if errBuildUpdate != nil {
				return storedPriceSource{}, false, nil, fmt.Errorf("build disable price source %d: %w", source.ID, errBuildUpdate)
			}
			if _, errExec := tx.Exec(ctx, sqlUpdate, pgx.NamedArgs(bindsUpdate)); errExec != nil {
				return storedPriceSource{}, false, nil, fmt.Errorf("disable price source %d: %w", source.ID, errExec)
			}
		}
	}

	var selected storedPriceSource
	if current == nil {
		changed = true

		insert := builder.NewInsert(table)
		insert.Value("instrument_id", instrumentID)
		insert.Value("provider", provider)
		insert.Value("provider_symbol", providerSymbol)
		insert.Value("enabled", enabled)
		insert.Return(builder.ColumnName{Table: table, Name: "id"})

		sqlInsert, bindsInsert, errBuildInsert := insert.Get()
		if errBuildInsert != nil {
			return storedPriceSource{}, false, nil, fmt.Errorf("build price source insert: %w", errBuildInsert)
		}

		rowsInsert, errQueryInsert := tx.Query(ctx, sqlInsert, pgx.NamedArgs(bindsInsert))
		if errQueryInsert != nil {
			return storedPriceSource{}, false, nil, fmt.Errorf("insert price source: %w", errQueryInsert)
		}

		id, errCollectInsert := pgx.CollectOneRow(rowsInsert, pgx.RowTo[int64])
		if errCollectInsert != nil {
			return storedPriceSource{}, false, nil, fmt.Errorf("collect inserted price source: %w", errCollectInsert)
		}

		selected = storedPriceSource{
			ID:             id,
			InstrumentID:   instrumentID,
			Provider:       provider,
			ProviderSymbol: providerSymbol,
			Enabled:        enabled,
		}
	} else {
		selected = *current
		symbolChanged := selected.ProviderSymbol != providerSymbol
		if symbolChanged || selected.Enabled != enabled {
			update := builder.NewUpdate(table)
			update.Set("provider_symbol", providerSymbol)
			update.Set("enabled", enabled)
			update.SetNow("updated_at")
			update.Where(builder.WhereEq{Table: table, Column: "id", Value: selected.ID})

			sqlUpdate, bindsUpdate, errBuildUpdate := update.Get()
			if errBuildUpdate != nil {
				return storedPriceSource{}, false, nil, fmt.Errorf("build price source update: %w", errBuildUpdate)
			}
			if _, errExec := tx.Exec(ctx, sqlUpdate, pgx.NamedArgs(bindsUpdate)); errExec != nil {
				return storedPriceSource{}, false, nil, fmt.Errorf("update price source: %w", errExec)
			}

			if symbolChanged {
				for _, tableName := range []string{
					"fund_snapshot_price_baselines",
					"instrument_daily_prices",
					"instrument_price_points",
					"instrument_prices",
				} {
					marketTable := builder.NewTable(tableName)
					deleteMarketState := builder.NewDelete(marketTable)
					deleteMarketState.Where(builder.WhereEq{Table: marketTable, Column: "price_source_id", Value: selected.ID})

					sqlDelete, bindsDelete, errBuildDelete := deleteMarketState.Get()
					if errBuildDelete != nil {
						return storedPriceSource{}, false, nil, fmt.Errorf("build %s cleanup for price source %d: %w", tableName, selected.ID, errBuildDelete)
					}
					if _, errExec := tx.Exec(ctx, sqlDelete, pgx.NamedArgs(bindsDelete)); errExec != nil {
						return storedPriceSource{}, false, nil, fmt.Errorf("clean %s for price source %d: %w", tableName, selected.ID, errExec)
					}
				}
			}

			selected.ProviderSymbol = providerSymbol
			selected.Enabled = enabled
		}
	}

	if !changed {
		return selected, false, nil, nil
	}

	priceState, errState := loadPriceState(ctx, tx)
	if errState != nil {
		return storedPriceSource{}, false, nil, fmt.Errorf("load price state after source update: %w", errState)
	}

	if errCommit := tx.Commit(ctx); errCommit != nil {
		return storedPriceSource{}, false, nil, fmt.Errorf("commit set price source transaction: %w", errCommit)
	}

	return selected, true, priceState, nil
}

func (r *Repository) CreatePriceSources(ctx context.Context, provider string, items []sourceMapping) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}

	tx, errBegin := r.db.Begin(ctx)
	if errBegin != nil {
		return 0, fmt.Errorf("begin price source discovery transaction: %w", errBegin)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	table := builder.NewTable("instrument_price_sources")
	created := 0

	for _, item := range items {
		existing := builder.NewSelect()
		existing.From(table)
		existing.Column(builder.ColumnName{Table: table, Name: "id"})
		existing.Where(builder.WhereEq{Table: table, Column: "instrument_id", Value: item.InstrumentID})
		existing.Limit(1)

		sqlExisting, bindsExisting, errBuildExisting := existing.Get()
		if errBuildExisting != nil {
			return 0, fmt.Errorf("build existing price source query for instrument %d: %w", item.InstrumentID, errBuildExisting)
		}

		rowsExisting, errQueryExisting := tx.Query(ctx, sqlExisting, pgx.NamedArgs(bindsExisting))
		if errQueryExisting != nil {
			return 0, fmt.Errorf("query existing price source for instrument %d: %w", item.InstrumentID, errQueryExisting)
		}

		_, errCollectExisting := pgx.CollectOneRow(rowsExisting, pgx.RowTo[int64])
		if errCollectExisting == nil {
			continue
		}
		if !errors.Is(errCollectExisting, pgx.ErrNoRows) {
			return 0, fmt.Errorf("collect existing price source for instrument %d: %w", item.InstrumentID, errCollectExisting)
		}

		query := builder.NewInsert(table)
		query.Value("instrument_id", item.InstrumentID)
		query.Value("provider", provider)
		query.Value("provider_symbol", item.ProviderSymbol)
		query.Value("enabled", true)

		sql, binds, errBuild := query.Get()
		if errBuild != nil {
			return 0, fmt.Errorf("build price source insert for instrument %d: %w", item.InstrumentID, errBuild)
		}

		if _, errExec := tx.Exec(ctx, sql, pgx.NamedArgs(binds)); errExec != nil {
			return 0, fmt.Errorf("insert price source for instrument %d: %w", item.InstrumentID, errExec)
		}

		created++
	}

	if errCommit := tx.Commit(ctx); errCommit != nil {
		return 0, fmt.Errorf("commit price source discovery transaction: %w", errCommit)
	}

	return created, nil
}

func (r *Repository) ApplyFundUnitMOEXQuote(
	ctx context.Context,
	priceSourceID int64,
	quote SourceQuote,
	fetchedAt time.Time,
) (bool, bool, appstate.InstrumentPrice, error) {
	tx, errBegin := r.db.Begin(ctx)
	if errBegin != nil {
		return false, false, appstate.InstrumentPrice{}, fmt.Errorf("begin MOEX fund unit quote transaction: %w", errBegin)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	changed, stale, errApply := applyCurrentPriceQuote(ctx, tx, priceSourceID, quote, fetchedAt)
	if errApply != nil {
		return false, false, appstate.InstrumentPrice{}, errApply
	}

	price, errPrice := loadInstrumentPrice(ctx, tx, priceSourceID)
	if errPrice != nil {
		return false, false, appstate.InstrumentPrice{}, errPrice
	}

	if errCommit := tx.Commit(ctx); errCommit != nil {
		return false, false, appstate.InstrumentPrice{}, fmt.Errorf("commit MOEX fund unit quote transaction: %w", errCommit)
	}

	return changed, stale, price, nil
}

func (r *Repository) ApplyQuotes(ctx context.Context, provider string, items []quoteToApply, fetchedAt time.Time) (applyQuotesResult, error) {
	result := applyQuotesResult{ChangedPrices: make([]appstate.InstrumentPrice, 0, len(items))}
	if len(items) == 0 {
		return result, nil
	}

	tx, errBegin := r.db.Begin(ctx)
	if errBegin != nil {
		return applyQuotesResult{}, fmt.Errorf("begin %s prices transaction: %w", provider, errBegin)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, item := range items {
		changed, stale, errApply := applyCurrentPriceQuote(ctx, tx, item.PriceSourceID, item.Quote, fetchedAt)
		if errApply != nil {
			return applyQuotesResult{}, fmt.Errorf("apply %s price source %d: %w", provider, item.PriceSourceID, errApply)
		}

		if stale {
			result.Stale++
			continue
		}

		if !changed {
			result.Unchanged++
			continue
		}

		price, errPrice := loadInstrumentPrice(ctx, tx, item.PriceSourceID)
		if errPrice != nil {
			return applyQuotesResult{}, fmt.Errorf("load %s price source %d after update: %w", provider, item.PriceSourceID, errPrice)
		}

		result.ChangedPrices = append(result.ChangedPrices, price)
	}

	if errCommit := tx.Commit(ctx); errCommit != nil {
		return applyQuotesResult{}, fmt.Errorf("commit %s prices transaction: %w", provider, errCommit)
	}

	return result, nil
}

func (r *Repository) ApplyFundUnitMOEXDailyPrices(
	ctx context.Context,
	priceSourceID int64,
	items []SourceDailyPrice,
) (int, int, *appstate.PriceState, error) {
	tx, errBegin := r.db.Begin(ctx)
	if errBegin != nil {
		return 0, 0, nil, fmt.Errorf("begin MOEX fund unit daily prices transaction: %w", errBegin)
	}
	defer func() { _ = tx.Rollback(ctx) }()

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

	state := &appstate.PriceState{
		Sources:     map[int64]appstate.InstrumentPrice{},
		DailyPrices: map[int64]appstate.InstrumentDailyPriceSeries{},
	}
	if errCurrent := loadCurrentPriceState(ctx, tx, state); errCurrent != nil {
		return 0, 0, nil, fmt.Errorf("build current price state after MOEX fund unit daily prices: %w", errCurrent)
	}
	if errDaily := loadDailyPriceState(ctx, tx, state); errDaily != nil {
		return 0, 0, nil, fmt.Errorf("build daily price state after MOEX fund unit daily prices: %w", errDaily)
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
	query.Column(builder.ColumnName{Table: table, Name: "id"})
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

	storedID, errCollect := pgx.CollectOneRow(rows, pgx.RowTo[int64])
	if errCollect == nil {
		return storedID, nil
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
	query.Column(builder.ColumnName{Table: table, Name: "id"})
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

	storedID, errCollect := pgx.CollectOneRow(rows, pgx.RowTo[int64])
	if errCollect == nil {
		return storedID, nil
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

func applyCurrentPriceQuote(ctx context.Context, tx pgx.Tx, priceSourceID int64, quote SourceQuote, fetchedAt time.Time) (bool, bool, error) {
	stored, errStored := loadCurrentPrice(ctx, tx, priceSourceID)
	if errStored != nil && !errors.Is(errStored, pgx.ErrNoRows) {
		return false, false, errStored
	}

	if stored != nil && quote.PricedAt.Before(stored.PricedAt) {
		return false, true, nil
	}

	if stored != nil && decimal.Equal(stored.UnitValue, quote.UnitValue) && stored.Currency == quote.Currency && stored.PricedAt.Equal(quote.PricedAt) {
		return false, false, nil
	}

	if errPersist := persistCurrentPrice(ctx, tx, priceSourceID, quote, fetchedAt, stored != nil); errPersist != nil {
		return false, false, errPersist
	}

	if errPoint := insertPricePoint(ctx, tx, priceSourceID, quote, fetchedAt); errPoint != nil {
		return false, false, errPoint
	}

	if errCleanup := deleteOldPricePoints(ctx, tx, priceSourceID, fetchedAt.Add(-pricePointRetention)); errCleanup != nil {
		return false, false, errCleanup
	}

	return true, false, nil
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

func loadPriceState(ctx context.Context, db priceQueryer) (*appstate.PriceState, error) {
	state := &appstate.PriceState{
		Sources:     map[int64]appstate.InstrumentPrice{},
		DailyPrices: map[int64]appstate.InstrumentDailyPriceSeries{},
		Points:      map[int64]appstate.InstrumentPricePointSeries{},
	}

	if errCurrent := loadCurrentPriceState(ctx, db, state); errCurrent != nil {
		return nil, errCurrent
	}
	if errDaily := loadDailyPriceState(ctx, db, state); errDaily != nil {
		return nil, errDaily
	}
	if errPoints := loadPricePointState(ctx, db, state); errPoints != nil {
		return nil, errPoints
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

func loadPricePointState(ctx context.Context, db priceQueryer, state *appstate.PriceState) error {
	instruments := builder.NewTable("instruments")
	sources := builder.NewTable("instrument_price_sources")
	points := builder.NewTable("instrument_price_points")

	query := builder.NewSelect()
	query.From(points)
	query.LeftJoin(sources, builder.OnEq{Table1: points, Column1: "price_source_id", Table2: sources, Column2: "id"})
	query.LeftJoin(instruments, builder.OnEq{Table1: sources, Column1: "instrument_id", Table2: instruments, Column2: "id"})
	query.Column(
		builder.ColumnName{Table: points, Name: "price_source_id"},
		builder.ColumnName{Table: sources, Name: "instrument_id"},
		builder.ColumnName{Table: instruments, Name: "asset_type"},
		builder.ColumnName{Table: instruments, Name: "isin"},
		builder.ColumnName{Table: instruments, Name: "name"},
		builder.ColumnName{Table: sources, Name: "provider"},
		builder.ColumnName{Table: sources, Name: "provider_symbol"},
		builder.ColumnName{Table: points, Name: "unit_value"},
		builder.ColumnName{Table: points, Name: "currency"},
		builder.ColumnName{Table: points, Name: "priced_at"},
		builder.ColumnName{Table: points, Name: "observed_at"},
	)
	query.Where(builder.WhereEq{Table: sources, Column: "enabled", Value: true})
	query.Order(
		builder.Order{Table: points, Column: "price_source_id"},
		builder.Order{Table: points, Column: "observed_at"},
		builder.Order{Table: points, Column: "priced_at"},
	)

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return fmt.Errorf("build price point state query: %w", errBuild)
	}

	rows, errQuery := db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return fmt.Errorf("query price point state: %w", errQuery)
	}
	defer rows.Close()

	stored, errCollect := pgx.CollectRows(rows, pgx.RowToStructByNameLax[storedPricePointState])
	if errCollect != nil {
		return fmt.Errorf("collect price point state: %w", errCollect)
	}

	for _, item := range stored {
		series, exists := state.Points[item.PriceSourceID]
		if !exists {
			series = appstate.InstrumentPricePointSeries{
				PriceSourceID:  item.PriceSourceID,
				InstrumentID:   item.InstrumentID,
				AssetType:      item.AssetType,
				ISIN:           item.ISIN,
				Name:           item.Name,
				Provider:       item.Provider,
				ProviderSymbol: item.ProviderSymbol,
				Items:          []appstate.InstrumentPricePoint{},
			}
		}

		series.Items = append(series.Items, appstate.InstrumentPricePoint{
			UnitValue:  item.UnitValue,
			Currency:   item.Currency,
			PricedAt:   item.PricedAt.UTC(),
			ObservedAt: item.ObservedAt.UTC(),
		})
		state.Points[item.PriceSourceID] = series
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
