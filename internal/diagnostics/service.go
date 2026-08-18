package diagnostics

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/decimal"
	"github.com/rufond/fpr-backend/internal/prices"
	"github.com/rufond/fpr-backend/internal/scheduler"
)

const (
	severityError   = "error"
	severityWarning = "warning"
)

type schedulerJobsReader interface {
	Jobs() []scheduler.JobInfo
}

type schedulerRunsReader interface {
	LatestFinishedRun(ctx context.Context, jobKey string) (*scheduler.JobRun, error)
}

type priceSourcesReader interface {
	AdminPriceSources(ctx context.Context) (*prices.AdminPriceSourcesResult, error)
}

type Service struct {
	state        *appstate.Manager
	scheduler    schedulerJobsReader
	runs         schedulerRunsReader
	priceSources priceSourcesReader
}

func NewService(
	state *appstate.Manager, schedulerReader schedulerJobsReader, runs schedulerRunsReader, priceSources priceSourcesReader) *Service {
	return &Service{
		state:        state,
		scheduler:    schedulerReader,
		runs:         runs,
		priceSources: priceSources,
	}
}

func (s *Service) List(ctx context.Context) (*Result, error) {
	current := s.state.Load()
	if current == nil {
		return nil, fmt.Errorf("application state is not initialized")
	}

	items, errScheduler := s.schedulerIssues(ctx)
	if errScheduler != nil {
		return nil, errScheduler
	}

	marketItems, errMarket := s.marketIssues(ctx, current)
	if errMarket != nil {
		return nil, errMarket
	}

	items = append(items, marketItems...)
	items = append(items, stateIssues(current)...)

	slices.SortFunc(items, func(left Issue, right Issue) int {
		if left.Severity != right.Severity {
			if left.Severity == severityError {
				return -1
			}
			if right.Severity == severityError {
				return 1
			}
		}

		if value := cmp.Compare(left.Source, right.Source); value != 0 {
			return value
		}
		if value := cmp.Compare(left.Type, right.Type); value != 0 {
			return value
		}
		return cmp.Compare(left.Key, right.Key)
	})

	result := &Result{
		Total: len(items),
		Items: items,
	}
	for _, issue := range items {
		switch issue.Severity {
		case severityError:
			result.Errors++
		case severityWarning:
			result.Warnings++
		}
	}

	return result, nil
}

func (s *Service) schedulerIssues(ctx context.Context) ([]Issue, error) {
	jobs := s.scheduler.Jobs()
	slices.SortFunc(jobs, func(left scheduler.JobInfo, right scheduler.JobInfo) int {
		return cmp.Compare(left.Key, right.Key)
	})

	items := make([]Issue, 0)

	for _, job := range jobs {
		run, errRun := s.runs.LatestFinishedRun(ctx, job.Key)
		if errRun != nil {
			return nil, fmt.Errorf("load latest scheduler run %s: %w", job.Key, errRun)
		}
		if run == nil {
			continue
		}

		if run.Status == scheduler.RunStatusFailed {
			items = append(items, Issue{
				Key:      "scheduler:" + job.Key,
				Severity: severityError,
				Source:   "scheduler",
				Type:     "job_failed",
				Message:  "Последний запуск фоновой задачи завершился ошибкой.",
				Details: map[string]any{
					"job_key":     job.Key,
					"job_name":    job.Name,
					"run_id":      run.ID,
					"run_source":  run.RunSource,
					"started_at":  run.StartedAt,
					"finished_at": run.FinishedAt,
					"error":       run.Error,
				},
			})
		}

		if conflicts := summaryInteger(run.Summary["history_conflicts"]); conflicts > 0 {
			items = append(items, Issue{
				Key:      "management_company:fixed_history_conflict",
				Severity: severityWarning,
				Source:   "management_company",
				Type:     "fixed_history_conflict",
				Message:  "УК отдаёт значения, отличающиеся от уже зафиксированной истории старше окна автоматической коррекции.",
				Details: map[string]any{
					"job_key":   job.Key,
					"run_id":    run.ID,
					"conflicts": conflicts,
				},
			})
		}

		if valuationError := summaryString(run.Summary["live_valuation_error"]); valuationError != "" {
			items = append(items, Issue{
				Key:      "live_valuation:refresh_failed:" + job.Key,
				Severity: severityError,
				Source:   "live_valuation",
				Type:     "live_valuation_refresh_failed",
				Message:  "Последний пересчёт live-оценки после фоновой задачи завершился ошибкой.",
				Details: map[string]any{
					"job_key": job.Key,
					"run_id":  run.ID,
					"error":   valuationError,
				},
			})
		}

		failedSources := summaryInteger(run.Summary["failed_sources"])
		missingSources := summaryInteger(run.Summary["missing_sources"])
		invalidSources := summaryInteger(run.Summary["invalid_sources"])

		if failedSources+missingSources+invalidSources > 0 {
			items = append(items, Issue{
				Key:      "market_prices:partial_sync:" + job.Key,
				Severity: severityWarning,
				Source:   "market_prices",
				Type:     "partial_price_sync",
				Message:  "Последний запуск получил цены не для всех ожидаемых источников.",
				Details: map[string]any{
					"job_key":         job.Key,
					"run_id":          run.ID,
					"failed_sources":  failedSources,
					"missing_sources": missingSources,
					"invalid_sources": invalidSources,
				},
			})
		}
	}

	return items, nil
}

