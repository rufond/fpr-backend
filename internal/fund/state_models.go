package fund

import "time"

type StateResult struct {
	OfficialSnapshot StateOfficialSnapshot `json:"official_snapshot"`
}

type HistoryResult struct {
	DailyValues []StateDailyValue `json:"daily_values"`
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
