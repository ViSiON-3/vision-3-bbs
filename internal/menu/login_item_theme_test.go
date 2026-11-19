package menu

import "testing"

// A login step with a theme runs only for a caller on that theme; one with
// none runs for everyone.
func TestLoginItemForTheme(t *testing.T) {
	for _, tc := range []struct {
		item, caller string
		want         bool
	}{
		{"", "v3", true},
		{"", "vision3", true},
		{"vision3", "vision3", true},
		{"ViSiON3", "vision3", true},
		{"vision3", "v3", false},
		{"vision3", "pms", false},
		{"vision3", "", false},
	} {
		if got := loginItemForTheme(tc.item, tc.caller); got != tc.want {
			t.Errorf("step theme %q, caller on %q: runs=%v, want %v", tc.item, tc.caller, got, tc.want)
		}
	}
}