func (s *Service) marketIssues(ctx context.Context, current *appstate.State) ([]Issue, error) {
	priceSources, errSources := s.priceSources.AdminPriceSources(ctx)
	if errSources != nil {
		return nil, fmt.Errorf("load price sources for diagnostics: %w", errSources)
	}

	currentPrices := map[int64]appstate.InstrumentPrice{}
	if current.Prices != nil {
		currentPrices = current.Prices.Sources
	}

	assetsByInstrument := make(map[int64][]appstate.FundAsset)
	if current.Fund != nil {
		for _, asset := range current.Fund.Snapshot.Assets {
			if asset.InstrumentID == nil {
				continue
			}

			assetsByInstrument[*asset.InstrumentID] = append(assetsByInstrument[*asset.InstrumentID], asset)
		}
	}

	items := make([]Issue, 0)

	for _, instrument := range priceSources.Items {
		switch instrument.AssetType {
		case "equity", "depositary_receipt":
		default:
			continue
		}

		if len(instrument.Sources) == 0 {
			instrumentID := instrument.InstrumentID
			items = append(items, Issue{
				Key:          "price_source:missing:" + strconv.FormatInt(instrument.InstrumentID, 10),
				Severity:     severityWarning,
				Source:       "price_sources",
				Type:         "missing_price_source",
				Message:      "Для текущего инструмента не назначен источник рыночной цены.",
				InstrumentID: &instrumentID,
				Details: map[string]any{
					"asset_type": instrument.AssetType,
					"isin":       instrument.ISIN,
					"name":       instrument.Name,
					"ticker":     instrument.Ticker,
				},
			})
			continue
		}

		var enabled *prices.AdminPriceSource
		for index := range instrument.Sources {
			if instrument.Sources[index].Enabled {
				enabled = &instrument.Sources[index]
				break
			}
		}

		if enabled == nil {
			instrumentID := instrument.InstrumentID
			items = append(items, Issue{
				Key:          "price_source:disabled:" + strconv.FormatInt(instrument.InstrumentID, 10),
				Severity:     severityWarning,
				Source:       "price_sources",
				Type:         "price_source_disabled",
				Message:      "У текущего инструмента нет включённого источника рыночной цены.",
				InstrumentID: &instrumentID,
				Details: map[string]any{
					"asset_type": instrument.AssetType,
					"isin":       instrument.ISIN,
					"name":       instrument.Name,
					"ticker":     instrument.Ticker,
				},
			})
			continue
		}

		if _, exists := currentPrices[enabled.ID]; !exists {
			instrumentID := instrument.InstrumentID
			items = append(items, Issue{
				Key:          "price:missing:" + strconv.FormatInt(enabled.ID, 10),
				Severity:     severityWarning,
				Source:       "market_prices",
				Type:         "missing_current_price",
				Message:      "Источник цены включён, но текущая рыночная цена ещё не получена.",
				InstrumentID: &instrumentID,
				Details: map[string]any{
					"price_source_id": enabled.ID,
					"provider":        enabled.Provider,
					"provider_symbol": enabled.ProviderSymbol,
					"isin":            instrument.ISIN,
					"name":            instrument.Name,
				},
			})
			continue
		}

		if current.Valuation == nil {
			continue
		}

		for _, asset := range assetsByInstrument[instrument.InstrumentID] {
			baseline, exists := current.Valuation.Baselines[asset.ID]
			if exists && baseline.PriceSourceID == enabled.ID {
				continue
			}

			instrumentID := instrument.InstrumentID
			items = append(items, Issue{
				Key:          "valuation:missing_baseline:" + strconv.FormatInt(asset.ID, 10),
				Severity:     severityWarning,
				Source:       "live_valuation",
				Type:         "missing_price_baseline",
				Message:      "Для позиции нет пригодного исторического baseline, поэтому она не участвует в live market delta.",
				InstrumentID: &instrumentID,
				Details: map[string]any{
					"asset_id":        asset.ID,
					"price_source_id": enabled.ID,
					"provider":        enabled.Provider,
					"provider_symbol": enabled.ProviderSymbol,
					"isin":            instrument.ISIN,
					"name":            instrument.Name,
				},
			})
		}
	}

	return items, nil
}

