package menu

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMenu(t *testing.T) {
	dir := t.TempDir()
	mnu := `{"CLR": true, "PROMPT1": "Cmd: ", "FALLBACK": "MAIN"}`
	if err := os.WriteFile(filepath.Join(dir, "MAIN.MNU"), []byte(mnu), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "BAD.MNU"), []byte("{nope"), 0644); err != nil {
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
	dir := t.TempDir()
	cfg := `[
		{"KEYS": "M", "CMD": "GOTO:MSG", "ACS": "s10", "HIDDEN": false},
		{"KEYS": "G", "CMD": "LOGOFF", "ACS": "", "HIDDEN": true}
	]`
	if err := os.WriteFile(filepath.Join(dir, "MAIN.CFG"), []byte(cfg), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "EMPTY.CFG"), nil, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "BAD.CFG"), []byte("nope"), 0644); err != nil {
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
	if !HasBarFile("MAIN", dir) {
		t.Error("MAIN.BAR should be detected")
	}
	if HasBarFile("OTHER", dir) {
		t.Error("OTHER.BAR should not be detected")
	}
}
