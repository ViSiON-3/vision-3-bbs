package formatspec

import (
	"fmt"
	"testing"
)

// TestParse covers the directive forms fmt accepts.
func TestParse(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []ArgKind
	}{
		{"plain text", "hello world", nil},
		{"pipe codes are not verbs", "|15Online Nodes:|07", nil},
		{"escaped percent", "100%% Go", nil},
		{"escaped then verb", "%% done: %d", []ArgKind{KindInt}},
		{"simple string", "Hello %s", []ArgKind{KindString}},
		{"two verbs", "<%s> %s", []ArgKind{KindString, KindString}},
		{"mixed", "Node %d: %s", []ArgKind{KindInt, KindString}},
		{"zero pad width", "%02d", []ArgKind{KindInt}},
		{"left align width", "%-20s", []ArgKind{KindString}},
		{"plain width", "%3d", []ArgKind{KindInt}},
		{"precision", "%.2f", []ArgKind{KindFloat}},
		{"width and precision", "%8.2f", []ArgKind{KindFloat}},
		{"star width", "%*d", []ArgKind{KindInt, KindInt}},
		{"star precision", "%.*f", []ArgKind{KindInt, KindFloat}},
		{"star both", "%*.*f", []ArgKind{KindInt, KindInt, KindFloat}},
		{"plus flag", "%+d", []ArgKind{KindInt}},
		{"space flag", "% d", []ArgKind{KindInt}},
		{"hash flag", "%#x", []ArgKind{KindIntOrString}},
		{"quoted", "%q", []ArgKind{KindString}},
		{"any verb", "%v", []ArgKind{KindAny}},
		{"bool", "%t", []ArgKind{KindBool}},
		{"error value", "error: %v", []ArgKind{KindAny}},
		{"indexed reorder", "%[2]s came before %[1]s", []ArgKind{KindString, KindString}},
		{"indexed reuse", "%[1]s and %[1]s", []ArgKind{KindString}},
		{"indexed mixed kinds", "%[2]d %[1]s", []ArgKind{KindString, KindInt}},
		{"index then implicit", "%[2]s %s", []ArgKind{KindAny, KindString, KindString}},
		{"real: node list entry", " |15Node %d|07: %s\r\n", []ArgKind{KindInt, KindString}},
		{"real: color set", "\r\n|07%s Color set to |%02d%d|07.\r\n",
			[]ArgKind{KindString, KindInt, KindInt}},
		{"real: run command error", "\r\n|01Error running command '%s': %v|07\r\n",
			[]ArgKind{KindString, KindAny}},
		{"real: column toggle", "  |15[%s]|07 %-12s : %s\r\n",
			[]ArgKind{KindString, KindString, KindString}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.in)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tt.in, err)
			}
			if len(got.Args) != len(tt.want) {
				t.Fatalf("Parse(%q) = %v, want %v", tt.in, got.Args, tt.want)
			}
			for i := range tt.want {
				if got.Args[i] != tt.want[i] {
					t.Errorf("Parse(%q) arg %d = %v, want %v", tt.in, i, got.Args[i], tt.want[i])
				}
			}
		})
	}
}

// TestParseErrors covers directives fmt could not render.
func TestParseErrors(t *testing.T) {
	for _, in := range []string{
		"trailing percent %",
		"unknown verb %y here",
		"bad index %[x]s",
		"unterminated index %[2s",
		"zero index %[0]s",
		"conflicting use %[1]s %[1]d",
	} {
		t.Run(in, func(t *testing.T) {
			if got, err := Parse(in); err == nil {
				t.Errorf("Parse(%q) = %v, want an error", in, got)
			}
		})
	}
}

// TestParseMatchesSprintf is the property that matters: whenever Parse reports
// a signature, filling it with values of those kinds must produce output with
// no fmt error markers in it.
func TestParseMatchesSprintf(t *testing.T) {
	formats := []string{
		"Hello %s", "<%s> %s", "Node %d: %s", "%02d", "%-20s", "%3d", "%.2f",
		"%*d", "%.*f", "%*.*f", "%+d", "%q", "%v", "%t", "%#x",
		"%[2]s came before %[1]s", "%[1]s and %[1]s", "100%% Go",
		"\r\n|07%s Color set to |%02d%d|07.\r\n",
		"  |15[%s]|07 %-12s : %s\r\n",
	}
	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			spec, err := Parse(format)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			args := make([]any, len(spec.Args))
			for i, k := range spec.Args {
				args[i] = sampleFor(k)
			}
			out := fmt.Sprintf(format, args...)
			if containsFormatError(out) {
				t.Errorf("Sprintf(%q, %v) = %q, which carries a fmt error marker",
					format, args, out)
			}
		})
	}
}

// sampleFor returns a value satisfying a kind.
func sampleFor(k ArgKind) any {
	switch k {
	case KindString:
		return "x"
	case KindInt, KindIntOrString:
		return 7
	case KindFloat:
		return 1.5
	case KindBool:
		return true
	case KindPointer:
		return new(int)
	}
	return "x"
}

// containsFormatError reports whether Sprintf output carries a %! marker.
func containsFormatError(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '%' && s[i+1] == '!' {
			return true
		}
	}
	return false
}

// TestCompatible covers the rules for accepting an edited string.
func TestCompatible(t *testing.T) {
	parse := func(t *testing.T, s string) Spec {
		t.Helper()
		spec, err := Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q): %v", s, err)
		}
		return spec
	}
	tests := []struct {
		name    string
		def     string // what the call site supplies
		edited  string // what the sysop wrote
		wantErr bool
	}{
		{"identical", "Node %d: %s", "Node %d: %s", false},
		{"padding added", "%s", "%-20s", false},
		{"zero padding added", "%d", "%03d", false},
		{"reordered by index", "%s from %s", "%[2]s to %[1]s", false},
		{"widened to any", "%d", "%v", false},
		{"narrowed from any", "%v", "%d", false},
		{"literal percent added", "%d done", "%d%% done", false},
		{"verb dropped", "Node %d: %s", "Node: %s", true},
		{"verb added", "%s", "%s %s", true},
		{"type changed", "%s", "%d", true},
		{"all verbs dropped", "%s", "plain text", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Compatible(parse(t, tt.edited), parse(t, tt.def))
			if (err != nil) != tt.wantErr {
				t.Errorf("Compatible(%q against %q) error = %v, wantErr %v",
					tt.edited, tt.def, err, tt.wantErr)
			}
		})
	}
}

// TestLiteralPercentIsNotADirective documents why validation must be scoped to
// keys that actually reach fmt.
//
// badUDRatio ends "(|15|RA%|09)": the % is a literal percent sign printed to
// the caller, followed by a pipe colour code. That is not valid as a format
// string, and it does not need to be -- nothing passes it to fmt. Parsing every
// BBS string would report working prompts as broken.
func TestLiteralPercentIsNotADirective(t *testing.T) {
	plain := "|09Bad UD Ratio (|15|RA%|09) - Needed (|15|RR%|09)"

	if _, err := Parse(plain); err == nil {
		t.Errorf("Parse(%q) succeeded; it is not a valid format string, which is "+
			"why stringformat scopes validation to keys that reach fmt", plain)
	}
}
