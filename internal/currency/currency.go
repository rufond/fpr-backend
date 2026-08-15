package currency

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
