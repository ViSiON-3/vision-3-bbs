package menueditor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// editorWithMenus builds a Model over a fresh menu base holding the named
// menus, returning it with its (bare) menu set.
func editorWithMenus(t *testing.T, names ...string) (Model, menuset.Set) {
	t.Helper()
	set := menuset.Bare(newMenuBase(t))
	for _, n := range names {
		if err := CreateMenu(set, n); err != nil {
			t.Fatalf("CreateMenu(%s): %v", n, err)
		}
	}
	m, err := New(set)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m, set
}

// editorWithCommands builds an editor over menu ALPHA seeded with n commands
// (keys K00, K01, ...) and opens ALPHA's command list.
func editorWithCommands(t *testing.T, n int) (Model, menuset.Set) {
	t.Helper()
	m, set := editorWithMenus(t, "ALPHA", "BETA")
	cmds := make([]CmdData, n)
	for i := range cmds {
		cmds[i] = CmdData{Keys: fmt.Sprintf("K%02d", i), Command: fmt.Sprintf("RUN:%02d", i)}
	}
	if err := SaveCommands(set, "ALPHA", cmds); err != nil {
		t.Fatalf("SaveCommands: %v", err)
	}
	m = keys(t, m, tea.KeyF10)
	if m.mode != modeCommandList || len(m.cmds) != n {
		t.Fatalf("setup: mode %v with %d commands", m.mode, len(m.cmds))
	}
	return m, set
}

// loadCmds reads ALPHA's commands back from disk.
func loadCmds(t *testing.T, set menuset.Set, name string) []CmdData {
	t.Helper()
	cmds, err := LoadCommands(set, name)
	if err != nil {
		t.Fatalf("LoadCommands(%s): %v", name, err)
	}
	return cmds
}

// loadMenu reads one menu's data back from disk.
func loadMenu(t *testing.T, set menuset.Set, name string) MenuData {
	t.Helper()
	menus, err := LoadMenus(set)
	if err != nil {
		t.Fatalf("LoadMenus: %v", err)
	}
	for _, me := range menus {
		if me.Name == name {
			return me.Data
		}
	}
	t.Fatalf("menu %s not on disk", name)
	return MenuData{}
}

// Init names the terminal window.
func TestInitSetsTheWindowTitle(t *testing.T) {
	m, _ := editorWithMenus(t, "ALPHA")
	if m.Init() == nil {
		t.Fatal("Init returned no command")
	}
}

// A resize below the minimum is clamped, a larger one honoured.
func TestWindowSizeClamps(t *testing.T) {
	m, _ := editorWithMenus(t, "ALPHA")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 10, Height: 5})
	m = asModel(t, updated)
	if m.width != minWidth || m.height != minHeight {
		t.Errorf("size = %dx%d, want %dx%d", m.width, m.height, minWidth, minHeight)
	}
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 132, Height: 50})
	m = asModel(t, updated)
	if m.width != 132 || m.height != 50 {
		t.Errorf("size = %dx%d, want 132x50", m.width, m.height)
	}
}

// With no menus at all, the keys that act on a highlighted menu do nothing.
func TestEmptyMenuListIgnoresItemKeys(t *testing.T) {
	m, _ := editorWithMenus(t)
	m = keys(t, m, tea.KeyEnter, tea.KeyF2, tea.KeyF10, tea.KeyEnd, tea.KeyPgDown)
	if m.mode != modeMenuList || m.menuCursor != 0 {
		t.Errorf("mode=%v cursor=%d, want an unchanged empty list", m.mode, m.menuCursor)
	}
}

// Paging through a long menu list scrolls the window so the cursor stays on
// screen, and the list shows each menu's title (or name) and its file pair.
func TestLongMenuListScrolls(t *testing.T) {
	names := make([]string, 25)
	for i := range names {
		names[i] = fmt.Sprintf("M%02d", i)
	}
	m, _ := editorWithMenus(t, names...)
	m = keys(t, m, tea.KeyPgDown)
	if m.menuCursor != listVisible || m.menuScroll == 0 {
		t.Errorf("PgDn: cursor %d scroll %d", m.menuCursor, m.menuScroll)
	}
	m = keys(t, m, tea.KeyEnd)
	view := stripANSI(m.View())
	if !strings.Contains(view, "M24.MNU / M24.CFG") {
		t.Error("the last menu is not on screen with the cursor on it")
	}
	if strings.Contains(view, "M00.MNU") {
		t.Error("the first menu should have scrolled off")
	}
	m = keys(t, m, tea.KeyUp, tea.KeyHome)
	if m.menuCursor != 0 || m.menuScroll != 0 {
		t.Errorf("Home: cursor %d scroll %d", m.menuCursor, m.menuScroll)
	}
}

