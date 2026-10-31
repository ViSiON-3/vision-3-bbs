package editor

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// The header's fields come from callers and other boards. They only get to colour
// it, whichever way a template asks for them, and the template keeps its own codes.
func TestHeaderFieldsCannotRunScreenCodes(t *testing.T) {
	const screenClear = "\x1b[2J"
	for _, tc := range []struct {
		name, template, subject, recipient string
	}{
		{"new format", "@E:30@ @S:30@", "a|CLb", "c|PPd"},
		{"new format, marker in the subject", "@E:30@", "|#1|CL", "x"},
		{"new format, cut by the field width", "@E:3@|CL", "ab|CLxx", "x"},
		{"legacy", "|E |S", "a|CLb", "c|PPd"},
		{"legacy, marker in the subject", "|E", "|#1|CL", "x"},
		// Run often: map order used to decide whether this one fired.
		{"legacy, code in the subject", "|E |S", "|S", "CL"},
		{"legacy, code in the recipient", "|S |E", "CL", "|E"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Screen{}
			for range 20 {
				got := string(s.processPipeCodes([]byte(tc.template), tc.subject, tc.recipient, "From", false))
				got = strings.TrimSuffix(got, string(ansi.ReplacePipeCodes([]byte("|CL")))) // what a template asks for itself
				if strings.Contains(got, screenClear) || strings.Contains(got, "\x1b[u") {
					t.Fatalf("a field ran a screen code: %q", got)
				}
			}
		})
	}

	s := &Screen{}
	got := string(s.processPipeCodes([]byte("|04@E:10@"), "|12red", "x", "From", false))
	if want := string(ansi.ReplacePipeCodes([]byte("|12"))); !strings.Contains(got, want) {
		t.Errorf("the subject lost its colour: %q", got)
	}
	if want := string(ansi.ReplacePipeCodes([]byte("|04"))); !strings.HasPrefix(got, want) {
		t.Errorf("the template lost its colour: %q", got)
	}
}
