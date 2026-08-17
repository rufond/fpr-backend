package valuation

import (
	"context"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/currency"
	"github.com/rufond/fpr-backend/internal/dateonly"
	"github.com/rufond/fpr-backend/internal/decimal"
	"github.com/rufond/fpr-backend/internal/fx"
	"github.com/rufond/fpr-backend/internal/prices"
)

type HistoricalPriceSource interface {
	HistoricalPricesAt(ctx context.Context, sources []appstate.InstrumentPrice, till time.Time) (*prices.HistoricalPricesResult, error)
}

type HistoricalFXSource interface {
	HistoricalUSDRUB(ctx context.Context, till time.Time) (fx.SourceRate, bool, error)
}

type serviceRepository interface {
	LoadBaselines(ctx context.Context, snapshotID int64) (map[int64]baseline, error)
	LoadValuePoints(ctx context.Context, snapshotID int64, cutoff time.Time) ([]valuePoint, error)
	ApplyRefresh(ctx context.Context, baselineChanges []baseline, point valuePoint, cutoff time.Time) error
}

type Service struct {
	repository serviceRepository
	state      *appstate.Manager
	prices     HistoricalPriceSource
	fx         HistoricalFXSource
	now        func() time.Time
}

type preparedBaseline struct {
	baseline
	InstrumentID int64
}

type historicalFXValue struct {
	Rate      string
	Provider  string
	PricedAt  time.Time
	Available bool
}

func NewService(repository serviceRepository, state *appstate.Manager, priceSource HistoricalPriceSource, fxSource HistoricalFXSource) *Service {
	return &Service{
		repository: repository,
		state:      state,
		prices:     priceSource,
		fx:         fxSource,
		now:        time.Now,
	}
}

func (s *Service) Start(ctx context.Context) error {
	if s.repository == nil {
		return fmt.Errorf("valuation repository is not configured")
	}
	if s.state == nil {
		return fmt.Errorf("application state manager is not configured")
	}
	if s.prices == nil {
		return fmt.Errorf("historical price source is not configured")
	}
	if s.fx == nil {
		return fmt.Errorf("historical FX source is not configured")
	}

	current := s.state.Load()
	if current == nil || current.Fund == nil {
		return fmt.Errorf("fund state is not initialized")
	}

	valuationState, errLoad := s.loadState(ctx, current.Fund.Snapshot.ID)
	if errLoad != nil {
		return errLoad
	}

	prepared, errPrepare := s.prepareBaselines(ctx, current, valuationState.Baselines)
	if errPrepare != nil {
		return errPrepare
	}

	_, _, errApply := s.apply(ctx, current.Fund.Snapshot.ID, valuationState, prepared)
	return errApply
}

func (s *Service) Refresh(ctx context.Context) (appstate.FundLiveValuation, bool, error) {
	current := s.state.Load()
	if current == nil || current.Fund == nil {
		return appstate.FundLiveValuation{}, false, fmt.Errorf("fund state is not initialized")
	}

	valuationState := current.Valuation
	if valuationState == nil || valuationState.SnapshotID != current.Fund.Snapshot.ID {
		loaded, errLoad := s.loadState(ctx, current.Fund.Snapshot.ID)
		if errLoad != nil {
			return appstate.FundLiveValuation{}, false, errLoad
		}
		valuationState = loaded
	}

	prepared, errPrepare := s.prepareBaselines(ctx, current, valuationState.Baselines)
	if errPrepare != nil {
		return appstate.FundLiveValuation{}, false, errPrepare
	}

	return s.apply(ctx, current.Fund.Snapshot.ID, valuationState, prepared)
}