// Alt-H opens help from the menu list and any key returns to the list.
func TestAltHHelpOnMenuList(t *testing.T) {
	m, _ := editorWithMenus(t, "ALPHA")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}, Alt: true})
	if m.mode != modeHelp {
		t.Fatalf("mode = %v, want modeHelp", m.mode)
	}
	if !strings.Contains(stripANSI(m.View()), "ViSiON/3 Menu Editor Help") {
		t.Error("help overlay is not drawn")
	}
	m = typeText(t, m, "x")
	if m.mode != modeMenuList {
		t.Errorf("mode = %v, want modeMenuList", m.mode)
	}
}

// The add-menu dialog cancels on Escape or an empty name, creating nothing.
func TestAddMenuCancel(t *testing.T) {
	m, set := editorWithMenus(t, "ALPHA")
	m = keys(t, m, tea.KeyF5)
	if !strings.Contains(stripANSI(m.View()), "Create New Menu") {
		t.Error("add dialog is not drawn")
	}
	m = typeText(t, m, "NEWONE")
	m = keys(t, m, tea.KeyEscape)
	if m.mode != modeMenuList || len(m.menus) != 1 {
		t.Fatalf("Escape: mode=%v menus=%d", m.mode, len(m.menus))
	}
	if ok, _ := MenuExists(set, "NEWONE"); ok {
		t.Error("cancelled menu was created")
	}
	m = keys(t, m, tea.KeyF5, tea.KeyEnter)
	if m.mode != modeMenuList || len(m.menus) != 1 {
		t.Errorf("empty name: mode=%v menus=%d", m.mode, len(m.menus))
	}
}

// Every menu field typed into and confirmed reaches the .MNU on disk, with
// the upper-casing the name-like fields apply.
func TestEveryMenuFieldPersists(t *testing.T) {
	m, set := editorWithMenus(t, "ALPHA")
	m = keys(t, m, tea.KeyEnter)
	values := []string{"Main", "", "", "|09Pick", "|15>", "fall", "s20", "pw", "hm", "2", "3", "4", ""}
	for i, v := range values {
		if m.menuEditFld != i {
			t.Fatalf("field cursor = %d, want %d", m.menuEditFld, i)
		}
		if m.menuFields[i].Type == ftYesNo {
			m = keys(t, m, tea.KeyEnter, tea.KeyDown) // toggle, move on
			continue
		}
		m = retype(t, m, v)
		m = keys(t, m, tea.KeyTab)
		if m.mode != modeMenuEdit {
			t.Fatalf("field %d rejected %q: %q", i, v, m.message)
		}
	}
	m = keys(t, m, tea.KeyEscape)

	got := loadMenu(t, set, "ALPHA")
	want := MenuData{
		Title: "Main", CLR: true, UsePrompt: false, Prompt1: "|09Pick", Prompt2: "|15>",
		Fallback: "FALL", ACS: "S20", Password: "pw", HelpMenu: "HM",
		ForceHelpLevel: 2, MesConf: 3, FileConf: 4, ForceHotKey: true,
	}
	if got != want {
		t.Errorf("on disk:\n got %+v\nwant %+v", got, want)
	}
}

// Up and Down stop at the ends of the menu field list; Up from inside an
// input confirms it and moves up, and an invalid number keeps the input open.
func TestMenuEditFieldCursorAndUpConfirm(t *testing.T) {
	m, _ := editorWithMenus(t, "ALPHA")
	m = keys(t, m, tea.KeyEnter, tea.KeyUp)
	if m.menuEditFld != 0 {
		t.Errorf("Up at the top = %d", m.menuEditFld)
	}
	for i := 0; i < 20; i++ {
		m = keys(t, m, tea.KeyDown)
	}
	if m.menuEditFld != len(m.menuFields)-1 {
		t.Errorf("Down past the end = %d", m.menuEditFld)
	}

	m = keys(t, m, tea.KeyUp) // File Conference
	m = retype(t, m, "x")
	m = keys(t, m, tea.KeyUp)
	if m.mode != modeMenuEditField || !strings.HasPrefix(m.message, "Invalid value") {
		t.Fatalf("invalid Up: mode=%v message=%q", m.mode, m.message)
	}
	m = retype(t, keys(t, m, tea.KeyEscape), "7")
	m = keys(t, m, tea.KeyUp)
	if m.mode != modeMenuEdit || m.menus[0].Data.FileConf != 7 {
		t.Errorf("valid Up: mode=%v fileconf=%d", m.mode, m.menus[0].Data.FileConf)
	}
	if m.menuFields[m.menuEditFld].Label != "Msg Conference " {
		t.Errorf("Up moved to %q", m.menuFields[m.menuEditFld].Label)
	}
}

