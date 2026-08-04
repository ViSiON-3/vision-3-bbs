package menu

import "testing"

// TestValidateHandleRules covers the Pascal-derived rules: length, reserved
// names, banned specials, and not purely numeric.
func TestValidateHandleRules(t *testing.T) {
	cases := []struct {
		handle string
		want   bool
	}{
		{"J0hnny A1pha", true},
		{"abc", true},
		{"ab", false},        // too short
		{"SysOp", false},     // reserved
		{"new", false},       // reserved
		{"what?", false},     // banned special
		{"12345", false},     // purely numeric
		{"", false},          // empty
		{"Fel:onius", false}, // banned special
	}
	for _, c := range cases {
		if got := validateHandle(c.handle); got != c.want {
			t.Errorf("validateHandle(%q) = %v, want %v", c.handle, got, c.want)
		}
	}
}

// TestValidateHandleRejectsControlCharacters verifies that terminal control
// bytes cannot be smuggled into a handle. A handle containing ESC/C0/DEL would
// otherwise replay escape sequences into any terminal that later renders it
// (node lists, WFC console, logs).
func TestValidateHandleRejectsControlCharacters(t *testing.T) {
	bad := []string{
		"Bad\x1b]0;pwn\x07Guy", // OSC title change
		"abc\x1b[2J",           // CSI clear screen
		"tab\tname",            // C0 tab
		"del\x7fete",           // DEL
		"nul\x00name",          // NUL
		"csi\u009b2Jname",      // C1 8-bit CSI
		"pad\u0080name",        // C1 PAD
		"bad\xffbyte",          // invalid UTF-8
		"lone\xc3",             // truncated multi-byte sequence
	}
	for _, h := range bad {
		if validateHandle(h) {
			t.Errorf("validateHandle(%q) = true, want false (control character)", h)
		}
	}
}
