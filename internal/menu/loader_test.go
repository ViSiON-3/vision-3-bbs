package menu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// setDir makes a menu set root with the given subdirectory and returns the root.
func setDir(t *testing.T, sub string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, sub), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return dir
}

func TestLoadMenu(t *testing.T) {
	root := setDir(t, "mnu")
	dir := menuset.Bare(root)
	mnu := `{"CLR": true, "PROMPT1": "Cmd: ", "FALLBACK": "MAIN"}`
	if err := os.WriteFile(filepath.Join(root, "mnu", "MAIN.MNU"), []byte(mnu), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "mnu", "BAD.MNU"), []byte("{nope"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	rec, err := LoadMenu("MAIN", dir)
	if err != nil {
		t.Fatalf("LoadMenu: %v", err)
	}
	if !rec.GetClrScrBefore() || rec.Prompt1 != "Cmd: " || rec.Fallback != "MAIN" {
		t.Errorf("record = %+v", rec)
	}

	if _, err := LoadMenu("MISSING", dir); err == nil {
		t.Error("missing menu should error")
	}
	if _, err := LoadMenu("BAD", dir); err == nil {
		t.Error("malformed JSON should error")
	}
}

func TestLoadCommandsFile(t *testing.T) {
	root := setDir(t, "cfg")
	dir := menuset.Bare(root)
	cfg := `[
		{"KEYS": "M", "CMD": "GOTO:MSG", "ACS": "s10", "HIDDEN": false},
		{"KEYS": "G", "CMD": "LOGOFF", "ACS": "", "HIDDEN": true}
	]`
	if err := os.WriteFile(filepath.Join(root, "cfg", "MAIN.CFG"), []byte(cfg), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "cfg", "EMPTY.CFG"), nil, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "cfg", "BAD.CFG"), []byte("nope"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cmds, err := LoadCommands("MAIN", dir)
	if err != nil {
		t.Fatalf("LoadCommands: %v", err)
	}
	if len(cmds) != 2 {
		t.Fatalf("want 2 commands, got %d", len(cmds))
	}
	if cmds[0].Keys != "M" || cmds[0].Command != "GOTO:MSG" || cmds[0].ACS != "s10" {
		t.Errorf("cmd[0] = %+v", cmds[0])
	}
	if !cmds[1].Hidden {
		t.Error("cmd[1] should be hidden")
	}

	// Missing file: valid, no commands.
	cmds, err = LoadCommands("NOPE", dir)
	if err != nil || len(cmds) != 0 {
		t.Errorf("missing cfg: cmds=%d err=%v, want 0/nil", len(cmds), err)
	}
	// Empty file: valid, no commands.
	cmds, err = LoadCommands("EMPTY", dir)
	if err != nil || len(cmds) != 0 {
		t.Errorf("empty cfg: cmds=%d err=%v, want 0/nil", len(cmds), err)
	}
	// Malformed JSON: error.
	if _, err := LoadCommands("BAD", dir); err == nil {
		t.Error("malformed cfg should error")
	}
}

func TestHasBarFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bar"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bar", "MAIN.BAR"), []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !HasBarFile("MAIN", menuset.Bare(dir)) {
		t.Error("MAIN.BAR should be detected")
	}
	if HasBarFile("OTHER", menuset.Bare(dir)) {
		t.Error("OTHER.BAR should not be detected")
	}
}

// TestLoaderReadsOverlayFirst covers the menus.d overlay end to end for the
// three loaders: a file in the overlay shadows the shipped one, a file only in
// the overlay is found, and everything else still comes from the shipped set.
func TestLoaderReadsOverlayFirst(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "menus", "v3")
	overlay := filepath.Join(root, "menus.d", "v3")
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(base, "mnu", "MAIN.MNU"), `{"PROMPT1": "shipped"}`)
	write(filepath.Join(base, "mnu", "MSG.MNU"), `{"PROMPT1": "shipped msg"}`)
	write(filepath.Join(base, "cfg", "MAIN.CFG"), `[{"KEYS": "A", "CMD": "GOTO:A"}]`)
	write(filepath.Join(base, "bar", "MAIN.BAR"), "x")
	write(filepath.Join(overlay, "mnu", "MAIN.MNU"), `{"PROMPT1": "mine"}`)
	write(filepath.Join(overlay, "cfg", "MAIN.CFG"), `[{"KEYS": "A", "CMD": "GOTO:A"}, {"KEYS": "B", "CMD": "GOTO:B"}]`)
	write(filepath.Join(overlay, "bar", "EXTRA.BAR"), "x")

	menus := menuset.FromPath(base)
	if menus.Overlay != overlay {
		t.Fatalf("overlay = %q, want %q", menus.Overlay, overlay)
	}

	rec, err := LoadMenu("MAIN", menus)
	if err != nil || rec.Prompt1 != "mine" {
		t.Errorf("MAIN.MNU: rec=%+v err=%v (want overlay copy)", rec, err)
	}
	rec, err = LoadMenu("MSG", menus)
	if err != nil || rec.Prompt1 != "shipped msg" {
		t.Errorf("MSG.MNU: rec=%+v err=%v (want shipped copy)", rec, err)
	}
	cmds, err := LoadCommands("MAIN", menus)
	if err != nil || len(cmds) != 2 {
		t.Errorf("MAIN.CFG: %d commands err=%v (want 2 from overlay)", len(cmds), err)
	}
	if !HasBarFile("MAIN", menus) || !HasBarFile("EXTRA", menus) || HasBarFile("NOPE", menus) {
		t.Error("HasBarFile did not merge layers")
	}

	// The executor derives the same set from its MenuSetPath.
	e := &MenuExecutor{MenuSetPath: base}
	if got := e.menuFile("mnu", "MAIN.MNU"); got != filepath.Join(overlay, "mnu", "MAIN.MNU") {
		t.Errorf("executor menuFile = %q", got)
	}
}
