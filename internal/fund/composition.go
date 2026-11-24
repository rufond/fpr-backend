package fund

import (
	"context"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/xloss/go-builder"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/dateonly"
	"github.com/rufond/fpr-backend/internal/decimal"
)

type compositionVersion struct {
	ID       int64     `db:"id"`
	AsOfDate time.Time `db:"as_of_date"`
}

type compositionRow struct {
	SnapshotID           int64     `db:"snapshot_id"`
	AsOfDate             time.Time `db:"as_of_date"`
	ISIN                 string    `db:"isin"`
	Name                 string    `db:"name"`
	Kind                 string    `db:"kind"`
	Quantity             *string   `db:"quantity"`
	AssetSharePercent    string    `db:"asset_share_percent"`
	AssetShareUpperBound bool      `db:"asset_share_upper_bound"`
	Rows                 int       `db:"row_count"`
}

func loadCompositionHistory(ctx context.Context, db queryer) ([]appstate.FundComposition, error) {
	snapshotsTable := builder.NewTable("fund_snapshots")
	versionsQuery := builder.NewSelect()
	versionsQuery.From(snapshotsTable)
	versionsQuery.Column(
		builder.ColumnName{Table: snapshotsTable, Name: "id"},
		builder.ColumnName{Table: snapshotsTable, Name: "as_of_date"},
	)
	versionsQuery.Order(
		builder.Order{Table: snapshotsTable, Column: "as_of_date"},
		builder.Order{Table: snapshotsTable, Column: "observed_at", Desc: true},
		builder.Order{Table: snapshotsTable, Column: "id", Desc: true},
	)

	versionsSQL, versionsBinds, errBuild := versionsQuery.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build composition versions query: %w", errBuild)
	}

	versionRows, errQuery := db.Query(ctx, versionsSQL, pgx.NamedArgs(versionsBinds))
	if errQuery != nil {
		return nil, fmt.Errorf("query composition versions: %w", errQuery)
	}

	versions, errCollect := pgx.CollectRows(versionRows, pgx.RowToStructByName[compositionVersion])
	if errCollect != nil {
		return nil, fmt.Errorf("collect composition versions: %w", errCollect)
	}

	byDate := make(map[time.Time][]appstate.FundHolding, len(versions))
	datesByID := make(map[int64]time.Time, len(versions))
	for _, version := range versions {
		date := dateonly.UTC(version.AsOfDate)
		if _, exists := byDate[date]; !exists {
			byDate[date] = nil
			datesByID[version.ID] = date
		}
	}

	assetsTable := builder.NewTable("fund_snapshot_assets")
	instrumentsTable := builder.NewTable("instruments")
	assetsQuery := builder.NewSelect()
	assetsQuery.From(assetsTable)
	assetsQuery.InnerJoin(instrumentsTable, builder.OnEq{Table1: assetsTable, Column1: "instrument_id", Table2: instrumentsTable, Column2: "id"})
	assetsQuery.Column(
		builder.ColumnName{Table: assetsTable, Name: "snapshot_id"},
		builder.ColumnName{Table: instrumentsTable, Name: "isin"},
		builder.ColumnName{Table: instrumentsTable, Name: "name"},
		builder.ColumnName{Table: instrumentsTable, Name: "asset_type", Alias: "kind"},
		builder.ColumnName{Table: assetsTable, Name: "quantity"},
		builder.ColumnName{Table: assetsTable, Name: "asset_share_percent"},
		builder.ColumnName{Table: assetsTable, Name: "asset_share_upper_bound"},
	)
	assetsQuery.Where(builder.WhereIn{Table: assetsTable, Column: "snapshot_id", Values: slices.Collect(maps.Keys(datesByID))})
	assetsQuery.Order(
		builder.Order{Table: assetsTable, Column: "snapshot_id"},
		builder.Order{Table: instrumentsTable, Column: "isin"},
	)

	assetsSQL, assetsBinds, errBuild := assetsQuery.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build composition assets query: %w", errBuild)
	}

	assetRows, errQuery := db.Query(ctx, assetsSQL, pgx.NamedArgs(assetsBinds))
	if errQuery != nil {
		return nil, fmt.Errorf("query composition assets: %w", errQuery)
	}

	items, errCollect := pgx.CollectRows(assetRows, pgx.RowToStructByNameLax[compositionRow])
	if errCollect != nil {
		return nil, fmt.Errorf("collect composition assets: %w", errCollect)
	}

	for len(items) > 0 {
		first := items[0]
		end := 1
		for end < len(items) && items[end].SnapshotID == first.SnapshotID && items[end].ISIN == first.ISIN {
			end++
		}

		holding, err := aggregateCompositionRows(items[:end])
		if err != nil {
			return nil, err
		}

		date := datesByID[first.SnapshotID]
		byDate[date] = append(byDate[date], holding)
		items = items[end:]
	}

	archiveTable := builder.NewTable("fund_archived_composition")
	archiveQuery := builder.NewSelect()
	archiveQuery.From(archiveTable)
	archiveQuery.Column(
		builder.ColumnName{Table: archiveTable, Name: "as_of_date"},
		builder.ColumnName{Table: archiveTable, Name: "isin"},
		builder.ColumnName{Table: archiveTable, Name: "name"},
		builder.ColumnName{Table: archiveTable, Name: "kind"},
		builder.ColumnName{Table: archiveTable, Name: "quantity"},
		builder.ColumnName{Table: archiveTable, Name: "asset_share_percent"},
		builder.ColumnName{Table: archiveTable, Name: "asset_share_upper_bound"},
		builder.ColumnName{Table: archiveTable, Name: "row_count"},
	)
	archiveQuery.Order(builder.Order{Table: archiveTable, Column: "isin"})

	archiveSQL, archiveBinds, errBuild := archiveQuery.Get()
	if errBuild != nil {
		return nil, fmt.Errorf("build archived composition query: %w", errBuild)
	}

	archiveRows, errQuery := db.Query(ctx, archiveSQL, pgx.NamedArgs(archiveBinds))
	if errQuery != nil {
		return nil, fmt.Errorf("query archived composition: %w", errQuery)
	}

	archived, errCollect := pgx.CollectRows(archiveRows, pgx.RowToStructByNameLax[compositionRow])
	if errCollect != nil {
		return nil, fmt.Errorf("collect archived composition: %w", errCollect)
	}

	liveDates := make(map[time.Time]struct{}, len(datesByID))
	for _, date := range datesByID {
		liveDates[date] = struct{}{}
	}

	for _, item := range archived {
		date := dateonly.UTC(item.AsOfDate)
		if _, live := liveDates[date]; !live {
			byDate[date] = append(byDate[date], appstate.FundHolding{
				ISIN:                 item.ISIN,
				Name:                 item.Name,
				Kind:                 item.Kind,
				Quantity:             stringValue(item.Quantity),
				AssetSharePercent:    item.AssetSharePercent,
				AssetShareUpperBound: item.AssetShareUpperBound,
				Rows:                 item.Rows,
			})
		}
	}

	result := make([]appstate.FundComposition, 0, len(byDate))
	for _, date := range slices.SortedFunc(maps.Keys(byDate), time.Time.Compare) {
		if holdings := byDate[date]; len(holdings) > 0 {
			result = append(result, appstate.FundComposition{AsOfDate: date, Holdings: holdings})
		}
	}

	return result, nil
}

