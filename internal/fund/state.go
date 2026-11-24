package fund

import (
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/currency"
	"github.com/rufond/fpr-backend/internal/decimal"
	"github.com/rufond/fpr-backend/internal/prices"
)

var ErrStateUnavailable = errors.New("fund state unavailable")

func (s *Service) State() (*StateResult, error) {
	state, err := s.currentState()
	if err != nil {
		return nil, err
	}

	return buildStateResult(state, s.now(), s.entryPrice), nil
}

func (s *Service) History(from *time.Time) (*HistoryResult, error) {
	state, err := s.currentState()
	if err != nil {
		return nil, err
	}

	return buildHistoryResult(state, from), nil
}

func (s *Service) MarketHistory(from *time.Time) (*MarketHistoryResult, error) {
	return s.InstrumentMarketHistory("", from)
}

func (s *Service) InstrumentMarketHistory(isin string, from *time.Time) (*MarketHistoryResult, error) {
	state, err := s.currentState()
	if err != nil {
		return nil, err
	}

	isFundUnit := isin == "" || isin == prices.FundUnitISIN
	var series []appstate.InstrumentPricePoint
	if isFundUnit {
		series = stateFundUnitPricePoints(state.Prices)
	} else if state.Prices != nil {
		for id, price := range state.Prices.Sources {
			if price.ISIN == isin {
				series = state.Prices.Points[id].Items
				break
			}
		}
	}
	result := &MarketHistoryResult{
		UnitPrices: make([]StateIntradayMarketPrice, 0, len(series)),
		LiveValues: []StateLiveValuePoint{},
	}

	// The RAM series follows observation order, which can differ from quote time.
	for _, item := range series {
		if from != nil && item.PricedAt.Before(*from) {
			continue
		}
		result.UnitPrices = append(result.UnitPrices, StateIntradayMarketPrice{
			UnitValue: item.UnitValue,
			Currency:  item.Currency,
			PricedAt:  item.PricedAt,
		})
	}

	slices.SortStableFunc(result.UnitPrices, func(a, b StateIntradayMarketPrice) int {
		return a.PricedAt.Compare(b.PricedAt)
	})

	// The last observation for a timestamp is the quote shown for that time.
	unique := result.UnitPrices[:0]
	for _, point := range result.UnitPrices {
		if len(unique) > 0 && unique[len(unique)-1].PricedAt.Equal(point.PricedAt) {
			unique[len(unique)-1] = point
			continue
		}

		unique = append(unique, point)
	}

	result.UnitPrices = unique

	result.UnitChange, result.UnitChangePercent = unitPriceChange(result.UnitPrices)
	if isFundUnit && state.FX != nil {
		if fx, ok := state.FX.Rates[appstate.FXPair{BaseCurrency: currency.USD, QuoteCurrency: currency.RUB}]; ok {
			result.USDRUB = &StateFXRate{Rate: fx.Rate, PricedAt: fx.PricedAt}
			for _, point := range result.UnitPrices {
				if point.Currency == currency.RUB {
					valueUSD := priceInUSD(point.UnitValue, point.Currency, fx.Rate)
					if valueUSD != "" {
						result.UnitPricesUSD = append(result.UnitPricesUSD, StateIntradayMarketPrice{UnitValue: valueUSD, Currency: currency.USD, PricedAt: point.PricedAt})
					}
				}
			}
		}
	}

	if isFundUnit && state.Valuation != nil && state.Valuation.SnapshotID == state.Fund.Snapshot.ID {
		liveStart := 0
		if from != nil {
			liveStart, _ = slices.BinarySearchFunc(state.Valuation.Points, *from, func(item appstate.FundValuePoint, target time.Time) int {
				return item.ObservedAt.Compare(target)
			})
		}

		result.LiveValues = make([]StateLiveValuePoint, 0, len(state.Valuation.Points)-liveStart)
		for _, item := range state.Valuation.Points[liveStart:] {
			result.LiveValues = append(result.LiveValues, StateLiveValuePoint{
				ObservedAt:                      item.ObservedAt,
				EstimatedNAVUSD:                 item.EstimatedNAVUSD,
				EstimatedCalculatedUnitValueUSD: item.EstimatedCalculatedUnitValueUSD,
				LiveDeltaUSD:                    item.LiveDeltaUSD,
				LiveCoveragePercent:             item.LiveCoveragePercent,
			})
		}
	}

	return result, nil
}

