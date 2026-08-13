package managementcompany

import (
	"fmt"
	"html"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rufond/fpr-backend/internal/fund"
)

var (
	headDataRE           = regexp.MustCompile(`(?is)<div\s+class=["']head-data["']>\s*(.*?)\s*<span>\s*([^<]+?)\s*</span>\s*<span>\s*на\s+([^<]+?)\s*</span>\s*</div>`)
	compositionSectionRE = regexp.MustCompile(`(?is)<div\s+class=["']fund-detail-item\s+assets["'][^>]*data-item=["']3["'][^>]*>(.*?)(?:<div\s+class=["']fund-detail-item["'][^>]*data-item=["']5["']|$)`)
	declaredAssetCountRE = regexp.MustCompile(`(?is)Активы\s+паевого\s+инвестиционного\s+фонда\s+инвестированы\s+в\s+(\d+)\s+объект`)
	compositionDateRE    = regexp.MustCompile(`(?is)Данные\s+по\s+состоянию\s+на\s+(\d{2}\.\d{2}\.\d{4})`)
	assetRowRE           = regexp.MustCompile(`(?is)<div\s+class=["']row\s+(?:top5|top0)["']>\s*<div\s+class=["']cell\s+title["']>(.*?)</div>\s*<div\s+class=["']cell["']>(.*?)</div>\s*<div\s+class=["']cell\s+right\s+desktop["']>(.*?)</div>\s*<div\s+class=["']cell\s+right["'][^>]*>(.*?)</div>\s*</div>`)
	assetTypeRE          = regexp.MustCompile(`(?is)<br\s*/?>\s*\(([^()]*)\)\s*$`)
	currencySuffixRE     = regexp.MustCompile(`\(([A-Z]{3})\)\s*$`)
	assetsRowsRE         = regexp.MustCompile(`(?is)assetsRows\s*=\s*\[(.*?)]\s*;`)
	assetCategoryRE      = regexp.MustCompile(`(?is)\[\s*'((?:\\.|[^'])*)'\s*,\s*([+-]?(?:\d+(?:\.\d*)?|\.\d+))\s*,`)
	shareRowsRE          = regexp.MustCompile(`(?is)shareRows\s*=\s*\[(.*?)]\s*;`)
	chaRowsRE            = regexp.MustCompile(`(?is)chaRows\s*=\s*\[(.*?)]\s*;`)
	historyPointRE       = regexp.MustCompile(`\[\s*new\s+Date\(\s*(\d{4})\s*,\s*(\d{1,2})\s*,\s*(\d{1,2})\s*\)\s*,\s*([+-]?(?:\d+(?:\.\d*)?|\.\d+))\s*]`)
	tagRE                = regexp.MustCompile(`(?is)<[^>]+>`)
	htmlCommentRE        = regexp.MustCompile(`(?is)<!--.*?-->`)
)

func ParsePage(body []byte) (*fund.SourcePage, error) {
	snapshot, err := ParseSnapshot(body)
	if err != nil {
		return nil, err
	}

	history, err := ParseHistory(body)
	if err != nil {
		return nil, err
	}
	if len(history) == 0 {
		return nil, fmt.Errorf("parse management company page: official history is empty")
	}

	latest := history[len(history)-1]
	if !sameDate(latest.AsOfDate, snapshot.AsOfDate) {
		return nil, fmt.Errorf(
			"parse management company page: current snapshot date %s differs from latest history date %s",
			snapshot.AsOfDate.Format("2006-01-02"),
			latest.AsOfDate.Format("2006-01-02"),
		)
	}
	if latest.CalculatedUnitValueUSD != snapshot.CalculatedUnitValueUSD || latest.NAVUSD != snapshot.NAVUSD {
		return nil, fmt.Errorf("parse management company page: current values differ from latest history values")
	}

	return &fund.SourcePage{
		Snapshot: *snapshot,
		History:  history,
	}, nil
}

