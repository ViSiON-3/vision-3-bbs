package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// An older strings.json's defColorN become the slot defaults; Bar keeps its
// background, and 0, the value of an unset field, counts as not set.
func TestLegacyColorSlots(t *testing.T) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(`{"defColor1": 14, "defColor2": 0, "defColor7": 15, "defColor3": "x"}`), &raw); err != nil {
		t.Fatal(err)
	}
	got := legacyColorSlots(raw)
	if got != [7]string{0: "|14", 6: "|15|B6"} {
		t.Errorf("got %q", got)
	}
}

// A cyan Bar foreground would vanish on the bar's cyan, so it gets blue.
func TestLegacyBarStaysReadable(t *testing.T) {
	for n, want := range map[int]string{3: "|03|B4", 11: "|11|B4", 15: "|15|B6", 1: "|01|B6"} {
		raw := map[string]json.RawMessage{"defColor7": json.RawMessage(fmt.Sprint(n))}
		if got := legacyColorSlots(raw)[6]; got != want {
			t.Errorf("defColor7 %d = %q, want %q", n, got, want)
		}
	}
}

// The legacy Bar background follows the built-in Bar slot's, so a change to
// the default carries over, and the readability check goes with it: on a
// blue bar, blue text gets cyan.
func TestLegacyBarFollowsDefault(t *testing.T) {
	old := ansi.ColorSlotDefaults[6]
	t.Cleanup(func() { ansi.ColorSlotDefaults[6] = old })
	ansi.ColorSlotDefaults[6] = "|15|B4"
	for n, want := range map[int]string{15: "|15|B4", 3: "|03|B4", 1: "|01|B6", 9: "|09|B6"} {
		raw := map[string]json.RawMessage{"defColor7": json.RawMessage(fmt.Sprint(n))}
		if got := legacyColorSlots(raw)[6]; got != want {
			t.Errorf("defColor7 %d on a blue bar = %q, want %q", n, got, want)
		}
	}
}

// The editors read the slot defaults straight from a strings.json; a file
// that is missing or isn't JSON sets none.
func TestLegacyColorSlotsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "strings.json")
	if got := LegacyColorSlotsFile(path); got != ([7]string{}) {
		t.Errorf("missing file: %q", got)
	}
	if err := os.WriteFile(path, []byte(`{"defColor1": 12, "defColor7": 1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LegacyColorSlotsFile(path); got != ([7]string{0: "|12", 6: "|01|B6"}) {
		t.Errorf("got %q", got)
	}
	if err := os.WriteFile(path, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LegacyColorSlotsFile(path); got != ([7]string{}) {
		t.Errorf("bad file: %q", got)
	}
}
