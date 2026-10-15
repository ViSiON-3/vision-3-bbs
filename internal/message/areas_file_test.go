package message

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func boolp(b bool) *bool { return &b }

func areaByID(t *testing.T, areas []MessageArea, id int) MessageArea {
	t.Helper()
	for _, a := range areas {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("area %d missing from %+v", id, areas)
	return MessageArea{}
}

func TestMergeAreasKeepsBothSidesFieldEdits(t *testing.T) {
	base := []MessageArea{
		{ID: 1, Position: 1, Tag: "A", Name: "Alpha"},
		{ID: 2, Position: 2, Tag: "B", Name: "Beta"},
	}
	ours := CloneAreas(base)
	ours[0].Name = "Alpha Renamed" // edited here
	theirs := CloneAreas(base)
	theirs[0].RealNameOnly = true // same area, other field, edited elsewhere
	theirs[1].AllowAnon = boolp(true)

	res, err := MergeAreas(base, ours, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) != 0 {
		t.Errorf("unexpected conflicts: %+v", res.Conflicts)
	}
	a := areaByID(t, res.Areas, 1)
	if a.Name != "Alpha Renamed" || !a.RealNameOnly {
		t.Errorf("area 1 = %+v, want both edits", a)
	}
	if b := areaByID(t, res.Areas, 2); b.AllowAnon == nil || !*b.AllowAnon {
		t.Errorf("area 2 lost the external edit: %+v", b)
	}
}

func TestMergeAreasConflictKeepsOurs(t *testing.T) {
	base := []MessageArea{{ID: 1, Tag: "A", ACSWrite: "s10"}}
	ours := []MessageArea{{ID: 1, Tag: "A", ACSWrite: "s20"}}
	theirs := []MessageArea{{ID: 1, Tag: "A", ACSWrite: "s30"}}
	res, err := MergeAreas(base, ours, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if got := areaByID(t, res.Areas, 1).ACSWrite; got != "s20" {
		t.Errorf("ACSWrite = %q, want ours", got)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Field != "acs_write" {
		t.Errorf("conflicts = %+v", res.Conflicts)
	}
}

func TestMergeAreasAddsAndDeletes(t *testing.T) {
	base := []MessageArea{
		{ID: 1, Tag: "KEEP"},
		{ID: 2, Tag: "DEL_HERE"},
		{ID: 3, Tag: "DEL_THERE"},
		{ID: 4, Tag: "DEL_THERE_EDIT_HERE"},
	}
	ours := []MessageArea{
		{ID: 1, Tag: "KEEP"},
		{ID: 3, Tag: "DEL_THERE"},
		{ID: 4, Tag: "DEL_THERE_EDIT_HERE", Name: "edited"},
		{ID: 5, Tag: "NEW_HERE"},
	}
	theirs := []MessageArea{
		{ID: 1, Tag: "KEEP"},
		{ID: 2, Tag: "DEL_HERE"},
		{ID: 6, Tag: "NEW_THERE"},
	}
	res, err := MergeAreas(base, ours, theirs)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, a := range res.Areas {
		got[a.Tag] = true
	}
	for tag, want := range map[string]bool{
		"KEEP": true, "DEL_HERE": false, "DEL_THERE": false,
		"DEL_THERE_EDIT_HERE": true, "NEW_HERE": true, "NEW_THERE": true,
	} {
		if got[tag] != want {
			t.Errorf("%s present = %v, want %v", tag, got[tag], want)
		}
	}
	if len(res.Conflicts) != 1 {
		t.Errorf("want one edit-vs-delete conflict, got %+v", res.Conflicts)
	}
}

func TestMergeAreasRenumbersIDCollision(t *testing.T) {
	ours := []MessageArea{{ID: 3, Tag: "MINE", BasePath: defaultAreaBasePath(3)}}
	theirs := []MessageArea{{ID: 3, Tag: "THEIRS", BasePath: defaultAreaBasePath(3)}}
	res, err := MergeAreas(nil, ours, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if areaByID(t, res.Areas, 3).Tag != "THEIRS" {
		t.Errorf("theirs should keep ID 3: %+v", res.Areas)
	}
	newID := res.Renumbered[3]
	mine := areaByID(t, res.Areas, newID)
	if newID != 4 || mine.Tag != "MINE" || mine.BasePath != defaultAreaBasePath(4) {
		t.Errorf("renumbered = %d, area = %+v", newID, mine)
	}
}

// writeAreasFixture writes areas to dir/message_areas.json the way another
// program would.
func writeAreasFixture(t *testing.T, path string, areas []MessageArea) {
	t.Helper()
	data, err := json.MarshalIndent(areas, "", "    ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readAreasFixture(t *testing.T, path string) []MessageArea {
	t.Helper()
	areas, _, err := ReadAreasFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return areas
}

// TestSaveAreasKeepsExternalEdit is issue #639: a sponsor save in the BBS
// must not undo a change helper or the config editor wrote meanwhile.
func TestSaveAreasKeepsExternalEdit(t *testing.T) {
	cfgDir := t.TempDir()
	path := filepath.Join(cfgDir, "message_areas.json")
	writeAreasFixture(t, path, []MessageArea{
		{ID: 1, Position: 1, Tag: "ONE", Name: "One", AreaType: "local"},
		{ID: 2, Position: 2, Tag: "TWO", Name: "Two", AreaType: "local"},
	})
	mm, err := NewMessageManager(t.TempDir(), cfgDir, "Test BBS", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Another program turns on real names everywhere.
	writeAreasFixture(t, path, []MessageArea{
		{ID: 1, Position: 1, Tag: "ONE", Name: "One", AreaType: "local", RealNameOnly: true},
		{ID: 2, Position: 2, Tag: "TWO", Name: "Two", AreaType: "local", RealNameOnly: true},
	})

	// Before the watcher reloads, a sponsor renames area 1.
	a, _ := mm.GetAreaByID(1)
	edited := *a
	edited.Name = "One Renamed"
	if err := mm.UpdateAreaByID(1, edited); err != nil {
		t.Fatal(err)
	}
	if err := mm.SaveAreas(); err != nil {
		t.Fatal(err)
	}

	disk := readAreasFixture(t, path)
	one, two := areaByID(t, disk, 1), areaByID(t, disk, 2)
	if one.Name != "One Renamed" || !one.RealNameOnly || !two.RealNameOnly {
		t.Errorf("on disk: one=%+v two=%+v", one, two)
	}
	// Memory picked up the external change too.
	if m2, _ := mm.GetAreaByID(2); !m2.RealNameOnly {
		t.Error("in-memory area 2 missed the merged external edit")
	}

	// A second save with nothing new on disk is a plain write.
	edited.RealNameOnly = true
	edited.Description = "desc"
	if err := mm.UpdateAreaByID(1, edited); err != nil {
		t.Fatal(err)
	}
	if err := mm.SaveAreas(); err != nil {
		t.Fatal(err)
	}
	if got := areaByID(t, readAreasFixture(t, path), 1).Description; got != "desc" {
		t.Errorf("second save lost: %q", got)
	}
}

func TestAddAreaReturnsRenumberedID(t *testing.T) {
	cfgDir := t.TempDir()
	path := filepath.Join(cfgDir, "message_areas.json")
	writeAreasFixture(t, path, []MessageArea{{ID: 1, Position: 1, Tag: "ONE", AreaType: "local"}})
	mm, err := NewMessageManager(t.TempDir(), cfgDir, "Test BBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Another program adds area 2 first.
	writeAreasFixture(t, path, []MessageArea{
		{ID: 1, Position: 1, Tag: "ONE", AreaType: "local"},
		{ID: 2, Position: 2, Tag: "OTHER", AreaType: "local"},
	})
	id, err := mm.AddArea(MessageArea{Tag: "MINE", AreaType: "echomail", Network: "fsxnet"})
	if err != nil {
		t.Fatal(err)
	}
	if id != 3 {
		t.Fatalf("AddArea returned %d, want 3", id)
	}
	if a, ok := mm.GetAreaByID(3); !ok || a.Tag != "MINE" {
		t.Errorf("area 3 in memory = %+v", a)
	}
	if a, ok := mm.GetAreaByID(2); !ok || a.Tag != "OTHER" {
		t.Errorf("area 2 in memory = %+v", a)
	}
}

// Two tools adding the same echo must not leave two areas with one tag.
func TestMergeAreasSameTagAddedTwice(t *testing.T) {
	t.Run("same ID merges fields", func(t *testing.T) {
		ours := []MessageArea{{ID: 3, Tag: "FD_X", Name: "Mine", MaxMessages: 100}}
		theirs := []MessageArea{{ID: 3, Tag: "FD_X", Name: "Theirs", RealNameOnly: true}}
		res, err := MergeAreas(nil, ours, theirs)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Areas) != 1 {
			t.Fatalf("areas = %+v, want one", res.Areas)
		}
		a := res.Areas[0]
		if a.Name != "Mine" || a.MaxMessages != 100 || !a.RealNameOnly {
			t.Errorf("merged = %+v", a)
		}
	})
	t.Run("different IDs keep theirs", func(t *testing.T) {
		ours := []MessageArea{{ID: 4, Tag: "FD_X", Name: "Mine"}}
		theirs := []MessageArea{{ID: 3, Tag: "FD_X", Name: "Theirs"}}
		res, err := MergeAreas(nil, ours, theirs)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Areas) != 1 || res.Areas[0].ID != 3 || res.Renumbered[4] != 3 {
			t.Errorf("areas = %+v, renumbered = %v", res.Areas, res.Renumbered)
		}
	})
	t.Run("ID taken by a different tag", func(t *testing.T) {
		ours := []MessageArea{{ID: 3, Tag: "FD_MINE"}}
		theirs := []MessageArea{{ID: 3, Tag: "FD_X"}, {ID: 4, Tag: "FD_MINE"}}
		res, err := MergeAreas(nil, ours, theirs)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Areas) != 2 || res.Renumbered[3] != 4 {
			t.Errorf("areas = %+v, renumbered = %v", res.Areas, res.Renumbered)
		}
	})
}

// A message_areas.json that exists but does not parse must not be replaced by
// the BBS's in-memory copy: the save fails and the file is left as it is.
func TestSaveAreasRefusesToOverwriteUnparsableFile(t *testing.T) {
	cfgDir := t.TempDir()
	path := filepath.Join(cfgDir, "message_areas.json")
	writeAreasFixture(t, path, []MessageArea{{ID: 1, Position: 1, Tag: "ONE", AreaType: "local"}})
	mm, err := NewMessageManager(t.TempDir(), cfgDir, "Test BBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	broken := []byte(`[{"id": 1, "tag": "ONE",}]`)
	if err := os.WriteFile(path, broken, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mm.SaveAreas(); err == nil {
		t.Fatal("SaveAreas succeeded over an unparsable file")
	}
	if got, _ := os.ReadFile(path); string(got) != string(broken) {
		t.Errorf("file changed to %q", got)
	}
}

// An area renamed here to the tag another program just gave a new area would
// leave two areas with one tag; the merge refuses rather than pick one.
func TestMergeAreasRefusesNewDuplicateTag(t *testing.T) {
	base := []MessageArea{{ID: 1, Tag: "A"}}
	ours := []MessageArea{{ID: 1, Tag: "B"}}
	theirs := []MessageArea{{ID: 1, Tag: "A"}, {ID: 2, Tag: "B"}}
	_, err := MergeAreas(base, ours, theirs)
	var dup *DuplicateTagError
	if !errors.As(err, &dup) || len(dup.Tags) != 1 || dup.Tags[0] != "B" {
		t.Fatalf("err = %v, want DuplicateTagError for B", err)
	}

	// A duplicate already on disk does not block an unrelated save.
	theirs = []MessageArea{{ID: 1, Tag: "A"}, {ID: 2, Tag: "X"}, {ID: 3, Tag: "X"}}
	if _, err := MergeAreas(theirs, []MessageArea{{ID: 1, Tag: "A", Name: "n"}, {ID: 2, Tag: "X"}, {ID: 3, Tag: "X"}}, theirs); err != nil {
		t.Errorf("existing duplicate blocked the save: %v", err)
	}
}
