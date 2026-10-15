package configmerge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type item struct {
	ID   int    `json:"id"`
	Tag  string `json:"tag"`
	Name string `json:"name,omitempty"`
	ACS  string `json:"acs,omitempty"`
}

type doc struct {
	Board    string          `json:"board"`
	Networks map[string]netw `json:"networks"`
	Links    []string        `json:"links,omitempty"`
}

type netw struct {
	Address string `json:"address"`
	Hub     string `json:"hub,omitempty"`
}

func mustMerge[T any](t *testing.T, base, ours, theirs T) (T, Result) {
	t.Helper()
	out, res, err := MergeTyped(base, ours, theirs)
	if err != nil {
		t.Fatal(err)
	}
	return out, res
}

func TestMergeObjectsKeyByKey(t *testing.T) {
	base := doc{Board: "B", Networks: map[string]netw{"fido": {Address: "1:1/1"}}}
	ours := doc{Board: "Renamed", Networks: map[string]netw{"fido": {Address: "1:1/1", Hub: "1:1/0"}}}
	theirs := doc{Board: "B", Networks: map[string]netw{
		"fido": {Address: "1:1/1"},
		"fsx":  {Address: "21:1/1"}, // added elsewhere, e.g. helper ftnsetup
	}}
	out, res := mustMerge(t, base, ours, theirs)
	if out.Board != "Renamed" || out.Networks["fido"].Hub != "1:1/0" || out.Networks["fsx"].Address != "21:1/1" {
		t.Errorf("merged = %+v", out)
	}
	if len(res.Conflicts) != 0 {
		t.Errorf("conflicts = %+v", res.Conflicts)
	}
}

func TestMergeConflictKeepsOursAndReports(t *testing.T) {
	base := doc{Board: "B"}
	out, res := mustMerge(t, base, doc{Board: "Ours"}, doc{Board: "Theirs"})
	if out.Board != "Ours" || len(res.Conflicts) != 1 || res.Conflicts[0].Path != "board" {
		t.Errorf("out = %+v, conflicts = %+v", out, res.Conflicts)
	}
}

func TestMergeUnkeyedListIsOneValue(t *testing.T) {
	base := doc{Links: []string{"a"}}
	out, _ := mustMerge(t, base, base, doc{Links: []string{"a", "b"}})
	if len(out.Links) != 2 {
		t.Errorf("links = %v", out.Links)
	}
}

func TestMergeDeletedNetworkEditedElsewhereIsKept(t *testing.T) {
	base := doc{Networks: map[string]netw{"fido": {Address: "1:1/1"}}}
	ours := doc{Networks: map[string]netw{}}
	theirs := doc{Networks: map[string]netw{"fido": {Address: "1:1/2"}}}
	out, res := mustMerge(t, base, ours, theirs)
	if out.Networks["fido"].Address != "1:1/2" || len(res.Conflicts) != 1 {
		t.Errorf("out = %+v, conflicts = %+v", out, res.Conflicts)
	}
}

func TestMergeKeyedList(t *testing.T) {
	base := []item{{ID: 1, Tag: "A"}, {ID: 2, Tag: "B"}, {ID: 3, Tag: "C"}}
	ours := []item{{ID: 1, Tag: "A", Name: "edited"}, {ID: 3, Tag: "C"}, {ID: 4, Tag: "MINE"}}
	theirs := []item{{ID: 1, Tag: "A", ACS: "s20"}, {ID: 2, Tag: "B"}, {ID: 3, Tag: "C"}, {ID: 4, Tag: "THEIRS"}, {ID: 5, Tag: "NEW"}}
	out, res := mustMerge(t, base, ours, theirs)

	byTag := map[string]item{}
	for _, it := range out {
		byTag[it.Tag] = it
	}
	if a := byTag["A"]; a.Name != "edited" || a.ACS != "s20" {
		t.Errorf("A = %+v, want both edits", a)
	}
	if _, ok := byTag["B"]; ok {
		t.Error("B was deleted here and untouched elsewhere; should be gone")
	}
	if byTag["THEIRS"].ID != 4 || byTag["NEW"].ID != 5 {
		t.Errorf("theirs' additions moved: %+v", out)
	}
	if mine := byTag["MINE"]; mine.ID != 6 || res.Renumbered[4] != 6 {
		t.Errorf("MINE = %+v, renumbered = %v", mine, res.Renumbered)
	}
}

func TestMergeKeyedListSameTagAddedTwice(t *testing.T) {
	ours := []item{{ID: 7, Tag: "FD_X", Name: "Mine"}}
	theirs := []item{{ID: 6, Tag: "FD_X", Name: "Theirs"}}
	out, res := mustMerge(t, nil, ours, theirs)
	if len(out) != 1 || out[0].ID != 6 || res.Renumbered[7] != 6 {
		t.Errorf("out = %+v, renumbered = %v", out, res.Renumbered)
	}

	// Same tag, same id: one item, fields merged.
	out, _ = mustMerge(t, nil, []item{{ID: 6, Tag: "FD_X", Name: "Mine"}}, []item{{ID: 6, Tag: "FD_X", ACS: "s5"}})
	if len(out) != 1 || out[0].Name != "Mine" || out[0].ACS != "s5" {
		t.Errorf("out = %+v", out)
	}
}

func itemsFile(path string) File[[]item] {
	return File[[]item]{
		Path: path,
		Parse: func(b []byte) ([]item, error) {
			var v []item
			return v, json.Unmarshal(b, &v)
		},
		Marshal: func(v []item) ([]byte, error) { return json.MarshalIndent(v, "", "  ") },
	}
}

func TestSaveFileMergesAndSkipsNoOps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "items.json")
	f := itemsFile(path)

	// Missing file: written from ours.
	base := []item{{ID: 1, Tag: "A"}}
	if _, _, err := SaveFile(f, base, base); err != nil {
		t.Fatal(err)
	}

	// Another program adds an item.
	if err := os.WriteFile(path, []byte(`[{"id":1,"tag":"A"},{"id":2,"tag":"B"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	ours := []item{{ID: 1, Tag: "A", Name: "edited"}}
	saved, _, err := SaveFile(f, base, ours)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 2 || saved[0].Name != "edited" {
		t.Fatalf("saved = %+v", saved)
	}

	// Nothing changed on either side: the file is not rewritten.
	before, _ := os.Stat(path)
	if _, _, err := SaveFile(f, saved, saved); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !os.SameFile(before, after) {
		t.Error("no-op save rewrote the file")
	}
}

// A merged save keeps the file's mode: ftn.json made 0600 to hide its
// passwords must not come back 0644.
func TestSaveFileKeepsFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "items.json")
	if err := os.WriteFile(path, []byte(`[{"id":1,"tag":"A"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	base := []item{{ID: 1, Tag: "A"}}
	if _, _, err := SaveFile(itemsFile(path), base, []item{{ID: 1, Tag: "A", Name: "edited"}}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}
