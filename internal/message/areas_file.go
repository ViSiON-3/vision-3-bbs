package message

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/filelock"
)

// message_areas.json has several writers: the BBS (sponsor area editor,
// auto-created areas), the config editor, and helper (ftnsetup, areas set).
// Each rewrites the whole file, so without coordination the last writer wins
// and silently discards the others' changes.
//
// Every writer therefore takes LockAreasFile across its read-modify-write.
// Writers that hold the areas in memory for a while (the BBS, the config
// editor) also keep the list as they last read it, and before writing fold in
// whatever changed on disk since with MergeAreas.

// areasLockTimeout is how long a writer waits for another to finish. Holders
// only read, merge, and write a small JSON file.
const areasLockTimeout = filelock.DefaultTimeout

// LockAreasFile takes the cross-process lock guarding the message areas file
// at path. Release it once the write is done.
//
// The lock is a sidecar file beside it, so the directory must exist; this
// does not create it, as a missing config directory is an error to report,
// not something to paper over.
func LockAreasFile(path string) (*filelock.Lock, error) {
	return filelock.Acquire(path, areasLockTimeout)
}

// ReadAreasFile reads and parses the message areas file, returning the areas
// and the raw bytes read. A missing or empty file is no areas, not an error,
// matching how the BBS loads it.
func ReadAreasFile(path string) ([]MessageArea, []byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	areas, err := parseAreas(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return areas, data, nil
}

func parseAreas(data []byte) ([]MessageArea, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var areas []MessageArea
	if err := json.Unmarshal(data, &areas); err != nil {
		return nil, err
	}
	return areas, nil
}

// MarshalAreas renders areas the way the message areas file is written:
// sorted by Position then ID, two-space indented, trailing newline.
func MarshalAreas(areas []MessageArea) ([]byte, error) {
	sorted := append([]MessageArea(nil), areas...)
	sortAreas(sorted)
	data, err := json.MarshalIndent(sorted, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal message areas: %w", err)
	}
	return append(data, '\n'), nil
}

func sortAreas(areas []MessageArea) {
	sort.SliceStable(areas, func(i, j int) bool {
		if areas[i].Position != areas[j].Position {
			return areas[i].Position < areas[j].Position
		}
		return areas[i].ID < areas[j].ID
	})
}

// WriteAreasFile atomically replaces the message areas file with areas and
// returns the bytes written. The caller must hold LockAreasFile.
func WriteAreasFile(path string, areas []MessageArea) ([]byte, error) {
	data, err := MarshalAreas(areas)
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(path, data); err != nil {
		return nil, err
	}
	return data, nil
}

// writeFileAtomic writes data to a temp file beside path, syncs it, and
// renames it into place, so readers see the whole old file or the whole new
// one.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "message_areas_*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp areas file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write temp areas file: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("sync temp areas file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp areas file: %w", err)
	}
	// CreateTemp makes the file 0600; keep the file readable as before.
	_ = os.Chmod(tmpName, 0o644)
	if err = os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename temp areas file: %w", err)
	}
	return nil
}

// UpdateAreasFile runs a locked read-modify-write of the message areas file
// for one-shot tools: fn receives the current areas and returns the areas to
// write, or write=false to leave the file alone.
func UpdateAreasFile(path string, fn func(areas []MessageArea) (out []MessageArea, write bool, err error)) error {
	lock, err := LockAreasFile(path)
	if err != nil {
		return fmt.Errorf("locking %s: %w", path, err)
	}
	defer lock.Release()

	areas, _, err := ReadAreasFile(path)
	if err != nil {
		return err
	}
	out, write, err := fn(areas)
	if err != nil || !write {
		return err
	}
	_, err = WriteAreasFile(path, out)
	return err
}

// AreaMergeConflict reports a field both sides changed to different values.
// The merge keeps ours (the side doing the save).
type AreaMergeConflict struct {
	ID    int
	Tag   string
	Field string // JSON field name, or "" when the whole area conflicted
	What  string // human-readable description
}

// AreaMergeResult is the outcome of MergeAreas.
type AreaMergeResult struct {
	Areas     []MessageArea
	Conflicts []AreaMergeConflict
	// Renumbered maps an area ID ours added to the ID it was moved to,
	// because theirs added a different area under the same ID.
	Renumbered map[int]int
}

