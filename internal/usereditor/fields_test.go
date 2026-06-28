package usereditor

import (
	"testing"
	"time"
)

func TestTimeFormatters(t *testing.T) {
	zero := time.Time{}
	if got := formatTime(zero); got != "Never" {
		t.Errorf("formatTime(zero) = %q, want Never", got)
	}
	if got := formatDate(zero); got != "Never" {
		t.Errorf("formatDate(zero) = %q, want Never", got)
	}
	if got := formatTimeOnly(zero); got != "" {
		t.Errorf("formatTimeOnly(zero) = %q, want empty", got)
	}

	ts := time.Date(2026, 1, 2, 15, 4, 0, 0, time.UTC)
	if got := formatTime(ts); got != "01/02/26 3:04PM" {
		t.Errorf("formatTime = %q, want %q", got, "01/02/26 3:04PM")
	}
	if got := formatDate(ts); got != "01/02/26" {
		t.Errorf("formatDate = %q, want %q", got, "01/02/26")
	}
	if got := formatTimeOnly(ts); got != "03:04 PM" {
		t.Errorf("formatTimeOnly = %q, want %q", got, "03:04 PM")
	}
}

func TestPadRightLeft(t *testing.T) {
	if got := padRight("ab", 5); got != "ab   " {
		t.Errorf("padRight short = %q", got)
	}
	if got := padRight("abcdef", 3); got != "abc" {
		t.Errorf("padRight truncate = %q, want abc", got)
	}
	if got := padLeft("ab", 5); got != "   ab" {
		t.Errorf("padLeft short = %q", got)
	}
	if got := padLeft("abcdef", 3); got != "abc" {
		t.Errorf("padLeft truncate = %q, want abc", got)
	}
}
