package fund

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
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
	calculatedUnitValue, err := canonicalHashDecimal(snapshot.CalculatedUnitValueUSD)
	if err != nil {
		return "", fmt.Errorf("normalize calculated unit value: %w", err)
	}
	nav, err := canonicalHashDecimal(snapshot.NAVUSD)
	if err != nil {
		return "", fmt.Errorf("normalize NAV: %w", err)
	}

	payload := snapshotHashPayload{
		AsOfDate:               dateOnlyUTC(snapshot.AsOfDate).Format("2006-01-02"),
		CalculatedUnitValueUSD: calculatedUnitValue,
		NAVUSD:                 nav,
		Assets:                 make([]snapshotHashAsset, 0, len(snapshot.Assets)),
		Categories:             make([]snapshotHashCategory, 0, len(snapshot.Categories)),
	}

	for index, asset := range snapshot.Assets {
		quantity := ""
		if strings.TrimSpace(asset.Quantity) != "" {
			quantity, err = canonicalHashDecimal(asset.Quantity)
			if err != nil {
				return "", fmt.Errorf("normalize asset %d quantity: %w", index+1, err)
			}
		}

		share, errShare := canonicalHashDecimal(asset.AssetSharePercent)
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
		share, errShare := canonicalHashDecimal(category.AssetSharePercent)
		if errShare != nil {
			return "", fmt.Errorf("normalize category %d share: %w", index+1, errShare)
		}
		payload.Categories = append(payload.Categories, snapshotHashCategory{
			SourceName:        strings.TrimSpace(category.SourceName),
			AssetSharePercent: share,
		})
	}

	sort.Slice(payload.Assets, func(i, j int) bool {
		return snapshotHashAssetKey(payload.Assets[i]) < snapshotHashAssetKey(payload.Assets[j])
	})
	sort.Slice(payload.Categories, func(i, j int) bool {
		left := payload.Categories[i]
		right := payload.Categories[j]
		if left.SourceName != right.SourceName {
			return left.SourceName < right.SourceName
		}
		return left.AssetSharePercent < right.AssetSharePercent
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

func canonicalHashDecimal(raw string) (string, error) {
	text := strings.TrimSpace(raw)
	text = strings.ReplaceAll(text, "\u00a0", "")
	text = strings.ReplaceAll(text, " ", "")
	text = strings.ReplaceAll(text, ",", ".")
	if text == "" {
		return "", fmt.Errorf("decimal is empty")
	}

	if _, ok := new(big.Rat).SetString(text); !ok {
		return "", fmt.Errorf("invalid decimal %q", raw)
	}

	sign := ""
	if after, ok := strings.CutPrefix(text, "+"); ok {
		text = after
	} else if strings.HasPrefix(text, "-") {
		sign = "-"
		text = strings.TrimPrefix(text, "-")
	}

	parts := strings.Split(text, ".")
	if len(parts) > 2 {
		return "", fmt.Errorf("invalid decimal %q", raw)
	}

	integer := strings.TrimLeft(parts[0], "0")
	if integer == "" {
		integer = "0"
	}

	fraction := ""
	if len(parts) == 2 {
		fraction = strings.TrimRight(parts[1], "0")
	}

	if integer == "0" && fraction == "" {
		return "0", nil
	}
	if fraction == "" {
		return sign + integer, nil
	}

	return sign + integer + "." + fraction, nil
}
