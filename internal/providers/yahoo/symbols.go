package yahoo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	http "github.com/bogdanfinn/fhttp"

	"github.com/rufond/fpr-backend/internal/prices"
)

const yahooSearchURL = "https://query2.finance.yahoo.com/v1/finance/search"

type searchEnvelope struct {
	Quotes []searchQuote `json:"quotes"`
}

type searchQuote struct {
	Symbol string `json:"symbol"`
}

func (p *Provider) ResolveSymbols(ctx context.Context, isins []string) (prices.YahooSymbolResolutionResult, error) {
	result := prices.YahooSymbolResolutionResult{
		RequestedISINs: len(isins),
		SymbolsByISIN:  make(map[string]string, len(isins)),
	}

	for index, isin := range isins {
		query := url.Values{}
		query.Set("q", isin)
		query.Set("quotesCount", "1")
		query.Set("enableFuzzyQuery", "false")
		query.Set("newsCount", "0")
		query.Set("listsCount", "0")
		query.Set("recommendedCount", "0")
		query.Set("quotesQueryId", "tss_match_phrase_query")

		req, errRequest := http.NewRequest(http.MethodGet, yahooSearchURL+"?"+query.Encode(), nil)
		if errRequest != nil {
			return prices.YahooSymbolResolutionResult{}, fmt.Errorf("create Yahoo symbol search request for %s: %w", isin, errRequest)
		}
		req = req.WithContext(ctx)
		req.Header = browserHeaders()

		resp, errDo := p.client.Do(req)
		if errDo != nil {
			return prices.YahooSymbolResolutionResult{}, fmt.Errorf("request Yahoo symbol search for %s: %w", isin, errDo)
		}

		body, errBody := readResponseBody(resp.Body)
		_ = resp.Body.Close()
		if errBody != nil {
			return prices.YahooSymbolResolutionResult{}, fmt.Errorf("read Yahoo symbol search response for %s: %w", isin, errBody)
		}
		if resp.StatusCode != http.StatusOK {
			return prices.YahooSymbolResolutionResult{}, fmt.Errorf(
				"Yahoo symbol search for %s returned HTTP %d: %s",
				isin,
				resp.StatusCode,
				responsePreview(body),
			)
		}

		var envelope searchEnvelope
		if errDecode := json.Unmarshal(body, &envelope); errDecode != nil {
			return prices.YahooSymbolResolutionResult{}, fmt.Errorf("decode Yahoo symbol search for %s: %w", isin, errDecode)
		}

		if len(envelope.Quotes) == 0 {
			result.MissingISINs = append(result.MissingISINs, isin)
		} else {
			symbol := normalizeSymbol(envelope.Quotes[0].Symbol)
			if symbol == "" {
				result.MissingISINs = append(result.MissingISINs, isin)
			} else {
				result.SymbolsByISIN[isin] = symbol
			}
		}

		if index+1 < len(isins) {
			if errDelay := p.waitRequestDelay(ctx); errDelay != nil {
				return prices.YahooSymbolResolutionResult{}, errDelay
			}
		}
	}

	return result, nil
}
