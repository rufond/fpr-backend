package yahoo

import (
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/rufond/fpr-backend/internal/currency"
)

type normalizedQuote struct {
	UnitValue string
	Currency  string
	PricedAt  time.Time
}

func normalizeCurrentQuote(quote quote) (normalizedQuote, error) {
	currencyCode, errCurrency := normalizeQuoteCurrency(quote.Currency)
	if errCurrency != nil {
		return normalizedQuote{}, errCurrency
	}

	unitValue, errPrice := normalizePositiveDecimal(quote.Price)
	if errPrice != nil {
		return normalizedQuote{}, fmt.Errorf("Yahoo price: %w", errPrice)
	}

	if quote.RegularMarketTime.IsZero() {
		return normalizedQuote{}, fmt.Errorf("Yahoo regular market time is missing or invalid")
	}

	return normalizedQuote{
		UnitValue: unitValue,
		Currency:  currencyCode,
		PricedAt:  quote.RegularMarketTime.UTC(),
	}, nil
}

func normalizeQuoteCurrency(value string) (string, error) {
	code := strings.TrimSpace(value)
	switch code {
	case currency.USD, currency.RUB, currency.KZT:
		return code, nil
	default:
		return "", fmt.Errorf("Yahoo price currency %q is unsupported", value)
	}
}

func normalizePositiveDecimal(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("value is empty")
	}

	number, valid := new(big.Rat).SetString(value)
	if !valid {
		return "", fmt.Errorf("value %q is not a decimal number", value)
	}

	if number.Sign() <= 0 {
		return "", fmt.Errorf("value must be positive")
	}

	return finiteDecimal(number)
}

func finiteDecimal(number *big.Rat) (string, error) {
	denominator := new(big.Int).Set(number.Denom())
	two := big.NewInt(2)
	five := big.NewInt(5)
	remainder := new(big.Int)
	quotient := new(big.Int)

	twos := 0
	for {
		quotient.QuoRem(denominator, two, remainder)
		if remainder.Sign() != 0 {
			break
		}
		denominator.Set(quotient)
		twos++
	}

	fives := 0
	for {
		quotient.QuoRem(denominator, five, remainder)
		if remainder.Sign() != 0 {
			break
		}
		denominator.Set(quotient)
		fives++
	}

	if denominator.Cmp(big.NewInt(1)) != 0 {
		return "", fmt.Errorf("value cannot be represented as a finite decimal")
	}

	scale := max(twos, fives)
	if scale == 0 {
		return number.Num().String(), nil
	}

	return strings.TrimRight(strings.TrimRight(number.FloatString(scale), "0"), "."), nil
}
