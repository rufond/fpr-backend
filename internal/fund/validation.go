package fund

import (
	"fmt"
	"math/big"
	"strings"
	"time"

	govalidate "github.com/xloss/go-validate"
	"github.com/xloss/go-validate/rules"

	"github.com/rufond/fpr-backend/internal/validationrules"
)

type snapshotValidationInput struct {
	CalculatedUnitValueUSD string `json:"calculated_unit_value_usd"`
	NAVUSD                 string `json:"nav_usd"`
	DeclaredAssetCount     int    `json:"declared_asset_count"`
}

type assetValidationInput struct {
	Kind              string `json:"kind"`
	SourceName        string `json:"source_name"`
	SourceType        string `json:"source_type"`
	ISIN              string `json:"isin"`
	Currency          string `json:"currency"`
	Quantity          string `json:"quantity"`
	AssetSharePercent string `json:"asset_share_percent"`
}

type categoryValidationInput struct {
	SourceName        string `json:"source_name"`
	AssetSharePercent string `json:"asset_share_percent"`
}

type dailyValueValidationInput struct {
	CalculatedUnitValueUSD string `json:"calculated_unit_value_usd"`
	NAVUSD                 string `json:"nav_usd"`
}

func ValidateSourcePage(page *SourcePage) error {
	if page == nil {
		return fmt.Errorf("source page is nil")
	}

	if err := validateSnapshot(page.Snapshot); err != nil {
		return err
	}
	if len(page.History) == 0 {
		return fmt.Errorf("source history is empty")
	}

	var previous time.Time
	for index, item := range page.History {
		if item.AsOfDate.IsZero() {
			return fmt.Errorf("history point %d date is empty", index+1)
		}
		if index > 0 && !item.AsOfDate.After(previous) {
			return fmt.Errorf("history point %d is not strictly newer than the previous point", index+1)
		}
		if err := validateDailyValueFields(item); err != nil {
			return fmt.Errorf("history point %d: %w", index+1, err)
		}
		if err := validatePositiveDecimal(item.CalculatedUnitValueUSD, "calculated unit value"); err != nil {
			return fmt.Errorf("history point %d: %w", index+1, err)
		}
		if err := validatePositiveDecimal(item.NAVUSD, "NAV"); err != nil {
			return fmt.Errorf("history point %d: %w", index+1, err)
		}
		previous = item.AsOfDate
	}

	latest := page.History[len(page.History)-1]
	if !sameCalendarDate(latest.AsOfDate, page.Snapshot.AsOfDate) {
		return fmt.Errorf("latest history date differs from snapshot date")
	}
	if !decimalEqual(latest.CalculatedUnitValueUSD, page.Snapshot.CalculatedUnitValueUSD) ||
		!decimalEqual(latest.NAVUSD, page.Snapshot.NAVUSD) {
		return fmt.Errorf("latest history values differ from snapshot values")
	}

	return nil
}

func validateSnapshot(snapshot SourceSnapshot) error {
	if snapshot.AsOfDate.IsZero() {
		return fmt.Errorf("snapshot date is empty")
	}

	if err := validateWithGoValidate[snapshotValidationInput](
		"snapshot",
		map[string]any{
			"calculated_unit_value_usd": snapshot.CalculatedUnitValueUSD,
			"nav_usd":                   snapshot.NAVUSD,
			"declared_asset_count":      snapshot.DeclaredAssetCount,
		},
		map[string][]any{
			"calculated_unit_value_usd": {rules.Required{}, rules.String{}},
			"nav_usd":                   {rules.Required{}, rules.String{}},
			"declared_asset_count":      {rules.Required{}, rules.Integer{}, rules.Min{Min: 1}},
		},
	); err != nil {
		return err
	}

	// Source decimals intentionally stay strings until persistence, so they do
	// not pass through go-validate's float64-oriented Numeric rule.
	if err := validatePositiveDecimal(snapshot.CalculatedUnitValueUSD, "snapshot calculated unit value"); err != nil {
		return err
	}
	if err := validatePositiveDecimal(snapshot.NAVUSD, "snapshot NAV"); err != nil {
		return err
	}
	if snapshot.DeclaredAssetCount != len(snapshot.Assets) {
		return fmt.Errorf("snapshot declares %d assets, got %d", snapshot.DeclaredAssetCount, len(snapshot.Assets))
	}
	if len(snapshot.Categories) == 0 {
		return fmt.Errorf("snapshot categories are empty")
	}

	for index, asset := range snapshot.Assets {
		if err := validateAsset(asset); err != nil {
			return fmt.Errorf("snapshot asset %d: %w", index+1, err)
		}
	}
	for index, category := range snapshot.Categories {
		if err := validateCategory(category); err != nil {
			return fmt.Errorf("snapshot category %d: %w", index+1, err)
		}
	}

	return nil
}

