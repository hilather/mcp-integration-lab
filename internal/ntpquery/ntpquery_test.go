package ntpquery

import (
	"testing"
	"time"
)

func TestNTPTimeRoundTrip(t *testing.T) {
	want := time.Date(2026, 8, 30, 18, 0, 0, 123456789, time.UTC)
	buf := make([]byte, 8)
	putNTPTime(buf, want)
	got := ntpTime(buf)
	delta := got.Sub(want)
	if delta < 0 {
		delta = -delta
	}
	if delta > time.Millisecond {
		t.Fatalf("round-trip delta %v, want <1ms (got %s want %s)", delta, got, want)
	}
}

func TestQueryShortReply(t *testing.T) {
	// Unreachable / closed UDP still returns an error; this pins the helper
	// does not panic on a bad address.
	_, err := Query("127.0.0.1:1", 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected error against closed UDP")
	}
}
