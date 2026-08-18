package fund

import "time"

type StateResult struct {
	OfficialSnapshot StateOfficialSnapshot `json:"official_snapshot"`
	Market           StateMarket           `json:"market"`
}

type HistoryResult struct {
	DailyValues      []StateDailyValue       `json:"daily_values"`
	UnitMarketPrices []StateDailyMarketPrice `json:"unit_market_prices"`
}

type MarketHistoryResult struct {
	UnitPrices []StateIntradayMarketPrice `json:"unit_prices"`
	LiveValues []StateLiveValuePoint      `json:"live_values"`
}

type StateMarket struct {
	UnitPrice     *StateMarketPrice   `json:"unit_price"`
	USDRUB        *StateFXRate        `json:"usd_rub"`
	LiveValuation *StateLiveValuation `json:"live_valuation"`
}

type StateLiveValuation struct {
	ObservedAt time.Time `json:"observed_at"`

	EstimatedNAVUSD                 string  `json:"estimated_nav_usd"`
	EstimatedCalculatedUnitValueUSD string  `json:"estimated_calculated_unit_value_usd"`
	EstimatedCalculatedUnitValueRUB *string `json:"estimated_calculated_unit_value_rub"`
	PremiumDiscountPercent          *string `json:"premium_discount_percent"`
	LiveDeltaUSD                    string  `json:"live_delta_usd"`
	LiveCoveragePercent             string  `json:"live_coverage_percent"`
}

type StateFXRate struct {
	Rate     string    `json:"rate"`
	PricedAt time.Time `json:"priced_at"`
}

type StateMarketPrice struct {
	InstrumentID int64     `json:"instrument_id"`
	UnitValue    string    `json:"unit_value"`
	Currency     string    `json:"currency"`
	PricedAt     time.Time `json:"priced_at"`
}

type StateOfficialSnapshot struct {
	AsOfDate               string          `json:"as_of_date"`
	ObservedAt             time.Time       `json:"observed_at"`
	CalculatedUnitValueUSD string          `json:"calculated_unit_value_usd"`
	NAVUSD                 string          `json:"nav_usd"`
	Assets                 []StateAsset    `json:"assets"`
	Categories             []StateCategory `json:"categories"`
}

type StateAsset struct {
	RowNo int `json:"row_no"`

	SourceName string `json:"source_name"`
	SourceType string `json:"source_type"`

	Instrument *StateInstrument `json:"instrument"`
	Currency   *string          `json:"currency"`
	Quantity   *string          `json:"quantity"`

	AssetSharePercent    string `json:"asset_share_percent"`
	AssetShareUpperBound bool   `json:"asset_share_upper_bound"`
}

type StateInstrument struct {
	ID        int64  `json:"id"`
	AssetType string `json:"asset_type"`
	ISIN      string `json:"isin"`
	Name      string `json:"name"`
	Issuer    string `json:"issuer,omitempty"`
	Ticker    string `json:"ticker,omitempty"`
}

type StateCategory struct {
	RowNo int `json:"row_no"`

	SourceName        string `json:"source_name"`
	AssetSharePercent string `json:"asset_share_percent"`
}

type StateDailyValue struct {
	AsOfDate               string `json:"as_of_date"`
	CalculatedUnitValueUSD string `json:"calculated_unit_value_usd"`
	NAVUSD                 string `json:"nav_usd"`
}

type StateDailyMarketPrice struct {
	AsOfDate  string `json:"as_of_date"`
	UnitValue string `json:"unit_value"`
	Currency  string `json:"currency"`
}

type StateIntradayMarketPrice struct {
	UnitValue string    `json:"unit_value"`
	Currency  string    `json:"currency"`
	PricedAt  time.Time `json:"priced_at"`
}

type StateLiveValuePoint struct {
	ObservedAt time.Time `json:"observed_at"`

	EstimatedNAVUSD                 string `json:"estimated_nav_usd"`
	EstimatedCalculatedUnitValueUSD string `json:"estimated_calculated_unit_value_usd"`
	LiveDeltaUSD                    string `json:"live_delta_usd"`
	LiveCoveragePercent             string `json:"live_coverage_percent"`
}
