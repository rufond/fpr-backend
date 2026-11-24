package prices

import (
	"math/big"

	"github.com/rufond/fpr-backend/internal/decimal"
)

type PriceReturn struct {
	UnitChange  string `json:"unit_change"`
	Percent     string `json:"percent"`
	Total       string `json:"total,omitempty"`
	EntryValue  string `json:"entry_value,omitempty"`
	Approximate bool   `json:"approximate,omitempty"`
}

// ComparePrices compares two prices for one unit, in the same currency.
func ComparePrices(entry, current, quantity string) *PriceReturn {
	base, hasBase := decimal.Parse(entry)
	value, hasValue := decimal.Parse(current)
	if !hasBase || !hasValue || base.Sign() <= 0 || value.Sign() < 0 {
		return nil
	}

	diff := new(big.Rat).Sub(value, base)
	percent := new(big.Rat).Quo(diff, base)
	result := &PriceReturn{
		UnitChange: diff.FloatString(8),
		Percent:    percent.Mul(percent, big.NewRat(100, 1)).FloatString(6),
	}

	if qty, ok := decimal.Parse(quantity); ok && qty.Sign() > 0 {
		result.Total = new(big.Rat).Mul(diff, qty).FloatString(8)
		result.EntryValue = new(big.Rat).Mul(base, qty).FloatString(8)
	}

	return result
}
