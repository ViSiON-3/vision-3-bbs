package configmerge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits")
	}
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

func TestMergeUntaggedAdditionsStaySeparate(t *testing.T) {
	type rec struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	out, res := mustMerge(t, nil, []rec{{ID: 1, Name: "mine"}}, []rec{{ID: 1, Name: "theirs"}})
	if len(out) != 2 || res.Renumbered[1] != 2 {
		t.Errorf("out = %+v, renumbered = %v", out, res.Renumbered)
	}
}

// Renaming an item here to the tag of an item added elsewhere would leave
// two items with one tag: SaveFile refuses and writes nothing.
func TestSaveFileRefusesNewDuplicateTag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "items.json")
	disk := `[{"id":1,"tag":"A"},{"id":2,"tag":"B"}]`
	if err := os.WriteFile(path, []byte(disk), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := SaveFile(itemsFile(path), []item{{ID: 1, Tag: "A"}}, []item{{ID: 1, Tag: "B"}})
	var dup *DuplicateTagError
	if !errors.As(err, &dup) || len(dup.Tags) != 1 || dup.Tags[0] != "B" {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != disk {
		t.Errorf("file changed: %s", got)
	}
}

func TestSaveFileFixHook(t *testing.T) {
	path := filepath.Join(t.TempDir(), "items.json")
	if err := os.WriteFile(path, []byte(`[{"id":1,"tag":"THEIRS","name":"dir_1"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	f := itemsFile(path)
	f.Fix = func(v []item, res Result) []item {
		for i := range v {
			if res.Renumbered[1] == v[i].ID && v[i].Name == "dir_1" {
				v[i].Name = "dir_2"
			}
		}
		return v
	}
	saved, _, err := SaveFile(f, nil, []item{{ID: 1, Tag: "MINE", Name: "dir_1"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range saved {
		if it.Tag == "MINE" && (it.ID != 2 || it.Name != "dir_2") {
			t.Errorf("MINE = %+v", it)
		}
	}
}

func TestMergeDuplicateTagMovedToAnotherItem(t *testing.T) {
	theirs := []item{{ID: 1, Tag: "X"}, {ID: 2, Tag: "X"}, {ID: 3, Tag: "A"}}
	_, res := mustMerge(t, theirs, []item{{ID: 1, Tag: "X"}, {ID: 2, Tag: "Y"}, {ID: 3, Tag: "X"}}, theirs)
	if len(res.DuplicateTags) != 1 || res.DuplicateTags[0] != "X" {
		t.Errorf("DuplicateTags = %v", res.DuplicateTags)
	}
	// Unchanged duplicate on disk: not reported.
	_, res = mustMerge(t, theirs, theirs, theirs)
	if len(res.DuplicateTags) != 0 {
		t.Errorf("existing duplicate reported: %v", res.DuplicateTags)
	}
}

type sub struct {
	Net  string `json:"net"`
	Hub  string `json:"hub"`
	Poll string `json:"poll,omitempty"`
	Key  string `json:"key,omitempty"`
}

type subsDoc struct {
	Subs []sub `json:"subs,omitempty"`
}

var subKeys = map[string][]string{"subs": {"net", "hub"}}

func mustMergeKeys(t *testing.T, base, ours, theirs subsDoc) (subsDoc, Result) {
	t.Helper()
	out, res, err := MergeTypedKeys(subKeys, base, ours, theirs)
	if err != nil {
		t.Fatal(err)
	}
	return out, res
}

func subsByNet(subs []sub) map[string]sub {
	m := make(map[string]sub, len(subs))
	for _, s := range subs {
		m[s.Net] = s
	}
	return m
}

func TestMergeFieldKeyedList(t *testing.T) {
	base := subsDoc{Subs: []sub{{Net: "a", Hub: "h"}, {Net: "b", Hub: "h"}, {Net: "c", Hub: "h"}}}
	// Here: edit a's poll interval and b's key.
	ours := subsDoc{Subs: []sub{{Net: "a", Hub: "h", Poll: "10m"}, {Net: "b", Hub: "h", Key: "mine"}, {Net: "c", Hub: "h"}}}
	// Elsewhere: pin a's key, change b's key, remove c, add d.
	theirs := subsDoc{Subs: []sub{{Net: "a", Hub: "h", Key: "pinned"}, {Net: "b", Hub: "h", Key: "theirs"}, {Net: "d", Hub: "h"}}}
	out, res := mustMergeKeys(t, base, ours, theirs)

	got := subsByNet(out.Subs)
	if len(out.Subs) != 3 {
		t.Errorf("subs = %+v, want a, b and d", out.Subs)
	}
	if a := got["a"]; a.Poll != "10m" || a.Key != "pinned" {
		t.Errorf("a = %+v, want both edits", a)
	}
	if b := got["b"]; b.Key != "mine" {
		t.Errorf("b = %+v, want this save's key", b)
	}
	if _, ok := got["c"]; ok {
		t.Error("c was removed elsewhere and untouched here; should be gone")
	}
	if _, ok := got["d"]; !ok {
		t.Error("d added elsewhere was lost")
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Path != "subs.b h.key" {
		t.Errorf("conflicts = %+v, want one on b's key", res.Conflicts)
	}
}

func TestMergeFieldKeyedListDeletions(t *testing.T) {
	base := subsDoc{Subs: []sub{{Net: "a", Hub: "h"}}}

	// Removed here, edited elsewhere: kept, edited, and reported.
	out, res := mustMergeKeys(t, base, subsDoc{}, subsDoc{Subs: []sub{{Net: "a", Hub: "h", Key: "pinned"}}})
	if len(out.Subs) != 1 || out.Subs[0].Key != "pinned" || len(res.Conflicts) != 1 {
		t.Errorf("out = %+v, conflicts = %+v", out, res.Conflicts)
	}

	// The last item removed here (the omitempty key goes with it) while
	// another is added elsewhere: only the addition remains.
	out, res = mustMergeKeys(t, base, subsDoc{}, subsDoc{Subs: []sub{{Net: "a", Hub: "h"}, {Net: "b", Hub: "h"}}})
	if len(out.Subs) != 1 || out.Subs[0].Net != "b" || len(res.Conflicts) != 0 {
		t.Errorf("out = %+v, conflicts = %+v", out, res.Conflicts)
	}

	// Moving a subscription to another hub is a removal and an addition.
	out, _ = mustMergeKeys(t, base, subsDoc{Subs: []sub{{Net: "a", Hub: "h2"}}}, base)
	if len(out.Subs) != 1 || out.Subs[0].Hub != "h2" {
		t.Errorf("out = %+v", out)
	}
}

// Clearing an omitempty string field drops it from the JSON; that is an edit
// to "", which wins over a different edit elsewhere like any other value.
func TestMergeFieldKeyedListBlankedFieldIsAnEdit(t *testing.T) {
	base := subsDoc{Subs: []sub{{Net: "a", Hub: "h", Key: "A"}}}
	ours := subsDoc{Subs: []sub{{Net: "a", Hub: "h"}}}
	theirs := subsDoc{Subs: []sub{{Net: "a", Hub: "h", Key: "B"}}}
	out, res := mustMergeKeys(t, base, ours, theirs)
	if len(out.Subs) != 1 || out.Subs[0].Key != "" || len(res.Conflicts) != 1 {
		t.Errorf("out = %+v, conflicts = %+v; want this save's blank key, reported", out, res.Conflicts)
	}

	// Blanked elsewhere and untouched here: blank too.
	out, _ = mustMergeKeys(t, base, base, ours)
	if len(out.Subs) != 1 || out.Subs[0].Key != "" {
		t.Errorf("out = %+v, want the blank from elsewhere", out)
	}
}

// Keys whose values differ only in where a space falls are different items.
func TestMergeFieldKeyedListKeysDoNotCollide(t *testing.T) {
	theirs := subsDoc{Subs: []sub{{Net: "a b", Hub: "c"}, {Net: "a", Hub: "b c"}}}
	out, _ := mustMergeKeys(t, subsDoc{}, subsDoc{}, theirs)
	if len(out.Subs) != 2 {
		t.Errorf("out = %+v, want both items", out)
	}
	ours := subsDoc{Subs: []sub{{Net: "a b", Hub: "c", Poll: "5m"}, {Net: "a", Hub: "b c"}}}
	out, res := mustMergeKeys(t, theirs, ours, theirs)
	if len(out.Subs) != 2 || out.Subs[0].Poll != "5m" || out.Subs[1].Poll != "" || len(res.Conflicts) != 0 {
		t.Errorf("out = %+v, conflicts = %+v", out, res.Conflicts)
	}
}

func TestMergeFieldKeyedListSameItemAddedTwice(t *testing.T) {
	out, _ := mustMergeKeys(t, subsDoc{},
		subsDoc{Subs: []sub{{Net: "a", Hub: "h", Poll: "5m"}}},
		subsDoc{Subs: []sub{{Net: "a", Hub: "h", Key: "pinned"}}})
	if len(out.Subs) != 1 || out.Subs[0].Poll != "5m" || out.Subs[0].Key != "pinned" {
		t.Errorf("out = %+v, want one item with both fields", out)
	}
}

// A list with two items under one key cannot be matched item by item, so it
// merges as a single value, as it did before it had keys.
func TestMergeFieldKeyedListDuplicateKeyIsOneValue(t *testing.T) {
	base := subsDoc{Subs: []sub{{Net: "a", Hub: "h"}}}
	theirs := subsDoc{Subs: []sub{{Net: "a", Hub: "h"}, {Net: "a", Hub: "h", Poll: "1m"}}}
	out, _ := mustMergeKeys(t, base, base, theirs)
	if len(out.Subs) != 2 {
		t.Errorf("out = %+v, want theirs whole", out)
	}
}

func TestSaveFileListKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subs.json")
	f := File[subsDoc]{
		Path: path,
		Parse: func(b []byte) (subsDoc, error) {
			var v subsDoc
			return v, json.Unmarshal(b, &v)
		},
		Marshal:  func(v subsDoc) ([]byte, error) { return json.MarshalIndent(v, "", "  ") },
		ListKeys: subKeys,
	}
	base := subsDoc{Subs: []sub{{Net: "a", Hub: "h"}}}
	if err := os.WriteFile(path, []byte(`{"subs":[{"net":"a","hub":"h"},{"net":"b","hub":"h"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	saved, _, err := SaveFile(f, base, subsDoc{Subs: []sub{{Net: "a", Hub: "h", Poll: "5m"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Subs) != 2 || saved.Subs[0].Poll != "5m" || saved.Subs[1].Net != "b" {
		t.Errorf("saved = %+v", saved)
	}
}