// Page Down and Page Up move between menus on the edit screen, wrapping, and
// save the menu being left if it was changed.
func TestMenuEditPagingSavesAndWraps(t *testing.T) {
	m, set := editorWithMenus(t, "ALPHA", "BETA")
	m = keys(t, m, tea.KeyEnter)
	m = retype(t, m, "First")
	m = keys(t, m, tea.KeyEnter, tea.KeyPgDown)
	if m.menus[m.menuEditIdx].Name != "BETA" || m.menuCursor != 1 {
		t.Fatalf("PgDn: editing %s cursor %d", m.menus[m.menuEditIdx].Name, m.menuCursor)
	}
	if got := loadMenu(t, set, "ALPHA").Title; got != "First" {
		t.Errorf("ALPHA on disk after paging away = %q, want First", got)
	}
	if !strings.Contains(stripANSI(m.View()), "Menu 2 of 2") {
		t.Error("edit screen does not show the position")
	}
	m = keys(t, m, tea.KeyPgDown)
	if m.menus[m.menuEditIdx].Name != "ALPHA" {
		t.Errorf("PgDn from the last menu did not wrap")
	}
	m = keys(t, m, tea.KeyPgUp)
	if m.menus[m.menuEditIdx].Name != "BETA" {
		t.Errorf("PgUp from the first menu did not wrap")
	}
}

// F2 on the edit screen asks before deleting: No returns to the edit screen
// with the menu intact, Yes removes it from disk and returns to the list.
func TestDeleteMenuFromEditScreen(t *testing.T) {
	m, set := editorWithMenus(t, "ALPHA", "BETA")
	m = keys(t, m, tea.KeyDown, tea.KeyEnter, tea.KeyF2)
	if !strings.Contains(stripANSI(m.View()), "Delete Menu") {
		t.Error("delete prompt is not drawn over the edit screen")
	}
	m = keys(t, m, tea.KeyEscape)
	if m.mode != modeMenuEdit || len(m.menus) != 2 {
		t.Fatalf("Escape: mode=%v menus=%d", m.mode, len(m.menus))
	}
	m = keys(t, m, tea.KeyF2, tea.KeyRight, tea.KeyEnter)
	if m.mode != modeMenuList || len(m.menus) != 1 || m.menuCursor != 0 {
		t.Fatalf("Yes: mode=%v menus=%d cursor=%d", m.mode, len(m.menus), m.menuCursor)
	}
	if ok, _ := MenuExists(set, "BETA"); ok {
		t.Error("BETA is still on disk")
	}
	if m.message != "Deleted menu: BETA" {
		t.Errorf("message = %q", m.message)
	}
}

// Enter on the default No of a delete prompt cancels it.
func TestDeletePromptDefaultsToNo(t *testing.T) {
	m, _ := editorWithMenus(t, "ALPHA")
	m = keys(t, m, tea.KeyF2)
	if !strings.Contains(stripANSI(m.View()), "Delete ALPHA.MNU") {
		t.Error("delete prompt does not name the menu")
	}
	m = keys(t, m, tea.KeyEnter)
	if m.mode != modeMenuList || len(m.menus) != 1 {
		t.Errorf("mode=%v menus=%d", m.mode, len(m.menus))
	}
}

// F10 from the edit screen saves the menu and opens its commands.
func TestF10FromMenuEditSavesAndOpensCommands(t *testing.T) {
	m, set := editorWithMenus(t, "ALPHA")
	m = keys(t, m, tea.KeyEnter)
	m = retype(t, m, "Saved")
	m = keys(t, m, tea.KeyEnter, tea.KeyF10)
	if m.mode != modeCommandList {
		t.Fatalf("mode = %v", m.mode)
	}
	if got := loadMenu(t, set, "ALPHA").Title; got != "Saved" {
		t.Errorf("title on disk = %q", got)
	}
}

