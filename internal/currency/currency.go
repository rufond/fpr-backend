package currency

const (
	KZT = "KZT"
	RUB = "RUB"
	USD = "USD"
)

func ValidCode(value string) bool {
	if len(value) != 3 {
		return false
	}

	for _, char := range value {
		if char < 'A' || char > 'Z' {
			return false
		}
	}

	return true
}
