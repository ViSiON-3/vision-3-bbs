package menueditor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// newOverlaySet builds menus/v3 with shipped menus MAIN and MSG and returns
// the Set whose overlay is menus.d/v3 (not yet created).
func newOverlaySet(t *testing.T) menuset.Set {
	t.Helper()
	root := t.TempDir()
	base := filepath.Join(root, "menus", "v3")
	bare := menuset.Bare(base)
	for _, d := range []string{"mnu", "cfg"} {
		if err := os.MkdirAll(filepath.Join(base, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"MAIN", "MSG"} {
		if err := CreateMenu(bare, name); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	return menuset.FromPath(base)
}

func TestOverlaySavesLandInOverlay(t *testing.T) {
	set := newOverlaySet(t)

	if err := SaveMenu(set, "MAIN", MenuData{Title: "Mine"}); err != nil {
		t.Fatalf("SaveMenu: %v", err)
	}
	if err := SaveCommands(set, "MAIN", []CmdData{{Keys: "Q", Command: "LOGOFF"}}); err != nil {
		t.Fatalf("SaveCommands: %v", err)
	}

	// The overlay has the new copies; the shipped ones are untouched.
	for _, p := range []string{filepath.Join(set.Overlay, "mnu", "MAIN.MNU"), filepath.Join(set.Overlay, "cfg", "MAIN.CFG")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
	shipped, err := loadMenuFile(filepath.Join(set.Base, "mnu", "MAIN.MNU"))
	if err != nil || shipped.Title != "" {
		t.Errorf("shipped MAIN.MNU changed: %+v err=%v", shipped, err)
	}

	// Reads go through the overlay and the listing marks it.
	menus, err := LoadMenus(set)
	if err != nil {
		t.Fatal(err)
	}
	if len(menus) != 2 {
		t.Fatalf("got %d menus, want 2 (merged, not duplicated)", len(menus))
	}
	if menus[0].Name != "MAIN" || menus[0].Data.Title != "Mine" || !menus[0].MnuOverlay || !menus[0].CfgOverlay {
		t.Errorf("MAIN = %+v", menus[0])
	}
	if menus[1].Name != "MSG" || menus[1].MnuOverlay || menus[1].CfgOverlay {
		t.Errorf("MSG = %+v", menus[1])
	}
	cmds, err := LoadCommands(set, "MAIN")
	if err != nil || len(cmds) != 1 || cmds[0].Keys != "Q" {
		t.Errorf("LoadCommands = %+v err=%v", cmds, err)
	}

	// A brand-new menu goes to the overlay too and shows up in the list.
	if err := CreateMenu(set, "NEW"); err != nil {
		t.Fatal(err)
	}
	if !menuExistsOK(t, set, "NEW") || menuExistsOK(t, menuset.Bare(set.Base), "NEW") {
		t.Error("NEW should exist in the overlay only")
	}
}

func TestOverlayDeleteCannotRemoveShipped(t *testing.T) {
	set := newOverlaySet(t)

	// Shipped only: refused, nothing changes.
	err := DeleteMenu(set, "MAIN")
	var shipped *ShippedMenuError
	if !errors.As(err, &shipped) || shipped.Reverted {
		t.Fatalf("DeleteMenu(shipped) = %v, want ShippedMenuError{Reverted:false}", err)
	}
	if !menuExistsOK(t, set, "MAIN") {
		t.Error("shipped MAIN was removed")
	}

	// Overridden: the overlay copy goes, the shipped one remains, reported as reverted.
	if err := SaveMenu(set, "MAIN", MenuData{Title: "Mine"}); err != nil {
		t.Fatal(err)
	}
	err = DeleteMenu(set, "MAIN")
	if !errors.As(err, &shipped) || !shipped.Reverted {
		t.Fatalf("DeleteMenu(overridden) = %v, want ShippedMenuError{Reverted:true}", err)
	}
	if _, statErr := os.Stat(filepath.Join(set.Overlay, "mnu", "MAIN.MNU")); !os.IsNotExist(statErr) {
		t.Error("overlay MAIN.MNU still present")
	}
	menus, _ := LoadMenus(set)
	if len(menus) != 2 || menus[0].Data.Title != "" {
		t.Errorf("after revert menus = %+v", menus)
	}

	// Overlay only: a plain delete.
	if err := CreateMenu(set, "NEW"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteMenu(set, "NEW"); err != nil {
		t.Errorf("DeleteMenu(overlay-only) = %v", err)
	}
	if menuExistsOK(t, set, "NEW") {
		t.Error("NEW still exists")
	}
}

func TestOverlayModelRevertsOnDeleteOfOverriddenMenu(t *testing.T) {
	set := newOverlaySet(t)
	if err := SaveMenu(set, "MAIN", MenuData{Title: "Mine"}); err != nil {
		t.Fatal(err)
	}
	m, err := New(set)
	if err != nil {
		t.Fatal(err)
	}
	if m.message == "" {
		t.Error("expected a startup notice naming the overlay")
	}
	m.menuCursor = 0
	m.pendingDeleteMenuIdx = 0
	m.mode = modeDeleteMenuConfirm
	m = asModel(t, mustUpdate(m.updateConfirm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})))
	if m.mode != modeMenuList {
		t.Errorf("mode = %v, want menu list", m.mode)
	}
	if len(m.menus) != 2 || m.menus[0].Name != "MAIN" || m.menus[0].Data.Title != "" {
		t.Errorf("menus after revert = %+v", m.menus)
	}
}

func TestDeleteMenuRollsBackPair(t *testing.T) {
	for _, failure := range []string{"stage", "cleanup"} {
		t.Run(failure, func(t *testing.T) {
			set := newOverlaySet(t)
			if err := CreateMenu(set, "MAIN"); err != nil {
				t.Fatal(err)
			}
			paths := []string{set.WritePath("mnu", "MAIN.MNU"), set.WritePath("cfg", "MAIN.CFG")}
			before := make([][]byte, len(paths))
			for i, path := range paths {
				var err error
				before[i], err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
			}
			injected := errors.New("second file failure")
			rename := func(old, new string) error {
				if failure == "stage" && old == paths[1] {
					return injected
				}
				return os.Rename(old, new)
			}
			removes := 0
			remove := func(path string) error {
				removes++
				if failure == "cleanup" && removes == 2 {
					return injected
				}
				return os.Remove(path)
			}
			removed, err := removeMenuFiles(paths, rename, remove)
			if removed || !errors.Is(err, injected) {
				t.Fatalf("removed=%v err=%v", removed, err)
			}
			for i, path := range paths {
				got, err := os.ReadFile(path)
				if err != nil || string(got) != string(before[i]) {
					t.Errorf("%s not restored: %q, %v", path, got, err)
				}
				entries, err := os.ReadDir(filepath.Dir(path))
				if err != nil || len(entries) != 1 {
					t.Errorf("backup left behind: %v, %v", entries, err)
				}
			}
		})
	}
}

func TestOverlayReadReportsInaccessibleCommands(t *testing.T) {
	set := newOverlaySet(t)
	path := set.WritePath("cfg", "MAIN.CFG")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("MAIN.CFG", path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := LoadCommands(set, "MAIN"); err == nil {
		t.Error("LoadCommands silently loaded shipped commands")
	}
	if _, err := LoadMenus(set); err == nil {
		t.Error("LoadMenus silently marked broken overlay commands as base")
	}
}

func TestSaveUpdatesOverlayMarkers(t *testing.T) {
	for _, mode := range []string{"overlay", "bare", "failed"} {
		for _, action := range []string{"menu", "commands", "all"} {
			t.Run(mode+"/"+action, func(t *testing.T) {
				set := newOverlaySet(t)
				if mode == "bare" {
					set = menuset.Bare(set.Base)
				}
				m, err := New(set)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "failed" {
					if err := os.MkdirAll(filepath.Dir(set.Overlay), 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(set.Overlay, []byte("not a directory"), 0644); err != nil {
						t.Fatal(err)
					}
				}
				m.menuEditIdx, m.cmdsMenuIdx = 0, 0
				name := m.menus[0].Name
				m.menus[0].Data.Title = "saved title"
				m.dirtyMenus[name] = true
				m.dirtyCmds = true
				switch action {
				case "menu":
					m.saveCurrentMenu()
				case "commands":
					_ = m.saveCurrentCommands()
				case "all":
					_ = m.saveAll()
				}
				wantMnu := mode == "overlay" && action != "commands"
				wantCfg := mode == "overlay" && action != "menu"
				if m.menus[0].MnuOverlay != wantMnu || m.menus[0].CfgOverlay != wantCfg {
					t.Errorf("markers=(%v,%v) want (%v,%v)", m.menus[0].MnuOverlay, m.menus[0].CfgOverlay, wantMnu, wantCfg)
				}
				if mode == "failed" && (m.message == "" || !m.dirtyMenus[name] || !m.dirtyCmds) {
					t.Errorf("failed save lost dirty state or error: %+v", m.message)
				}
			})
		}
	}
}