func (s *Service) loadState(ctx context.Context, snapshotID int64) (*appstate.ValuationState, error) {
	baselines, errBaselines := s.repository.LoadBaselines(ctx, snapshotID)
	if errBaselines != nil {
		return nil, errBaselines
	}

	cutoff := s.now().UTC().Add(-valuePointRetention)
	storedPoints, errPoints := s.repository.LoadValuePoints(ctx, snapshotID, cutoff)
	if errPoints != nil {
		return nil, errPoints
	}

	points := make([]appstate.FundValuePoint, 0, len(storedPoints))
	for _, item := range storedPoints {
		points = append(points, appstate.FundValuePoint{
			SnapshotID:                      item.SnapshotID,
			ObservedAt:                      item.ObservedAt,
			EstimatedNAVUSD:                 item.EstimatedNAVUSD,
			EstimatedCalculatedUnitValueUSD: item.EstimatedCalculatedUnitValueUSD,
			LiveDeltaUSD:                    item.LiveDeltaUSD,
			LiveCoveragePercent:             item.LiveCoveragePercent,
		})
	}

	appBaselines := make(map[int64]appstate.FundAssetPriceBaseline, len(baselines))
	for assetID, item := range baselines {
		appBaselines[assetID] = appStateBaseline(item)
	}

	return &appstate.ValuationState{
		SnapshotID: snapshotID,
		Baselines:  appBaselines,
		Points:     points,
	}, nil
}

func (s *Service) prepareBaselines(ctx context.Context, state *appstate.State, existing map[int64]appstate.FundAssetPriceBaseline) ([]preparedBaseline, error) {
	pricesByInstrument := make(map[int64]appstate.InstrumentPrice)
	if state.Prices != nil {
		for _, price := range state.Prices.Sources {
			pricesByInstrument[price.InstrumentID] = price
		}
	}

	assets := make([]appstate.FundAsset, 0)
	sources := make([]appstate.InstrumentPrice, 0)
	seenSources := make(map[int64]struct{})

	for _, asset := range state.Fund.Snapshot.Assets {
		if asset.InstrumentID == nil || asset.Quantity == "" {
			continue
		}

		price, exists := pricesByInstrument[*asset.InstrumentID]
		if !exists {
			continue
		}

		switch price.Currency {
		case currency.USD, currency.RUB:
		default:
			continue
		}

		if current, exists := existing[asset.ID]; exists &&
			current.PriceSourceID == price.PriceSourceID &&
			current.Provider == price.Provider &&
			current.ProviderSymbol == price.ProviderSymbol {
			continue
		}

		assets = append(assets, asset)
		if _, seen := seenSources[price.PriceSourceID]; !seen {
			seenSources[price.PriceSourceID] = struct{}{}
			sources = append(sources, price)
		}
	}

	if len(sources) == 0 {
		return nil, nil
	}

	asOfDate := dateonly.UTC(state.Fund.Snapshot.AsOfDate)
	historical, errHistorical := s.prices.HistoricalPricesAt(ctx, sources, asOfDate)
	if errHistorical != nil {
		return nil, errHistorical
	}

	for _, issue := range historical.Issues {
		log.Warn().
			Int64("price_source_id", issue.PriceSourceID).
			Str("provider", issue.Provider).
			Str("provider_symbol", issue.ProviderSymbol).
			Str("as_of_date", asOfDate.Format(time.DateOnly)).
			Str("error", issue.Error).
			Msg("historical baseline price unavailable")
	}
	for _, priceSourceID := range historical.MissingSourceIDs {
		log.Debug().
			Int64("price_source_id", priceSourceID).
			Str("as_of_date", asOfDate.Format(time.DateOnly)).
			Msg("historical baseline price not found")
	}

	fxCache := make(map[time.Time]historicalFXValue)
	result := make([]preparedBaseline, 0, len(assets))
	for _, asset := range assets {
		priceSource := pricesByInstrument[*asset.InstrumentID]
		historicalPrice, exists := historical.PricesBySource[priceSource.PriceSourceID]
		if !exists {
			continue
		}

		fxRate, fxProvider, fxPricedAt, available, errFX := s.historicalFXToUSD(
			ctx,
			historicalPrice.Currency,
			historicalPrice.PriceDate,
			historicalPrice.PricedAt,
			fxCache,
		)
		if errFX != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}

			log.Warn().
				Err(errFX).
				Int64("asset_id", asset.ID).
				Str("currency", historicalPrice.Currency).
				Str("price_date", historicalPrice.PriceDate.Format(time.DateOnly)).
				Msg("historical baseline FX unavailable")
			continue
		}
		if !available {
			continue
		}

		quantity, okQuantity := decimal.Parse(asset.Quantity)
		priceValue, okPrice := decimal.Parse(historicalPrice.UnitValue)
		fxValue, okFX := decimal.Parse(fxRate)
		if !okQuantity || !okPrice || !okFX {
			return nil, fmt.Errorf("calculate baseline for asset %d: trusted decimal state is invalid", asset.ID)
		}

		marketValue := new(big.Rat).Mul(quantity, priceValue)
		marketValue.Mul(marketValue, fxValue)

		result = append(result, preparedBaseline{
			InstrumentID: *asset.InstrumentID,
			baseline: baseline{
				AssetID:        asset.ID,
				PriceSourceID:  priceSource.PriceSourceID,
				Provider:       priceSource.Provider,
				ProviderSymbol: priceSource.ProviderSymbol,
				UnitValue:      historicalPrice.UnitValue,
				Currency:       historicalPrice.Currency,
				PricedAt:       historicalPrice.PricedAt,
				FXRateToUSD:    fxRate,
				FXProvider:     fxProvider,
				FXPricedAt:     fxPricedAt,
				MarketValueUSD: decimalValue(marketValue),
			},
		})
	}

	return result, nil
}

