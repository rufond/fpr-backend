package currency

import "testing"

func TestValidCode(t *testing.T) {
	if !ValidCode("USD") {
		t.Fatal("USD should be valid")
	}
	for _, value := range []string{"usd", "US", "US1", "EURO"} {
		if ValidCode(value) {
			t.Fatalf("%q should be invalid", value)
		}
	}
}