// MergeAreas does a three-way merge of message area lists, keyed by area ID
// and then field by field. base is the list as ours last read it from disk,
// ours is that list with our edits, and theirs is what is on disk now.
//
//   - A field changed on only one side takes that side's value, so edits to
//     different fields of one area, or to different areas, all survive.
//   - A field both sides changed differently keeps ours and is reported.
//   - An area deleted on one side and edited on the other is kept, edited
//     (a deletion is easy to redo; a lost edit is not), and reported.
//   - An area both sides added with the same tag is one area: under the same
//     ID its fields merge; under different IDs theirs is kept and ours' ID
//     maps to it in Renumbered. No merge produces a duplicate tag.
//   - Two different areas added under the same ID: theirs keeps the ID and
//     ours is given the next free one, reported in Renumbered.
//
// The result is sorted by Position then ID.
func MergeAreas(base, ours, theirs []MessageArea) (AreaMergeResult, error) {
	res := AreaMergeResult{}
	bm, err := areaFieldMaps(base)
	if err != nil {
		return res, err
	}
	om, err := areaFieldMaps(ours)
	if err != nil {
		return res, err
	}
	tm, err := areaFieldMaps(theirs)
	if err != nil {
		return res, err
	}
	byID := func(list []MessageArea) map[int]MessageArea {
		m := make(map[int]MessageArea, len(list))
		for _, a := range list {
			m[a.ID] = a
		}
		return m
	}
	oursByID, theirsByID := byID(ours), byID(theirs)

	ids := make(map[int]bool)
	maxID := 0
	for _, m := range []map[int]map[string]json.RawMessage{bm, om, tm} {
		for id := range m {
			ids[id] = true
			if id > maxID {
				maxID = id
			}
		}
	}
	sortedIDs := make([]int, 0, len(ids))
	for id := range ids {
		sortedIDs = append(sortedIDs, id)
	}
	sort.Ints(sortedIDs)

	var merged []MessageArea
	var added []MessageArea
	for _, id := range sortedIDs {
		b, inB := bm[id]
		o, inO := om[id]
		t, inT := tm[id]
		switch {
		case inB && inO && inT:
			fields, conflicts := mergeFields(b, o, t)
			a, err := fieldsToArea(fields)
			if err != nil {
				return res, err
			}
			for _, f := range conflicts {
				res.Conflicts = append(res.Conflicts, AreaMergeConflict{ID: id, Tag: a.Tag, Field: f,
					What: fmt.Sprintf("area %s (%d): %s changed in both places; kept this save's value", a.Tag, id, f)})
			}
			merged = append(merged, a)
		case inB && inO: // deleted on disk
			if !fieldsEqual(b, o) {
				res.Conflicts = append(res.Conflicts, AreaMergeConflict{ID: id, Tag: oursByID[id].Tag,
					What: fmt.Sprintf("area %s (%d) was deleted elsewhere but edited here; kept it", oursByID[id].Tag, id)})
				merged = append(merged, oursByID[id])
			}
		case inB && inT: // deleted by ours
			if !fieldsEqual(b, t) {
				res.Conflicts = append(res.Conflicts, AreaMergeConflict{ID: id, Tag: theirsByID[id].Tag,
					What: fmt.Sprintf("area %s (%d) was deleted here but edited elsewhere; kept it", theirsByID[id].Tag, id)})
				merged = append(merged, theirsByID[id])
			}
		case inB: // deleted on both sides
		case inO && inT: // added on both sides
			switch {
			case fieldsEqual(o, t):
				merged = append(merged, theirsByID[id])
			case oursByID[id].Tag == theirsByID[id].Tag:
				// The same area, added by both: merge its fields as if
				// from empty.
				fields, conflicts := mergeFields(map[string]json.RawMessage{}, o, t)
				a, err := fieldsToArea(fields)
				if err != nil {
					return res, err
				}
				for _, f := range conflicts {
					res.Conflicts = append(res.Conflicts, AreaMergeConflict{ID: id, Tag: a.Tag, Field: f,
						What: fmt.Sprintf("area %s (%d) was added here and elsewhere: %s differs; kept this save's value", a.Tag, id, f)})
				}
				merged = append(merged, a)
			default:
				merged = append(merged, theirsByID[id])
				added = append(added, oursByID[id])
			}
		case inO:
			added = append(added, oursByID[id])
		case inT:
			merged = append(merged, theirsByID[id])
		}
	}

	// Areas ours added that theirs does not already have under their ID. An
	// area theirs added with the same tag is the same area added twice (two
	// tools importing one echo): keep theirs rather than create a duplicate
	// tag, which the BBS cannot route. One whose ID theirs took for a
	// different area moves to the next free ID.
	byTag := make(map[string]int, len(merged))
	for _, a := range merged {
		byTag[a.Tag] = a.ID
	}
	for _, a := range added {
		if otherID, dup := byTag[a.Tag]; dup {
			if otherID != a.ID {
				if res.Renumbered == nil {
					res.Renumbered = make(map[int]int)
				}
				res.Renumbered[a.ID] = otherID
			}
			res.Conflicts = append(res.Conflicts, AreaMergeConflict{ID: otherID, Tag: a.Tag,
				What: fmt.Sprintf("area %s was added here and elsewhere; kept the other copy (ID %d)", a.Tag, otherID)})
			continue
		}
		if _, taken := theirsByID[a.ID]; !taken {
			merged = append(merged, a)
			byTag[a.Tag] = a.ID
			continue
		}
		maxID++
		if res.Renumbered == nil {
			res.Renumbered = make(map[int]int)
		}
		res.Renumbered[a.ID] = maxID
		res.Conflicts = append(res.Conflicts, AreaMergeConflict{ID: maxID, Tag: a.Tag,
			What: fmt.Sprintf("area %s was added here and elsewhere under ID %d; renumbered it to %d", a.Tag, a.ID, maxID)})
		// AddArea's default base path is named after the ID, so the area
		// that kept the ID would otherwise share this one's JAM base.
		if a.BasePath == defaultAreaBasePath(a.ID) {
			a.BasePath = defaultAreaBasePath(maxID)
		}
		a.ID = maxID
		merged = append(merged, a)
		byTag[a.Tag] = a.ID
	}
	if dups := newDuplicateTags(merged, theirs); len(dups) > 0 {
		return res, &DuplicateTagError{Tags: dups}
	}
	sortAreas(merged)
	res.Areas = merged
	return res, nil
}

