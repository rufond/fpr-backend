package fund

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/xloss/go-builder"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/dateonly"
)

type storedUnitCount struct {
	AsOfDate         time.Time `db:"as_of_date"`
	UnitsOutstanding string    `db:"units_outstanding"`
	SourceURL        string    `db:"source_url"`
}

type reportedUnitCount struct {
	AsOfDate time.Time `db:"as_of_date"`
	Report   struct {
		Units     string `json:"units"`
		SourceURL string `json:"source_url"`
	} `db:"report"`
}

// LoadUnitCount finds the latest count as of the given date, preferring a PDF
// report when it and a manually entered count share the same date.
func LoadUnitCount(ctx context.Context, db queryer, asOfDate time.Time) (*appstate.FundUnitCount, error) {
	asOfDate = dateonly.UTC(asOfDate)
	manualTable := builder.NewTable("fund_unit_counts")
	manualQuery := builder.NewSelect()
	manualQuery.From(manualTable)
	manualQuery.Column(
		builder.ColumnName{Table: manualTable, Name: "as_of_date"},
		builder.ColumnName{Table: manualTable, Name: "units_outstanding"},
		builder.ColumnName{Table: manualTable, Name: "source_url"},
	)
	manualQuery.Where(builder.WhereLessEq{Table: manualTable, Column: "as_of_date", Value: asOfDate})
	manualQuery.Order(builder.Order{Table: manualTable, Column: "as_of_date", Desc: true})
	manualQuery.Limit(1)

	manualSQL, manualBinds, errBuild := manualQuery.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build manual unit count query: %w", errBuild)
	}

	manualRows, errQuery := db.Query(ctx, manualSQL, pgx.NamedArgs(manualBinds))
	if errQuery != nil {
		return nil, fmt.Errorf("query manual unit count: %w", errQuery)
	}

	manual, errCollect := pgx.CollectOneRow(manualRows, pgx.RowToStructByName[storedUnitCount])
	if errCollect != nil && !errors.Is(errCollect, pgx.ErrNoRows) {
		return nil, fmt.Errorf("collect manual unit count: %w", errCollect)
	}

	manualExists := errCollect == nil

	reportsTable := builder.NewTable("fund_portfolio_reports")
	dates := []builder.Where{builder.WhereLessEq{Table: reportsTable, Column: "as_of_date", Value: asOfDate}}
	if manualExists {
		manual.AsOfDate = dateonly.UTC(manual.AsOfDate)
		dates = append(dates, builder.WhereMoreEq{Table: reportsTable, Column: "as_of_date", Value: manual.AsOfDate})
	}
	latestQuery := builder.NewSelect()
	latestQuery.From(reportsTable)
	latestQuery.Column(
		builder.ColumnName{Table: reportsTable, Name: "as_of_date"},
		builder.ColumnName{Table: reportsTable, Name: "imported_at"},
	)
	latestQuery.Where(builder.WhereAnd{List: dates})
	latestQuery.Order(
		builder.Order{Table: reportsTable, Column: "as_of_date", Desc: true},
		builder.Order{Table: reportsTable, Column: "imported_at", Desc: true},
	)
	latestQuery.Limit(1)

	latest := builder.NewTableSub(latestQuery)
	reportsQuery := builder.NewSelect()
	reportsQuery.From(latest)
	reportsQuery.Column(
		builder.ColumnName{Table: latest, Name: "as_of_date"},
		builder.ColumnName{Table: reportsTable, Name: "report"},
	)
	reportsQuery.InnerJoin(reportsTable, builder.OnAnd{List: []builder.On{
		builder.OnEq{Table1: reportsTable, Column1: "as_of_date", Table2: latest, Column2: "as_of_date"},
		builder.OnEq{Table1: reportsTable, Column1: "imported_at", Table2: latest, Column2: "imported_at"},
	}})

	reportsSQL, reportsBinds, errBuild := reportsQuery.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build reported unit count query: %w", errBuild)
	}

	reportRows, errQuery := db.Query(ctx, reportsSQL, pgx.NamedArgs(reportsBinds))
	if errQuery != nil {
		return nil, fmt.Errorf("query reported unit count: %w", errQuery)
	}

	counts, errCollect := pgx.CollectRows(reportRows, pgx.RowToStructByName[reportedUnitCount])
	if errCollect != nil {
		return nil, fmt.Errorf("collect reported unit count: %w", errCollect)
	}

	var count *appstate.FundUnitCount
	for _, item := range counts {
		if count == nil || item.Report.SourceURL < count.SourceURL {
			count = &appstate.FundUnitCount{
				AsOfDate:         dateonly.UTC(item.AsOfDate),
				UnitsOutstanding: item.Report.Units,
				SourceURL:        item.Report.SourceURL,
			}
		}
	}

	if count == nil && manualExists {
		count = &appstate.FundUnitCount{
			AsOfDate:         manual.AsOfDate,
			UnitsOutstanding: manual.UnitsOutstanding,
			SourceURL:        manual.SourceURL,
		}
	}

	return count, nil
}