func stateIssues(current *appstate.State) []Issue {
	items := make([]Issue, 0, 3)

	if current.FX == nil {
		items = append(items, missingUSDRUBIssue())
	} else {
		pair := appstate.FXPair{BaseCurrency: "USD", QuoteCurrency: "RUB"}
		if _, exists := current.FX.Rates[pair]; !exists {
			items = append(items, missingUSDRUBIssue())
		}
	}

	fundUnitPriceFound := false
	if current.Prices != nil {
		for _, price := range current.Prices.Sources {
			if price.AssetType == prices.FundUnitAssetType && price.ISIN == prices.FundUnitISIN {
				fundUnitPriceFound = true
				break
			}
		}
	}

	if !fundUnitPriceFound {
		items = append(items, Issue{
			Key:      "market_prices:fund_unit_missing",
			Severity: severityWarning,
			Source:   "market_prices",
			Type:     "fund_unit_price_missing",
			Message:  "Текущая биржевая цена пая MOEX ещё не получена.",
			Details:  map[string]any{"isin": prices.FundUnitISIN},
		})
	}

	if current.Valuation == nil || current.Valuation.Current.EstimatedNAVUSD == "" {
		items = append(items, Issue{
			Key:      "live_valuation:unavailable",
			Severity: severityWarning,
			Source:   "live_valuation",
			Type:     "live_valuation_unavailable",
			Message:  "Live-оценка фонда пока недоступна.",
			Details:  map[string]any{},
		})
	} else if !decimal.Equal(current.Valuation.Current.LiveCoveragePercent, "100") {
		items = append(items, Issue{
			Key:      "live_valuation:partial_coverage",
			Severity: severityWarning,
			Source:   "live_valuation",
			Type:     "partial_live_coverage",
			Message:  "Live-оценка использует рыночный delta только для части официального состава.",
			Details: map[string]any{
				"live_coverage_percent": current.Valuation.Current.LiveCoveragePercent,
				"observed_at":           current.Valuation.Current.ObservedAt,
			},
		})
	}

	return items
}

func missingUSDRUBIssue() Issue {
	return Issue{
		Key:      "fx:usd_rub_missing",
		Severity: severityWarning,
		Source:   "fx",
		Type:     "missing_usd_rub",
		Message:  "Текущий курс USD/RUB ещё не получен.",
		Details:  map[string]any{"pair": "USD/RUB"},
	}
}

func summaryInteger(value any) int64 {
	switch item := value.(type) {
	case int:
		return int64(item)
	case int32:
		return int64(item)
	case int64:
		return item
	case float64:
		return int64(item)
	default:
		return 0
	}
}

func summaryString(value any) string {
	item, _ := value.(string)

	return item
}
