package validationrules

import govalidate "github.com/xloss/go-validate"

// ISIN validates a canonical ISO 6166 identifier, including its Luhn check digit.
// Missing/nil values are accepted so the rule composes with rules.Required{} in
// the same way as go-validate built-in optional rules.
type ISIN struct{}

var _ govalidate.Rule = ISIN{}

func (ISIN) GetName() string {
	return "isin"
}

func (ISIN) GetValues() map[string]any {
	return map[string]any{}
}

func (ISIN) Validate(_ string, value any, _ map[string]any) bool {
	if value == nil {
		return true
	}

	isin, ok := value.(string)
	if !ok || len(isin) != 12 {
		return false
	}

	digits := make([]int, 0, 24)
	for index, char := range isin {
		switch {
		case char >= '0' && char <= '9':
			digits = append(digits, int(char-'0'))
		case char >= 'A' && char <= 'Z' && index < 11:
			c := int(char-'A') + 10
			digits = append(digits, c/10, c%10)
		default:
			return false
		}
	}

	sum := 0
	double := false
	for index := len(digits) - 1; index >= 0; index-- {
		d := digits[index]
		if double {
			d *= 2
		}
		sum += d/10 + d%10
		double = !double
	}

	return sum%10 == 0
}
