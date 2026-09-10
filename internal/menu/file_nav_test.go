package menu

import "testing"

// TestStepIndex covers the wraparound used by file (and mirrors message) area /
// conference next-prev navigation, including the "current item not found" case.
func TestStepIndex(t *testing.T) {
	tests := []struct {
		name       string
		current, n int
		forward    bool
		want       int
	}{
		{"forward mid", 1, 4, true, 2},
		{"forward wraps at end", 3, 4, true, 0},
		{"backward mid", 2, 4, false, 1},
		{"backward wraps at start", 0, 4, false, 3},
		{"not found goes first (forward)", -1, 4, true, 0},
		{"not found goes first (backward)", -1, 4, false, 0},
		{"single item forward", 0, 1, true, 0},
		{"single item backward", 0, 1, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := stepIndex(tc.current, tc.n, tc.forward); got != tc.want {
				t.Errorf("stepIndex(%d,%d,%v) = %d, want %d", tc.current, tc.n, tc.forward, got, tc.want)
			}
		})
	}
}
