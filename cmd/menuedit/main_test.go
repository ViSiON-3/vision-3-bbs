package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Without --menus, menuedit edits the theme config.json selects.
func TestDefaultMenuSet(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"configs", "menus/v3", "menus/amber"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := filepath.Join(root, "configs", "config.json")

	if got, err := defaultMenuSet(root); err != nil || got != filepath.Join(root, "menus", "v3") {
		t.Errorf("no config.json: %q, %v", got, err)
	}
	if err := os.WriteFile(cfg, []byte(`{"theme": {"name": "amber"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := defaultMenuSet(root); err != nil || got != filepath.Join(root, "menus", "amber") {
		t.Errorf("amber: %q, %v", got, err)
	}
	if err := os.WriteFile(cfg, []byte(`{"theme": {"name": "neon"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := defaultMenuSet(root); err == nil || got != "" || !strings.Contains(err.Error(), `theme "neon" not found`) {
		t.Errorf("missing theme: %q, %v", got, err)
	}
}
