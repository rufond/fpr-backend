package fx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xloss/go-builder"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/decimal"
)

type Repository struct {
	db *pgxpool.Pool
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type storedRate struct {
	BaseCurrency  string    `db:"base_currency"`
	QuoteCurrency string    `db:"quote_currency"`
	Provider      string    `db:"provider"`
	Rate          string    `db:"rate"`
	PricedAt      time.Time `db:"priced_at"`
	FetchedAt     time.Time `db:"fetched_at"`
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) LoadState(ctx context.Context) (*appstate.FXState, error) {
	return loadFXState(ctx, r.db)
}

func (r *Repository) ApplyRate(
	ctx context.Context,
	rate SourceRate,
	fetchedAt time.Time,
) (bool, bool, *appstate.FXState, appstate.FXRate, error) {
	tx, errBegin := r.db.Begin(ctx)
	if errBegin != nil {
		return false, false, nil, appstate.FXRate{}, fmt.Errorf("begin FX rate transaction: %w", errBegin)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	stored, errStored := loadRate(ctx, tx, rate.BaseCurrency, rate.QuoteCurrency)
	if errStored != nil && !errors.Is(errStored, pgx.ErrNoRows) {
		return false, false, nil, appstate.FXRate{}, errStored
	}

	if stored != nil && rate.PricedAt.Before(stored.PricedAt) {
		current := appStateRate(*stored)
		if errCommit := tx.Commit(ctx); errCommit != nil {
			return false, false, nil, appstate.FXRate{}, fmt.Errorf("commit stale FX rate transaction: %w", errCommit)
		}
		return false, true, nil, current, nil
	}

	if stored != nil &&
		stored.Provider == rate.Provider &&
		decimal.Equal(stored.Rate, rate.Rate) &&
		stored.PricedAt.Equal(rate.PricedAt) {
		current := appStateRate(*stored)
		if errCommit := tx.Commit(ctx); errCommit != nil {
			return false, false, nil, appstate.FXRate{}, fmt.Errorf("commit unchanged FX rate transaction: %w", errCommit)
		}
		return false, false, nil, current, nil
	}

	if stored == nil {
		if errInsert := insertRate(ctx, tx, rate, fetchedAt); errInsert != nil {
			return false, false, nil, appstate.FXRate{}, errInsert
		}
	} else if errUpdate := updateRate(ctx, tx, rate, fetchedAt); errUpdate != nil {
		return false, false, nil, appstate.FXRate{}, errUpdate
	}

	state, errState := loadFXState(ctx, tx)
	if errState != nil {
		return false, false, nil, appstate.FXRate{}, fmt.Errorf("build FX state after rate update: %w", errState)
	}

	current, ok := state.Rates[appstate.FXPair{
		BaseCurrency:  rate.BaseCurrency,
		QuoteCurrency: rate.QuoteCurrency,
	}]
	if !ok {
		return false, false, nil, appstate.FXRate{}, fmt.Errorf("updated FX rate is missing from rebuilt state")
	}

	if errCommit := tx.Commit(ctx); errCommit != nil {
		return false, false, nil, appstate.FXRate{}, fmt.Errorf("commit FX rate transaction: %w", errCommit)
	}

	return true, false, state, current, nil
}

func loadFXState(ctx context.Context, db queryer) (*appstate.FXState, error) {
	table := builder.NewTable("fx_rates")
	query := builder.NewSelect()
	query.From(table)
	query.Column(
		builder.ColumnName{Table: table, Name: "base_currency"},
		builder.ColumnName{Table: table, Name: "quote_currency"},
		builder.ColumnName{Table: table, Name: "provider"},
		builder.ColumnName{Table: table, Name: "rate"},
		builder.ColumnName{Table: table, Name: "priced_at"},
		builder.ColumnName{Table: table, Name: "fetched_at"},
	)

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build FX state query: %w", errBuild)
	}

	rows, errQuery := db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return nil, fmt.Errorf("query FX state: %w", errQuery)
	}

	stored, errCollect := pgx.CollectRows(rows, pgx.RowToStructByNameLax[storedRate])
	if errCollect != nil {
		return nil, fmt.Errorf("collect FX state: %w", errCollect)
	}

	state := &appstate.FXState{Rates: make(map[appstate.FXPair]appstate.FXRate, len(stored))}
	for _, item := range stored {
		rate := appStateRate(item)
		state.Rates[appstate.FXPair{
			BaseCurrency:  rate.BaseCurrency,
			QuoteCurrency: rate.QuoteCurrency,
		}] = rate
	}

	return state, nil
}

