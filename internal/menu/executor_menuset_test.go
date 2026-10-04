package menu

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTemplateFilePrefersOverlayWithDifferentSuffix(t *testing.T) {
	e := &MenuExecutor{MenuSetPath: filepath.Join(t.TempDir(), "menus", "v3")}
	base := filepath.Join(e.MenuSetPath, "templates", "X.TOP")
	overlay := filepath.Join(e.Menus().Overlay, "templates", "X.TOP.ans")
	for path, content := range map[string]string{base: "shipped", overlay: "override"} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Compare content because .ANS may also match .ans on case-insensitive volumes.
	got, err := os.ReadFile(e.templateFile("X.TOP"))
	if err != nil || string(got) != "override" {
		t.Fatalf("templateFile read = %q, %v; want override", got, err)
	}
}
