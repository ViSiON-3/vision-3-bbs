package ansi

import "testing"

// PipeCodeAt finds the code ReplacePipeCodes expands, longest first.
func TestPipeCodeAt(t *testing.T) {
	for in, want := range map[string]string{
		"|B10x": "|B10", "|B1x": "|B1", "|PPx": "|PP", "|Px": "|P", "|P": "|P",
		"|XY01,24x": "|XY01,24", "|07": "|07", "|23": "|23", "|CL": "|CL",
		"||07": "", "|ZZ": "", "x|07": "", "|": "",
	} {
		code, exp, n := PipeCodeAt([]byte(in), 0)
		if code != want || n != len(want) || (n > 0) != (exp != "") {
			t.Errorf("PipeCodeAt(%q) = %q, %q, %d; want %q", in, code, exp, n, want)
		}
	}
}