func (s *Service) historicalFXToUSD(ctx context.Context, currencyCode string, priceDate time.Time, priceTime time.Time, cache map[time.Time]historicalFXValue) (string, string, time.Time, bool, error) {
	if currencyCode == currency.USD {
		return "1", identityFXProvider, priceTime.UTC(), true, nil
	}

	key := dateonly.UTC(priceDate)
	if cached, exists := cache[key]; exists {
		return cached.Rate, cached.Provider, cached.PricedAt, cached.Available, nil
	}

	if currencyCode != currency.RUB {
		cache[key] = historicalFXValue{}
		return "", "", time.Time{}, false, nil
	}

	rate, exists, errRate := s.fx.HistoricalUSDRUB(ctx, key)
	if errRate != nil {
		return "", "", time.Time{}, false, errRate
	}
	if !exists {
		cache[key] = historicalFXValue{}
		return "", "", time.Time{}, false, nil
	}

	value, ok := decimal.Parse(rate.Rate)
	if !ok || value.Sign() <= 0 {
		return "", "", time.Time{}, false, fmt.Errorf("historical USD/RUB rate is invalid")
	}

	converted := historicalFXValue{
		Rate:      decimalValue(new(big.Rat).Inv(value)),
		Provider:  rate.Provider,
		PricedAt:  rate.PricedAt.UTC(),
		Available: true,
	}
	cache[key] = converted

	return converted.Rate, converted.Provider, converted.PricedAt, true, nil
}

func (s *Service) apply(ctx context.Context, snapshotID int64, loaded *appstate.ValuationState, prepared []preparedBaseline) (appstate.FundLiveValuation, bool, error) {
	var live appstate.FundLiveValuation
	var changed bool

	errUpdate := s.state.Update(func(current *appstate.State) (*appstate.State, error) {
		if current.Fund == nil || current.Fund.Snapshot.ID != snapshotID {
			return nil, fmt.Errorf("fund snapshot changed during live valuation refresh")
		}

		valuationState := loaded
		if current.Valuation != nil && current.Valuation.SnapshotID == snapshotID {
			valuationState = current.Valuation
		}
		if valuationState == nil {
			valuationState = &appstate.ValuationState{SnapshotID: snapshotID, Baselines: map[int64]appstate.FundAssetPriceBaseline{}}
		}

		baselines := maps.Clone(valuationState.Baselines)
		if baselines == nil {
			baselines = map[int64]appstate.FundAssetPriceBaseline{}
		}

		baselineChanges := applicableBaselineChanges(current, baselines, prepared)
		persistedChanges := make([]baseline, 0, len(baselineChanges))
		for _, item := range baselineChanges {
			baselines[item.AssetID] = appStateBaseline(item.baseline)
			persistedChanges = append(persistedChanges, item.baseline)
		}

		observedAt := s.now().UTC()
		calculated, errCalculate := calculateLiveValuation(current, baselines, observedAt)
		if errCalculate != nil {
			return nil, errCalculate
		}

		force := valuationState.Current.SnapshotID != snapshotID
		valuationChanged := force || len(persistedChanges) != 0 || !sameLiveValuation(valuationState.Current, calculated)
		live = calculated
		changed = valuationChanged
		if !valuationChanged {
			return current, nil
		}

		point := valuePoint{
			SnapshotID:                      calculated.SnapshotID,
			ObservedAt:                      calculated.ObservedAt,
			EstimatedNAVUSD:                 calculated.EstimatedNAVUSD,
			EstimatedCalculatedUnitValueUSD: calculated.EstimatedCalculatedUnitValueUSD,
			LiveDeltaUSD:                    calculated.LiveDeltaUSD,
			LiveCoveragePercent:             calculated.LiveCoveragePercent,
		}
		cutoff := observedAt.Add(-valuePointRetention)
		if errApply := s.repository.ApplyRefresh(ctx, persistedChanges, point, cutoff); errApply != nil {
			return nil, errApply
		}

		points := slices.Clone(valuationState.Points)
		points = append(points, appstate.FundValuePoint{
			SnapshotID:                      calculated.SnapshotID,
			ObservedAt:                      calculated.ObservedAt,
			EstimatedNAVUSD:                 calculated.EstimatedNAVUSD,
			EstimatedCalculatedUnitValueUSD: calculated.EstimatedCalculatedUnitValueUSD,
			LiveDeltaUSD:                    calculated.LiveDeltaUSD,
			LiveCoveragePercent:             calculated.LiveCoveragePercent,
		})
		points = slices.DeleteFunc(points, func(item appstate.FundValuePoint) bool {
			return item.ObservedAt.Before(cutoff)
		})

		next := new(*current)
		next.Valuation = &appstate.ValuationState{
			SnapshotID: snapshotID,
			Baselines:  baselines,
			Current:    calculated,
			Points:     points,
		}
		return next, nil
	})
	if errUpdate != nil {
		return appstate.FundLiveValuation{}, false, errUpdate
	}

	return live, changed, nil
}

