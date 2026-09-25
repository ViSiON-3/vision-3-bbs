package menu

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

var headerCSIRe = regexp.MustCompile(`^\x1b\[([0-9;?]*)([A-Za-z])`)

// headerMaxColumn draws processed header bytes on an unbounded screen (no
// autowrap) and returns the rightmost column any character lands in.
func headerMaxColumn(data []byte) int {
	x, maxCol := 1, 0
	for i := 0; i < len(data); {
		if m := headerCSIRe.FindSubmatch(data[i:]); m != nil {
			fields := strings.Split(string(m[1]), ";")
			p := func(k int) int {
				if k < len(fields) {
					if n, err := strconv.Atoi(fields[k]); err == nil && n > 0 {
						return n
					}
				}
				return 1
			}
			switch m[2][0] {
			case 'C':
				x += p(0)
			case 'D':
				x = max(x-p(0), 1)
			case 'G':
				x = p(0)
			case 'H', 'f':
				x = p(1)
			}
			i += len(m[0])
			continue
		}
		switch b := data[i]; {
		case b == '\r' || b == '\n':
			x = 1
		case b >= 0x20:
			maxCol = max(maxCol, x)
			x++
		}
		i++
	}
	return maxCol
}

// TestShippedHeadersStayWithin79Columns renders every shipped MSGHDR template with
// values far longer than any real message would carry and checks that no row
// runs past column 79, so a long subject, name or note is truncated rather
// than pushing borders out or wrapping.
func TestShippedHeadersStayWithin79Columns(t *testing.T) {
	long := strings.Repeat("x", 100)
	subs := map[byte]string{
		'B': long, 'T': long, 'F': long, 'S': long, 'U': long, 'M': long,
		'L': "255", '#': "9999999", 'N': "9999999", 'C': "[9999999/9999999]",
		'V': "9999999 of 9999999", 'D': "12/31/26", 'W': "12:59 pm", 'P': "9999999",
		'E': "9999", 'O': long, 'A': long, 'Z': long, 'X': long, 'K': "999",
	}
	dir := filepath.Join("..", "..", "menus", "v3", "templates", "message_headers")
	for n := 1; n <= 15; n++ {
		name := "MSGHDR." + strconv.Itoa(n) + ".ans"
		tpl, err := ansi.GetAnsiFileContent(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out := processTemplate(tpl, subs, buildAutoWidths(subs, 9999999, 80, true))
		if col := headerMaxColumn(out); col > 79 {
			t.Errorf("%s: longest row reaches column %d, want at most 79", name, col)
		}
	}
}
