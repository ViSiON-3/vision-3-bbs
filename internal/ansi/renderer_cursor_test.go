package ansi

import (
	"strings"
	"testing"
)

// #857: ESC[G (column), ESC[E (next line), ESC[F (previous line) and ESC[d
// (row) position the cursor as on a terminal.
func TestRendererPositionsWithGEFd(t *testing.T) {
	for _, tc := range []struct {
		name, art string
		want      []string
	}{
		{"G moves to a column", "abc\x1b[10Gx", []string{"abc      x"}},
		{"G with no parameter goes to column 1", "abc\x1b[Gx", []string{"xbc"}},
		{"G past the edge clamps", "a\x1b[200Gx", []string{"a" + strings.Repeat(" ", 18) + "x"}},
		{"E moves down to column 1", "abc\x1b[2Ex", []string{"abc", "", "x"}},
		{"F moves up to column 1", "one\r\ntwo\r\nthree\x1b[2Fx", []string{"xne", "two", "three"}},
		{"d moves to a row, same column", "ab\x1b[3dX", []string{"ab", "", "  X"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := RenderANSIArtToLines(tc.art, 20, 10)
			plain := make([]string, len(got))
			for i, line := range got {
				plain[i] = strings.TrimRight(StripAnsi(line), " ")
			}
			if strings.Join(plain, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("rows:\n got %q\nwant %q", plain, tc.want)
			}
		})
	}
}
