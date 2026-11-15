package menuset

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// DefaultTheme is the theme a board runs when config.json names none.
const DefaultTheme = "v3"

// ThemesDir is the directory under the BBS root that holds one menu set
// per theme.
const ThemesDir = "menus"

// ValidThemeName reports why name can't be a theme, or nil. A theme is one
// directory under menus/, so its name is a single path element: letters,
// digits, '.', '_' and '-', not starting with a dot.
func ValidThemeName(name string) error {
	if name == "" {
		return fmt.Errorf("theme name is empty")
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("theme name %q starts with a dot", name)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			return fmt.Errorf("theme name %q may hold only letters, digits, '.', '_' and '-'", name)
		}
	}
	return nil
}

// ThemeDir returns the menu set directory of theme name under bbsRoot,
// menus/<name>, without checking either.
func ThemeDir(bbsRoot, name string) string {
	return filepath.Join(bbsRoot, ThemesDir, name)
}

// CheckTheme returns theme name's menu set directory under bbsRoot, or an
// error saying what is wrong with the selection: a name that isn't one, or
// no such directory.
func CheckTheme(bbsRoot, name string) (string, error) {
	if err := ValidThemeName(name); err != nil {
		return "", err
	}
	dir := ThemeDir(bbsRoot, name)
	info, err := os.Stat(dir)
	switch {
	case os.IsNotExist(err):
		have := ListThemes(bbsRoot)
		if len(have) == 0 {
			return "", fmt.Errorf("theme %q not found: %s does not exist", name, dir)
		}
		return "", fmt.Errorf("theme %q not found: %s does not exist (themes here: %s)", name, dir, strings.Join(have, ", "))
	case err != nil:
		return "", fmt.Errorf("theme %q: %w", name, err)
	case !info.IsDir():
		return "", fmt.Errorf("theme %q: %s is not a directory", name, dir)
	}
	return dir, nil
}

// ListThemes returns the names of the themes under bbsRoot: the directories
// in menus/ whose names are valid, sorted. It is empty when there are none.
func ListThemes(bbsRoot string) []string {
	entries, err := os.ReadDir(filepath.Join(bbsRoot, ThemesDir))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if ValidThemeName(e.Name()) != nil {
			continue
		}
		// Stat, not e.IsDir: a theme may be a symlink to a directory.
		if info, err := os.Stat(filepath.Join(bbsRoot, ThemesDir, e.Name())); err == nil && info.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

var (
	runningMu sync.RWMutex
	running   string
)

// SetRunning records the menu set directory this process runs. The server
// sets it once at startup, and the parts of the BBS that find the menu set
// on their own (the message editor, the scripting engine) use it, so they
// stay on the theme the server started with when config.json changes.
func SetRunning(dir string) {
	runningMu.Lock()
	running = dir
	runningMu.Unlock()
}

// Running returns the directory given to SetRunning, or "" in a process
// that never set one (the editors and tools).
func Running() string {
	runningMu.RLock()
	defer runningMu.RUnlock()
	return running
}