// DuplicateTagError is returned by MergeAreas when combining the two sides
// would give two areas one tag, e.g. an area renamed here to a tag another
// program just gave a new area. Neither side is wrong on its own, so the save
// is refused for the sysop to resolve by renaming one.
type DuplicateTagError struct {
	Tags []string
}

func (e *DuplicateTagError) Error() string {
	return fmt.Sprintf("message area tag(s) %s would be used by two areas after merging with changes made "+
		"elsewhere; rename one and save again", strings.Join(e.Tags, ", "))
}

// newDuplicateTags lists tags held by more than one area in merged where
// some area holding it did not already hold it in theirs. A duplicate already
// on disk, held by the same areas, is warned about at load and must not block
// every save; one that moves to a different area is new.
func newDuplicateTags(merged, theirs []MessageArea) []string {
	had := make(map[string]map[int]bool, len(theirs))
	for _, a := range theirs {
		if had[a.Tag] == nil {
			had[a.Tag] = make(map[int]bool)
		}
		had[a.Tag][a.ID] = true
	}
	holders := make(map[string][]int, len(merged))
	for _, a := range merged {
		holders[a.Tag] = append(holders[a.Tag], a.ID)
	}
	var dups []string
	for tag, ids := range holders {
		if len(ids) < 2 {
			continue
		}
		for _, id := range ids {
			if !had[tag][id] {
				dups = append(dups, tag)
				break
			}
		}
	}
	sort.Strings(dups)
	return dups
}

// defaultAreaBasePath is the JAM base path AddArea gives an area with no
// BasePath of its own.
func defaultAreaBasePath(id int) string {
	return fmt.Sprintf("msgbases/area_%d", id)
}

// areaFieldMaps renders each area as its JSON fields, keyed by ID. Working on
// the JSON form makes the merge cover every field, including ones added to
// MessageArea later, and treats an omitted field the same as its zero value.
func areaFieldMaps(areas []MessageArea) (map[int]map[string]json.RawMessage, error) {
	out := make(map[int]map[string]json.RawMessage, len(areas))
	for _, a := range areas {
		data, err := json.Marshal(a)
		if err != nil {
			return nil, err
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return nil, err
		}
		out[a.ID] = fields
	}
	return out, nil
}

func mergeFields(b, o, t map[string]json.RawMessage) (map[string]json.RawMessage, []string) {
	keys := make(map[string]bool)
	for _, m := range []map[string]json.RawMessage{b, o, t} {
		for k := range m {
			keys[k] = true
		}
	}
	out := make(map[string]json.RawMessage, len(keys))
	var conflicts []string
	for k := range keys {
		bv, ov, tv := b[k], o[k], t[k]
		var v json.RawMessage
		switch {
		case bytes.Equal(ov, bv):
			v = tv
		case bytes.Equal(tv, bv), bytes.Equal(ov, tv):
			v = ov
		default:
			v = ov
			conflicts = append(conflicts, k)
		}
		if v != nil {
			out[k] = v
		}
	}
	sort.Strings(conflicts)
	return out, conflicts
}

func fieldsEqual(a, b map[string]json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !bytes.Equal(v, b[k]) {
			return false
		}
	}
	return true
}

func fieldsToArea(fields map[string]json.RawMessage) (MessageArea, error) {
	var a MessageArea
	data, err := json.Marshal(fields)
	if err != nil {
		return a, err
	}
	err = json.Unmarshal(data, &a)
	return a, err
}

// CloneAreas deep-copies areas, so a kept base snapshot cannot be changed
// through a shared AllowAnon pointer.
func CloneAreas(areas []MessageArea) []MessageArea {
	if areas == nil {
		return nil
	}
	out := make([]MessageArea, len(areas))
	for i, a := range areas {
		if a.AllowAnon != nil {
			v := *a.AllowAnon
			a.AllowAnon = &v
		}
		out[i] = a
	}
	return out
}
