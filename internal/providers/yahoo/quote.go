package yahoo

import (
	"fmt"
	"math/big"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/rufond/fpr-backend/internal/currency"
)

type NormalizedQuote struct {
	Symbol    string
	UnitValue string
	Currency  string
	PricedAt  time.Time
}

type NormalizedPreviousClose struct {
	Symbol    string
	UnitValue string
	Currency  string
	PriceDate time.Time
}

func NormalizeCurrentQuote(quote Quote) (NormalizedQuote, error) {
	symbol := normalizeSymbol(quote.Symbol)
	if symbol == "" {
		return NormalizedQuote{}, fmt.Errorf("Yahoo symbol is empty")
	}

	currencyCode, decimalShift, errCurrency := normalizeQuoteCurrency(quote.Currency)
	if errCurrency != nil {
		return NormalizedQuote{}, errCurrency
	}

	unitValue, errPrice := normalizePositiveDecimal(quote.Price, decimalShift)
	if errPrice != nil {
		return NormalizedQuote{}, fmt.Errorf("Yahoo price: %w", errPrice)
	}

	if quote.RegularMarketTime.IsZero() {
		return NormalizedQuote{}, fmt.Errorf("Yahoo regular market time is missing or invalid")
	}

	return NormalizedQuote{
		Symbol:    symbol,
		UnitValue: unitValue,
		Currency:  currencyCode,
		PricedAt:  quote.RegularMarketTime.UTC(),
	}, nil
}

func NormalizePreviousClose(quote Quote) (*NormalizedPreviousClose, error) {
	if strings.TrimSpace(quote.PreviousClose) == "" {
		return nil, nil
	}

	symbol := normalizeSymbol(quote.Symbol)
	if symbol == "" {
		return nil, fmt.Errorf("Yahoo symbol is empty")
	}

	currencyCode, decimalShift, errCurrency := normalizeQuoteCurrency(quote.Currency)
	if errCurrency != nil {
		return nil, errCurrency
	}

	unitValue, errPrice := normalizePositiveDecimal(quote.PreviousClose, decimalShift)
	if errPrice != nil {
		return nil, fmt.Errorf("Yahoo previous close: %w", errPrice)
	}

	if quote.RegularMarketTime.IsZero() {
		return nil, fmt.Errorf("Yahoo regular market time is missing or invalid")
	}

	timezone := strings.TrimSpace(quote.ExchangeTimezoneName)
	if timezone == "" {
		return nil, fmt.Errorf("Yahoo exchange timezone is empty")
	}

	location, errLocation := time.LoadLocation(timezone)
	if errLocation != nil {
		return nil, fmt.Errorf("load Yahoo exchange timezone %s: %w", timezone, errLocation)
	}

	year, month, day := quote.RegularMarketTime.In(location).Date()
	priceDate := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)

	return &NormalizedPreviousClose{
		Symbol:    symbol,
		UnitValue: unitValue,
		Currency:  currencyCode,
		PriceDate: priceDate,
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