// Leaving the editor with unsaved changes and answering No quits without
// writing them; answering Yes writes them and quits.
func TestExitPromptSaveOrDiscard(t *testing.T) {
	for _, tc := range []struct {
		answer string
		want   string
	}{{"n", ""}, {"y", "Pending"}} {
		t.Run(tc.answer, func(t *testing.T) {
			m, set := editorWithMenus(t, "ALPHA")
			// F5 then Escape from the edit screen is the path back to the list
			// that leaves the edit unsaved.
			m = keys(t, m, tea.KeyEnter)
			m = retype(t, m, "Pending")
			m = keys(t, m, tea.KeyEnter, tea.KeyF5, tea.KeyEscape, tea.KeyEscape)
			if m.mode != modeExitConfirm {
				t.Fatalf("mode = %v, want modeExitConfirm", m.mode)
			}
			if !strings.Contains(stripANSI(m.View()), "Save all changes before exit?") {
				t.Error("exit prompt is not drawn")
			}
			updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.answer)})
			asModel(t, updated)
			if !quits(cmd) {
				t.Fatal("answer did not quit")
			}
			if got := loadMenu(t, set, "ALPHA").Title; got != tc.want {
				t.Errorf("title on disk = %q, want %q", got, tc.want)
			}
		})
	}
}

// A save that fails on the way out keeps the editor open with the error
// showing, rather than quitting and losing the edits.
func TestFailedSaveOnExitDoesNotQuit(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs unix directory permissions enforced")
	}
	m, set := editorWithMenus(t, "ALPHA")
	m = keys(t, m, tea.KeyEnter)
	m = retype(t, m, "Blocked")
	m = keys(t, m, tea.KeyEnter, tea.KeyF5, tea.KeyEscape, tea.KeyEscape)

	mnu := filepath.Join(set.Base, "mnu")
	if err := os.Chmod(mnu, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(mnu, 0o755) })

	m, cmd := sendKey(t, m, tea.KeyEnter)
	if quits(cmd) {
		t.Fatal("quit although the save failed")
	}
	if m.mode != modeMenuList || !strings.HasPrefix(m.message, "Save error") {
		t.Errorf("mode=%v message=%q", m.mode, m.message)
	}
	if !m.dirtyMenus["ALPHA"] {
		t.Error("the unsaved menu is no longer marked dirty")
	}
}

// The command list moves by line and by page, clamps at both ends and
// scrolls; Enter opens the highlighted command.
func TestCommandListNavigation(t *testing.T) {
	m, _ := editorWithCommands(t, 30)
	m = keys(t, m, tea.KeyUp)
	if m.cmdCursor != 0 {
		t.Errorf("Up at top = %d", m.cmdCursor)
	}
	m = keys(t, m, tea.KeyPgDown, tea.KeyDown)
	if m.cmdCursor != listVisible+1 || m.cmdScroll == 0 {
		t.Errorf("PgDn+Down: cursor %d scroll %d", m.cmdCursor, m.cmdScroll)
	}
	m = keys(t, m, tea.KeyPgDown, tea.KeyPgDown, tea.KeyDown)
	if m.cmdCursor != 29 {
		t.Errorf("PgDn past the end = %d", m.cmdCursor)
	}
	if !strings.Contains(stripANSI(m.View()), "RUN:29") {
		t.Error("last command is not on screen")
	}
	m = keys(t, m, tea.KeyPgUp, tea.KeyPgUp, tea.KeyPgUp)
	if m.cmdCursor != 0 {
		t.Errorf("PgUp past the top = %d", m.cmdCursor)
	}
	m = keys(t, m, tea.KeyEnd, tea.KeyHome, tea.KeyDown, tea.KeyEnter)
	if m.mode != modeCommandEdit || m.cmdEditIdx != 1 {
		t.Errorf("Enter: mode=%v idx=%d", m.mode, m.cmdEditIdx)
	}
	if !strings.Contains(stripANSI(m.View()), "K01") {
		t.Error("command edit screen does not show the command")
	}
}

