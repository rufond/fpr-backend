package decimal

import (
	"fmt"
	"math/big"
	"strings"
)

func Parse(raw string) (*big.Rat, bool) {
	text := normalize(raw)
	if text == "" {
		return nil, false
	}

	return new(big.Rat).SetString(text)
}

func Equal(left string, right string) bool {
	leftValue, leftOK := Parse(left)
	rightValue, rightOK := Parse(right)
	return leftOK && rightOK && leftValue.Cmp(rightValue) == 0
}

func Canonical(raw string) (string, error) {
	text := normalize(raw)
	if text == "" {
		return "", fmt.Errorf("decimal is empty")
	}
	if _, ok := new(big.Rat).SetString(text); !ok {
		return "", fmt.Errorf("invalid decimal %q", raw)
	}

	sign := ""
	if after, ok := strings.CutPrefix(text, "+"); ok {
		text = after
	} else if after, ok := strings.CutPrefix(text, "-"); ok {
		sign = "-"
		text = after
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
		return "0", nil
	}
	if fraction == "" {
		return sign + integer, nil
	}

	return sign + integer + "." + fraction, nil
}

func normalize(raw string) string {
	text := strings.TrimSpace(raw)
	text = strings.ReplaceAll(text, "\u00a0", "")
	text = strings.ReplaceAll(text, " ", "")
	return strings.ReplaceAll(text, ",", ".")
}
