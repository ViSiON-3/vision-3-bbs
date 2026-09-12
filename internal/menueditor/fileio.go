package menueditor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// validMenuName matches a safe 1-8 character uppercase alphanumeric + underscore name.
var validMenuName = regexp.MustCompile(`^[A-Z0-9_]{1,8}$`)

// normalizeMenuName uppercases and validates a menu name to prevent path traversal.
func normalizeMenuName(name string) (string, error) {
	n := strings.ToUpper(strings.TrimSpace(name))
	if !validMenuName.MatchString(n) {
		return "", fmt.Errorf("invalid menu name %q: must be 1-8 chars [A-Z0-9_]", name)
	}
	return n, nil
}

// MenuData represents the JSON stored in a .MNU file.
type MenuData struct {
	Title          string `json:"TITLE"`
	CLR            bool   `json:"CLR"`
	UsePrompt      bool   `json:"USEPROMPT"`
	Prompt1        string `json:"PROMPT1"`
	Prompt2        string `json:"PROMPT2"`
	Fallback       string `json:"FALLBACK"`
	ACS            string `json:"ACS"`
	Password       string `json:"PASS"`
	HelpMenu       string `json:"HELPMENU"`
	ForceHelpLevel int    `json:"FORCEHELPLEVEL"`
	MesConf        int    `json:"MES_CONF"`
	FileConf       int    `json:"FILE_CONF"`
	ForceHotKey    bool   `json:"FORCEHOTKEY"`
}

// CmdData represents a single entry in a .CFG JSON array.
type CmdData struct {
	Keys         string `json:"KEYS"`
	Command      string `json:"CMD"`
	ACS          string `json:"ACS"`
	Hidden       bool   `json:"HIDDEN"`
	AutoRun      string `json:"AUTORUN,omitempty"`
	NodeActivity string `json:"NODE_ACTIVITY,omitempty"`
}

// menuEntry holds a loaded menu with its on-disk name.
type menuEntry struct {
	Name string // basename without extension, e.g. "MAIN"
	Data MenuData
	// MnuOverlay and CfgOverlay report whether each file currently resolves
	// from the menu set's overlay (menus.d) rather than the shipped tree.
	MnuOverlay bool
	CfgOverlay bool
}

// ShippedMenuError is returned by DeleteMenu when the menu remains in the
// shipped tree, which the overlay cannot remove. Reverted reports whether an
// overlay copy was removed in the process, i.e. the menu is back to shipped.
type ShippedMenuError struct {
	Name     string
	Shipped  string // path of the shipped .MNU that remains
	Reverted bool
}

func (e *ShippedMenuError) Error() string {
	if e.Reverted {
		return fmt.Sprintf("%s reverted to the shipped copy; %s cannot be removed through the overlay", e.Name, e.Shipped)
	}
	return fmt.Sprintf("%s is part of the shipped menu set (%s) and cannot be removed through the overlay; run menuedit --no-overlay to edit the shipped set", e.Name, e.Shipped)
}

// LoadMenus reads all .MNU files from the set's mnu/ directory — the overlay
// merged over the shipped tree — and returns them sorted alphabetically by
// name.
func LoadMenus(set menuset.Set) ([]menuEntry, error) {
	entries, err := set.ReadDir("mnu")
	if err != nil {
		return nil, fmt.Errorf("reading menu dir %s: %w", set.Path(menuset.LayerBase, "mnu"), err)
	}

	var menus []menuEntry
	for _, e := range entries {
		name := e.Name
		if !strings.HasSuffix(strings.ToUpper(name), ".MNU") {
			continue
		}
		stem := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))

		data, err := loadMenuFile(e.Path)
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", name, err)
		}
		_, cfgLayer, _ := set.Locate("cfg", stem+".CFG")
		menus = append(menus, menuEntry{
			Name:       stem,
			Data:       data,
			MnuOverlay: e.Layer == menuset.LayerOverlay,
			CfgOverlay: cfgLayer == menuset.LayerOverlay,
		})
	}

	sort.Slice(menus, func(i, j int) bool {
		return menus[i].Name < menus[j].Name
	})

	return menus, nil
}