func applicableBaselineChanges(state *appstate.State, existing map[int64]appstate.FundAssetPriceBaseline, prepared []preparedBaseline) []preparedBaseline {
	if len(prepared) == 0 || state.Prices == nil {
		return nil
	}

	assetsByID := make(map[int64]appstate.FundAsset, len(state.Fund.Snapshot.Assets))
	for _, asset := range state.Fund.Snapshot.Assets {
		assetsByID[asset.ID] = asset
	}

	result := make([]preparedBaseline, 0, len(prepared))
	for _, item := range prepared {
		asset, exists := assetsByID[item.AssetID]
		if !exists || asset.InstrumentID == nil || *asset.InstrumentID != item.InstrumentID {
			continue
		}

		price, exists := state.Prices.Sources[item.PriceSourceID]
		if !exists || price.InstrumentID != item.InstrumentID ||
			price.Provider != item.Provider || price.ProviderSymbol != item.ProviderSymbol {
			continue
		}

		if baselineItem, existsItem := existing[item.AssetID]; existsItem &&
			baselineItem.PriceSourceID == item.PriceSourceID &&
			baselineItem.Provider == item.Provider && baselineItem.ProviderSymbol == item.ProviderSymbol {
			continue
		}

		result = append(result, item)
	}

	return result
}