func ParseSnapshot(body []byte) (*fund.SourceSnapshot, error) {
	source := string(body)

	calculatedUnitValue, calculatedUnitValueDate, nav, navDate, err := parseCurrentValues(source)
	if err != nil {
		return nil, err
	}
	if !sameDate(calculatedUnitValueDate, navDate) {
		return nil, fmt.Errorf(
			"parse management company snapshot: calculated unit value date %s differs from NAV date %s",
			calculatedUnitValueDate.Format("2006-01-02"),
			navDate.Format("2006-01-02"),
		)
	}

	sectionMatch := compositionSectionRE.FindStringSubmatch(source)
	if len(sectionMatch) != 2 {
		return nil, fmt.Errorf("parse management company snapshot: asset section not found")
	}
	assetSection := sectionMatch[1]

	declaredCount, err := parseDeclaredAssetCount(source)
	if err != nil {
		return nil, err
	}
	compositionDate, err := parseCompositionDate(assetSection)
	if err != nil {
		return nil, err
	}
	if !sameDate(calculatedUnitValueDate, compositionDate) {
		return nil, fmt.Errorf(
			"parse management company snapshot: value date %s differs from composition date %s",
			calculatedUnitValueDate.Format("2006-01-02"),
			compositionDate.Format("2006-01-02"),
		)
	}

	assets, err := parseAssets(assetSection)
	if err != nil {
		return nil, err
	}
	if len(assets) != declaredCount {
		return nil, fmt.Errorf(
			"parse management company snapshot: parsed %d asset rows, source declares %d objects",
			len(assets),
			declaredCount,
		)
	}

	categories, err := parseCategories(assetSection)
	if err != nil {
		return nil, err
	}

	return &fund.SourceSnapshot{
		AsOfDate:               calculatedUnitValueDate,
		CalculatedUnitValueUSD: calculatedUnitValue,
		NAVUSD:                 nav,
		DeclaredAssetCount:     declaredCount,
		Assets:                 assets,
		Categories:             categories,
	}, nil
}

func ParseHistory(body []byte) ([]fund.DailyValue, error) {
	source := string(body)

	calculatedUnitValues, err := parseHistorySeries(source, shareRowsRE, "shareRows")
	if err != nil {
		return nil, err
	}
	nav, err := parseHistorySeries(source, chaRowsRE, "chaRows")
	if err != nil {
		return nil, err
	}
	if len(calculatedUnitValues) != len(nav) {
		return nil, fmt.Errorf("parse management company history: shareRows has %d points, chaRows has %d", len(calculatedUnitValues), len(nav))
	}

	dates := make([]time.Time, 0, len(calculatedUnitValues))
	for key, point := range calculatedUnitValues {
		if _, ok := nav[key]; !ok {
			return nil, fmt.Errorf("parse management company history: NAV value is missing for %s", key)
		}
		dates = append(dates, point.Date)
	}
	for key := range nav {
		if _, ok := calculatedUnitValues[key]; !ok {
			return nil, fmt.Errorf("parse management company history: calculated unit value is missing for %s", key)
		}
	}

	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })

	result := make([]fund.DailyValue, 0, len(dates))
	for _, date := range dates {
		key := date.Format("2006-01-02")
		result = append(result, fund.DailyValue{
			AsOfDate:               date,
			CalculatedUnitValueUSD: calculatedUnitValues[key].Value,
			NAVUSD:                 nav[key].Value,
		})
	}

	return result, nil
}

func parseCurrentValues(source string) (string, time.Time, string, time.Time, error) {
	matches := headDataRE.FindAllStringSubmatch(source, -1)
	if len(matches) == 0 {
		return "", time.Time{}, "", time.Time{}, fmt.Errorf("parse management company snapshot: head-data values not found")
	}

	var (
		calculatedUnitValue     string
		calculatedUnitValueDate time.Time
		nav                     string
		navDate                 time.Time
	)

	for _, match := range matches {
		if len(match) != 4 {
			continue
		}

		label := strings.ToLower(cleanText(match[1]))
		valueText := cleanText(match[2])
		dateText := cleanText(match[3])

		switch {
		case strings.Contains(label, "расчетная стоимость пая"):
			value, err := parseUSDValue(valueText)
			if err != nil {
				return "", time.Time{}, "", time.Time{}, fmt.Errorf("parse management company snapshot calculated unit value: %w", err)
			}
			date, err := parseRussianDate(dateText)
			if err != nil {
				return "", time.Time{}, "", time.Time{}, fmt.Errorf("parse management company snapshot calculated unit value date: %w", err)
			}
			calculatedUnitValue, calculatedUnitValueDate = value, date

		case strings.Contains(label, "чистых активов"):
			value, err := parseUSDValue(valueText)
			if err != nil {
				return "", time.Time{}, "", time.Time{}, fmt.Errorf("parse management company snapshot NAV: %w", err)
			}
			date, err := parseRussianDate(dateText)
			if err != nil {
				return "", time.Time{}, "", time.Time{}, fmt.Errorf("parse management company snapshot NAV date: %w", err)
			}
			nav, navDate = value, date
		}
	}

	if calculatedUnitValue == "" || calculatedUnitValueDate.IsZero() {
		return "", time.Time{}, "", time.Time{}, fmt.Errorf("parse management company snapshot: current calculated unit value not found")
	}
	if nav == "" || navDate.IsZero() {
		return "", time.Time{}, "", time.Time{}, fmt.Errorf("parse management company snapshot: current NAV not found")
	}

	return calculatedUnitValue, calculatedUnitValueDate, nav, navDate, nil
}