func unitPriceChange(points []StateIntradayMarketPrice) (amount, percent string) {
	if len(points) < 2 {
		return "", ""
	}

	last := points[len(points)-1]
	first := last
	// A currency change is a boundary between discontinuous price series.
	for i := len(points) - 2; i >= 0 && points[i].Currency == last.Currency; i-- {
		first = points[i]
	}

	if !first.PricedAt.Before(last.PricedAt) {
		return "", ""
	}

	opening, hasOpening := decimal.Parse(first.UnitValue)
	latest, hasLatest := decimal.Parse(last.UnitValue)
	if !hasOpening || !hasLatest || opening.Sign() <= 0 {
		return "", ""
	}

	diff := new(big.Rat).Sub(latest, opening)
	ratio := new(big.Rat).Quo(diff, opening)
	return diff.FloatString(8), ratio.Mul(ratio, big.NewRat(100, 1)).FloatString(6)
}

func (s *Service) currentState() (*appstate.State, error) {
	if s.state == nil {
		return nil, fmt.Errorf("application state manager is not configured")
	}

	current := s.state.Load()
	if current == nil || current.Fund == nil {
		return nil, ErrStateUnavailable
	}

	return current, nil
}

func buildStateResult(state *appstate.State, now time.Time, entry func(string, string, string) EntryPrice) *StateResult {
	fundState := state.Fund
	result := &StateResult{
		OfficialSnapshot: StateOfficialSnapshot{
			AsOfDate:               fundState.Snapshot.AsOfDate.Format(time.DateOnly),
			ObservedAt:             fundState.Snapshot.ObservedAt,
			CalculatedUnitValueUSD: fundState.Snapshot.CalculatedUnitValueUSD,
			NAVUSD:                 fundState.Snapshot.NAVUSD,
			Assets:                 make([]StateAsset, 0, len(fundState.Snapshot.Assets)),
			Categories:             make([]StateCategory, 0, len(fundState.Snapshot.Categories)),
		},
		Market: StateMarket{
			UnitPrice:   stateFundUnitPrice(state.Prices, now),
			AssetPrices: stateAssetPrices(state, now, entry),
		},
	}

	if item := fundState.Snapshot.UnitCount; item != nil {
		result.OfficialSnapshot.UnitCount = &StateUnitCount{
			AsOfDate:         item.AsOfDate.Format(time.DateOnly),
			UnitsOutstanding: item.UnitsOutstanding,
			SourceURL:        item.SourceURL,
		}
	}

	if state.FX != nil {
		if rate, ok := state.FX.Rates[appstate.FXPair{BaseCurrency: currency.USD, QuoteCurrency: currency.RUB}]; ok {
			result.Market.USDRUB = &StateFXRate{
				Rate:     rate.Rate,
				PricedAt: rate.PricedAt,
			}
			if quote := result.Market.UnitPrice; quote != nil {
				result.Market.UnitPriceUSD = priceInUSD(quote.UnitValue, quote.Currency, rate.Rate)
			}
		}
	}

	if state.Valuation != nil && state.Valuation.SnapshotID == fundState.Snapshot.ID {
		live := state.Valuation.Current
		result.Market.LiveValuation = &StateLiveValuation{
			ObservedAt:                      live.ObservedAt,
			EstimatedNAVUSD:                 live.EstimatedNAVUSD,
			EstimatedCalculatedUnitValueUSD: live.EstimatedCalculatedUnitValueUSD,
			LiveDeltaUSD:                    live.LiveDeltaUSD,
			LiveDeltaPercent:                live.LiveDeltaPercent,
			LiveCoveragePercent:             live.LiveCoveragePercent,
		}

		if live.EstimatedCalculatedUnitValueRUB != "" {
			result.Market.LiveValuation.EstimatedCalculatedUnitValueRUB = new(live.EstimatedCalculatedUnitValueRUB)
		}
		if live.PremiumDiscountPercent != "" {
			result.Market.LiveValuation.PremiumDiscountPercent = new(live.PremiumDiscountPercent)
		}
	}

	previous, previousDate := previousDisclosedHoldings(fundState)
	occurrences := make(map[string]int)
	for _, item := range fundState.Snapshot.Assets {
		occurrences[item.ISIN]++
	}

	nav, hasNAV := decimal.Parse(fundState.Snapshot.NAVUSD)
	for _, item := range fundState.Snapshot.Assets {
		asset := stateAsset(item)
		share, hasShare := decimal.Parse(item.AssetSharePercent)
		if item.ISIN != "" && hasShare && !item.AssetShareUpperBound {
			if quantity, ok := decimal.Parse(item.Quantity); ok && hasNAV && nav.Sign() > 0 && share.Sign() > 0 && quantity.Sign() > 0 {
				// The website publishes NAV and a share of all assets. Using NAV as a
				// proxy reproduces the approximate price without labelling it a quote.
				estimate := new(big.Rat).Mul(nav, share)
				denominator := new(big.Rat).Mul(quantity, big.NewRat(100, 1))
				asset.DisclosureUnitEstimateUSD = estimate.Quo(estimate, denominator).FloatString(8)
			}
			if prior, exists := previous[item.ISIN]; exists && !prior.AssetShareUpperBound && prior.Rows == 1 && occurrences[item.ISIN] == 1 {
				if priorShare, ok := decimal.Parse(prior.AssetSharePercent); ok {
					asset.ShareChangePP = new(big.Rat).Sub(share, priorShare).FloatString(4)
					asset.PreviousShareDate = previousDate
				}
			}
		}
		result.OfficialSnapshot.Assets = append(result.OfficialSnapshot.Assets, asset)
	}

	for _, item := range fundState.Snapshot.Categories {
		result.OfficialSnapshot.Categories = append(result.OfficialSnapshot.Categories, StateCategory{
			RowNo:             item.RowNo,
			SourceName:        item.SourceName,
			AssetSharePercent: item.AssetSharePercent,
		})
	}

	return result
}

