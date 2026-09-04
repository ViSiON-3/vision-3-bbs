package menu

import "testing"

func matrixTestOptions() []LightbarOption {
	// Mirrors the shipped PDMATRIX.BAR ordering.
	return []LightbarOption{
		{HotKey: "J", Text: "Journey onward."},
		{HotKey: "C", Text: "Create an account."},
		{HotKey: "A", Text: "Check your access."},
		{HotKey: "D", Text: "Disconnect."},
	}
}

func TestMatrixPrintableKey(t *testing.T) {
	options := matrixTestOptions()

	// Hotkeys pick their own option. Regression guard: the matrix used to
	// hand every hotkey press back to whatever was already highlighted, so
	// C ("Create an account") logged the caller in instead of starting the
	// new user application, and there was no way to sign up at all.
	for _, tc := range []struct {
		key  rune
		want int
	}{
		{'J', 0}, {'C', 1}, {'A', 2}, {'D', 3},
		{'c', 1}, // lower case matches too
		{'1', 0}, {'2', 1}, {'3', 2}, {'4', 3},
	} {
		got, ok := matrixPrintableKey(tc.key, options)
		if !ok || got != tc.want {
			t.Errorf("matrixPrintableKey(%q) = (%d, %v), want (%d, true)", tc.key, got, ok, tc.want)
		}
	}

	// Keys that match nothing, and digits past the end of the list, are
	// reported as unmatched rather than selecting option 0.
	for _, key := range []rune{'Z', '5', '9', '!'} {
		if got, ok := matrixPrintableKey(key, options); ok {
			t.Errorf("matrixPrintableKey(%q) = (%d, true), want no match", key, got)
		}
	}
}
