package fund

import "time"

const (
	AssetKindEquity            = "equity"
	AssetKindBond              = "bond"
	AssetKindDepositaryReceipt = "depositary_receipt"
	AssetKindClaim             = "claim"
	AssetKindBrokerCash        = "broker_cash"
	AssetKindBankCash          = "bank_cash"
)

type SourceSnapshot struct {
	AsOfDate time.Time

	CalculatedUnitValueUSD string
	NAVUSD                 string

	DeclaredAssetCount int
	Assets             []SourceAsset
	Categories         []SourceCategory
}

type SourceAsset struct {
	SourceName string
	SourceType string
	Kind       string

	ISIN     string
	Currency string
	Quantity string

	AssetSharePercent    string
	AssetShareUpperBound bool
}

func (a SourceAsset) IsSecurity() bool {
	switch a.Kind {
	case AssetKindEquity, AssetKindBond, AssetKindDepositaryReceipt:
		return true
	default:
		return false
	}
}

type SourceCategory struct {
	SourceName        string
	AssetSharePercent string
}

type DailyValue struct {
	AsOfDate               time.Time
	CalculatedUnitValueUSD string
	NAVUSD                 string
}

type SourcePage struct {
	Snapshot SourceSnapshot
	History  []DailyValue
}