func previousDisclosedHoldings(state *appstate.FundState) (map[string]appstate.FundHolding, string) {
	for _, composition := range slices.Backward(state.CompositionHistory) {

		if composition.AsOfDate.Before(state.Snapshot.AsOfDate) {
			holdings := make(map[string]appstate.FundHolding, len(composition.Holdings))
			for _, holding := range composition.Holdings {
				holdings[holding.ISIN] = holding
			}

			return holdings, composition.AsOfDate.Format(time.DateOnly)
		}
	}

	return nil, ""
}

func buildHistoryResult(state *appstate.State, from *time.Time) *HistoryResult {
	fundState := state.Fund
	start := 0
	if from != nil {
		start, _ = slices.BinarySearchFunc(fundState.DailyValues, *from, func(item appstate.FundDailyValue, target time.Time) int {
			return item.AsOfDate.Compare(target)
		})
	}

	result := &HistoryResult{
		DailyValues:      make([]StateDailyValue, 0, len(fundState.DailyValues)-start),
		UnitMarketPrices: []StateDailyMarketPrice{},
	}

	for _, item := range fundState.DailyValues[start:] {
		result.DailyValues = append(result.DailyValues, StateDailyValue{
			AsOfDate:               item.AsOfDate.Format(time.DateOnly),
			CalculatedUnitValueUSD: item.CalculatedUnitValueUSD,
			NAVUSD:                 item.NAVUSD,
		})
	}

	series := stateFundUnitDailyPrices(state.Prices)
	marketStart := 0
	if from != nil {
		marketStart, _ = slices.BinarySearchFunc(series, *from, func(item appstate.InstrumentDailyPrice, target time.Time) int {
			return item.PriceDate.Compare(target)
		})
	}

	result.UnitMarketPrices = make([]StateDailyMarketPrice, 0, len(series)-marketStart)
	for _, item := range series[marketStart:] {
		result.UnitMarketPrices = append(result.UnitMarketPrices, StateDailyMarketPrice{
			AsOfDate:  item.PriceDate.Format(time.DateOnly),
			UnitValue: item.UnitValue,
			Currency:  item.Currency,
		})
	}

	return result
}

