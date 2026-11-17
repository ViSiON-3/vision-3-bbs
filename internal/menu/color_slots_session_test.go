//go:build !windows

package menu

import (
	"strings"
	"testing"
)

// A strings.json value fills in the caller's colours like the other session
// placeholders, each setting its background, black where it has none.
func TestColorSlotsInStrings(t *testing.T) {
	f := newStringHookFixture(t, "")
	f.bbs.User.SetColorCode(1, "|13")
	out := f.write(t, "|C1hi|C3there")
	if !strings.Contains(out, esc("|13|B0")+"hi"+esc("|07|B0")+"there") {
		t.Errorf("output = %q", out)
	}
}
