package configeditor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestDoorCategoriesEditorRoundTrip(t *testing.T) {
	m, dir := newDiskModel(t)
	m = press(t, m, "c", "i", "enter")
	if m.mode != modeRecordEdit || m.recordType != "doorcategory" {
		t.Fatalf("%v %q", m.mode, m.recordType)
	}
	m = setRecField(t, m, "Code", "games")
	m = setRecField(t, m, "Name", "Games")
	m = setRecField(t, m, "Description", "Games for callers")
	m = setRecField(t, m, "Min Access Level", "20")
	m = setRecField(t, m, "ACS", "s20")
	m = setRecField(t, m, "Sort Order", "3")
	m = pickRecField(t, m, "Door Sort", "manual")
	saveAndQuit(t, m)
	ac := reloadConfigs(t, dir)
	if len(ac.Server.DoorCategories) != 1 {
		t.Fatal(ac.Server.DoorCategories)
	}
	cat := ac.Server.DoorCategories[0]
	if cat.Code != "GAMES" || cat.Name != "Games" || cat.MinAccessLevel != 20 || cat.ACS != "s20" || cat.SortOrder != 3 || cat.Sort != "manual" {
		t.Fatal(cat)
	}
}

func TestDoorCategoryRenameDeleteAndPicker(t *testing.T) {
	m := &Model{configs: &allConfigs{Server: config.ServerConfig{DoorCategories: []config.DoorCategory{{Code: "GAMES", Name: "Games"}}}, Doors: map[string]config.DoorConfig{"A": {Code: "A", Name: "A", Category: "GAMES"}}}, recordEditIdx: 0}
	fields := m.fieldsDoorCategory()
	if err := fields[0].Set("FUN"); err != nil {
		t.Fatal(err)
	}
	if m.configs.Doors["A"].Category != "FUN" {
		t.Fatal("rename left dangling reference")
	}
	for _, f := range m.fieldsDoor() {
		if f.Label == "Category" {
			items := f.LookupItems()
			if len(items) != 2 || items[1].Value != "FUN" {
				t.Fatal(items)
			}
		}
	}
	if err := m.fieldsDoorCategory()[0].Set("OTHER"); err == nil {
		t.Fatal("reserved code accepted")
	}
	m.recordType = "doorcategory"
	m.recordCursor = 0
	m.deleteRecord()
	if len(m.configs.Server.DoorCategories) != 0 || m.configs.Doors["A"].Category != "" {
		t.Fatal("delete left dangling reference")
	}
}

func TestDoorMenuFieldsRoundTrip(t *testing.T) {
	m, dir := newDoorRecord(t)
	m.configs.Server.DoorCategories = []config.DoorCategory{{Code: "GAMES", Name: "Games"}}
	m = pickRecField(t, m, "Category", "GAMES")
	m = setRecField(t, m, "Description", "Example game")
	m = setRecField(t, m, "Sort Order", "7")
	m = press(t, gotoField(t, m, "Hidden"), "enter")
	d := savedDoor(t, m, dir)
	if d.Category != "GAMES" || d.Description != "Example game" || d.SortOrder != 7 || !d.Hidden {
		t.Fatal(d)
	}
}

func TestSaveDoorsPreservesConfigOrder(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "doors.json")
	if err := os.WriteFile(p, []byte(`[{"code":"Z","name":"first"},{"code":"A","name":"second"}]`), 0644); err != nil {
		t.Fatal(err)
	}
	doors, err := config.LoadDoors(p)
	if err != nil {
		t.Fatal(err)
	}
	doors["B"] = config.DoorConfig{Code: "B", ConfigOrder: nextDoorConfigOrder(doors)}
	if err := saveDoors(dir, doors); err != nil {
		t.Fatal(err)
	}
	got, err := config.LoadDoors(p)
	if err != nil {
		t.Fatal(err)
	}
	if got["Z"].ConfigOrder != 0 || got["A"].ConfigOrder != 1 || got["B"].ConfigOrder != 2 {
		t.Fatal(got)
	}
}
