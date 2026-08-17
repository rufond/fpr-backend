package valuation

import "time"

const (
	identityFXProvider  = "identity"
	valuePrecision      = 18
	valuePointRetention = 48 * time.Hour
)

type baseline struct {
	AssetID        int64
	PriceSourceID  int64
	Provider       string
	ProviderSymbol string

	UnitValue string
	Currency  string
	PricedAt  time.Time

	FXRateToUSD string
	FXProvider  string
	FXPricedAt  time.Time

	MarketValueUSD string
}

type valuePoint struct {
	SnapshotID int64
	ObservedAt time.Time

	EstimatedNAVUSD                 string
	EstimatedCalculatedUnitValueUSD string
	LiveDeltaUSD                    string
	LiveCoveragePercent             string
}
