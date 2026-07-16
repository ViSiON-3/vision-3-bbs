package menueditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newMenuBase creates a menu base directory with mnu/ and cfg/ subdirs.
func newMenuBase(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	for _, d := range []string{"mnu", "cfg"} {
		if err := os.MkdirAll(filepath.Join(base, d), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	return base
}

func TestNormalizeMenuName(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"upper passthrough", "MAIN", "MAIN", false},
		{"lowercase normalized", "main", "MAIN", false},
		{"trims whitespace", "  main ", "MAIN", false},
		{"underscore and digits", "MENU_2", "MENU_2", false},
		{"max length", "ABCDEFGH", "ABCDEFGH", false},
		{"too long", "ABCDEFGHI", "", true},
		{"empty", "", "", true},
		{"path traversal", "../MAIN", "", true},
		{"slash", "A/B", "", true},
		{"dot", "A.B", "", true},
		{"space inside", "A B", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeMenuName(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("normalizeMenuName(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("normalizeMenuName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSaveAndLoadMenuRoundTrip(t *testing.T) {
	base := newMenuBase(t)
	want := MenuData{
		Title:    "Main Menu",
		CLR:      true,
		Prompt1:  "Cmd: ",
		Fallback: "MAIN",
		ACS:      "s10",
		MesConf:  2,
	}
	if err := SaveMenu(base, "main", want); err != nil {
		t.Fatalf("SaveMenu: %v", err)
	}

	menus, err := LoadMenus(base)
	if err != nil {
		t.Fatalf("LoadMenus: %v", err)
	}
	if len(menus) != 1 {
		t.Fatalf("want 1 menu, got %d", len(menus))
	}
	if menus[0].Name != "MAIN" {
		t.Errorf("name = %q, want MAIN", menus[0].Name)
	}
	if menus[0].Data != want {
		t.Errorf("data = %+v, want %+v", menus[0].Data, want)
	}

	// No temp files should remain after the atomic write.
	entries, err := os.ReadDir(filepath.Join(base, "mnu"))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestLoadMenus_SkipsNonMenuFiles(t *testing.T) {
	base := newMenuBase(t)
	if err := SaveMenu(base, "B", MenuData{Title: "B"}); err != nil {
		t.Fatalf("SaveMenu: %v", err)
	}
	if err := SaveMenu(base, "A", MenuData{Title: "A"}); err != nil {
		t.Fatalf("SaveMenu: %v", err)
	}
	// Non-.MNU file and a subdirectory must be skipped.
	if err := os.WriteFile(filepath.Join(base, "mnu", "README.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(base, "mnu", "sub"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	menus, err := LoadMenus(base)
	if err != nil {
		t.Fatalf("LoadMenus: %v", err)
	}
	if len(menus) != 2 {
		t.Fatalf("want 2 menus, got %d", len(menus))
	}
	// Sorted alphabetically.
	if menus[0].Name != "A" || menus[1].Name != "B" {
		t.Errorf("order = %s, %s; want A, B", menus[0].Name, menus[1].Name)
	}
}

func TestLoadMenus_MissingDir(t *testing.T) {
	if _, err := LoadMenus(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("missing mnu dir should error")
	}
}

func TestSaveAndLoadCommands(t *testing.T) {
	base := newMenuBase(t)

	// Missing .CFG returns an empty slice, not an error.
	cmds, err := LoadCommands(base, "MAIN")
	if err != nil {
		t.Fatalf("LoadCommands missing: %v", err)
	}
	if len(cmds) != 0 {
		t.Errorf("want 0 commands, got %d", len(cmds))
	}

	want := []CmdData{
		{Keys: "M", Command: "GOTO:MSGMENU", ACS: "s10"},
		{Keys: "G", Command: "LOGOFF", Hidden: true},
	}
	if err := SaveCommands(base, "main", want); err != nil {
		t.Fatalf("SaveCommands: %v", err)
	}
	got, err := LoadCommands(base, "MAIN")
	if err != nil {
		t.Fatalf("LoadCommands: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("want %d commands, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cmd[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	// Empty .CFG file also yields an empty slice.
	if err := os.WriteFile(filepath.Join(base, "cfg", "EMPTY.CFG"), nil, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cmds, err = LoadCommands(base, "EMPTY")
	if err != nil {
		t.Fatalf("LoadCommands empty: %v", err)
	}
	if len(cmds) != 0 {
		t.Errorf("want 0 commands from empty file, got %d", len(cmds))
	}

	// Invalid names are rejected.
	if _, err := LoadCommands(base, "../etc"); err == nil {
		t.Error("LoadCommands with traversal name should error")
	}
	if err := SaveCommands(base, "bad name", nil); err == nil {
		t.Error("SaveCommands with invalid name should error")
	}
	if err := SaveMenu(base, "toolongname", MenuData{}); err == nil {
		t.Error("SaveMenu with invalid name should error")
	}
}

func TestCreateDeleteExists(t *testing.T) {
	base := newMenuBase(t)

	if MenuExists(base, "NEW") {
		t.Fatal("NEW should not exist yet")
	}
	if err := CreateMenu(base, "new"); err != nil {
		t.Fatalf("CreateMenu: %v", err)
	}
	if !MenuExists(base, "new") {
		t.Error("MenuExists should be true after CreateMenu (case-insensitive)")
	}

	// CreateMenu seeds UsePrompt=true and Fallback=self.
	menus, err := LoadMenus(base)
	if err != nil {
		t.Fatalf("LoadMenus: %v", err)
	}
	if len(menus) != 1 || !menus[0].Data.UsePrompt || menus[0].Data.Fallback != "NEW" {
		t.Errorf("created menu = %+v; want UsePrompt=true, Fallback=NEW", menus[0])
	}
	// An empty .CFG is created alongside.
	if _, err := os.Stat(filepath.Join(base, "cfg", "NEW.CFG")); err != nil {
		t.Errorf("expected NEW.CFG to exist: %v", err)
	}

	if err := DeleteMenu(base, "NEW"); err != nil {
		t.Fatalf("DeleteMenu: %v", err)
	}
	if MenuExists(base, "NEW") {
		t.Error("NEW should be gone after DeleteMenu")
	}
	if _, err := os.Stat(filepath.Join(base, "cfg", "NEW.CFG")); !os.IsNotExist(err) {
		t.Errorf("NEW.CFG should be removed, stat err = %v", err)
	}

	// Deleting a nonexistent menu is not an error.
	if err := DeleteMenu(base, "GHOST"); err != nil {
		t.Errorf("DeleteMenu(nonexistent) = %v, want nil", err)
	}
	// Invalid names.
	if err := CreateMenu(base, "../X"); err == nil {
		t.Error("CreateMenu with traversal name should error")
	}
	if err := DeleteMenu(base, "../X"); err == nil {
		t.Error("DeleteMenu with traversal name should error")
	}
	if MenuExists(base, "../X") {
		t.Error("MenuExists with invalid name should be false")
	}
}