func validateAsset(asset SourceAsset) error {
	switch asset.Kind {
	case AssetKindEquity, AssetKindBond, AssetKindDepositaryReceipt, AssetKindClaim, AssetKindBrokerCash, AssetKindBankCash:
	default:
		return fmt.Errorf("unknown asset kind %q", asset.Kind)
	}

	fieldRules := map[string][]any{
		"kind":                {rules.Required{}, rules.String{}},
		"source_name":         {rules.String{}},
		"source_type":         {rules.Required{}, rules.String{}},
		"isin":                {rules.String{}},
		"currency":            {rules.String{}},
		"quantity":            {rules.String{}},
		"asset_share_percent": {rules.Required{}, rules.String{}},
	}
	if asset.IsSecurity() {
		fieldRules["source_name"] = []any{rules.Required{}, rules.String{}}
		fieldRules["isin"] = []any{rules.Required{}, rules.String{}, validationrules.ISIN{}}
		fieldRules["quantity"] = []any{rules.Required{}, rules.String{}}
	}

	if err := validateWithGoValidate[assetValidationInput](
		"asset",
		map[string]any{
			"kind":                asset.Kind,
			"source_name":         asset.SourceName,
			"source_type":         asset.SourceType,
			"isin":                asset.ISIN,
			"currency":            asset.Currency,
			"quantity":            asset.Quantity,
			"asset_share_percent": asset.AssetSharePercent,
		},
		fieldRules,
	); err != nil {
		return err
	}

	if err := validateShare(asset.AssetSharePercent); err != nil {
		return err
	}
	if asset.Currency != "" && !validCurrencyCode(asset.Currency) {
		return fmt.Errorf("invalid currency %q", asset.Currency)
	}

	if !asset.IsSecurity() {
		return nil
	}

	quantity, ok := decimalRat(asset.Quantity)
	if !ok || quantity.Sign() < 0 {
		return fmt.Errorf("security %s has invalid quantity %q", asset.ISIN, asset.Quantity)
	}

	return nil
}

func validateCategory(category SourceCategory) error {
	if err := validateWithGoValidate[categoryValidationInput](
		"category",
		map[string]any{
			"source_name":         category.SourceName,
			"asset_share_percent": category.AssetSharePercent,
		},
		map[string][]any{
			"source_name":         {rules.Required{}, rules.String{}},
			"asset_share_percent": {rules.Required{}, rules.String{}},
		},
	); err != nil {
		return err
	}

	return validateShare(category.AssetSharePercent)
}

func validateDailyValueFields(item DailyValue) error {
	return validateWithGoValidate[dailyValueValidationInput](
		"daily value",
		map[string]any{
			"calculated_unit_value_usd": item.CalculatedUnitValueUSD,
			"nav_usd":                   item.NAVUSD,
		},
		map[string][]any{
			"calculated_unit_value_usd": {rules.Required{}, rules.String{}},
			"nav_usd":                   {rules.Required{}, rules.String{}},
		},
	)
}

func validateWithGoValidate[T any](scope string, data map[string]any, fieldRules map[string][]any) error {
	_, validationErrors := govalidate.Run[T](data, fieldRules)
	if len(validationErrors) == 0 {
		return nil
	}

	validationError := validationErrors[0]
	if validationError.Attribute == "" {
		return fmt.Errorf("%s validation failed: %s", scope, validationError.Name)
	}

	return fmt.Errorf(
		"%s field %s failed %s validation",
		scope,
		validationError.Attribute,
		validationError.Name,
	)
}

func validateShare(raw string) error {
	value, ok := decimalRat(raw)
	if !ok {
		return fmt.Errorf("invalid asset share %q", raw)
	}
	if value.Sign() < 0 || value.Cmp(big.NewRat(100, 1)) > 0 {
		return fmt.Errorf("asset share %q is outside 0..100", raw)
	}
	return nil
}

func validatePositiveDecimal(raw string, name string) error {
	value, ok := decimalRat(raw)
	if !ok || value.Sign() <= 0 {
		return fmt.Errorf("%s must be positive, got %q", name, raw)
	}
	return nil
}

func decimalEqual(left string, right string) bool {
	leftValue, leftOK := decimalRat(left)
	rightValue, rightOK := decimalRat(right)
	return leftOK && rightOK && leftValue.Cmp(rightValue) == 0
}

func decimalRat(raw string) (*big.Rat, bool) {
	text := strings.TrimSpace(raw)
	text = strings.ReplaceAll(text, "\u00a0", "")
	text = strings.ReplaceAll(text, " ", "")
	text = strings.ReplaceAll(text, ",", ".")
	return new(big.Rat).SetString(text)
}

func validCurrencyCode(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, char := range value {
		if char < 'A' || char > 'Z' {
			return false
		}
	}
	return true
}

func sameCalendarDate(left time.Time, right time.Time) bool {
	return left.Year() == right.Year() && left.Month() == right.Month() && left.Day() == right.Day()
}
