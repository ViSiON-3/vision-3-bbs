// Package configmerge saves JSON config files that more than one program
// writes, without one program's save undoing another's changes.
//
// The config editor, helper, and the BBS each rewrite whole files such as
// conferences.json or ftn.json. A program that loaded a file, held it while a
// sysop worked, and then wrote it back would silently discard anything another
// program wrote in between. SaveFile instead takes a cross-process lock, reads
// what is on disk now, and three-way merges it with the caller's version
// against the version the caller originally loaded (Merge). One-shot tools
// that read and write in one go just hold Lock across the two.
package configmerge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"github.com/ViSiON-3/vision-3-bbs/internal/filelock"
)

// Lock takes the cross-process lock guarding the config file at path, a
// sidecar file beside it (filelock.SidecarPath). Every writer of a shared
// config file holds it across its read-modify-write. The directory must
// exist.
func Lock(path string) (*filelock.Lock, error) {
	lock, err := filelock.Acquire(path, filelock.DefaultTimeout)
	if err != nil {
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	return lock, nil
}

// Conflict is a value both sides changed differently. The merge keeps ours
// (the side doing the save), except that an edit always beats a deletion.
type Conflict struct {
	// Path locates the value, e.g. "networks.fidonet.own_address" or
	// "FD_LINUX (id 3).acs_write". List items are named by their tag or name.
	Path string
	// ID is the id of the top-level list item the conflict is in, when the
	// document is a list of items with ids; Field is the path within that
	// item ("" when the conflict is about the whole item).
	ID    int
	Field string
	// What describes the conflict and how it was resolved, for a log line or
	// status message.
	What string
}

// Result is the outcome of a merge.
type Result struct {
	Conflicts []Conflict
	// Renumbered maps the id of each top-level list item that ours added to
	// the id it was moved to, because theirs added a different item under the
	// same id.
	Renumbered map[int]int
	// DuplicateTags lists tags the merged top-level list gives to more than
	// one item where theirs did not, e.g. an item renamed here to the tag of
	// an item added elsewhere. SaveFile refuses to write such a result.
	DuplicateTags []string
}

// missing marks a key absent from one side of the merge.
type val struct {
	v  any
	ok bool
}

type merger struct {
	res Result
	// keys maps the path of a list whose items have no id to the fields
	// that together identify an item (MergeTypedKeys).
	keys map[string][]string
}

// Merge does a three-way merge of decoded JSON values (as produced by
// Decode): base is the document as ours originally read it, ours is that with
// our edits, and theirs is what is on disk now.
//
//   - A value changed on only one side takes that side's value.
//   - Objects merge key by key, so edits to different keys all survive.
//   - Lists whose items are all objects with an integer "id" merge item by
//     item, matched on id. Other lists are merged as a single value, unless
//     the caller names fields that identify their items (MergeTypedKeys).
//   - A value both sides changed differently keeps ours and is reported.
//   - A key or item deleted on one side and edited on the other is kept,
//     edited (a deletion is easy to redo; a lost edit is not), and reported.
//   - An item both sides added with the same "tag" is one item: under the
//     same id its fields merge; under different ids theirs is kept and ours'
//     id maps to it in Result.Renumbered. No merge produces a duplicate tag.
//   - Two different items added to a list under the same id: theirs keeps
//     the id and ours is given the next free one (Result.Renumbered, for a
//     top-level list).
func Merge(base, ours, theirs any) (any, Result) {
	return mergeKeys(nil, base, ours, theirs)
}

func mergeKeys(keys map[string][]string, base, ours, theirs any) (any, Result) {
	m := &merger{keys: keys}
	out := m.value("", 0, "", val{base, true}, val{ours, true}, val{theirs, true})
	if merged, ok := out.v.([]any); ok {
		if before, ok := theirs.([]any); ok {
			m.res.DuplicateTags = newDuplicateTags(merged, before)
		}
	}
	return out.v, m.res
}

// newDuplicateTags lists non-empty tags held by more than one item in merged
// where some item holding it did not already hold it in theirs. A duplicate
// already on disk, held by the same items, is someone else's to fix and must
// not block every save; one that moves to a different item is new.
func newDuplicateTags(merged, theirs []any) []string {
	had := make(map[string]map[int]bool)
	for _, item := range theirs {
		if tag := itemTag(item); tag != "" {
			if had[tag] == nil {
				had[tag] = make(map[int]bool)
			}
			id, _ := itemID(item)
			had[tag][id] = true
		}
	}
	holders := make(map[string][]int)
	for _, item := range merged {
		if tag := itemTag(item); tag != "" {
			id, _ := itemID(item)
			holders[tag] = append(holders[tag], id)
		}
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

// DuplicateTagError is returned by SaveFile when the merge would give two
// items one tag. Neither side is wrong on its own, so nothing is written and
// the sysop resolves it by renaming one.
type DuplicateTagError struct {
	Path string
	Tags []string
}

func (e *DuplicateTagError) Error() string {
	return fmt.Sprintf("%s: tag(s) %s would be used twice after merging with changes made elsewhere; "+
		"rename one and save again", filepath.Base(e.Path), strings.Join(e.Tags, ", "))
}

func equal(a, b val) bool {
	return a.ok == b.ok && reflect.DeepEqual(a.v, b.v)
}

// value merges one value. path is the human-readable location, itemID and
// field locate it within a top-level list item (itemID 0 when not in one).
func (m *merger) value(path string, itemID int, field string, b, o, t val) val {
	switch {
	case equal(o, b):
		return t
	case equal(t, b), equal(o, t):
		return o
	}

	// A base that is absent or null (a nil slice or map) merges as empty:
	// both sides added to it.
	baseEmpty := !b.ok || b.v == nil

	bo, bIsObj := b.v.(map[string]any)
	oo, oIsObj := o.v.(map[string]any)
	to, tIsObj := t.v.(map[string]any)
	if baseEmpty {
		bo, bIsObj = map[string]any{}, true
	}
	if o.ok && t.ok && bIsObj && oIsObj && tIsObj {
		return val{m.object(path, itemID, field, bo, oo, to), true}
	}

	bl, bIsList := b.v.([]any)
	ol, oIsList := o.v.([]any)
	tl, tIsList := t.v.([]any)
	if baseEmpty {
		bl, bIsList = nil, true
	}
	if fields, ok := m.keys[path]; ok {
		// An absent or null list here is an empty one: the field is
		// usually omitempty, so deleting the last item removes the key.
		if !o.ok || o.v == nil {
			ol, oIsList = nil, true
		}
		if !t.ok || t.v == nil {
			tl, tIsList = nil, true
		}
		if bIsList && oIsList && tIsList {
			if out, ok := m.fieldList(path, fields, bl, ol, tl); ok {
				return val{out, true}
			}
		}
	}
	if o.ok && t.ok && bIsList && oIsList && tIsList && keyedList(bl) && keyedList(ol) && keyedList(tl) {
		return val{m.list(path, bl, ol, tl), true}
	}

	// A real conflict. An edit beats a deletion; otherwise ours wins.
	switch {
	case !o.ok:
		m.conflict(path, itemID, field, "deleted here but changed elsewhere; kept it")
		return t
	case !t.ok:
		m.conflict(path, itemID, field, "deleted elsewhere but changed here; kept it")
		return o
	default:
		m.conflict(path, itemID, field, "changed here and elsewhere; kept this save's value")
		return o
	}
}

func (m *merger) conflict(path string, itemID int, field, what string) {
	where := path
	if where == "" {
		where = "file"
	}
	m.res.Conflicts = append(m.res.Conflicts, Conflict{
		Path: path, ID: itemID, Field: field, What: where + ": " + what,
	})
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func (m *merger) object(path string, itemID int, field string, b, o, t map[string]any) map[string]any {
	keys := make(map[string]bool)
	for _, mp := range []map[string]any{b, o, t} {
		for k := range mp {
			keys[k] = true
		}
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	out := make(map[string]any, len(keys))
	for _, k := range sorted {
		bv, bok := b[k]
		ov, ook := o[k]
		tv, tok := t[k]
		r := m.value(joinPath(path, k), itemID, joinPath(field, k), val{bv, bok}, val{ov, ook}, val{tv, tok})
		if r.ok {
			out[k] = r.v
		}
	}
	return out
}

// itemID returns a list item's integer "id".
func itemID(item any) (int, bool) {
	obj, ok := item.(map[string]any)
	if !ok {
		return 0, false
	}
	switch n := obj["id"].(type) {
	case json.Number:
		id, err := strconv.Atoi(n.String())
		return id, err == nil
	case float64:
		return int(n), n == float64(int(n))
	}
	return 0, false
}

// keyedList reports whether every item is an object with a distinct integer
// id. An empty list qualifies.
func keyedList(list []any) bool {
	seen := make(map[int]bool, len(list))
	for _, item := range list {
		id, ok := itemID(item)
		if !ok || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// itemLabel names a list item for messages: its tag or name, and its id.
func itemLabel(item any, id int) string {
	if obj, ok := item.(map[string]any); ok {
		for _, k := range []string{"tag", "name"} {
			if s, ok := obj[k].(string); ok && s != "" {
				return fmt.Sprintf("%s (id %d)", s, id)
			}
		}
	}
	return fmt.Sprintf("id %d", id)
}

func (m *merger) list(path string, b, o, t []any) []any {
	index := func(list []any) map[int]any {
		mp := make(map[int]any, len(list))
		for _, item := range list {
			id, _ := itemID(item)
			mp[id] = item
		}
		return mp
	}
	bm, om, tm := index(b), index(o), index(t)

	maxID := 0
	for _, mp := range []map[int]any{bm, om, tm} {
		for id := range mp {
			if id > maxID {
				maxID = id
			}
		}
	}

	// Ours' order, then items only theirs has (or that ours deleted), in
	// theirs' order.
	var order []int
	placed := make(map[int]bool)
	for _, list := range [][]any{o, t} {
		for _, item := range list {
			id, _ := itemID(item)
			if !placed[id] {
				placed[id] = true
				order = append(order, id)
			}
		}
	}

	topLevel := path == ""
	var out []any
	var added []any // items ours added that theirs does not have under that id
	for _, id := range order {
		bv, bok := bm[id]
		ov, ook := om[id]
		tv, tok := tm[id]
		elemID := 0
		if topLevel {
			elemID = id
		}
		if !bok && ook {
			switch {
			case !tok:
				added = append(added, ov)
				continue
			case !reflect.DeepEqual(ov, tv) && (itemTag(ov) == "" || itemTag(ov) != itemTag(tv)):
				// Two different items added under one id.
				out = append(out, tv)
				added = append(added, ov)
				continue
			}
			// Otherwise the same item added by both (same tag): merge its
			// fields as if from empty, below.
		}
		elemPath := joinPath(path, itemLabel(firstPresent(ov, tv), id))
		r := m.value(elemPath, elemID, "", val{bv, bok}, val{ov, ook}, val{tv, tok})
		if r.ok {
			out = append(out, r.v)
		}
	}

	// An item ours added with the tag of an item already in the result is the
	// same thing added twice (two tools importing one area): keep the other
	// rather than create a duplicate tag. An item whose id was taken by a
	// different item moves to the next free id.
	tags := make(map[string]int)
	for _, item := range out {
		if tag := itemTag(item); tag != "" {
			tags[tag], _ = itemID(item)
		}
	}
	for _, item := range added {
		oldID, _ := itemID(item)
		where := joinPath(path, itemLabel(item, oldID))
		if tag := itemTag(item); tag != "" {
			if otherID, dup := tags[tag]; dup {
				if topLevel && otherID != oldID {
					m.renumbered(oldID, otherID)
				}
				m.res.Conflicts = append(m.res.Conflicts, Conflict{
					Path: where, ID: otherID,
					What: fmt.Sprintf("%s: added here and elsewhere; kept the other copy (id %d)", where, otherID),
				})
				continue
			}
		}
		if _, taken := tm[oldID]; !taken {
			out = append(out, item)
			tags[itemTag(item)] = oldID
			continue
		}
		maxID++
		moved := make(map[string]any, len(item.(map[string]any)))
		for k, v := range item.(map[string]any) {
			moved[k] = v
		}
		moved["id"] = json.Number(strconv.Itoa(maxID))
		out = append(out, moved)
		tags[itemTag(item)] = maxID
		if topLevel {
			m.renumbered(oldID, maxID)
		}
		m.res.Conflicts = append(m.res.Conflicts, Conflict{
			Path: where, ID: maxID,
			What: fmt.Sprintf("%s: added here and elsewhere under the same id; renumbered this one to %d", where, maxID),
		})
	}
	if out == nil {
		out = []any{}
	}
	return out
}

// fieldKey returns the key a list item is matched on, the values of fields
// (which must all be strings) encoded as a JSON array so that no two
// different sets of values share a key, and a label naming the item in
// messages.
func fieldKey(item any, fields []string) (key, label string, ok bool) {
	obj, ok := item.(map[string]any)
	if !ok {
		return "", "", false
	}
	parts := make([]string, len(fields))
	for i, f := range fields {
		if parts[i], ok = obj[f].(string); !ok {
			return "", "", false
		}
	}
	data, err := json.Marshal(parts)
	if err != nil {
		return "", "", false
	}
	return string(data), strings.Join(parts, " "), true
}

// blankStrings returns the versions of one list item with every string
// field that some version has, and another lacks, set to "" in the one that
// lacks it. An omitempty string field cleared on one side is then an edit
// to "" rather than a deletion, which an edit elsewhere would beat. Absent
// versions (the item deleted on that side) are left alone.
func blankStrings(vs ...val) []val {
	strs := make(map[string]bool)
	for _, v := range vs {
		if obj, ok := v.v.(map[string]any); ok && v.ok {
			for k, x := range obj {
				if _, isStr := x.(string); isStr {
					strs[k] = true
				}
			}
		}
	}
	out := make([]val, len(vs))
	for i, v := range vs {
		out[i] = v
		obj, ok := v.v.(map[string]any)
		if !ok || !v.ok {
			continue
		}
		var filled map[string]any
		for k := range strs {
			if _, has := obj[k]; has {
				continue
			}
			if filled == nil {
				filled = make(map[string]any, len(obj)+len(strs))
				for kk, x := range obj {
					filled[kk] = x
				}
			}
			filled[k] = ""
		}
		if filled != nil {
			out[i] = val{filled, true}
		}
	}
	return out
}

// fieldList merges a list whose items are matched on the values of fields
// rather than an id. Items merge like the keys of an object: one added on
// one side is kept, one deleted on one side and untouched on the other is
// gone, and one both sides added merges field by field. Within an item a
// string field one side cleared counts as set to "" (blankStrings). It
// reports false, and the list is merged as a single value, when an item
// lacks a key or two items on one side share one.
func (m *merger) fieldList(path string, fields []string, b, o, t []any) ([]any, bool) {
	labels := make(map[string]string)
	index := func(list []any) (map[string]any, []string, bool) {
		mp := make(map[string]any, len(list))
		order := make([]string, 0, len(list))
		for _, item := range list {
			k, label, ok := fieldKey(item, fields)
			if !ok {
				return nil, nil, false
			}
			if _, dup := mp[k]; dup {
				return nil, nil, false
			}
			mp[k] = item
			labels[k] = label
			order = append(order, k)
		}
		return mp, order, true
	}
	bm, _, ok1 := index(b)
	om, oOrder, ok2 := index(o)
	tm, tOrder, ok3 := index(t)
	if !ok1 || !ok2 || !ok3 {
		return nil, false
	}

	// Ours' order, then items only theirs has, in theirs' order.
	out := []any{}
	placed := make(map[string]bool)
	for _, k := range append(oOrder, tOrder...) {
		if placed[k] {
			continue
		}
		placed[k] = true
		bv, bok := bm[k]
		ov, ook := om[k]
		tv, tok := tm[k]
		vs := []val{{bv, bok}, {ov, ook}, {tv, tok}}
		if bok {
			// An item both sides added has nothing to clear: each side's
			// fields are additions.
			vs = blankStrings(vs...)
		}
		r := m.value(joinPath(path, labels[k]), 0, "", vs[0], vs[1], vs[2])
		if r.ok {
			out = append(out, r.v)
		}
	}
	return out, true
}

func (m *merger) renumbered(from, to int) {
	if m.res.Renumbered == nil {
		m.res.Renumbered = make(map[int]int)
	}
	m.res.Renumbered[from] = to
}

// itemTag returns a list item's "tag", or "".
func itemTag(item any) string {
	if obj, ok := item.(map[string]any); ok {
		if s, ok := obj["tag"].(string); ok {
			return s
		}
	}
	return ""
}

func firstPresent(a, b any) any {
	if a != nil {
		return a
	}
	return b
}

// Decode parses JSON into the generic form Merge works on, keeping numbers
// exact.
func Decode(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func toGeneric(v any) (any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return Decode(data)
}

// MergeTyped merges three versions of a config value through their JSON
// form (see Merge), so every field is covered, including ones added to the
// type later, and an omitted field reads the same as its zero value.
func MergeTyped[T any](base, ours, theirs T) (T, Result, error) {
	return MergeTypedKeys(nil, base, ours, theirs)
}

// MergeTypedKeys is MergeTyped for a value holding lists whose items have no
// id. keys maps the path of each such list, as Conflict.Path names it (e.g.
// "leaves"), to the item fields that together identify an item; those fields
// must hold strings. Such a list merges item by item instead of as a single
// value, and a string field cleared in an item reads as set to "", so
// blanking an omitempty field is an edit, not a deletion.
func MergeTypedKeys[T any](keys map[string][]string, base, ours, theirs T) (T, Result, error) {
	var zero T
	g := make([]any, 3)
	for i, v := range []T{base, ours, theirs} {
		var err error
		if g[i], err = toGeneric(v); err != nil {
			return zero, Result{}, err
		}
	}
	merged, res := mergeKeys(keys, g[0], g[1], g[2])
	data, err := json.Marshal(merged)
	if err != nil {
		return zero, Result{}, err
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return zero, Result{}, err
	}
	return out, res, nil
}

// Clone deep-copies v through its JSON form. Use it to keep the base snapshot
// a later SaveFile merges against, so edits to the working copy cannot reach
// it through shared maps, slices, or pointers.
func Clone[T any](v T) (T, error) {
	var out T
	data, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(data, &out)
	return out, err
}

// File describes how to read and write one config file for SaveFile.
type File[T any] struct {
	Path string
	// Parse reads the file's contents. It must normalise the same way the
	// caller's load did (defaults, migrations), or the merge would read those
	// differences as edits.
	Parse func([]byte) (T, error)
	// Marshal renders the file's contents.
	Marshal func(T) ([]byte, error)
	// Prepare, if set, adjusts ours and theirs before they are merged
	// against base and returns them, for an edit the generic merge would
	// misread: a list item whose identifying field changed reads as one
	// item removed and another added, and an edit elsewhere to the removed
	// one would keep it beside the new one. It must not modify the values
	// it is passed in place; theirs as read is still what SaveFile compares
	// the result with.
	Prepare func(base, ours, theirs T) (T, T)
	// Fix, if set, adjusts a merged result before it is written, for
	// fields derived from an item's id that the generic merge cannot know
	// about (a generated directory named after the id, say).
	Fix func(merged T, res Result) T
	// ListKeys names the fields that identify the items of lists that have
	// no id, so they merge item by item (MergeTypedKeys). A list without an
	// entry here is kept whole from one side.
	ListKeys map[string][]string
	// Perm is the mode for a newly created file (0644 if zero). An existing
	// file keeps its own mode; see FileMode.
	Perm os.FileMode
}

// FileMode returns the permission bits of the file at path, or def if it
// does not exist. An atomic save replaces the file, so a writer must carry
// the old mode over explicitly: a sysop who made ftn.json 0600 to keep its
// passwords private must not find it 0644 after the next save.
func FileMode(path string, def os.FileMode) os.FileMode {
	if fi, err := os.Stat(path); err == nil {
		return fi.Mode().Perm()
	}
	return def
}

// SaveFile writes ours to the file, keeping changes another program made
// since the caller read base: under Lock, it merges the file's current
// contents with ours against base (MergeTyped) and writes the result. When
// the result is what the file already holds it is not rewritten, so a running
// BBS is not made to reload it. A missing file is written from ours.
//
// It returns the value now on disk, which the caller should adopt as both its
// working copy and its new base.
func SaveFile[T any](f File[T], base, ours T) (T, Result, error) {
	var zero T
	lock, err := Lock(f.Path)
	if err != nil {
		return zero, Result{}, err
	}
	defer lock.Release()

	data, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return ours, Result{}, write(f, ours)
	}
	if err != nil {
		return zero, Result{}, err
	}
	theirs, err := f.Parse(data)
	if err != nil {
		return zero, Result{}, fmt.Errorf("parsing %s: %w", f.Path, err)
	}
	mergeOurs, mergeTheirs := ours, theirs
	if f.Prepare != nil {
		mergeOurs, mergeTheirs = f.Prepare(base, ours, theirs)
	}
	merged, res, err := MergeTypedKeys(f.ListKeys, base, mergeOurs, mergeTheirs)
	if err != nil {
		return zero, Result{}, fmt.Errorf("merging %s: %w", f.Path, err)
	}
	if len(res.DuplicateTags) > 0 {
		return zero, res, &DuplicateTagError{Path: f.Path, Tags: res.DuplicateTags}
	}
	if f.Fix != nil {
		merged = f.Fix(merged, res)
	}

	mg, err1 := toGeneric(merged)
	tg, err2 := toGeneric(theirs)
	if err1 == nil && err2 == nil && reflect.DeepEqual(mg, tg) {
		return merged, res, nil
	}
	return merged, res, write(f, merged)
}

func write[T any](f File[T], v T) error {
	data, err := f.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", f.Path, err)
	}
	perm := f.Perm
	if perm == 0 {
		perm = 0o644
	}
	if err := atomicfile.WriteFile(f.Path, data, FileMode(f.Path, perm)); err != nil {
		return fmt.Errorf("writing %s: %w", f.Path, err)
	}
	return nil
}
