package scheduler

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xloss/go-builder"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{
		db: db,
	}
}

func (r *Repository) LatestFinishedRun(ctx context.Context, jobKey string) (*JobRun, error) {
	table := builder.NewTable("scheduler_job_runs")

	query := builder.NewSelect()
	query.From(table)
	query.Column(
		builder.ColumnName{Table: table, Name: "id"},
		builder.ColumnName{Table: table, Name: "job_key"},
		builder.ColumnName{Table: table, Name: "run_source"},
		builder.ColumnName{Table: table, Name: "status"},
		builder.ColumnName{Table: table, Name: "summary"},
		builder.ColumnName{Table: table, Name: "error"},
		builder.ColumnName{Table: table, Name: "started_at"},
		builder.ColumnName{Table: table, Name: "finished_at"},
	)
	query.Where(
		builder.WhereAnd{
			List: []builder.Where{
				builder.WhereEq{Table: table, Column: "job_key", Value: jobKey},
				builder.WhereOr{
					List: []builder.Where{
						builder.WhereEq{Table: table, Column: "status", Value: RunStatusCompleted},
						builder.WhereEq{Table: table, Column: "status", Value: RunStatusFailed},
						builder.WhereEq{Table: table, Column: "status", Value: RunStatusNoop},
					},
				},
			},
		},
	)
	query.Order(builder.Order{Table: table, Column: "started_at", Desc: true})
	query.Order(builder.Order{Table: table, Column: "id", Desc: true})
	query.Limit(1)

	sql, binds, errBuildSQL := query.Get()
	if errBuildSQL != nil {
		return nil, fmt.Errorf("build latest finished scheduler run query: %w", errBuildSQL)
	}

	rows, errQuery := r.db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return nil, fmt.Errorf("query latest finished scheduler run: %w", errQuery)
	}
	defer rows.Close()

	result, errCollect := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByNameLax[JobRun])
	if errCollect != nil {
		if errors.Is(errCollect, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, fmt.Errorf("collect latest finished scheduler run: %w", errCollect)
	}

	result.StartedAt = result.StartedAt.UTC()

	if result.FinishedAt != nil {
		result.FinishedAt = new(result.FinishedAt.UTC())
	}

	return result, nil
}

func (r *Repository) CreateRun(ctx context.Context, jobKey string, runSource string) (int64, error) {
	table := builder.NewTable("scheduler_job_runs")

	query := builder.NewInsert(table)
	query.Value("job_key", jobKey)
	query.Value("run_source", runSource)
	query.Value("status", RunStatusRunning)
	query.Return(builder.ColumnName{Table: table, Name: "id"})

	sql, binds, errBuildSQL := query.Get()
	if errBuildSQL != nil {
		return 0, fmt.Errorf("build create scheduler job run query: %w", errBuildSQL)
	}

	rows, errQuery := r.db.Query(ctx, sql, pgx.NamedArgs(binds))
	if errQuery != nil {
		return 0, fmt.Errorf("create scheduler job run: %w", errQuery)
	}
	defer rows.Close()

	id, errCollect := pgx.CollectOneRow(rows, pgx.RowTo[int64])
	if errCollect != nil {
		return 0, fmt.Errorf("collect scheduler job run id: %w", errCollect)
	}

	return id, nil
}

func (r *Repository) FinishRun(
	ctx context.Context,
	id int64,
	status string,
	messages []map[string]any,
	summary map[string]any,
	errorText string,
) error {
	table := builder.NewTable("scheduler_job_runs")

	if summary == nil {
		summary = map[string]any{}
	}

	query := builder.NewUpdate(table)
	query.Set("status", status)
	query.Set("message", messages)
	query.Set("summary", summary)
	query.Set("error", errorText)
	query.SetNow("finished_at")
	query.Where(builder.WhereEq{Table: table, Column: "id", Value: id})

	sql, binds, errBuildSQL := query.Get()
	if errBuildSQL != nil {
		return fmt.Errorf("build finish scheduler job run query: %w", errBuildSQL)
	}

	tag, errExec := r.db.Exec(ctx, sql, pgx.NamedArgs(binds))
	if errExec != nil {
		return fmt.Errorf("finish scheduler job run: %w", errExec)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("finish scheduler job run: run %d not found", id)
	}

	return nil
}

func (r *Repository) UpdateRunMessages(ctx context.Context, id int64, messages []map[string]any) error {
	table := builder.NewTable("scheduler_job_runs")

	query := builder.NewUpdate(table)
	query.Set("message", messages)
	query.Where(builder.WhereEq{Table: table, Column: "id", Value: id})

	sql, binds, errBuildSQL := query.Get()
	if errBuildSQL != nil {
		return fmt.Errorf("build update scheduler job run messages query: %w", errBuildSQL)
	}

	if _, errExec := r.db.Exec(ctx, sql, pgx.NamedArgs(binds)); errExec != nil {
		return fmt.Errorf("update scheduler job run messages: %w", errExec)
	}

	return nil
}
