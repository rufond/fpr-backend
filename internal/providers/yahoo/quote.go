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
	currencyCode, decimalShift, errCurrency := normalizeQuoteCurrency(quote.Currency)
	if errCurrency != nil {
		return normalizedQuote{}, errCurrency
	}

	unitValue, errPrice := normalizePositiveDecimal(quote.Price, decimalShift)
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

func normalizeQuoteCurrency(value string) (string, int, error) {
	code := strings.TrimSpace(value)

	// Yahoo uses the case-sensitive GBp form for prices quoted in pence.
	// Uppercasing first would turn it into GBP and silently multiply the
	// position value by 100.
	if code == "GBp" {
		return "GBP", 2, nil
	}

	code = strings.ToUpper(code)
	switch code {
	case "GBX":
		return "GBP", 2, nil
	case "ILA":
		return "ILS", 2, nil
	}

	if !currency.ValidCode(code) {
		return "", 0, fmt.Errorf("Yahoo price currency %q is invalid", value)
	}

	return code, 0, nil
}

func normalizePositiveDecimal(value string, decimalShift int) (string, error) {
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

	if decimalShift > 0 {
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimalShift)), nil)
		number.Quo(number, new(big.Rat).SetInt(divisor))
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
