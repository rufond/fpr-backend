package fund

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rufond/fpr-backend/internal/dateonly"
	"github.com/rufond/fpr-backend/internal/decimal"
)

type snapshotHashPayload struct {
	AsOfDate               string                 `json:"as_of_date"`
	CalculatedUnitValueUSD string                 `json:"calculated_unit_value_usd"`
	NAVUSD                 string                 `json:"nav_usd"`
	Assets                 []snapshotHashAsset    `json:"assets"`
	Categories             []snapshotHashCategory `json:"categories"`
}

type snapshotHashAsset struct {
	SourceType           string `json:"source_type"`
	SourceName           string `json:"source_name"`
	ISIN                 string `json:"isin"`
	Currency             string `json:"currency"`
	Quantity             string `json:"quantity"`
	AssetSharePercent    string `json:"asset_share_percent"`
	AssetShareUpperBound bool   `json:"asset_share_upper_bound"`
}

type snapshotHashCategory struct {
	SourceName        string `json:"source_name"`
	AssetSharePercent string `json:"asset_share_percent"`
}

func SnapshotSourceHash(snapshot SourceSnapshot) (string, error) {
	calculatedUnitValue, err := decimal.Canonical(snapshot.CalculatedUnitValueUSD)
	if err != nil {
		return "", fmt.Errorf("normalize calculated unit value: %w", err)
	}
	nav, err := decimal.Canonical(snapshot.NAVUSD)
	if err != nil {
		return "", fmt.Errorf("normalize NAV: %w", err)
	}

	payload := snapshotHashPayload{
		AsOfDate:               dateonly.UTC(snapshot.AsOfDate).Format(time.DateOnly),
		CalculatedUnitValueUSD: calculatedUnitValue,
		NAVUSD:                 nav,
		Assets:                 make([]snapshotHashAsset, 0, len(snapshot.Assets)),
		Categories:             make([]snapshotHashCategory, 0, len(snapshot.Categories)),
	}

	for index, asset := range snapshot.Assets {
		quantity := ""
		if strings.TrimSpace(asset.Quantity) != "" {
			quantity, err = decimal.Canonical(asset.Quantity)
			if err != nil {
				return "", fmt.Errorf("normalize asset %d quantity: %w", index+1, err)
			}
		}

		share, errShare := decimal.Canonical(asset.AssetSharePercent)
		if errShare != nil {
			return "", fmt.Errorf("normalize asset %d share: %w", index+1, errShare)
		}

		payload.Assets = append(payload.Assets, snapshotHashAsset{
			SourceType:           strings.TrimSpace(asset.SourceType),
			SourceName:           strings.TrimSpace(asset.SourceName),
			ISIN:                 strings.ToUpper(strings.TrimSpace(asset.ISIN)),
			Currency:             strings.ToUpper(strings.TrimSpace(asset.Currency)),
			Quantity:             quantity,
			AssetSharePercent:    share,
			AssetShareUpperBound: asset.AssetShareUpperBound,
		})
	}

	for index, category := range snapshot.Categories {
		share, errShare := decimal.Canonical(category.AssetSharePercent)
		if errShare != nil {
			return "", fmt.Errorf("normalize category %d share: %w", index+1, errShare)
		}
		payload.Categories = append(payload.Categories, snapshotHashCategory{
			SourceName:        strings.TrimSpace(category.SourceName),
			AssetSharePercent: share,
		})
	}

	slices.SortFunc(payload.Assets, func(left, right snapshotHashAsset) int {
		return strings.Compare(snapshotHashAssetKey(left), snapshotHashAssetKey(right))
	})
	slices.SortFunc(payload.Categories, func(left, right snapshotHashCategory) int {
		if byName := strings.Compare(left.SourceName, right.SourceName); byName != 0 {
			return byName
		}
		return strings.Compare(left.AssetSharePercent, right.AssetSharePercent)
	})

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal snapshot hash payload: %w", err)
	}

	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func snapshotHashAssetKey(asset snapshotHashAsset) string {
	return strings.Join([]string{
		asset.SourceType,
		asset.SourceName,
		asset.ISIN,
		asset.Currency,
		asset.Quantity,
		asset.AssetSharePercent,
		fmt.Sprintf("%t", asset.AssetShareUpperBound),
	}, "\x00")
}
