package dateonly

import (
	"testing"
	"time"
)

func TestUTC(t *testing.T) {
	value := time.Date(2026, time.August, 15, 23, 42, 11, 0, time.FixedZone("UTC+3", 3*60*60))
	got := UTC(value)
	want := time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("UTC() = %s, want %s", got, want)
	}
}

func TestEqual(t *testing.T) {
	left := time.Date(2026, time.August, 15, 1, 0, 0, 0, time.UTC)
	right := time.Date(2026, time.August, 15, 23, 0, 0, 0, time.UTC)
	if !Equal(left, right) {
		t.Fatal("Equal should compare calendar dates")
	}
}
