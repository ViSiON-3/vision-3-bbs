package stringeditor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadStrings_MissingFileCreatesDefaults checks that a missing file is
// created with default strings and no placeholder keys.
func TestLoadStrings_MissingFileCreatesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strings.json")
	got, err := LoadStrings(path)
	if err != nil {
		t.Fatalf("LoadStrings: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected non-empty defaults map")
	}
	// The file must now exist and be valid JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("defaults file not created: %v", err)
	}
	var onDisk map[string]string
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("created file is not valid JSON: %v", err)
	}
	// No internal placeholder keys are persisted.
	for k := range onDisk {
		if strings.HasPrefix(k, "_") {
			t.Errorf("placeholder key %q should not be persisted", k)
		}
	}
}

// TestLoadStrings_InvalidJSON checks that malformed JSON returns an error.
func TestLoadStrings_InvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strings.json")
	if err := os.WriteFile(path, []byte("{broken"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadStrings(path); err == nil {
		t.Fatal("invalid JSON should return an error")
	}
}

// TestSaveStrings_RoundTripSortedAndFiltered checks the save/load round trip
// filters placeholder keys and writes keys in sorted order.
func TestSaveStrings_RoundTripSortedAndFiltered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strings.json")
	in := map[string]string{
		"zebra":     "last",
		"alpha":     "first",
		"_reserved": "must not persist",
		"mid":       `has "quotes" and |07 codes`,
	}
	if err := SaveStrings(path, in); err != nil {
		t.Fatalf("SaveStrings: %v", err)
	}

	got, err := LoadStrings(path)
	if err != nil {
		t.Fatalf("LoadStrings: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 keys (placeholder dropped), got %d: %v", len(got), got)
	}
	if _, ok := got["_reserved"]; ok {
		t.Error("_reserved should have been filtered out")
	}
	if got["mid"] != in["mid"] {
		t.Errorf("mid = %q, want %q", got["mid"], in["mid"])
	}

	// Keys are written in sorted order.
	raw, _ := os.ReadFile(path)
	ia := strings.Index(string(raw), `"alpha"`)
	im := strings.Index(string(raw), `"mid"`)
	iz := strings.Index(string(raw), `"zebra"`)
	if ia < 0 || im < 0 || iz < 0 || ia >= im || im >= iz {
		t.Errorf("keys not sorted on disk: alpha@%d mid@%d zebra@%d", ia, im, iz)
	}
}

// TestMarshalOrdered_Empty checks that a nil map marshals to an empty object.
func TestMarshalOrdered_Empty(t *testing.T) {
	data, err := marshalOrdered(nil)
	if err != nil {
		t.Fatalf("marshalOrdered: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("empty output not valid JSON: %v (data=%q)", err, data)
	}
	if len(m) != 0 {
		t.Errorf("want empty object, got %v", m)
	}
}

// TestDefaultStrings_SkipsPlaceholders checks defaults exclude placeholder
// keys but cover every non-placeholder metadata entry.
func TestDefaultStrings_SkipsPlaceholders(t *testing.T) {
	defaults := DefaultStrings()
	if len(defaults) == 0 {
		t.Fatal("expected non-empty defaults")
	}
	for k := range defaults {
		if strings.HasPrefix(k, "_") {
			t.Errorf("placeholder key %q should not be in defaults", k)
		}
	}
	// Every non-placeholder metadata entry must have a default.
	for _, e := range StringEntries() {
		if strings.HasPrefix(e.Key, "_") {
			continue
		}
		if _, ok := defaults[e.Key]; !ok {
			t.Errorf("metadata key %q missing from defaults", e.Key)
		}
	}
}
