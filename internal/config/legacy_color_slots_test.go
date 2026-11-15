package config

import (
	"encoding/json"
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
