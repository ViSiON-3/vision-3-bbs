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
	if !MenuExists(set, "NEW") || MenuExists(menuset.Bare(set.Base), "NEW") {
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
	if !MenuExists(set, "MAIN") {
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
	if MenuExists(set, "NEW") {
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
