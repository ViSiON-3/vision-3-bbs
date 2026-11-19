package menu

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// vision3ThemeDir is the ViSiON/3 theme, menus/vision3, or "" in a tree
// without it.
func vision3ThemeDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "menus", "vision3"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return ""
	} else if err != nil {
		t.Fatal(err)
	}
	return dir
}

// vision3ThemeCopy copies the ViSiON/3 theme to menus/vision3 of a fresh BBS
// root with no other theme and no menus.d, as a sysop installs it, and
// returns the copy. The files named in without are left out.
func vision3ThemeCopy(t *testing.T, without ...string) string {
	t.Helper()
	src := vision3ThemeDir(t)
	if src == "" {
		t.Fatal("no menus/vision3 in this tree")
	}
	for _, name := range without {
		if _, err := os.Stat(filepath.Join(src, filepath.FromSlash(name))); err != nil {
			t.Fatalf("%s is not in the ViSiON/3 theme to leave out: %v", name, err)
		}
	}
	dst := menuset.ThemeDir(t.TempDir(), "vision3")
	copyThemeTree(t, src, dst, without...)
	return dst
}

// vision3Submenus is every menu a caller can reach from the ViSiON/3 main
// menu by GOTO, directly or through another menu, Main excluded, sorted.
func vision3Submenus(t *testing.T, dir string) []string {
	t.Helper()
	set := menuset.FromPath(dir)
	seen := map[string]bool{"MAIN": true}
	todo := []string{"MAIN"}
	for len(todo) > 0 {
		name := todo[0]
		todo = todo[1:]
		if ok, err := set.Exists("cfg", name+".CFG"); err != nil || !ok {
			continue
		}
		cmds, err := LoadCommands(name, set)
		if err != nil {
			t.Fatalf("%s.CFG: %v", name, err)
		}
		for _, c := range cmds {
			for _, part := range strings.Split(c.Command, ";") {
				target, ok := strings.CutPrefix(strings.TrimSpace(part), "GOTO:")
				if !ok {
					continue
				}
				target = strings.ToUpper(target)
				if !seen[target] {
					seen[target] = true
					todo = append(todo, target)
				}
			}
		}
	}
	var out []string
	for name := range seen {
		if name != "MAIN" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// TestVision3ThemeHasEveryStockFile checks the ViSiON/3 theme holds a regular
// file for every file of the shipped v3 theme, so it stands alone, except
// the connection check and the login matrix, which belong to the board
// (TestVision3ThemeHasNoMatrix).
func TestVision3ThemeHasEveryStockFile(t *testing.T) {
	theme := vision3ThemeDir(t)
	if theme == "" {
		t.Fatal("no menus/vision3 in this tree")
	}
	stock := filepath.Join("..", "..", "menus", "v3")
	err := filepath.WalkDir(stock, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(stock, p)
		if err != nil {
			return err
		}
		if menuset.IsPreloginFile(filepath.Dir(rel), filepath.Base(rel)) {
			return nil
		}
		if fi, err := os.Lstat(filepath.Join(theme, rel)); err != nil || !fi.Mode().IsRegular() {
			t.Errorf("menus/vision3 has no regular file %s (err %v)", filepath.ToSlash(rel), err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestVision3ThemeHasNoMatrix checks the ViSiON/3 theme holds none of the
// connection check and login matrix files: the board reads them from its
// prelogin/ directory, else stock's, and never from a theme.
func TestVision3ThemeHasNoMatrix(t *testing.T) {
	theme := vision3ThemeDir(t)
	if theme == "" {
		t.Fatal("no menus/vision3 in this tree")
	}
	for _, sub := range []string{"ansi", "bar", "cfg", "mnu"} {
		entries, err := os.ReadDir(filepath.Join(theme, sub))
		if err != nil {
			t.Fatal(err)
		}
		for _, de := range entries {
			if menuset.IsPreloginFile(sub, de.Name()) {
				t.Errorf("menus/vision3/%s/%s is a pre-login file; it belongs in the board's prelogin/", sub, de.Name())
			}
		}
	}
}

// TestVision3ThemeStandsAlone loads the ViSiON/3 theme from a BBS root with
// no other theme and no menus.d: a caller can choose it, its theme.json
// loads, every menu and command list loads, every GOTO lands on a menu the
// theme has, Main runs mainmenu.js and every menu reachable from Main runs
// submenu.js, both from the theme's scripts/.
func TestVision3ThemeStandsAlone(t *testing.T) {
	if vision3ThemeDir(t) == "" {
		t.Fatal("no menus/vision3 in this tree")
	}
	dir := vision3ThemeCopy(t)
	if err := checkCallerTheme(dir); err != nil {
		t.Fatalf("not a usable theme: %v", err)
	}
	if _, err := config.LoadThemeConfig(dir); err != nil {
		t.Fatalf("theme.json: %v", err)
	}
	set := menuset.FromPath(dir)
	recs := themeMenuRecords(t, dir)
	for name, rec := range recs {
		if words := strings.Fields(rec.Script); len(words) > 0 {
			if !isThemeScript(words[0]) {
				t.Errorf("%s.MNU SCRIPT %q names a door, not a theme script", name, rec.Script)
			} else if ok, err := set.Exists("scripts", words[0]); err != nil || !ok {
				t.Errorf("%s.MNU SCRIPT %q: no scripts/%s in the theme (err %v)", name, rec.Script, words[0], err)
			}
		}
		if ok, err := set.Exists("cfg", name+".CFG"); err != nil || !ok {
			continue
		}
		cmds, err := LoadCommands(name, set)
		if err != nil {
			t.Errorf("%s.CFG: %v", name, err)
			continue
		}
		for _, c := range cmds {
			for _, part := range strings.Split(c.Command, ";") {
				if target, ok := strings.CutPrefix(strings.TrimSpace(part), "GOTO:"); ok {
					if _, has := recs[strings.ToUpper(target)]; !has {
						t.Errorf("%s.CFG %s goes to %s, which the theme has no menu for", name, c.Keys, target)
					}
				}
			}
		}
	}
	if rec := recs["MAIN"]; rec == nil || rec.Script != "mainmenu.js" {
		t.Errorf("MAIN.MNU = %+v, want SCRIPT mainmenu.js", rec)
	}
	subs := vision3Submenus(t, dir)
	if len(subs) == 0 {
		t.Fatal("no menu is reachable from Main")
	}
	for _, name := range subs {
		rec := recs[name]
		if rec == nil {
			continue // reported above
		}
		if words := strings.Fields(rec.Script); len(words) == 0 || words[0] != "submenu.js" {
			t.Errorf("%s is reachable from Main but SCRIPT is %q, want submenu.js", name, rec.Script)
		}
	}
}

// TestVision3MenusLabelEveryCommand checks every command a caller can see on
// a ViSiON/3 submenu has a LABEL, since a scripted menu lists only labelled
// commands once any has one. Main is left out: its unlabelled commands are
// shortcuts it keeps off the screen on purpose.
func TestVision3MenusLabelEveryCommand(t *testing.T) {
	theme := vision3ThemeDir(t)
	if theme == "" {
		t.Fatal("no menus/vision3 in this tree")
	}
	set := menuset.FromPath(theme)
	for name, rec := range themeMenuRecords(t, theme) {
		if words := strings.Fields(rec.Script); len(words) == 0 || words[0] != "submenu.js" {
			continue
		}
		if ok, err := set.Exists("cfg", name+".CFG"); err != nil || !ok {
			continue
		}
		cmds, err := LoadCommands(name, set)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, c := range cmds {
			keys := strings.Fields(c.Keys)
			if len(keys) == 0 || keys[0] == "//" || keys[0] == "~~" || c.Hidden {
				continue
			}
			if strings.TrimSpace(c.Label) == "" {
				t.Errorf("%s: %s (%s) has no LABEL", name, c.Keys, c.Command)
			}
		}
	}
}

// themeMenuRecords is every .MNU of the theme at dir, by menu name.
func themeMenuRecords(t *testing.T, dir string) map[string]*MenuRecord {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "mnu", "*.MNU"))
	if err != nil {
		t.Fatal(err)
	}
	recs := map[string]*MenuRecord{}
	for _, p := range paths {
		name := strings.TrimSuffix(filepath.Base(p), ".MNU")
		rec, err := LoadMenu(name, menuset.FromPath(dir))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		recs[name] = rec
	}
	return recs
}

// copyThemeTree copies the directory tree src to dst, leaving out the
// files named in without by their slash-separated path under src. Every
// other entry must be a directory or a regular file: a symlink, which could
// lead out of the theme, fails the test.
func copyThemeTree(t *testing.T, src, dst string, without ...string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if !d.Type().IsRegular() {
			t.Errorf("%s is not a regular file (mode %v)", p, d.Type())
			return nil
		}
		if slices.Contains(without, filepath.ToSlash(rel)) {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}
