// Package portfolio reconstructs holdings from dated disclosures.
package portfolio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rufond/fpr-backend/internal/currency"
	"github.com/rufond/fpr-backend/internal/decimal"
	"github.com/rufond/fpr-backend/internal/validationrules"
	"github.com/xloss/go-builder"
)

type Report struct {
	ReportSummary
	SHA256    string     `json:"sha256"`
	Positions []Position `json:"positions"`
}

type ReportSummary struct {
	AsOfDate          string `json:"as_of_date"`
	Currency          string `json:"currency"`
	SourceURL         string `json:"source_url"`
	TotalAssets       string `json:"total_assets"`
	NAV               string `json:"nav"`
	Units             string `json:"units"`
	Cash              string `json:"cash,omitempty"`
	Receivables       string `json:"receivables,omitempty"`
	Liabilities       string `json:"liabilities,omitempty"`
	DeclaredCurrency  string `json:"declared_currency,omitempty"`
	CurrencySourceURL string `json:"currency_source_url,omitempty"`
}

type Position struct {
	ISIN              string   `json:"isin"`
	Name              string   `json:"name"`
	Kind              string   `json:"kind"`
	Quantity          string   `json:"quantity"`
	Value             string   `json:"value"`
	Currency          string   `json:"currency,omitempty"`
	Maturity          string   `json:"maturity,omitempty"`
	SourceIdentifiers []string `json:"source_identifiers,omitempty"`
}

type Conversion struct {
	ToTicker  string `json:"to_ticker"`
	Date      string `json:"date"`
	Ratio     string `json:"ratio"`
	SourceURL string `json:"source_url"`
}

type IPO struct {
	Conversion          *Conversion `json:"conversion,omitempty"`
	SplitAdjusted       bool        `json:"split_adjusted,omitempty"`
	Ticker              string      `json:"ticker"`
	Name                string      `json:"name"`
	Date                string      `json:"date"`
	Quantity            string      `json:"quantity"`
	EntryPriceUSD       string      `json:"entry_price_usd"`
	ISIN                string      `json:"isin,omitempty"`
	Source              string      `json:"source,omitempty"`
	Approximate         bool        `json:"approximate,omitempty"`
	ReferencePrice      string      `json:"reference_price_usd,omitempty"`
	ReferenceDate       string      `json:"reference_date,omitempty"`
	LockupEnd           string      `json:"lockup_end,omitempty"`
	SourceURL           string      `json:"source_url"`
	ClosedInLegacy      *bool       `json:"closed_in_legacy,omitempty"`
	PreviousLegacyPrice string      `json:"previous_legacy_price_usd,omitempty"`
}

func ReadReports(source io.Reader) ([]Report, error) {
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 64<<10), MaxImportBytes+1)
	var reports []Report
	for line := 1; scanner.Scan(); line++ {
		data := bytes.TrimSpace(scanner.Bytes())
		if len(data) == 0 {
			continue
		}

		var report Report
		if err := json.Unmarshal(data, &report); err != nil {
			return nil, fmt.Errorf("строка %d содержит некорректный JSON справки", line)
		}

		if err := validateReport(report); err != nil {
			return nil, fmt.Errorf("строка %d: %w", line, err)
		}

		reports = append(reports, report)
	}

	return reports, scanner.Err()
}

