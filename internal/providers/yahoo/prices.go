package yahoo

import (
	"context"
	"slices"

	"github.com/rufond/fpr-backend/internal/prices"
)

var _ prices.YahooSource = (*Provider)(nil)

func (p *Provider) FetchPrices(ctx context.Context, symbols []string) (prices.YahooSourceResult, error) {
	if len(symbols) == 0 {
		return prices.YahooSourceResult{QuotesByRequest: map[string]prices.YahooSourceQuote{}}, nil
	}

	run, closeRun, errRun := p.runProvider(ctx)
	if errRun != nil {
		return prices.YahooSourceResult{}, errRun
	}
	defer closeRun()

	fetched, err := run.fetch(ctx, symbols)
	if err != nil {
		return prices.YahooSourceResult{}, err
	}

	result := prices.YahooSourceResult{
		RequestedSymbols: fetched.RequestedSymbols,
		ReturnedSymbols:  fetched.ReturnedSymbols,
		Batches:          fetched.Batches,
		QuotesByRequest:  make(map[string]prices.YahooSourceQuote, len(symbols)),
		Missing:          slices.Clone(fetched.Missing),
		Unexpected:       slices.Clone(fetched.Unexpected),
		Duplicates:       slices.Clone(fetched.Duplicates),
	}

	for _, requested := range symbols {
		quoteObject, exists := fetched.Quotes[requested]
		if !exists {
			result.MissingRequests++

			continue
		}

		normalized, errNormalize := normalizeCurrentQuote(quoteObject)
		if errNormalize != nil {
			result.Invalid = append(result.Invalid, prices.YahooQuoteIssue{
				Symbol: requested,
				Error:  errNormalize.Error(),
			})
			result.InvalidRequests++

			continue
		}

		result.QuotesByRequest[requested] = prices.YahooSourceQuote{
			UnitValue: normalized.UnitValue,
			Currency:  normalized.Currency,
			PricedAt:  normalized.PricedAt,
		}
	}

	return result, nil
}
