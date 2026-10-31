//go:build !windows

package menu

import (
	"strings"
	"testing"
)

// A script hook receives its arguments unescaped, so it sees the real value. The
// fixture's script prints them with console.print, which expands pipe codes, so
// the codes having run here shows they reached it as they were: what a script
// prints is the script's to escape.
func TestStringHookScriptGetsArgumentsUnescaped(t *testing.T) {
	f := newStringHookFixture(t, "")
	out := f.write(t, "!say extra", "a|CLb")
	if !strings.Contains(out, "extra,a\x1b[2J") {
		t.Errorf("a script did not get the argument as it is: %q", out)
	}
}
