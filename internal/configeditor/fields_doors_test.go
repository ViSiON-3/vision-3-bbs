package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// newDoorModel builds a Model seeded with the given doors map, on the door screen.
func newDoorModel(doors map[string]config.DoorConfig) *Model {
	return &Model{
		configs:    &allConfigs{Doors: doors},
		recordType: "door",
	}
}

// Doors are keyed by their internal Code (Synchronet-style slug); Name is a
// free-form display label. A new door gets a unique code with key, Code, and
// a placeholder Name.
func TestInsertDoorKeyedByCode(t *testing.T) {
	m := newDoorModel(map[string]config.DoorConfig{})
	m.insertRecord()
	d, ok := m.configs.Doors["NEWDOOR1"]
	if !ok {
		t.Fatalf("expected key NEWDOOR1, got keys %v", m.doorKeys())
	}
	if d.Code != "NEWDOOR1" {
		t.Errorf("Code = %q, want NEWDOOR1 (key and Code must match)", d.Code)
	}
	if d.Name != "New Door" {
		t.Errorf("Name = %q, want New Door", d.Name)
	}
	m.insertRecord()
	if d2, ok := m.configs.Doors["NEWDOOR2"]; !ok || d2.Code != "NEWDOOR2" {
		t.Errorf("second insert: got keys %v, NEWDOOR2.Code = %q", m.doorKeys(), d2.Code)
	}
}

func TestFieldsDoorCodeRenamesKey(t *testing.T) {
	m := newDoorModel(map[string]config.DoorConfig{
		"LORD":   {Code: "LORD", Name: "Legend of the Red Dragon", WorkingDirectory: "doors/lord"},
		"TW2002": {Code: "TW2002", Name: "Trade Wars 2002"},
	})
	m.recordEditIdx = 0 // sorted keys: LORD, TW2002
	fields := m.buildRecordFields()
	code := findField(t, fields, "Code")

	// Renaming uppercases (DOOR:CODE menu lookups are uppercase) and re-keys the map.
	if err := code.Set("legend"); err != nil {
		t.Fatalf("Set(legend): %v", err)
	}
	if _, ok := m.configs.Doors["LORD"]; ok {
		t.Error("old key LORD still present after rename")
	}
	d, ok := m.configs.Doors["LEGEND"]
	if !ok {
		t.Fatalf("renamed key LEGEND missing, keys = %v", m.doorKeys())
	}
	if d.Code != "LEGEND" {
		t.Errorf("Code = %q, want LEGEND", d.Code)
	}
	if d.Name != "Legend of the Red Dragon" {
		t.Errorf("rename touched display Name: %q", d.Name)
	}
	if d.WorkingDirectory != "doors/lord" {
		t.Errorf("rename dropped config: WorkingDirectory = %q", d.WorkingDirectory)
	}

	// AfterSet re-syncs the edit index to the renamed entry and stays on the field.
	if code.AfterSet == nil {
		t.Fatal("Code field must have AfterSet to re-sync indices after rename")
	}
	code.AfterSet(m, "LEGEND")
	if m.recordEditIdx != 0 { // sorted: LEGEND, TW2002
		t.Errorf("recordEditIdx = %d, want 0", m.recordEditIdx)
	}
	if !m.stayOnField {
		t.Error("stayOnField should be true after rename")
	}
}

func TestFieldsDoorCodeValidation(t *testing.T) {
	m := newDoorModel(map[string]config.DoorConfig{
		"LORD":   {Code: "LORD", Name: "Legend of the Red Dragon"},
		"TW2002": {Code: "TW2002", Name: "Trade Wars 2002"},
	})
	m.recordEditIdx = 0
	code := findField(t, m.buildRecordFields(), "Code")

	if err := code.Set(""); err == nil {
		t.Error("empty code should be rejected")
	}
	if err := code.Set("tw2002"); err == nil {
		t.Error("duplicate code (case-insensitive) should be rejected")
	}
	if err := code.Set("bad code!"); err == nil {
		t.Error("code with spaces/punctuation should be rejected")
	}
	if err := code.Set("WAYTOOLONGDOORCODE"); err == nil {
		t.Error("code longer than 16 chars should be rejected")
	}
	if err := code.Set("LORD"); err != nil {
		t.Errorf("re-setting the same code should be a no-op, got %v", err)
	}
	if _, ok := m.configs.Doors["LORD"]; !ok {
		t.Error("LORD should still exist after no-op rename")
	}
}

// Name is a display label: case is preserved verbatim and editing it must not
// re-key the map.
func TestFieldsDoorNamePreservesCase(t *testing.T) {
	m := newDoorModel(map[string]config.DoorConfig{
		"LORD": {Code: "LORD", Name: "LORD"},
	})
	m.recordEditIdx = 0
	name := findField(t, m.buildRecordFields(), "Name")

	if err := name.Set("Legend of the Red Dragon"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	d, ok := m.configs.Doors["LORD"]
	if !ok {
		t.Fatalf("key LORD missing after Name edit, keys = %v", m.doorKeys())
	}
	if d.Name != "Legend of the Red Dragon" {
		t.Errorf("Name = %q, want case preserved", d.Name)
	}
}