// On an empty command list, Enter, F2 and the paging keys do nothing, and
// Escape without changes returns to the menu edit screen without writing.
func TestEmptyCommandList(t *testing.T) {
	m, _ := editorWithCommands(t, 0)
	m = keys(t, m, tea.KeyEnter, tea.KeyF2, tea.KeyEnd, tea.KeyPgDown)
	if m.mode != modeCommandList || m.cmdCursor != 0 {
		t.Fatalf("mode=%v cursor=%d", m.mode, m.cmdCursor)
	}
	m = keys(t, m, tea.KeyEscape)
	if m.mode != modeMenuEdit || m.menus[m.menuEditIdx].Name != "ALPHA" {
		t.Errorf("Escape: mode=%v", m.mode)
	}
}

// Every command field typed into and confirmed reaches the .CFG on disk.
func TestEveryCommandFieldPersists(t *testing.T) {
	m, set := editorWithCommands(t, 1)
	m = keys(t, m, tea.KeyEnter)
	for i, v := range []string{"Reading mail", "m", "MAIL:READ", "s10", "", "|MAIN"} {
		if m.cmdFields[i].Type == ftYesNo {
			m = keys(t, m, tea.KeyEnter, tea.KeyDown)
			continue
		}
		m = retype(t, m, v)
		m = keys(t, m, tea.KeyDown)
		if m.mode != modeCommandEdit {
			t.Fatalf("field %d: mode %v", i, m.mode)
		}
	}
	m = keys(t, m, tea.KeyEscape, tea.KeyEscape)

	got := loadCmds(t, set, "ALPHA")
	want := CmdData{NodeActivity: "Reading mail", Keys: "M", Command: "MAIL:READ", ACS: "S10", Hidden: true, AutoRun: "|MAIN"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("on disk:\n got %+v\nwant %+v", got, want)
	}
}

// Inside a command field, Escape abandons the typed value and Up confirms and
// moves up; the field cursor clamps at both ends.
func TestCommandFieldEscapeAndUp(t *testing.T) {
	m, _ := editorWithCommands(t, 1)
	m = keys(t, m, tea.KeyEnter, tea.KeyUp)
	if m.cmdEditFld != 0 {
		t.Errorf("Up at top = %d", m.cmdEditFld)
	}
	for i := 0; i < 10; i++ {
		m = keys(t, m, tea.KeyTab)
	}
	if m.cmdEditFld != len(m.cmdFields)-1 {
		t.Errorf("Tab past the end = %d", m.cmdEditFld)
	}

	m = keys(t, m, tea.KeyHome) // ignored on the edit screen
	for m.cmdEditFld > 2 {
		m = keys(t, m, tea.KeyUp)
	}
	m = retype(t, m, "DISCARDED")
	m = keys(t, m, tea.KeyEscape)
	if m.mode != modeCommandEdit || m.cmds[0].Command != "RUN:00" {
		t.Fatalf("Escape: mode=%v command=%q", m.mode, m.cmds[0].Command)
	}
	m = retype(t, m, "KEPT")
	m = keys(t, m, tea.KeyUp)
	if m.cmds[0].Command != "KEPT" || m.cmdEditFld != 1 {
		t.Errorf("Up: command=%q field=%d", m.cmds[0].Command, m.cmdEditFld)
	}
}

// Page Down and Page Up step between commands on the edit screen, wrapping,
// and keep the list cursor on the command being edited.
func TestCommandEditPaging(t *testing.T) {
	m, _ := editorWithCommands(t, 3)
	m = keys(t, m, tea.KeyEnter, tea.KeyPgUp)
	if m.cmdEditIdx != 2 || m.cmdCursor != 2 {
		t.Errorf("PgUp from first: idx %d cursor %d, want 2", m.cmdEditIdx, m.cmdCursor)
	}
	m = keys(t, m, tea.KeyPgDown)
	if m.cmdEditIdx != 0 || m.cmdCursor != 0 {
		t.Errorf("PgDn from last: idx %d cursor %d, want 0", m.cmdEditIdx, m.cmdCursor)
	}
}