func validateReport(report Report) error {
	if _, err := time.Parse(time.DateOnly, report.AsOfDate); err != nil {
		return fmt.Errorf("ожидается дата справки в формате ГГГГ-ММ-ДД")
	}

	source, err := url.Parse(report.SourceURL)
	if err != nil || source.Scheme != "https" || source.Host == "" || source.User != nil {
		return fmt.Errorf("ожидается HTTPS-ссылка на справку")
	}

	hash, err := hex.DecodeString(report.SHA256)
	if err != nil || len(hash) != 32 {
		return fmt.Errorf("некорректная контрольная сумма справки")
	}

	if !currency.ValidCode(report.Currency) {
		return fmt.Errorf("некорректный код валюты справки")
	}

	for _, value := range []string{report.TotalAssets, report.NAV, report.Units} {
		if exact, ok := decimal.Parse(value); !ok || exact.Sign() <= 0 {
			return fmt.Errorf("стоимость активов, СЧА и число паёв должны быть положительными")
		}
	}

	if len(report.Positions) == 0 {
		return fmt.Errorf("в справке нет ценных бумаг")
	}

	seen := make(map[string]struct{}, len(report.Positions))
	for _, item := range report.Positions {
		qty, qOK := decimal.Parse(item.Quantity)
		value, vOK := decimal.Parse(item.Value)
		if !(validationrules.ISIN{}).Validate("", item.ISIN, nil) || item.Name == "" || !qOK || qty.Sign() <= 0 || !vOK || value.Sign() < 0 {
			return fmt.Errorf("проверьте ISIN, название, количество и стоимость бумаги %q", item.ISIN)
		}

		if _, exists := seen[item.ISIN]; exists {
			return fmt.Errorf("бумага %q указана дважды", item.ISIN)
		}

		seen[item.ISIN] = struct{}{}
	}

	return nil
}

func insertReports(ctx context.Context, tx pgx.Tx, reports []Report) (int64, error) {
	table := builder.NewTable("fund_portfolio_reports")
	batch := &pgx.Batch{}
	var inserted int64
	for _, report := range reports {
		date, err := time.Parse(time.DateOnly, report.AsOfDate)
		if err != nil {
			return 0, fmt.Errorf("parse report date: %w", err)
		}

		query := builder.NewInsert(table)
		query.Value("as_of_date", date)
		query.Value("source_sha256", report.SHA256)
		query.Value("report", report)
		query.OnConflictDoNothing("as_of_date", "source_sha256")

		sql, binds, err := query.Get()
		if err != nil {
			return 0, fmt.Errorf("build report insert: %w", err)
		}

		queued := batch.Queue(sql, pgx.NamedArgs(binds))
		queued.Exec(func(tag pgconn.CommandTag) error {
			inserted += tag.RowsAffected()
			return nil
		})
	}

	results := tx.SendBatch(ctx, batch)
	if err := results.Close(); err != nil {
		return 0, fmt.Errorf("import portfolio reports: %w", err)
	}

	return inserted, nil
}

func loadArchive(ctx context.Context, db interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}) (*Archive, error) {
	reportsTable := builder.NewTable("fund_portfolio_reports")
	reportQuery := builder.NewSelect()
	reportQuery.From(reportsTable)
	reportQuery.Column(builder.ColumnName{Table: reportsTable, Name: "report"})
	reportQuery.Order(
		builder.Order{Table: reportsTable, Column: "as_of_date"},
		builder.Order{Table: reportsTable, Column: "imported_at", Desc: true},
		builder.Order{Table: reportsTable, Column: "source_sha256", Desc: true},
	)

	sql, binds, err := reportQuery.Get()
	if err != nil {
		return nil, fmt.Errorf("build portfolio reports query: %w", err)
	}

	rows, err := db.Query(ctx, sql, pgx.NamedArgs(binds))
	if err != nil {
		return nil, err
	}

	reports, err := pgx.CollectRows(rows, pgx.RowTo[Report])
	if err != nil {
		return nil, err
	}

	referencesTable := builder.NewTable("fund_portfolio_references")
	referenceQuery := builder.NewSelect()
	referenceQuery.From(referencesTable)
	referenceQuery.Column(builder.ColumnName{Table: referencesTable, Name: "reference"})
	referenceQuery.Order(builder.Order{Table: referencesTable, Column: "id"})
	sql, binds, err = referenceQuery.Get()
	if err != nil {
		return nil, fmt.Errorf("build portfolio references query: %w", err)
	}

	referenceRows, err := db.Query(ctx, sql, pgx.NamedArgs(binds))
	if err != nil {
		return nil, err
	}

	ipos, err := pgx.CollectRows(referenceRows, pgx.RowTo[IPO])
	if err != nil {
		return nil, err
	}

	// Rows with the same date are ordered with the latest edition first.
	reports = slices.CompactFunc(reports, func(left, right Report) bool { return left.AsOfDate == right.AsOfDate })
	return NewArchive(reports, ipos), nil
}
