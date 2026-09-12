// Package menuset resolves files in a menu set through an optional overlay.
//
// A menu set is a directory tree such as menus/v3 holding ansi/, bar/, cfg/,
// mnu/, templates/ and theme.json. The shipped set is tracked in git, so a
// sysop who edits it in place fights every git pull. The overlay is a second
// tree with the same layout — menus.d/v3 for menus/v3 — that is searched
// first, file by file. A file present in the overlay shadows the shipped one;
// everything else falls through to the shipped set. The overlay is not
// tracked, so upgrades never touch it and it never has to be re-copied.
//
// Resolution is per file, not per directory. Overriding MAIN.ANS does not
// require copying the rest of ansi/. There are no whiteouts: a file cannot be
// removed from the shipped set by way of the overlay.
package menuset

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// OverlaySuffix is appended to the menu root directory name to find the
// overlay root: menus/v3 pairs with menus.d/v3.
const OverlaySuffix = ".d"

// Layer names which tree a file was resolved from.
type Layer int

const (
	// LayerBase is the shipped menu set.
	LayerBase Layer = iota
	// LayerOverlay is the sysop's override tree.
	LayerOverlay
)

func (l Layer) String() string {
	if l == LayerOverlay {
		return "overlay"
	}
	return "base"
}

// Set is a menu set with an optional overlay.
type Set struct {
	// Base is the shipped menu set directory, e.g. menus/v3.
	Base string
	// Overlay is the override directory, e.g. menus.d/v3. Empty disables
	// the overlay entirely.
	Overlay string
}

// FromPath returns the Set for a shipped menu set directory, pairing it with
// the overlay derived from its location: <parent>.d/<name>. The pairing is
// defined here and nowhere else so that every reader of a menu set — the
// server, the editors, the scripting engine — agrees on where the overlay
// lives, for relative and absolute paths alike.
func FromPath(base string) Set {
	return Set{Base: base, Overlay: OverlayFor(base)}
}

// Bare returns a Set that reads and writes the shipped tree only.
func Bare(base string) Set {
	return Set{Base: base}
}

// OverlayFor returns the overlay directory paired with a shipped menu set
// directory: menus/v3 → menus.d/v3, /opt/bbs/menus/v3 → /opt/bbs/menus.d/v3.
func OverlayFor(base string) string {
	base = filepath.Clean(base)
	parent, name := filepath.Split(base)
	parent = filepath.Clean(parent)
	if parent == "." || parent == string(filepath.Separator) || parent == "" {
		// A set with no parent (e.g. "v3") has nowhere to hang a sibling.
		return ""
	}
	return filepath.Join(parent+OverlaySuffix, name)
}

// HasOverlay reports whether an overlay directory is configured.
func (s Set) HasOverlay() bool {
	return s.Overlay != ""
}

// Path returns the path of a file in the given layer without checking that
// it exists. elem is the path relative to the set root, e.g. "ansi",
// "MAIN.ANS".
func (s Set) Path(layer Layer, elem ...string) string {
	root := s.Base
	if layer == LayerOverlay {
		root = s.Overlay
	}
	return filepath.Join(append([]string{root}, elem...)...)
}

// Resolve returns the path to read for a file: the overlay copy if one exists,
// otherwise the shipped path — whether or not that exists, so an error from
// the subsequent open names the place the file was expected.
func (s Set) Resolve(elem ...string) string {
	path, _, _ := s.Locate(elem...)
	return path
}

// Locate is Resolve with the layer the file was found in. ok is false when
// the file exists in neither layer, in which case path is the shipped path.
func (s Set) Locate(elem ...string) (path string, layer Layer, ok bool) {
	if s.Overlay != "" {
		p := s.Path(LayerOverlay, elem...)
		if isFile(p) {
			return p, LayerOverlay, true
		}
	}
	p := s.Path(LayerBase, elem...)
	return p, LayerBase, isFile(p)
}

// ResolveFirst tries several file names in order within one subdirectory
// and returns the first that exists in either layer, overlay first. When none
// exists it returns the shipped path of the first name.
func (s Set) ResolveFirst(sub string, names ...string) string {
	for _, n := range names {
		if p, _, ok := s.Locate(sub, n); ok {
			return p
		}
	}
	if len(names) == 0 {
		return s.Path(LayerBase, sub)
	}
	return s.Path(LayerBase, sub, names[0])
}