func loadMenuFile(path string) (MenuData, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return MenuData{}, err
	}
	var d MenuData
	if err := json.Unmarshal(raw, &d); err != nil {
		return MenuData{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return d, nil
}

// LoadCommands reads the .CFG file for the given menu name from the set's
// cfg/ directory, overlay first. Returns an empty slice if no .CFG exists.
func LoadCommands(set menuset.Set, name string) ([]CmdData, error) {
	n, err := normalizeMenuName(name)
	if err != nil {
		return nil, err
	}
	path := set.Resolve("cfg", n+".CFG")
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []CmdData{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(raw) == 0 {
		return []CmdData{}, nil
	}
	var cmds []CmdData
	if err := json.Unmarshal(raw, &cmds); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return cmds, nil
}

// SaveMenu writes a MenuData record atomically to mnu/{name}.MNU in the
// set's write layer: the overlay when it has one, else the shipped tree.
func SaveMenu(set menuset.Set, name string, data MenuData) error {
	n, err := normalizeMenuName(name)
	if err != nil {
		return err
	}
	return atomicWriteJSON(set.WritePath("mnu", n+".MNU"), data)
}

// SaveCommands writes a command slice atomically to cfg/{name}.CFG in the
// set's write layer.
func SaveCommands(set menuset.Set, name string, cmds []CmdData) error {
	n, err := normalizeMenuName(name)
	if err != nil {
		return err
	}
	return atomicWriteJSON(set.WritePath("cfg", n+".CFG"), cmds)
}

// DeleteMenu removes the .MNU and .CFG files for the given menu name from the
// set's write layer. With an overlay, only overlay copies are removed: a menu
// that also exists in the shipped tree is left there and a *ShippedMenuError
// reports it, since there is no way to hide a shipped file from the overlay.
func DeleteMenu(set menuset.Set, name string) error {
	n, err := normalizeMenuName(name)
	if err != nil {
		return err
	}
	mnuPath := set.WritePath("mnu", n+".MNU")
	cfgPath := set.WritePath("cfg", n+".CFG")

	removed, err := removeMenuFiles([]string{mnuPath, cfgPath}, os.Rename, os.Remove)
	if err != nil {
		return err
	}
	if set.HasOverlay() {
		if shipped := set.Path(menuset.LayerBase, "mnu", n+".MNU"); fileExists(shipped) {
			return &ShippedMenuError{Name: n, Shipped: shipped, Reverted: removed}
		}
	}
	return nil
}

// removeMenuFiles stages the pair beside the originals before deleting either
// backup. Keep contents until cleanup succeeds so even a second cleanup failure
// can restore the complete pair. Rollback errors are reported with the cause.
func removeMenuFiles(paths []string, rename func(string, string) error, remove func(string) error) (bool, error) {
	type stagedFile struct {
		path, backup string
		data         []byte
		mode         os.FileMode
		deleted      bool
	}
	var staged []stagedFile
	rollback := func(cause error) (bool, error) {
		for i := len(staged) - 1; i >= 0; i-- {
			f := staged[i]
			var err error
			if f.deleted {
				err = atomicfile.WriteFile(f.path, f.data, f.mode)
			} else {
				err = rename(f.backup, f.path)
			}
			if err != nil {
				cause = errors.Join(cause, fmt.Errorf("restoring %s: %w", f.path, err))
			}
		}
		return false, cause
	}
	for _, path := range paths {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return rollback(fmt.Errorf("removing %s: %w", path, err))
		}
		if !info.Mode().IsRegular() {
			return rollback(fmt.Errorf("removing %s: not a regular file", path))
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return rollback(fmt.Errorf("removing %s: %w", path, err))
		}
		tmp, err := os.CreateTemp(filepath.Dir(path), ".menu-delete-*")
		if err != nil {
			return rollback(fmt.Errorf("removing %s: %w", path, err))
		}
		backup := tmp.Name()
		if err := tmp.Close(); err != nil {
			return rollback(errors.Join(err, os.Remove(backup)))
		}
		if err := rename(path, backup); err != nil {
			return rollback(errors.Join(fmt.Errorf("removing %s: %w", path, err), os.Remove(backup)))
		}
		staged = append(staged, stagedFile{path: path, backup: backup, data: data, mode: info.Mode().Perm()})
	}
	for i := range staged {
		if err := remove(staged[i].backup); err != nil {
			return rollback(fmt.Errorf("removing %s: %w", staged[i].path, err))
		}
		staged[i].deleted = true
	}
	return len(staged) > 0, nil
}

// CreateMenu writes a new empty .MNU and empty .CFG for the given name.
// If writing the .CFG fails, the .MNU is removed so no half-created state is left on disk.
func CreateMenu(set menuset.Set, name string) error {
	n, err := normalizeMenuName(name)
	if err != nil {
		return err
	}
	name = n
	d := MenuData{
		CLR:       false,
		UsePrompt: true,
		Fallback:  name,
	}
	if err := SaveMenu(set, name, d); err != nil {
		return err
	}
	if err := SaveCommands(set, name, []CmdData{}); err != nil {
		// Best-effort rollback: remove the .MNU we just wrote.
		os.Remove(set.WritePath("mnu", name+".MNU")) //nolint:errcheck
		return err
	}
	return nil
}

// MenuExists reports whether a .MNU file with the given name exists in
// either layer.
func MenuExists(set menuset.Set, name string) bool {
	n, err := normalizeMenuName(name)
	if err != nil {
		return false
	}
	return set.Exists("mnu", n+".MNU")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// atomicWriteJSON marshals v to pretty-printed JSON and writes it to path
// via a temp file + rename for atomicity, creating the directory first: an
// overlay's mnu/ and cfg/ do not exist until the first save.
func atomicWriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := atomicfile.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
