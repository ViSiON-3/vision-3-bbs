package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// An older strings.json's defColorN become the slot defaults; Bar keeps its
// background, and 0, the value of an unset field, counts as not set.
func TestLegacyColorSlots(t *testing.T) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(`{"defColor1": 14, "defColor2": 0, "defColor7": 15, "defColor3": "x"}`), &raw); err != nil {
		t.Fatal(err)
	}
	got := legacyColorSlots(raw)
	if got != [7]string{0: "|14", 6: "|15|B3"} {
		t.Errorf("got %q", got)
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
	if got := LegacyColorSlotsFile(path); got != ([7]string{0: "|12", 6: "|01|B3"}) {
		t.Errorf("got %q", got)
	}
	if err := os.WriteFile(path, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LegacyColorSlotsFile(path); got != ([7]string{}) {
		t.Errorf("bad file: %q", got)
	}
}