// F2 on the command edit screen deletes the command being edited after
// confirmation; the change is written when the list is left.
func TestDeleteCommandFromEditScreen(t *testing.T) {
	m, set := editorWithCommands(t, 3)
	m = keys(t, m, tea.KeyDown, tea.KeyEnter, tea.KeyF2)
	if !strings.Contains(stripANSI(m.View()), "Delete command 'K01'?") {
		t.Error("delete prompt does not name the command")
	}
	m = keys(t, m, tea.KeyEscape)
	if m.mode != modeCommandList || len(m.cmds) != 3 {
		t.Fatalf("Escape: mode=%v cmds=%d", m.mode, len(m.cmds))
	}
	m = keys(t, m, tea.KeyEnter, tea.KeyF2)
	m = typeText(t, m, "y")
	if m.mode != modeCommandList || len(m.cmds) != 2 {
		t.Fatalf("Yes: mode=%v cmds=%d", m.mode, len(m.cmds))
	}
	m = keys(t, m, tea.KeyEscape)
	got := loadCmds(t, set, "ALPHA")
	if len(got) != 2 || got[0].Keys != "K00" || got[1].Keys != "K02" {
		t.Errorf("on disk = %+v, want K00, K02", got)
	}
}

// Deleting the last command leaves the list cursor on the new last one.
func TestDeleteLastCommandClampsCursor(t *testing.T) {
	m, _ := editorWithCommands(t, 2)
	m = keys(t, m, tea.KeyEnd, tea.KeyF2)
	m = typeText(t, m, "Y")
	if m.cmdCursor != 0 || len(m.cmds) != 1 {
		t.Errorf("cursor %d cmds %d, want 0 and 1", m.cmdCursor, len(m.cmds))
	}
}

// F5 on the command edit screen appends a blank command and opens it; F8
// returns to the list.
func TestAddCommandFromEditScreen(t *testing.T) {
	m, set := editorWithCommands(t, 1)
	m = keys(t, m, tea.KeyEnter, tea.KeyF5)
	if m.mode != modeCommandEdit || len(m.cmds) != 2 || m.cmdEditIdx != 1 || m.cmdCursor != 1 {
		t.Fatalf("F5: mode=%v cmds=%d idx=%d", m.mode, len(m.cmds), m.cmdEditIdx)
	}
	m = keys(t, m, tea.KeyDown)
	m = retype(t, m, "x")
	m = keys(t, m, tea.KeyEnter, tea.KeyF8)
	if m.mode != modeCommandList {
		t.Fatalf("F8: mode = %v", m.mode)
	}
	m = keys(t, m, tea.KeyEscape)
	if got := loadCmds(t, set, "ALPHA"); len(got) != 2 || got[1].Keys != "X" {
		t.Errorf("on disk = %+v", got)
	}
}

// With an overlay, the editor says where saves go, and once a menu is saved
// the list marks its file as coming from the overlay.
func TestOverlayNoticeAndMarkers(t *testing.T) {
	set := newOverlaySet(t)
	m, err := New(set)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stripANSI(m.View()), "Saving to overlay") {
		t.Error("overlay notice is not shown")
	}
	if strings.Contains(stripANSI(m.View()), "MAIN.MNU*") {
		t.Fatal("shipped menu marked as overlay before any save")
	}
	m = keys(t, m, tea.KeyEnter)
	m = retype(t, m, "Mine")
	m = keys(t, m, tea.KeyEnter, tea.KeyEscape)
	if !strings.Contains(stripANSI(m.View()), "MAIN.MNU* / MAIN.CFG") {
		t.Error("saved menu is not marked as an overlay file")
	}
}

// Deleting a shipped menu through an overlay is refused with the reason, and
// the menu stays listed and on disk.
func TestDeletingAShippedMenuThroughTheOverlayIsRefused(t *testing.T) {
	set := newOverlaySet(t)
	m, err := New(set)
	if err != nil {
		t.Fatal(err)
	}
	m = keys(t, m, tea.KeyF2)
	m = typeText(t, m, "y")
	if m.mode != modeMenuList || len(m.menus) != 2 {
		t.Fatalf("mode=%v menus=%d, want both menus still listed", m.mode, len(m.menus))
	}
	if !strings.Contains(m.message, "cannot be removed through the overlay") {
		t.Errorf("message = %q", m.message)
	}
	if ok, _ := MenuExists(set, "MAIN"); !ok {
		t.Error("shipped MAIN is gone")
	}
}

// A menu file that is not valid JSON stops the editor from opening, rather
// than listing a blank menu that a later save would overwrite.
func TestNewRejectsACorruptMenuFile(t *testing.T) {
	base := newMenuBase(t)
	if err := os.WriteFile(filepath.Join(base, "mnu", "BAD.MNU"), []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(menuset.Bare(base)); err == nil {
		t.Error("New accepted a corrupt .MNU")
	}
}
