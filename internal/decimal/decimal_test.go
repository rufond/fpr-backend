package decimal

import "testing"

func TestCanonical(t *testing.T) {
	tests := map[string]string{
		"1 234,5600":     "1234.56",
		"492 986 650.00": "492986650",
		"00031.1800":     "31.18",
		"0.0100":         "0.01",
		"1,2500":         "1.25",
		"+001.2300":      "1.23",
		"-001.2300":      "-1.23",
		"-0.000":         "0",
		"0":              "0",
	}

	for input, want := range tests {
		got, err := Canonical(input)
		if err != nil {
			t.Fatalf("Canonical(%q) error = %v", input, err)
		}
		if got != want {
			t.Fatalf("Canonical(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestEqual(t *testing.T) {
	if !Equal("1,2300", "1.23") {
		t.Fatal("Equal should compare exact decimal values after normalization")
	}
	if Equal("1.23", "1.24") {
		t.Fatal("Equal should reject different values")
	}
}
