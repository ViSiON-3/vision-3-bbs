package geoip

import (
	"os"
	"testing"
)

func TestLabelWithoutDatabase(t *testing.T) {
	var db *DB
	cases := map[string]string{
		"127.0.0.1":       "LAN",
		"10.77.77.1":      "LAN",
		"192.168.1.20":    "LAN",
		"::1":             "LAN",
		"::ffff:10.0.0.5": "LAN",
		"100.64.0.10":     "LAN",
		"8.8.8.8":         "",
		"not-an-ip":       "",
	}
	for ip, want := range cases {
		if got := db.Label(ip); got != want {
			t.Errorf("Label(%q) = %q, want %q", ip, got, want)
		}
	}
}

// TestLabelWithDatabase runs against a real City Lite file when
// V3_GEOIP_TEST_DB points at one.
func TestLabelWithDatabase(t *testing.T) {
	path := os.Getenv("V3_GEOIP_TEST_DB")
	if path == "" {
		t.Skip("V3_GEOIP_TEST_DB not set")
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for ip, want := range map[string]string{
		"8.8.8.8":     "CA/US",
		"81.2.69.142": "GB",
		"10.0.0.1":    "LAN",
	} {
		if got := db.Label(ip); got != want {
			t.Errorf("Label(%q) = %q, want %q", ip, got, want)
		}
	}
}
