package menu

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanHeaderUsesSuffixedOverlay(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "menus", "v3")
	overlay := filepath.Join(root, "menus.d", "v3")
	for _, layer := range []string{base, overlay} {
		dir := filepath.Join(layer, "templates", "system_header")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "HEADER.ANS"), []byte(layer), 0644); err != nil {
			t.Fatal(err)
		}
	}
	e := &MenuExecutor{MenuSetPath: base}
	got, err := e.readScanHeaderTemplate()
	if err != nil || string(got) != overlay {
		t.Fatalf("header=%q err=%v, want overlay", got, err)
	}
	// Removing an override still allows the shipped suffixed header.
	if err := os.Remove(filepath.Join(overlay, "templates", "system_header", "HEADER.ANS")); err != nil {
		t.Fatal(err)
	}
	got, err = e.readScanHeaderTemplate()
	if err != nil || string(got) != base {
		t.Fatalf("header=%q err=%v, want base", got, err)
	}
}

func TestTemplateDanglingLinkStopsSuffixFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "HEADER")
	if err := os.Symlink("missing", path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.WriteFile(path+".ANS", []byte("must not be read"), 0644); err != nil {
		t.Fatal(err)
	}
	if got, err := readTemplateFile(path); err == nil || got != nil {
		t.Fatalf("template=%q err=%v", got, err)
	}
}