// Exists reports whether the file exists in either layer.
func (s Set) Exists(elem ...string) bool {
	_, _, ok := s.Locate(elem...)
	return ok
}

// WritePath returns where an editor should save a file: the overlay when one
// is configured, otherwise the shipped tree. It does not create directories.
func (s Set) WritePath(elem ...string) string {
	if s.Overlay != "" {
		return s.Path(LayerOverlay, elem...)
	}
	return s.Path(LayerBase, elem...)
}

// Entry is one file in a merged directory listing.
type Entry struct {
	Name  string // base name
	Path  string // full path in the layer it resolved to
	Layer Layer
}

// ReadDir lists the regular files of a subdirectory across both layers,
// merged by name with the overlay winning, sorted by name. Directories are
// omitted. The error is that of the shipped directory when neither layer has
// the subdirectory; a subdirectory present in only one layer is not an error.
func (s Set) ReadDir(elem ...string) ([]Entry, error) {
	merged := map[string]Entry{}

	baseDir := s.Path(LayerBase, elem...)
	baseEntries, baseErr := os.ReadDir(baseDir)
	for _, de := range baseEntries {
		if de.IsDir() {
			continue
		}
		merged[de.Name()] = Entry{Name: de.Name(), Path: filepath.Join(baseDir, de.Name()), Layer: LayerBase}
	}

	overlayMissing := true
	if s.Overlay != "" {
		overlayDir := s.Path(LayerOverlay, elem...)
		overlayEntries, err := os.ReadDir(overlayDir)
		if err == nil {
			overlayMissing = false
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		for _, de := range overlayEntries {
			if de.IsDir() {
				continue
			}
			merged[de.Name()] = Entry{Name: de.Name(), Path: filepath.Join(overlayDir, de.Name()), Layer: LayerOverlay}
		}
	}

	if baseErr != nil && overlayMissing {
		return nil, baseErr
	}

	out := make([]Entry, 0, len(merged))
	for _, e := range merged {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Glob matches a shell pattern against the files of a subdirectory across
// both layers, merged by name with the overlay winning. Results are sorted by
// name. The pattern applies to the base name only.
func (s Set) Glob(sub, pattern string) ([]Entry, error) {
	if _, err := filepath.Match(pattern, ""); err != nil {
		return nil, fmt.Errorf("bad pattern %q: %w", pattern, err)
	}
	entries, err := s.ReadDir(sub)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Entry
	for _, e := range entries {
		if ok, _ := filepath.Match(pattern, e.Name); ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// Summary describes what an overlay currently contributes. It exists for the
// startup log so a sysop can see at a glance that their overrides are being
// read, and catch a mistyped subdirectory.
type Summary struct {
	// Overrides counts overlay files that shadow a shipped file.
	Overrides int
	// Additions counts overlay files with no shipped counterpart.
	Additions int
	// UnknownDirs lists top-level overlay subdirectories that the shipped set
	// does not have, e.g. "ANSI" typed for "ansi" on a case-sensitive filesystem.
	UnknownDirs []string
}

// Summarize walks the overlay and classifies each file against the shipped
// set. It returns ok=false with a zero Summary when the overlay does not exist.
func (s Set) Summarize() (sum Summary, ok bool, err error) {
	if s.Overlay == "" {
		return Summary{}, false, nil
	}
	info, statErr := os.Stat(s.Overlay)
	if statErr != nil {
		if errors.Is(statErr, fs.ErrNotExist) {
			return Summary{}, false, nil
		}
		return Summary{}, false, statErr
	}
	if !info.IsDir() {
		return Summary{}, false, fmt.Errorf("%s is not a directory", s.Overlay)
	}

	walkErr := filepath.WalkDir(s.Overlay, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, relErr := filepath.Rel(s.Overlay, path)
		if relErr != nil || rel == "." {
			return nil
		}
		if d.IsDir() {
			// Only the first path component is checked: deeper layout is the
			// shipped set's business.
			if !strings.Contains(rel, string(filepath.Separator)) {
				if !isDir(filepath.Join(s.Base, rel)) {
					sum.UnknownDirs = append(sum.UnknownDirs, rel)
				}
			}
			return nil
		}
		if isFile(filepath.Join(s.Base, rel)) {
			sum.Overrides++
		} else {
			sum.Additions++
		}
		return nil
	})
	if walkErr != nil {
		return Summary{}, true, walkErr
	}
	sort.Strings(sum.UnknownDirs)
	return sum, true, nil
}

func isFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
