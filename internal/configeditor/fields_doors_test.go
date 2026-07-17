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

// Doors are persisted as a JSON array keyed by Name on load, so a new door's
// map key must equal its Name — otherwise the list shows a stale slug like
// "newdoor1" that silently disappears on reload.
func TestInsertDoorKeyedByName(t *testing.T) {
	m := newDoorModel(map[string]config.DoorConfig{})
	m.insertRecord()
	d, ok := m.configs.Doors["NEWDOOR1"]
	if !ok {
		t.Fatalf("expected key NEWDOOR1, got keys %v", m.doorKeys())
	}
	if d.Name != "NEWDOOR1" {
		t.Errorf("Name = %q, want NEWDOOR1 (key and Name must match)", d.Name)
	}
	m.insertRecord()
	if d2, ok := m.configs.Doors["NEWDOOR2"]; !ok || d2.Name != "NEWDOOR2" {
		t.Errorf("second insert: got keys %v, NEWDOOR2.Name = %q", m.doorKeys(), d2.Name)
	}
}

func TestFieldsDoorNameRenamesKey(t *testing.T) {
	m := newDoorModel(map[string]config.DoorConfig{
		"LORD":   {Name: "LORD", WorkingDirectory: "doors/lord"},
		"TW2002": {Name: "TW2002"},
	})
	m.recordEditIdx = 0 // sorted keys: LORD, TW2002
	fields := m.buildRecordFields()
	name := findField(t, fields, "Name")

	// Renaming uppercases (DOOR:NAME menu lookups are uppercase) and re-keys the map.
	if err := name.Set("legend"); err != nil {
		t.Fatalf("Set(legend): %v", err)
	}
	if _, ok := m.configs.Doors["LORD"]; ok {
		t.Error("old key LORD still present after rename")
	}
	d, ok := m.configs.Doors["LEGEND"]
	if !ok {
		t.Fatalf("renamed key LEGEND missing, keys = %v", m.doorKeys())
	}
	if d.Name != "LEGEND" {
		t.Errorf("Name = %q, want LEGEND", d.Name)
	}
	if d.WorkingDirectory != "doors/lord" {
		t.Errorf("rename dropped config: WorkingDirectory = %q", d.WorkingDirectory)
	}

	// AfterSet re-syncs the edit index to the renamed entry and stays on the field.
	if name.AfterSet == nil {
		t.Fatal("Name field must have AfterSet to re-sync indices after rename")
	}
	name.AfterSet(m, "LEGEND")
	if m.recordEditIdx != 0 { // sorted: LEGEND, TW2002
		t.Errorf("recordEditIdx = %d, want 0", m.recordEditIdx)
	}
	if !m.stayOnField {
		t.Error("stayOnField should be true after rename")
	}
}

func TestFieldsDoorNameValidation(t *testing.T) {
	m := newDoorModel(map[string]config.DoorConfig{
		"LORD":   {Name: "LORD"},
		"TW2002": {Name: "TW2002"},
	})
	m.recordEditIdx = 0
	name := findField(t, m.buildRecordFields(), "Name")

	if err := name.Set(""); err == nil {
		t.Error("empty name should be rejected")
	}
	if err := name.Set("tw2002"); err == nil {
		t.Error("duplicate name (case-insensitive) should be rejected")
	}
	if err := name.Set("LORD"); err != nil {
		t.Errorf("re-setting the same name should be a no-op, got %v", err)
	}
	if _, ok := m.configs.Doors["LORD"]; !ok {
		t.Error("LORD should still exist after no-op rename")
	}
}

// A door keyed uppercase but carrying a mixed-case Name (possible in a
// hand-edited doors.json before load normalization existed) must have its
// Name normalized even when the key itself doesn't change.
func TestFieldsDoorNameSameKeyNormalizesName(t *testing.T) {
	m := newDoorModel(map[string]config.DoorConfig{
		"LORD": {Name: "Lord"},
	})
	m.recordEditIdx = 0
	name := findField(t, m.buildRecordFields(), "Name")

	if err := name.Set("lord"); err != nil {
		t.Fatalf("Set(lord): %v", err)
	}
	d, ok := m.configs.Doors["LORD"]
	if !ok {
		t.Fatalf("key LORD missing, keys = %v", m.doorKeys())
	}
	if d.Name != "LORD" {
		t.Errorf("Name = %q, want LORD (same-key set must still normalize Name)", d.Name)
	}
}
