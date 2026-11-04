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

func TestDropfileCaseField(t *testing.T) {
	m := newDoorModel(map[string]config.DoorConfig{
		"LORD": {Code: "LORD", Name: "LORD"},
	})
	m.recordEditIdx = 0
	fld := findField(t, m.buildRecordFields(), "Dropfile Case")

	if err := fld.Set("lower"); err != nil {
		t.Fatalf("Set(lower): %v", err)
	}
	if got := m.configs.Doors["LORD"].DropfileCase; got != "lower" {
		t.Errorf("persisted DropfileCase = %q, want lower", got)
	}
	if got := fld.Get(); got != "lower" {
		t.Errorf("Get() = %q, want lower", got)
	}
}

// Dropfile Case and Dropfile Location only affect native/Windows doors, so
// the fields are hidden for DOS doors: dosDropfileName ignores the case, and
// the dropfiles always go in C:\NODES\TEMPn whatever the location says.
func TestDropfileCaseFieldHiddenForDOS(t *testing.T) {
	m := newDoorModel(map[string]config.DoorConfig{
		"DOSGAME": {Code: "DOSGAME", Name: "DOSGAME", IsDOS: true},
	})
	m.recordEditIdx = 0
	for _, f := range m.buildRecordFields() {
		if f.Label == "Dropfile Case" || f.Label == "Dropfile Location" {
			t.Errorf("%s field should be hidden for DOS doors", f.Label)
		}
	}
}

// The run limit is a VPL setting: it is shown for v3_script doors only, takes
// -1 (no limit), 0 (default) or minutes, and refuses anything else.
func TestMaxRunMinsField(t *testing.T) {
	m := newDoorModel(map[string]config.DoorConfig{
		"VAULT": {Code: "VAULT", Name: "The Vault", Type: "v3_script", Script: "vault.js"},
	})
	m.recordEditIdx = 0
	fld := findField(t, m.buildRecordFields(), "Max Run Mins")
	if fld.Min != -1 {
		t.Errorf("Min = %d, want -1", fld.Min)
	}
	for _, v := range []string{"-1", "0", "120"} {
		if err := fld.Set(v); err != nil {
			t.Fatalf("Set(%s): %v", v, err)
		}
		if got := fld.Get(); got != v {
			t.Errorf("after Set(%s), Get() = %q", v, got)
		}
	}
	if got := m.configs.Doors["VAULT"].MaxRunMinutes; got != 120 {
		t.Errorf("persisted MaxRunMinutes = %d, want 120", got)
	}
	for _, v := range []string{"-2", "soon"} {
		if err := fld.Set(v); err == nil {
			t.Errorf("Set(%q) accepted, want an error", v)
		}
	}
	if got := m.configs.Doors["VAULT"].MaxRunMinutes; got != 120 {
		t.Errorf("a refused value changed MaxRunMinutes to %d", got)
	}
}

func TestMaxRunMinsFieldOnlyForVPL(t *testing.T) {
	for _, d := range []config.DoorConfig{
		{Code: "LORD", Name: "LORD"},
		{Code: "SYNC", Name: "Sync", Type: "synchronet_js", Script: "x.js"},
		{Code: "BBSLINK", Name: "Link", Type: "rlogin", Host: "example.org"},
	} {
		m := newDoorModel(map[string]config.DoorConfig{d.Code: d})
		m.recordEditIdx = 0
		for _, f := range m.buildRecordFields() {
			if f.Label == "Max Run Mins" {
				t.Errorf("%s door (type %q) shows Max Run Mins", d.Code, d.Type)
			}
		}
	}
}
