package portfolio

import (
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/decimal"
	"github.com/rufond/fpr-backend/internal/prices"
)

type Archive struct {
	reports      []Report
	summaries    []ReportSummary
	observations map[string][]Observation
	changes      []Change
	ipos         []IPO
	iposByName   map[string]IPO
	iposByISIN   map[string]IPO
}

type Observation struct {
	Position
	AsOfDate       string `json:"as_of_date"`
	ReportCurrency string `json:"report_currency"`
	SourceURL      string `json:"source_url"`
	UnitValue      string `json:"unit_value"`
	AssetShare     string `json:"asset_share"`
	NAVShare       string `json:"nav_share"`
}

type Holding struct {
	Observation
	Previous          *Observation `json:"previous,omitempty"`
	QuantityChange    string       `json:"quantity_change,omitempty"`
	ValueChange       string       `json:"value_change,omitempty"`
	UnitChangePercent string       `json:"unit_change_percent,omitempty"`
	IPO               *IPO         `json:"ipo,omitempty"`
	IPOComparisonUSD  string       `json:"ipo_comparison_usd,omitempty"`
	IPOComparisonPct  string       `json:"ipo_comparison_percent,omitempty"`
}

type Change struct {
	ISIN       string `json:"isin"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Type       string `json:"type"`
	FromDate   string `json:"from_date"`
	ToDate     string `json:"to_date"`
	FromSource string `json:"from_source"`
	ToSource   string `json:"to_source"`
	Before     string `json:"before"`
	After      string `json:"after"`
}

type ClosingReference struct {
	Price     string              `json:"price"`
	Currency  string              `json:"currency"`
	Basis     string              `json:"basis"`
	Date      string              `json:"date,omitempty"`
	SourceURL string              `json:"source_url,omitempty"`
	Return    *prices.PriceReturn `json:"return,omitempty"`
}

type ClosedPosition struct {
	Conversion  *Conversion       `json:"conversion,omitempty"`
	Entry       *IPO              `json:"entry,omitempty"`
	Exit        *ClosingReference `json:"exit,omitempty"`
	ISIN        string            `json:"isin"`
	Name        string            `json:"name"`
	Kind        string            `json:"kind"`
	Ticker      string            `json:"ticker,omitempty"`
	Quantity    string            `json:"quantity,omitempty"`
	LastSeen    string            `json:"last_seen,omitempty"`
	FirstAbsent string            `json:"first_absent,omitempty"`
	FromSource  string            `json:"from_source,omitempty"`
	ToSource    string            `json:"to_source,omitempty"`
}

type Snapshot struct {
	ReportSummary
	Rows []Holding `json:"rows"`
}

type Index struct {
	Reports          []ReportSummary    `json:"reports"`
	LatestValuations map[string]Holding `json:"latest_valuations"`
}

type Detail struct {
	Entry        *IPO          `json:"entry,omitempty"`
	ISIN         string        `json:"isin"`
	Name         string        `json:"name"`
	Observations []Observation `json:"observations"`
	Changes      []Change      `json:"changes"`
	Market       []MarketPoint `json:"market"`
}

type MarketPoint struct {
	At       string `json:"at"`
	Provider string `json:"provider"`
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

func NewArchive(reports []Report, ipos []IPO) *Archive {
	a := &Archive{
		reports: reports, ipos: ipos,
		summaries:    make([]ReportSummary, 0, len(reports)),
		observations: map[string][]Observation{},
		changes:      []Change{}, iposByName: map[string]IPO{}, iposByISIN: map[string]IPO{},
	}
	names := map[string]bool{}
	for _, ipo := range ipos {
		if ipo.ISIN != "" {
			a.iposByISIN[ipo.ISIN] = ipo
			continue
		}

		key := issuerKey(ipo.Name)
		if names[key] {
			delete(a.iposByName, key)
			continue
		}

		a.iposByName[key] = ipo
		names[key] = true
	}

	for i, report := range reports {
		a.summaries = append(a.summaries, report.ReportSummary)
		for _, position := range report.Positions {
			observation := makeObservation(position, report.ReportSummary)
			a.observations[position.ISIN] = append(a.observations[position.ISIN], observation)
		}
		if i > 0 {
			a.changes = append(a.changes, compare(positionsOf(reports[i-1]), positionsOf(report))...)
		}
	}

	return a
}

func (a *Archive) Index() Index {
	result := Index{
		Reports:          slices.Clone(a.summaries),
		LatestValuations: map[string]Holding{},
	}
	// Find the latest documented valuation for each instrument. The reporting
	// date travels with the value, including for instruments absent since then.
	for isin, series := range a.observations {
		last := series[len(series)-1]
		result.LatestValuations[isin] = a.holding(last)
	}

	return result
}

func (a *Archive) Snapshot(date string) (Snapshot, bool) {
	i, found := slices.BinarySearchFunc(a.reports, date, func(report Report, target string) int {
		return strings.Compare(report.AsOfDate, target)
	})
	if !found {
		return Snapshot{}, false
	}
	report := a.reports[i]
	result := Snapshot{ReportSummary: report.ReportSummary, Rows: make([]Holding, 0, len(report.Positions))}
	for _, position := range report.Positions {
		series := a.observations[position.ISIN]
		index, _ := findObservation(series, date)
		result.Rows = append(result.Rows, a.holding(series[index]))
	}

	slices.SortStableFunc(result.Rows, func(a, b Holding) int {
		left, _ := decimal.Parse(a.Value)
		right, _ := decimal.Parse(b.Value)
		return right.Cmp(left)
	})

	return result, true
}

func (a *Archive) holding(today Observation) Holding {
	row := Holding{Observation: today}
	series := a.observations[today.ISIN]
	index, _ := findObservation(series, today.AsOfDate)

	// Compare consecutive reports only. A re-entry starts a new series of
	// holdings for comparisons, even though the instrument's full history remains.
	reportIndex, _ := slices.BinarySearchFunc(a.summaries, today.AsOfDate, func(report ReportSummary, target string) int {
		return strings.Compare(report.AsOfDate, target)
	})

	qty, _ := decimal.Parse(today.Quantity)
	value, _ := decimal.Parse(today.Value)
	if index > 0 && reportIndex > 0 && series[index-1].AsOfDate == a.summaries[reportIndex-1].AsOfDate {
		previous := series[index-1]
		row.Previous = &previous
		oldQuantity, _ := decimal.Parse(previous.Quantity)
		row.QuantityChange = formatDecimal(new(big.Rat).Sub(qty, oldQuantity), 8)
		if previous.ReportCurrency == today.ReportCurrency {
			oldValue, _ := decimal.Parse(previous.Value)
			row.ValueChange = formatDecimal(new(big.Rat).Sub(value, oldValue), 2)
			row.UnitChangePercent = percentageChange(new(big.Rat).Quo(value, qty), new(big.Rat).Quo(oldValue, oldQuantity))
		}
	}

	if ipo, found := a.Entry(today.ISIN, today.Name, today.AsOfDate); found && today.Kind != "bond" {
		row.IPO = &ipo
		// Compare per unit without assuming the reported quantity matches the original lot.
		offer, ok := decimal.Parse(ipo.EntryPriceUSD)
		if today.ReportCurrency == "USD" && ok && offer.Sign() > 0 {
			unitValue := new(big.Rat).Quo(value, qty)
			row.IPOComparisonUSD = formatDecimal(new(big.Rat).Sub(unitValue, offer), 6)
			row.IPOComparisonPct = percentageChange(unitValue, offer)
		}
	}

	return row
}

func (a *Archive) Entry(isin, name, asOf string) (IPO, bool) {
	ipo, found := a.iposByISIN[isin]
	if !found || ipo.EntryPriceUSD == "" {
		if sheet, matched := a.iposByName[issuerKey(name)]; matched {
			ipo, found = sheet, true
		}
	}

	return ipo, found && (asOf == "" || ipo.Date <= asOf)
}

func (a *Archive) IPOs() []IPO {
	return slices.Clone(a.ipos)
}

func (a *Archive) Changes(state *appstate.State, officialURL string) []Change {
	if state == nil || state.Fund == nil || len(state.Fund.CompositionHistory) == 0 {
		return slices.Clone(a.changes)
	}

	snapshots := make([]positionSnapshot, 0, len(a.reports)+len(state.Fund.CompositionHistory))
	dates := make(map[string]struct{}, len(a.reports))
	for _, report := range a.reports {
		dates[report.AsOfDate] = struct{}{}
		snapshots = append(snapshots, positionsOf(report))
	}
	for _, snapshot := range state.Fund.CompositionHistory {
		date := snapshot.AsOfDate.Format(time.DateOnly)
		if _, exists := dates[date]; exists {
			continue
		}
		next := positionSnapshot{
			date:      date,
			source:    officialURL,
			positions: make(map[string]Position, len(snapshot.Holdings)),
		}
		for _, item := range snapshot.Holdings {
			next.positions[item.ISIN] = Position{
				ISIN:     item.ISIN,
				Name:     item.Name,
				Kind:     item.Kind,
				Quantity: item.Quantity,
			}
		}
		snapshots = append(snapshots, next)
	}

	slices.SortFunc(snapshots, func(left, right positionSnapshot) int { return strings.Compare(left.date, right.date) })

	var changes []Change
	for i := 1; i < len(snapshots); i++ {
		changes = append(changes, compare(snapshots[i-1], snapshots[i])...)
	}

	return changes
}

func (a *Archive) Closed(state *appstate.State, officialURL string) []ClosedPosition {
	var result []ClosedPosition
	if state == nil || state.Fund == nil {
		return result
	}

	active := make(map[string]struct{}, len(state.Fund.Snapshot.Assets))
	for _, asset := range state.Fund.Snapshot.Assets {
		if asset.ISIN != "" {
			active[asset.ISIN] = struct{}{}
		}
	}

	latestChanges := map[string]Change{}
	for _, item := range a.Changes(state, officialURL) {
		latestChanges[item.ISIN] = item
	}

	seen := make(map[string]struct{}, len(latestChanges))
	for isin, item := range latestChanges {
		if _, held := active[isin]; held || item.Type != "removed" {
			continue
		}
		result = append(result, ClosedPosition{
			ISIN:        isin,
			Name:        item.Name,
			Kind:        item.Kind,
			Ticker:      a.iposByISIN[isin].Ticker,
			Quantity:    item.Before,
			LastSeen:    item.FromDate,
			FirstAbsent: item.ToDate,
			FromSource:  item.FromSource,
			ToSource:    item.ToSource,
		})
		seen[isin] = struct{}{}
	}

	for isin, entry := range a.iposByISIN {
		_, held := active[isin]
		_, dated := seen[isin]
		if held || dated || entry.ClosedInLegacy == nil || !*entry.ClosedInLegacy {
			continue
		}

		kind := "equity"
		if observations := a.observations[isin]; len(observations) != 0 {
			kind = observations[len(observations)-1].Kind
		}
		result = append(result, ClosedPosition{
			ISIN: isin, Name: entry.Name, Kind: kind, Ticker: entry.Ticker, Quantity: entry.Quantity,
		})
	}

	for i := range result {
		a.addClosingReference(&result[i])
	}

	slices.SortFunc(result, func(left, right ClosedPosition) int {
		if byDate := strings.Compare(right.FirstAbsent, left.FirstAbsent); byDate != 0 {
			return byDate
		}
		return strings.Compare(left.ISIN, right.ISIN)
	})

	return result
}

func (a *Archive) addClosingReference(closed *ClosedPosition) {
	legacy := a.iposByISIN[closed.ISIN]
	closed.Conversion = legacy.Conversion
	entry, hasEntry := a.Entry(closed.ISIN, closed.Name, closed.LastSeen)
	if hasEntry && entry.EntryPriceUSD != "" && closed.Kind != "bond" {
		closed.Entry = &entry
	}

	if series := a.observations[closed.ISIN]; len(series) > 0 {
		last := series[len(series)-1]
		if closed.LastSeen != "" {
			if i, ok := findObservation(series, closed.LastSeen); ok {
				last = series[i]
			}
		}
		if price, ok := decimal.Parse(last.UnitValue); ok && price.Sign() > 0 {
			closed.Exit = &ClosingReference{
				Price: last.UnitValue, Currency: last.ReportCurrency, Basis: "disclosure", Date: last.AsOfDate, SourceURL: last.SourceURL,
			}
			// Use the quantity belonging to this valuation, including if an intervening
			// composition-only disclosure shows a different final quantity.
			if closed.Entry != nil && last.ReportCurrency == "USD" {
				closed.Exit.Return = prices.ComparePrices(entry.EntryPriceUSD, last.UnitValue, last.Quantity)
			}
		}
	} else if sheet, matched := a.iposByName[issuerKey(legacy.Name)]; matched && sheet.Ticker == legacy.Ticker && isSheetClosingReference(sheet) {
		closed.Entry = &sheet
		closed.Exit = &ClosingReference{
			Price: sheet.ReferencePrice, Currency: "USD", Basis: "sheet", SourceURL: sheet.SourceURL,
			Return: prices.ComparePrices(sheet.EntryPriceUSD, sheet.ReferencePrice, sheet.Quantity),
		}
	} else if legacy.ReferencePrice != "" {
		closed.Exit = &ClosingReference{
			Price: legacy.ReferencePrice, Currency: "USD", Basis: "legacy",
		}
		if closed.Entry != nil {
			closed.Exit.Return = prices.ComparePrices(entry.EntryPriceUSD, legacy.ReferencePrice, "")
		}
	}
	if closed.Exit != nil && closed.Exit.Return != nil && closed.Entry != nil {
		closed.Exit.Return.Approximate = closed.Entry.Approximate
	}
}

// The lower block of the original sheet lists closed positions. Its date
// column is the lock-up end, so it does not establish an execution date.
func isSheetClosingReference(entry IPO) bool {
	_, cell, found := strings.Cut(entry.SourceURL, "&range=A")
	if !found || entry.ReferenceDate != "" || entry.ReferencePrice == "" {
		return false
	}
	row, _, _ := strings.Cut(cell, ":")
	n, err := strconv.Atoi(row)
	return err == nil && n >= 77 && n <= 162
}

func (a *Archive) Detail(isin string, state *appstate.State, officialURL string) Detail {
	result := Detail{
		ISIN:         isin,
		Observations: []Observation{},
		Changes:      []Change{},
		Market:       []MarketPoint{},
	}
	if series := a.observations[isin]; len(series) != 0 {
		result.Observations = slices.Clone(series)
		result.Name = series[len(series)-1].Name
	}

	if entry, exists := a.iposByISIN[isin]; exists {
		result.Entry = &entry
		if result.Name == "" {
			result.Name = entry.Name
		}
	}

	if result.Name == "" && state != nil && state.Fund != nil {
		for _, item := range state.Fund.Snapshot.Assets {
			if item.ISIN == isin {
				result.Name = item.InstrumentName
				break
			}
		}
	}

	for _, item := range a.Changes(state, officialURL) {
		if item.ISIN == isin {
			result.Changes = append(result.Changes, item)
			if result.Name == "" {
				result.Name = item.Name
			}
		}
	}

	if state == nil || state.Prices == nil {
		return result
	}
	for _, series := range state.Prices.DailyPrices {
		if series.ISIN != isin {
			continue
		}

		for _, point := range series.Items {
			result.Market = append(result.Market, MarketPoint{point.PriceDate.Format(time.DateOnly), series.Provider, point.UnitValue, point.Currency})
		}
	}

	for _, series := range state.Prices.Points {
		if series.ISIN != isin {
			continue
		}

		for _, point := range series.Items {
			result.Market = append(result.Market, MarketPoint{point.PricedAt.Format(time.RFC3339), series.Provider, point.UnitValue, point.Currency})
		}
	}

	slices.SortFunc(result.Market, func(left, right MarketPoint) int { return strings.Compare(left.At, right.At) })

	return result
}

type positionSnapshot struct {
	date      string
	source    string
	positions map[string]Position
}

func positionsOf(report Report) positionSnapshot {
	snapshot := positionSnapshot{date: report.AsOfDate, source: report.SourceURL, positions: make(map[string]Position, len(report.Positions))}
	for _, item := range report.Positions {
		snapshot.positions[item.ISIN] = item
	}
	return snapshot
}

func compare(before, after positionSnapshot) []Change {
	var result []Change
	for isin, old := range before.positions {
		item, exists := after.positions[isin]
		if !exists {
			result = append(result, change(before, after, old, "removed", old.Quantity, "0"))
			continue
		}

		if !decimal.Equal(old.Quantity, item.Quantity) {
			result = append(result, change(before, after, item, "quantity_changed", old.Quantity, item.Quantity))
		}
	}

	for isin, item := range after.positions {
		if _, exists := before.positions[isin]; !exists {
			result = append(result, change(before, after, item, "added", "0", item.Quantity))
		}
	}

	slices.SortFunc(result, func(left, right Change) int { return strings.Compare(left.ISIN, right.ISIN) })

	return result
}

func change(before, after positionSnapshot, item Position, kind, oldQuantity, newQuantity string) Change {
	return Change{
		ISIN:       item.ISIN,
		Name:       item.Name,
		Kind:       item.Kind,
		Type:       kind,
		FromDate:   before.date,
		ToDate:     after.date,
		FromSource: before.source,
		ToSource:   after.source,
		Before:     oldQuantity,
		After:      newQuantity,
	}
}

func makeObservation(position Position, report ReportSummary) Observation {
	value, _ := decimal.Parse(position.Value)
	qty, _ := decimal.Parse(position.Quantity)
	assets, _ := decimal.Parse(report.TotalAssets)
	nav, _ := decimal.Parse(report.NAV)

	return Observation{
		Position:       position,
		AsOfDate:       report.AsOfDate,
		ReportCurrency: report.Currency,
		SourceURL:      report.SourceURL,
		UnitValue:      formatDecimal(new(big.Rat).Quo(value, qty), 8),
		AssetShare:     percent(value, assets),
		NAVShare:       percent(value, nav),
	}
}

func findObservation(series []Observation, date string) (int, bool) {
	return slices.BinarySearchFunc(series, date, func(point Observation, target string) int {
		return strings.Compare(point.AsOfDate, target)
	})
}

func issuerKey(name string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, name)
}

func percentageChange(value, previous *big.Rat) string {
	if previous.Sign() == 0 {
		return ""
	}
	return percent(new(big.Rat).Sub(value, previous), previous)
}

func percent(value, base *big.Rat) string {
	ratio := new(big.Rat).Quo(value, base)
	return formatDecimal(ratio.Mul(ratio, big.NewRat(100, 1)), 6)
}

func formatDecimal(value *big.Rat, places int) string {
	text := strings.TrimRight(strings.TrimRight(value.FloatString(places), "0"), ".")
	if text == "-0" || text == "" {
		return "0"
	}
	return text
}