func calculateLiveValuation(state *appstate.State, baselines map[int64]appstate.FundAssetPriceBaseline, observedAt time.Time) (appstate.FundLiveValuation, error) {
	officialNAV, okNAV := decimal.Parse(state.Fund.Snapshot.NAVUSD)
	officialUnitValue, okUnitValue := decimal.Parse(state.Fund.Snapshot.CalculatedUnitValueUSD)
	if !okNAV || !okUnitValue || officialNAV.Sign() <= 0 {
		return appstate.FundLiveValuation{}, fmt.Errorf("calculate live valuation: trusted official fund values are invalid")
	}

	currentPrices := map[int64]appstate.InstrumentPrice{}
	if state.Prices != nil {
		currentPrices = state.Prices.Sources
	}

	liveDelta := new(big.Rat)
	coveredShare := new(big.Rat)

	for _, asset := range state.Fund.Snapshot.Assets {
		baselineItem, exists := baselines[asset.ID]
		if !exists || asset.Quantity == "" {
			continue
		}

		price, exists := currentPrices[baselineItem.PriceSourceID]
		if !exists || price.Provider != baselineItem.Provider || price.ProviderSymbol != baselineItem.ProviderSymbol {
			continue
		}

		fxRate, available, errFX := currentFXRateToUSD(state.FX, price.Currency)
		if errFX != nil {
			return appstate.FundLiveValuation{}, errFX
		}
		if !available {
			continue
		}

		quantity, okQuantity := decimal.Parse(asset.Quantity)
		priceValue, okPrice := decimal.Parse(price.UnitValue)
		fxValue, okFX := decimal.Parse(fxRate)
		baselineMarketValue, okBaseline := decimal.Parse(baselineItem.MarketValueUSD)
		if !okQuantity || !okPrice || !okFX || !okBaseline {
			return appstate.FundLiveValuation{}, fmt.Errorf("calculate live value for asset %d: trusted decimal state is invalid", asset.ID)
		}

		currentMarketValue := new(big.Rat).Mul(quantity, priceValue)
		currentMarketValue.Mul(currentMarketValue, fxValue)
		assetDelta := new(big.Rat).Sub(currentMarketValue, baselineMarketValue)
		liveDelta.Add(liveDelta, assetDelta)

		share, okShare := decimal.Parse(asset.AssetSharePercent)
		if !okShare {
			return appstate.FundLiveValuation{}, fmt.Errorf("calculate live coverage for asset %d: trusted share is invalid", asset.ID)
		}

		coveredShare.Add(coveredShare, share)
	}

	estimatedNAV := new(big.Rat).Add(new(big.Rat).Set(officialNAV), liveDelta)
	estimatedUnitValue := new(big.Rat).Mul(officialUnitValue, estimatedNAV)
	estimatedUnitValue.Quo(estimatedUnitValue, officialNAV)

	if coveredShare.Cmp(big.NewRat(100, 1)) > 0 {
		coveredShare.SetInt64(100)
	}

	return appstate.FundLiveValuation{
		SnapshotID:                      state.Fund.Snapshot.ID,
		ObservedAt:                      observedAt.UTC(),
		EstimatedNAVUSD:                 decimalValue(estimatedNAV),
		EstimatedCalculatedUnitValueUSD: decimalValue(estimatedUnitValue),
		LiveDeltaUSD:                    decimalValue(liveDelta),
		LiveCoveragePercent:             decimalValue(coveredShare),
	}, nil
}

func currentFXRateToUSD(state *appstate.FXState, fromCurrency string) (string, bool, error) {
	switch fromCurrency {
	case currency.USD:
		return "1", true, nil
	case currency.RUB:
		if state == nil {
			return "", false, nil
		}

		rate, exists := state.Rates[appstate.FXPair{BaseCurrency: currency.USD, QuoteCurrency: currency.RUB}]
		if !exists {
			return "", false, nil
		}

		value, ok := decimal.Parse(rate.Rate)
		if !ok || value.Sign() <= 0 {
			return "", false, fmt.Errorf("calculate RUB/USD conversion: trusted USD/RUB rate is invalid")
		}

		return decimalValue(new(big.Rat).Inv(value)), true, nil
	default:
		return "", false, nil
	}
}

func decimalValue(value *big.Rat) string {
	text := value.FloatString(valuePrecision)
	text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	if text == "" || text == "-0" {
		return "0"
	}

	return text
}

func sameLiveValuation(left appstate.FundLiveValuation, right appstate.FundLiveValuation) bool {
	return left.SnapshotID == right.SnapshotID &&
		decimal.Equal(left.EstimatedNAVUSD, right.EstimatedNAVUSD) &&
		decimal.Equal(left.EstimatedCalculatedUnitValueUSD, right.EstimatedCalculatedUnitValueUSD) &&
		decimal.Equal(left.LiveDeltaUSD, right.LiveDeltaUSD) &&
		decimal.Equal(left.LiveCoveragePercent, right.LiveCoveragePercent)
}

func appStateBaseline(item baseline) appstate.FundAssetPriceBaseline {
	return appstate.FundAssetPriceBaseline{
		AssetID:        item.AssetID,
		PriceSourceID:  item.PriceSourceID,
		Provider:       item.Provider,
		ProviderSymbol: item.ProviderSymbol,
		UnitValue:      item.UnitValue,
		Currency:       item.Currency,
		PricedAt:       item.PricedAt,
		FXRateToUSD:    item.FXRateToUSD,
		FXProvider:     item.FXProvider,
		FXPricedAt:     item.FXPricedAt,
		MarketValueUSD: item.MarketValueUSD,
	}
}