func parseDeclaredAssetCount(source string) (int, error) {
	source = htmlCommentRE.ReplaceAllString(source, "")
	match := declaredAssetCountRE.FindStringSubmatch(source)
	if len(match) != 2 {
		return 0, fmt.Errorf("parse management company snapshot: declared asset count not found")
	}

	count, err := strconv.Atoi(match[1])
	if err != nil || count < 1 {
		return 0, fmt.Errorf("parse management company snapshot: invalid declared asset count %q", match[1])
	}

	return count, nil
}

func parseCompositionDate(section string) (time.Time, error) {
	match := compositionDateRE.FindStringSubmatch(section)
	if len(match) != 2 {
		return time.Time{}, fmt.Errorf("parse management company snapshot: composition date not found")
	}

	date, err := parseRussianDate(match[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("parse management company snapshot composition date: %w", err)
	}

	return date, nil
}

func parseAssets(section string) ([]fund.SourceAsset, error) {
	matches := assetRowRE.FindAllStringSubmatch(section, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("parse management company snapshot: asset rows not found")
	}

	assets := make([]fund.SourceAsset, 0, len(matches))
	for index, match := range matches {
		if len(match) != 5 {
			continue
		}

		name, sourceType, err := parseAssetTitle(match[1])
		if err != nil {
			return nil, fmt.Errorf("parse management company snapshot asset row %d: %w", index+1, err)
		}
		kind, err := sourceTypeKind(sourceType)
		if err != nil {
			return nil, fmt.Errorf("parse management company snapshot asset row %d: %w", index+1, err)
		}

		isin := strings.ToUpper(strings.TrimSpace(cleanText(match[2])))
		quantityText := cleanText(match[3])
		quantity := ""
		if quantityText != "" {
			quantity, err = canonicalDecimal(quantityText)
			if err != nil {
				return nil, fmt.Errorf("parse management company snapshot asset row %d quantity: %w", index+1, err)
			}
		}

		share, upperBound, err := parseAssetShare(cleanText(match[4]))
		if err != nil {
			return nil, fmt.Errorf("parse management company snapshot asset row %d share: %w", index+1, err)
		}

		asset := fund.SourceAsset{
			SourceName:           name,
			SourceType:           sourceType,
			Kind:                 kind,
			ISIN:                 isin,
			Currency:             extractCurrency(name),
			Quantity:             quantity,
			AssetSharePercent:    share,
			AssetShareUpperBound: upperBound,
		}

		if asset.IsSecurity() {
			if asset.ISIN == "" {
				return nil, fmt.Errorf("parse management company snapshot asset row %d: security %q has no ISIN", index+1, name)
			}
			if asset.Quantity == "" {
				return nil, fmt.Errorf("parse management company snapshot asset row %d: security %q has no quantity", index+1, name)
			}
		}

		assets = append(assets, asset)
	}

	return assets, nil
}

func parseAssetTitle(raw string) (string, string, error) {
	match := assetTypeRE.FindStringSubmatch(raw)
	if len(match) != 2 {
		return "", "", fmt.Errorf("asset source type not found")
	}

	sourceType := cleanText(match[1])
	if sourceType == "" {
		return "", "", fmt.Errorf("asset source type is empty")
	}

	nameRaw := assetTypeRE.ReplaceAllString(raw, "")
	return cleanText(nameRaw), sourceType, nil
}

func sourceTypeKind(sourceType string) (string, error) {
	switch strings.TrimSpace(sourceType) {
	case "Акции":
		return fund.AssetKindEquity, nil
	case "Облигации":
		return fund.AssetKindBond, nil
	case "Права требования к эмитентам и контрагентам":
		return fund.AssetKindClaim, nil
	case "Денежные средства у брокера":
		return fund.AssetKindBrokerCash, nil
	case "Денежные средства в кредитной организации":
		return fund.AssetKindBankCash, nil
	default:
		if strings.HasPrefix(sourceType, "Депозитарная расписка") {
			return fund.AssetKindDepositaryReceipt, nil
		}
	}

	return "", fmt.Errorf("unknown management company asset source type %q", sourceType)
}

func parseCategories(section string) ([]fund.SourceCategory, error) {
	block := assetsRowsRE.FindStringSubmatch(section)
	if len(block) != 2 {
		return nil, fmt.Errorf("parse management company snapshot: assetsRows not found")
	}

	matches := assetCategoryRE.FindAllStringSubmatch(block[1], -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("parse management company snapshot: assetsRows is empty")
	}

	categories := make([]fund.SourceCategory, 0, len(matches))
	for index, match := range matches {
		if len(match) != 3 {
			continue
		}

		name := strings.ReplaceAll(match[1], `\'`, `'`)
		name = strings.ReplaceAll(name, `\\`, `\`)
		name = strings.TrimSpace(html.UnescapeString(name))
		share, err := canonicalDecimal(match[2])
		if err != nil {
			return nil, fmt.Errorf("parse management company snapshot category %d share: %w", index+1, err)
		}

		categories = append(categories, fund.SourceCategory{
			SourceName:        name,
			AssetSharePercent: share,
		})
	}

	return categories, nil
}

type historyPoint struct {
	Date  time.Time
	Value string
}

func parseHistorySeries(source string, blockRE *regexp.Regexp, name string) (map[string]historyPoint, error) {
	block := blockRE.FindStringSubmatch(source)
	if len(block) != 2 {
		return nil, fmt.Errorf("parse management company history: %s not found", name)
	}

	matches := historyPointRE.FindAllStringSubmatch(block[1], -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("parse management company history: %s is empty", name)
	}

	result := make(map[string]historyPoint, len(matches))
	for index, match := range matches {
		if len(match) != 5 {
			continue
		}

		year, _ := strconv.Atoi(match[1])
		jsMonth, _ := strconv.Atoi(match[2])
		day, _ := strconv.Atoi(match[3])
		if jsMonth < 0 || jsMonth > 11 {
			return nil, fmt.Errorf("parse management company history: %s point %d has invalid JS month %d", name, index+1, jsMonth)
		}

		date := time.Date(year, time.Month(jsMonth+1), day, 0, 0, 0, 0, time.UTC)
		if date.Year() != year || int(date.Month()) != jsMonth+1 || date.Day() != day {
			return nil, fmt.Errorf("parse management company history: %s point %d has invalid date", name, index+1)
		}

		value, err := canonicalDecimal(match[4])
		if err != nil {
			return nil, fmt.Errorf("parse management company history: %s point %d value: %w", name, index+1, err)
		}

		key := date.Format("2006-01-02")
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("parse management company history: %s has duplicate date %s", name, key)
		}
		result[key] = historyPoint{Date: date, Value: value}
	}

	return result, nil
}

func parseUSDValue(raw string) (string, error) {
	text := strings.TrimSpace(raw)
	upper := strings.ToUpper(text)
	if !strings.HasSuffix(upper, "USD") {
		return "", fmt.Errorf("expected USD value, got %q", raw)
	}

	text = strings.TrimSpace(text[:len(text)-len("USD")])
	return canonicalDecimal(text)
}

func parseAssetShare(raw string) (string, bool, error) {
	text := strings.TrimSpace(raw)
	text = strings.TrimSuffix(text, "%")
	text = strings.TrimSpace(text)

	upperBound := false
	lower := strings.ToLower(text)
	switch {
	case strings.HasPrefix(lower, "менее"):
		upperBound = true
		runes := []rune(text)
		text = strings.TrimSpace(string(runes[len([]rune("менее")):]))
	case strings.HasPrefix(text, "<"):
		upperBound = true
		text = strings.TrimSpace(strings.TrimPrefix(text, "<"))
	}

	value, err := canonicalDecimal(text)
	if err != nil {
		return "", false, err
	}

	return value, upperBound, nil
}

func extractCurrency(name string) string {
	match := currencySuffixRE.FindStringSubmatch(strings.TrimSpace(name))
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func parseRussianDate(raw string) (time.Time, error) {
	date, err := time.Parse("02.01.2006", strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC), nil
}

func canonicalDecimal(raw string) (string, error) {
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
		sign = ""
	}
	if fraction == "" {
		return sign + integer, nil
	}

	return sign + integer + "." + fraction, nil
}

func cleanText(raw string) string {
	text := tagRE.ReplaceAllString(raw, " ")
	text = html.UnescapeString(text)
	text = strings.Map(func(r rune) rune {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\ufeff':
			return -1
		default:
			return r
		}
	}, text)
	return strings.Join(strings.Fields(text), " ")
}

func sameDate(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}