func loadRate(ctx context.Context, db queryer, baseCurrency string, quoteCurrency string) (*storedRate, error) {
	table := builder.NewTable("fx_rates")
	query := builder.NewSelect()
	query.From(table)
	query.Column(
		builder.ColumnName{Table: table, Name: "base_currency"},
		builder.ColumnName{Table: table, Name: "quote_currency"},
		builder.ColumnName{Table: table, Name: "provider"},
		builder.ColumnName{Table: table, Name: "rate"},
		builder.ColumnName{Table: table, Name: "priced_at"},
		builder.ColumnName{Table: table, Name: "fetched_at"},
	)
	query.Where(builder.WhereAnd{List: []builder.Where{
		builder.WhereEq{Table: table, Column: "base_currency", Value: baseCurrency},
		builder.WhereEq{Table: table, Column: "quote_currency", Value: quoteCurrency},
	}})
	query.Limit(1)

	sql, binds, errBuild := query.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build FX rate query: %w", errBuild)
	}

	rows, errQuery := db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return nil, fmt.Errorf("query FX rate: %w", errQuery)
	}

	item, errCollect := pgx.CollectOneRow(rows, pgx.RowToStructByNameLax[storedRate])
	if errCollect != nil {
		return nil, errCollect
	}

	return &item, nil
}

func insertRate(ctx context.Context, tx pgx.Tx, rate SourceRate, fetchedAt time.Time) error {
	table := builder.NewTable("fx_rates")
	insert := builder.NewInsert(table)
	insert.Value("base_currency", rate.BaseCurrency)
	insert.Value("quote_currency", rate.QuoteCurrency)
	insert.Value("provider", rate.Provider)
	insert.Value("rate", rate.Rate)
	insert.Value("priced_at", rate.PricedAt)
	insert.Value("fetched_at", fetchedAt)

	sql, binds, errBuild := insert.Get()
	if errBuild != nil {
		return fmt.Errorf("build insert FX rate query: %w", errBuild)
	}
	if _, errExec := tx.Exec(ctx, sql, pgx.NamedArgs(binds)); errExec != nil {
		return fmt.Errorf("insert FX rate: %w", errExec)
	}

	return nil
}

func updateRate(ctx context.Context, tx pgx.Tx, rate SourceRate, fetchedAt time.Time) error {
	table := builder.NewTable("fx_rates")
	update := builder.NewUpdate(table)
	update.Set("provider", rate.Provider)
	update.Set("rate", rate.Rate)
	update.Set("priced_at", rate.PricedAt)
	update.Set("fetched_at", fetchedAt)
	update.Where(builder.WhereAnd{List: []builder.Where{
		builder.WhereEq{Table: table, Column: "base_currency", Value: rate.BaseCurrency},
		builder.WhereEq{Table: table, Column: "quote_currency", Value: rate.QuoteCurrency},
	}})

	sql, binds, errBuild := update.Get()
	if errBuild != nil {
		return fmt.Errorf("build update FX rate query: %w", errBuild)
	}
	if _, errExec := tx.Exec(ctx, sql, pgx.NamedArgs(binds)); errExec != nil {
		return fmt.Errorf("update FX rate: %w", errExec)
	}

	return nil
}

func appStateRate(item storedRate) appstate.FXRate {
	return appstate.FXRate{
		BaseCurrency:  item.BaseCurrency,
		QuoteCurrency: item.QuoteCurrency,
		Provider:      item.Provider,
		Rate:          item.Rate,
		PricedAt:      item.PricedAt.UTC(),
		FetchedAt:     item.FetchedAt.UTC(),
	}
}
