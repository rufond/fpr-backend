package kase

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/rufond/fpr-backend/internal/decimal"
	"github.com/rufond/fpr-backend/internal/prices"
	"golang.org/x/net/html"
)

var symbolPattern = regexp.MustCompile(`^[A-Z0-9_]{1,24}$`)
var almaty = time.FixedZone("Asia/Almaty", 5*60*60)

type Provider struct {
	client *http.Client
}

func NewProvider() *Provider {
	return &Provider{client: &http.Client{Timeout: 12 * time.Second}}
}

func (p *Provider) FetchSecurityPrices(ctx context.Context, symbols []string) (prices.MOEXSourceResult, error) {
	result := prices.MOEXSourceResult{RequestedSymbols: len(symbols), QuotesBySymbol: make(map[string]prices.SourceQuote)}
	for _, symbol := range symbols {
		quote, err := p.fetch(ctx, symbol)
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		if err != nil {
			result.Issues = append(result.Issues, prices.MOEXQuoteIssue{Symbol: symbol, Error: err.Error()})
			continue
		}

		result.QuotesBySymbol[symbol] = quote
	}

	return result, nil
}

func (p *Provider) fetch(ctx context.Context, symbol string) (prices.SourceQuote, error) {
	if !symbolPattern.MatchString(symbol) {
		return prices.SourceQuote{}, fmt.Errorf("invalid KASE ticker")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://kase.kz/en/investors/shares/"+symbol, nil)
	if err != nil {
		return prices.SourceQuote{}, err
	}

	response, err := p.client.Do(request)
	if err != nil {
		return prices.SourceQuote{}, err
	}

	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return prices.SourceQuote{}, fmt.Errorf("KASE returned HTTP %d", response.StatusCode)
	}

	return parseQuote(io.LimitReader(response.Body, 3<<20))
}

func parseQuote(body io.Reader) (prices.SourceQuote, error) {
	page, err := html.Parse(body)
	if err != nil {
		return prices.SourceQuote{}, err
	}

	current := prices.SourceQuote{}
	var tradeTable *html.Node
	for tr := range page.Descendants() {
		if tr.DataAtom.String() != "tr" {
			continue
		}

		var cells []string
		for child := range tr.ChildNodes() {
			if child.Data == "td" || child.Data == "th" {
				cells = append(cells, cellText(child))
			}
		}

		if len(cells) == 0 {
			continue
		}

		if cells[0] == "Date/Period" && slices.Index(cells, "Last") == 3 && slices.Contains(cells, "m. KZT") {
			// Header and data may be in distinct row groups. The dated rows are the
			// next table rows in document order.
			tradeTable = tableOf(tr)
			current.Source = prices.ProviderKASE
			continue
		}

		if tradeTable == nil || tableOf(tr) != tradeTable || len(cells) < 4 {
			continue
		}

		day := strings.TrimSpace(cells[0])
		stamp, errTime := time.ParseInLocation("02.01.06 (15:04)", day, almaty)
		if errTime != nil {
			stamp, errTime = time.ParseInLocation("02.01.06", day, almaty)
		}

		value := strings.ReplaceAll(strings.Join(strings.Fields(cells[3]), ""), ",", ".")
		numeric, ok := decimal.Parse(value)
		if errTime != nil || !ok || numeric.Sign() <= 0 {
			continue
		}

		if current.UnitValue == "" {
			current.UnitValue, current.Currency, current.PricedAt = value, "KZT", stamp.UTC()
		} else if stamp.In(almaty).Format(time.DateOnly) < current.PricedAt.In(almaty).Format(time.DateOnly) {
			current.PreviousClose = value
			break
		}
	}

	if current.UnitValue == "" {
		return prices.SourceQuote{}, fmt.Errorf("KASE share page has no dated trade in KZT")
	}

	return current, nil
}

func cellText(element *html.Node) string {
	var content strings.Builder
	for node := range element.Descendants() {
		if node.Type == html.TextNode {
			content.WriteString(node.Data)
		}
	}

	return strings.Join(strings.Fields(content.String()), " ")
}

func tableOf(node *html.Node) *html.Node {
	for node != nil && node.Data != "table" {
		node = node.Parent
	}

	return node
}
