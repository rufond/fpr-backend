package fund

import (
	"time"

	"github.com/rufond/fpr-backend/internal/prices"
)

type StateResult struct {
	OfficialSnapshot StateOfficialSnapshot `json:"official_snapshot"`
	Market           StateMarket           `json:"market"`
}

type HistoryResult struct {
	DailyValues      []StateDailyValue       `json:"daily_values"`
	UnitMarketPrices []StateDailyMarketPrice `json:"unit_market_prices"`
}

type MarketHistoryResult struct {
	UnitPricesUSD     []StateIntradayMarketPrice `json:"unit_prices_usd,omitempty"`
	USDRUB            *StateFXRate               `json:"usd_rub,omitempty"`
	UnitChange        string                     `json:"unit_change,omitempty"`
	UnitChangePercent string                     `json:"unit_change_percent,omitempty"`
	UnitPrices        []StateIntradayMarketPrice `json:"unit_prices"`
	LiveValues        []StateLiveValuePoint      `json:"live_values"`
}

type StateMarket struct {
	UnitPriceUSD  string                     `json:"unit_price_usd,omitempty"`
	UnitPrice     *StateMarketPrice          `json:"unit_price"`
	AssetPrices   map[int64]StateMarketPrice `json:"asset_prices"`
	USDRUB        *StateFXRate               `json:"usd_rub"`
	LiveValuation *StateLiveValuation        `json:"live_valuation"`
}

type StateLiveValuation struct {
	ObservedAt time.Time `json:"observed_at"`

	EstimatedNAVUSD                 string  `json:"estimated_nav_usd"`
	EstimatedCalculatedUnitValueUSD string  `json:"estimated_calculated_unit_value_usd"`
	EstimatedCalculatedUnitValueRUB *string `json:"estimated_calculated_unit_value_rub"`
	PremiumDiscountPercent          *string `json:"premium_discount_percent"`
	LiveDeltaUSD                    string  `json:"live_delta_usd"`
	LiveDeltaPercent                string  `json:"live_delta_percent,omitempty"`
	LiveCoveragePercent             string  `json:"live_coverage_percent"`
}

type StateFXRate struct {
	Rate     string    `json:"rate"`
	PricedAt time.Time `json:"priced_at"`
}

type StateMarketPrice struct {
	EntryFX       *StateFXRate        `json:"entry_fx,omitempty"`
	PreviousClose string              `json:"previous_close,omitempty"`
	DayChange     *prices.PriceReturn `json:"day_change,omitempty"`
	EntryReturn   *prices.PriceReturn `json:"entry_return,omitempty"`
	Archived      bool                `json:"archived,omitempty"`
	Symbol        string              `json:"symbol,omitempty"`
	PositionValue string              `json:"position_value,omitempty"`
	InstrumentID  int64               `json:"instrument_id"`
	Provider      string              `json:"provider"`
	UnitValue     string              `json:"unit_value"`
	Currency      string              `json:"currency"`
	PricedAt      time.Time           `json:"priced_at"`
}

type StateOfficialSnapshot struct {
	AsOfDate               string          `json:"as_of_date"`
	ObservedAt             time.Time       `json:"observed_at"`
	CalculatedUnitValueUSD string          `json:"calculated_unit_value_usd"`
	NAVUSD                 string          `json:"nav_usd"`
	UnitCount              *StateUnitCount `json:"unit_count"`
	Assets                 []StateAsset    `json:"assets"`
	Categories             []StateCategory `json:"categories"`
}

type StateUnitCount struct {
	AsOfDate         string `json:"as_of_date"`
	UnitsOutstanding string `json:"units_outstanding"`
	SourceURL        string `json:"source_url"`
}

type StateAsset struct {
	DisclosureUnitEstimateUSD string `json:"disclosure_unit_estimate_usd,omitempty"`
	ShareChangePP             string `json:"share_change_pp,omitempty"`
	PreviousShareDate         string `json:"previous_share_date,omitempty"`
	RowNo                     int    `json:"row_no"`

	SourceName string `json:"source_name"`
	SourceType string `json:"source_type"`

	Instrument *StateInstrument `json:"instrument"`
	Currency   *string          `json:"currency"`
	Quantity   *string          `json:"quantity"`

	AssetSharePercent    string `json:"asset_share_percent"`
	AssetShareUpperBound bool   `json:"asset_share_upper_bound"`
}

type StateInstrument struct {
	ID          int64             `json:"id"`
	AssetType   string            `json:"asset_type"`
	ISIN        string            `json:"isin"`
	Name        string            `json:"name"`
	Issuer      string            `json:"issuer,omitempty"`
	Ticker      string            `json:"ticker,omitempty"`
	BondDetails *StateBondDetails `json:"bond_details,omitempty"`
}

type StateBondDetails struct {
	NominalValue      string `json:"nominal_value,omitempty"`
	NominalCurrency   string `json:"nominal_currency,omitempty"`
	MaturityDate      string `json:"maturity_date,omitempty"`
	CouponRatePercent string `json:"coupon_rate_percent,omitempty"`
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