func stateFundUnitPricePoints(state *appstate.PriceState) []appstate.InstrumentPricePoint {
	if state == nil {
		return nil
	}

	for _, series := range state.Points {
		if series.ISIN == prices.FundUnitISIN && series.Provider == prices.ProviderMOEX {
			return series.Items
		}
	}

	return nil
}

func stateFundUnitDailyPrices(state *appstate.PriceState) []appstate.InstrumentDailyPrice {
	if state == nil {
		return nil
	}

	for _, series := range state.DailyPrices {
		if series.ISIN == prices.FundUnitISIN && series.Provider == prices.ProviderMOEX {
			return series.Items
		}
	}

	return nil
}

func stateFundUnitPrice(state *appstate.PriceState, now time.Time) *StateMarketPrice {
	if state == nil {
		return nil
	}

	for _, item := range state.Sources {
		if item.ISIN != prices.FundUnitISIN || item.Provider != prices.ProviderMOEX {
			continue
		}

		quote := stateMarketPrice(item, state.DailyPrices[item.PriceSourceID], "", now)
		return &quote
	}

	return nil
}

func stateAssetPrices(state *appstate.State, now time.Time, entry func(string, string, string) EntryPrice) map[int64]StateMarketPrice {
	result := make(map[int64]StateMarketPrice)
	if state.Prices == nil {
		return result
	}

	quantities := prices.PositionQuantities(state.Fund)
	asOf := state.Fund.Snapshot.AsOfDate.Format(time.DateOnly)
	for _, item := range state.Prices.Sources {
		quantity, held := quantities[item.InstrumentID]
		if !held {
			continue
		}

		if existing, ok := result[item.InstrumentID]; ok && !item.PricedAt.After(existing.PricedAt) {
			continue
		}

		qty := quantity.FloatString(8)
		quote := stateMarketPrice(item, state.Prices.DailyPrices[item.PriceSourceID], qty, now)
		quote.Archived = quote.Archived || item.PricedAt.Before(now.Add(-7*24*time.Hour))
		if value, ok := decimal.Parse(item.UnitValue); ok {
			quote.PositionValue = new(big.Rat).Mul(quantity, value).FloatString(8)
		}

		if entry != nil {
			reference := entry(item.ISIN, item.Name, asOf)
			fx := appstate.FXRate{}
			if state.FX != nil && !quote.Archived {
				fx = state.FX.Rates[appstate.FXPair{BaseCurrency: currency.USD, QuoteCurrency: item.Currency}]
				if fx.PricedAt.Before(item.PricedAt.Add(-5*24*time.Hour)) || fx.PricedAt.After(item.PricedAt.Add(24*time.Hour)) {
					fx = appstate.FXRate{}
				}
			}

			quote.EntryReturn = prices.ComparePrices(reference.USD, priceInUSD(item.UnitValue, item.Currency, fx.Rate), qty)
			if quote.EntryReturn != nil {
				quote.EntryReturn.Approximate = reference.Approximate
				if fx.Rate != "" {
					quote.EntryFX = &StateFXRate{Rate: fx.Rate, PricedAt: fx.PricedAt}
				}
			}
		}

		result[item.InstrumentID] = quote
	}

	return result
}