func aggregateCompositionRows(items []compositionRow) (appstate.FundHolding, error) {
	first := items[0]
	holding := appstate.FundHolding{ISIN: first.ISIN, Name: first.Name, Kind: first.Kind}
	var quantity, share compositionSum
	for _, item := range items {
		if item.Quantity != nil && !quantity.add(*item.Quantity) {
			return appstate.FundHolding{}, fmt.Errorf("invalid composition quantity for %s", item.ISIN)
		}

		if !share.add(item.AssetSharePercent) {
			return appstate.FundHolding{}, fmt.Errorf("invalid composition share for %s", item.ISIN)
		}

		holding.AssetShareUpperBound = holding.AssetShareUpperBound || item.AssetShareUpperBound
	}

	if !quantity.valid {
		return appstate.FundHolding{}, fmt.Errorf("missing composition quantity for %s", holding.ISIN)
	}

	holding.Quantity = quantity.value.FloatString(quantity.scale)
	holding.AssetSharePercent = share.value.FloatString(share.scale)
	holding.Rows = len(items)
	return holding, nil
}

type compositionSum struct {
	value big.Rat
	scale int
	valid bool
}

func (s *compositionSum) add(text string) bool {
	value, ok := decimal.Parse(text)
	if !ok {
		return false
	}

	_, fraction, _ := strings.Cut(text, ".")
	s.value.Add(&s.value, value)
	s.scale = max(s.scale, len(fraction))
	s.valid = true
	return true
}
