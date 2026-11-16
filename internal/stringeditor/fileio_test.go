package stringeditor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	configtemplates "github.com/ViSiON-3/vision-3-bbs/templates/configs"
)

// TestLoadStrings_MissingFileWritesNothing checks that a missing file is no
// strings, and is not created: the BBS has the shipped strings built in.
func TestLoadStrings_MissingFileWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strings.json")
	got, err := LoadStrings(path)
	if err != nil {
		t.Fatalf("LoadStrings: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d values; want none", len(got))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("strings.json created (%v)", err)
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
		"zebra":   "last",
		"alpha":   "first",
		"_extra3": "must not persist",
		"_note":   "a sysop's note persists",
		"mid":     `has "quotes" and |07 codes`,
	}
	if err := SaveStrings(path, in); err != nil {
		t.Fatalf("SaveStrings: %v", err)
	}

	got, err := LoadStrings(path)
	if err != nil {
		t.Fatalf("LoadStrings: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 keys (placeholder dropped), got %d: %v", len(got), got)
	}
	if _, ok := got["_extra3"]; ok {
		t.Error("the reserved placeholder _extra3 should have been filtered out")
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

// TestMissingTemplateInstallation covers an installed binary with no templates
// directory on disk and no strings.json: the editor shows the shipped
// defaults from the copy embedded in the binary, and creates no file until
// there is something to save.
func TestMissingTemplateInstallation(t *testing.T) {
	embedded, err := configtemplates.StringDefaults()
	if err != nil {
		t.Fatalf("embedded defaults unavailable: %v", err)
	}
	path := filepath.Join(t.TempDir(), "strings.json")
	m, err := New(path, embedded)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := m.getValue("pageOnlineNodesHeader"); got != embedded["pageOnlineNodesHeader"] {
		t.Errorf("pageOnlineNodesHeader = %q, want the shipped default %q", got, embedded["pageOnlineNodesHeader"])
	}
	if m.stateOf("pageOnlineNodesHeader") != stateDefault {
		t.Error("pageOnlineNodesHeader not shown as the default")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("strings.json created on open (%v)", err)
	}
}

// TestNoDefaultsAtAllStillOpens checks the editor opens against a file with no
// factory defaults available, rather than failing.
func TestNoDefaultsAtAllStillOpens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strings.json")
	m, err := New(path, nil)
	if err != nil {
		t.Fatalf("New with no defaults: %v", err)
	}
	if len(m.entries) == 0 {
		t.Error("no entries listed")
	}
	if m.View() == "" {
		t.Error("View rendered nothing")
	}
}
