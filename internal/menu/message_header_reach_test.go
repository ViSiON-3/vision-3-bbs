package menu

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// makeHeaderSet writes MSGHDR.<n>.ans for each n and returns the menu set root.
func makeHeaderSet(t *testing.T, numbers ...int) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "templates", "message_headers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, n := range numbers {
		f := filepath.Join(dir, "MSGHDR."+strconv.Itoa(n)+".ans")
		if err := os.WriteFile(f, []byte("@F@ @T@"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return root
}

// The reported bug: MSGHDR.15 ships and the selector offers it, but a fixed
// upper bound of 14 rejected it, so the reader re-prompted on every read.
func TestHeaderStyleAvailableAcceptsEveryShippedTemplate(t *testing.T) {
	root := makeHeaderSet(t, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)

	for n := 1; n <= 15; n++ {
		if !headerStyleAvailable(root, n) {
			t.Errorf("style %d has a template but was rejected", n)
		}
	}
	if headerStyleAvailable(root, 16) {
		t.Error("style 16 has no template and should be rejected")
	}
	if headerStyleAvailable(root, 0) {
		t.Error("0 means unset and must not count as available")
	}
	if headerStyleAvailable(root, -1) {
		t.Error("negative styles must be rejected")
	}
}

// Membership, not a maximum: a menu set with a gap should reject only the gap.
func TestHeaderStyleAvailableHandlesGaps(t *testing.T) {
	root := makeHeaderSet(t, 1, 2, 5)

	for _, n := range []int{1, 2, 5} {
		if !headerStyleAvailable(root, n) {
			t.Errorf("style %d exists but was rejected", n)
		}
	}
	for _, n := range []int{3, 4, 6} {
		if headerStyleAvailable(root, n) {
			t.Errorf("style %d has no template but was accepted", n)
		}
	}
}

// A menu set with more than fifteen templates must work without a code change.
func TestHeaderStyleAvailableIsNotCappedAtFifteen(t *testing.T) {
	root := makeHeaderSet(t, 1, 20, 42)
	for _, n := range []int{20, 42} {
		if !headerStyleAvailable(root, n) {
			t.Errorf("style %d exists but was rejected; the bound is still fixed", n)
		}
	}
}

// If discovery fails there is no basis to reject a stored preference, and
// forcing the selector open on every read would be worse than accepting it.
func TestHeaderStyleAvailableAcceptsWhenDiscoveryFails(t *testing.T) {
	if !headerStyleAvailable(filepath.Join(t.TempDir(), "nonexistent"), 7) {
		t.Error("with no templates discoverable, a positive style should be accepted")
	}
}