func stateMarketPrice(item appstate.InstrumentPrice, daily appstate.InstrumentDailyPriceSeries, quantity string, now time.Time) StateMarketPrice {
	previous := item.PreviousClose
	if previous == "" {
		day := item.PricedAt.UTC().Format(time.DateOnly)
		for _, point := range slices.Backward(daily.Items) {

			if point.PriceDate.Format(time.DateOnly) < day && point.Currency == item.Currency {
				// Avoid deriving a daily comparison across long holes in the archive.
				if item.PricedAt.Sub(point.PriceDate) <= 7*24*time.Hour {
					previous = point.UnitValue
				}
				break
			}
		}
	}

	quote := StateMarketPrice{
		InstrumentID:  item.InstrumentID,
		Provider:      item.Provider,
		Symbol:        item.ProviderSymbol,
		UnitValue:     item.UnitValue,
		Currency:      item.Currency,
		PricedAt:      item.PricedAt,
		PreviousClose: previous,
	}
	if item.ImportedProvider != "" {
		quote.Provider = item.ImportedProvider
		quote.Archived = true
		return quote
	}
	if quoteDate(item.PricedAt, item.Provider) == quoteDate(now, item.Provider) {
		quote.DayChange = prices.ComparePrices(previous, item.UnitValue, quantity)
	}
	return quote
}

var (
	moscowTime     = time.FixedZone("Europe/Moscow", 3*3600)
	almatyTime     = time.FixedZone("Asia/Almaty", 5*3600)
	newYorkTime, _ = time.LoadLocation("America/New_York")
)

func quoteDate(at time.Time, provider string) string {
	location := time.UTC
	switch provider {
	case prices.ProviderMOEX:
		location = moscowTime
	case prices.ProviderKASE:
		location = almatyTime
	case prices.ProviderNasdaq:
		if newYorkTime != nil {
			location = newYorkTime
		}
	}

	return at.In(location).Format(time.DateOnly)
}

func priceInUSD(value, code, usdRate string) string {
	if code == currency.USD {
		return value
	}

	rate, rateOK := decimal.Parse(usdRate)
	price, priceOK := decimal.Parse(value)
	if !rateOK || !priceOK || rate.Sign() <= 0 {
		return ""
	}

	return new(big.Rat).Quo(price, rate).FloatString(8)
}

func stateAsset(item appstate.FundAsset) StateAsset {
	result := StateAsset{
		RowNo:                item.RowNo,
		SourceName:           item.SourceName,
		SourceType:           item.SourceType,
		AssetSharePercent:    item.AssetSharePercent,
		AssetShareUpperBound: item.AssetShareUpperBound,
	}

	if item.Currency != "" {
		result.Currency = new(item.Currency)
	}
	if item.Quantity != "" {
		result.Quantity = new(item.Quantity)
	}

	if item.InstrumentID != nil {
		result.Instrument = &StateInstrument{
			ID:        *item.InstrumentID,
			AssetType: item.InstrumentType,
			ISIN:      item.ISIN,
			Name:      item.InstrumentName,
			Issuer:    item.Issuer,
			Ticker:    item.Ticker,
		}
	}

	if result.Instrument != nil && item.BondDetails != nil {
		details := item.BondDetails
		result.Instrument.BondDetails = &StateBondDetails{
			NominalValue:      details.NominalValue,
			NominalCurrency:   details.NominalCurrency,
			CouponRatePercent: details.CouponRatePercent,
		}
		if !details.MaturityDate.IsZero() {
			result.Instrument.BondDetails.MaturityDate = details.MaturityDate.Format(time.DateOnly)
		}
	}

	return result
}
