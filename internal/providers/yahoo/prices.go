package yahoo

import (
	"context"
	"slices"

	"github.com/rufond/fpr-backend/internal/prices"
)

var _ prices.YahooSource = (*Provider)(nil)

func (p *Provider) FetchPrices(ctx context.Context, symbols []string) (*prices.YahooSourceResult, error) {
	fetched, err := p.Fetch(ctx, symbols)
	if err != nil {
		return nil, err
	}

	result := &prices.YahooSourceResult{
		RequestedSymbols: fetched.RequestedSymbols,
		ReturnedSymbols:  fetched.ReturnedSymbols,
		Batches:          fetched.Batches,
		QuotesByRequest:  make(map[string]prices.YahooSourceQuote, len(symbols)),
		Missing:          slices.Clone(fetched.Missing),
		Unexpected:       slices.Clone(fetched.Unexpected),
		Duplicates:       slices.Clone(fetched.Duplicates),
	}

	normalizedQuotes := make(map[string]prices.YahooSourceQuote, len(fetched.Quotes))
	invalidQuotes := make(map[string]struct{})

	for _, requested := range symbols {
		symbol := normalizeSymbol(requested)

		quote, exists := fetched.Quotes[symbol]
		if !exists {
			result.MissingRequests++
			continue
		}

		if normalized, exists := normalizedQuotes[symbol]; exists {
			result.QuotesByRequest[requested] = normalized
			continue
		}

		if _, invalid := invalidQuotes[symbol]; invalid {
			result.InvalidRequests++
			continue
		}

		normalized, errNormalize := NormalizeCurrentQuote(quote)
		if errNormalize != nil {
			issue := prices.YahooQuoteIssue{
				Symbol: symbol,
				Error:  errNormalize.Error(),
			}
			invalidQuotes[symbol] = struct{}{}
			result.Invalid = append(result.Invalid, issue)
			result.InvalidRequests++

			continue
		}

		price := prices.YahooSourceQuote{
			UnitValue: normalized.UnitValue,
			Currency:  normalized.Currency,
			PricedAt:  normalized.PricedAt,
		}

		normalizedQuotes[symbol] = price
		result.QuotesByRequest[requested] = price
	}

	return result, nil
}
