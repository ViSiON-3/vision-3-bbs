package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// SaveThemeConfig writes the overlay, leaves the shipped theme.json alone,
// keeps keys it does not know, and the result loads back.
func TestSaveThemeConfigWritesOverlay(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "menus", "v3")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	shipped := `{"yesNoHighlightColor": 31, "sysopChatScreen": "CHAT1", "futureKey": [1, 2]}`
	if err := os.WriteFile(filepath.Join(base, "theme.json"), []byte(shipped), 0o644); err != nil {
		t.Fatal(err)
	}
	theme, err := LoadThemeConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	theme.SysopChatScreen = "random"
	if err := SaveThemeConfig(base, theme); err != nil {
		t.Fatal(err)
	}

	if data, _ := os.ReadFile(filepath.Join(base, "theme.json")); string(data) != shipped {
		t.Fatalf("shipped theme.json changed: %s", data)
	}
	data, err := os.ReadFile(filepath.Join(root, "menus.d", "v3", "theme.json"))
	if err != nil {
		t.Fatalf("overlay not written: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["futureKey"]; !ok {
		t.Errorf("unknown key dropped: %s", data)
	}
	got, err := LoadThemeConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	if got != theme {
		t.Errorf("loaded %+v; want %+v", got, theme)
	}

	// A second save starts from the overlay.
	got.ChatUserColor = 14
	if err := SaveThemeConfig(base, got); err != nil {
		t.Fatal(err)
	}
	if again, _ := LoadThemeConfig(base); again != got {
		t.Errorf("after a second save loaded %+v; want %+v", again, got)
	}
}

// A menu set with no theme.json anywhere gets one in the overlay.
func TestSaveThemeConfigWithoutThemeFile(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "menus", "v3")
	theme, err := LoadThemeConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	theme.SysopChatScreen = "CHAT1"
	if err := SaveThemeConfig(base, theme); err != nil {
		t.Fatal(err)
	}
	if got, _ := LoadThemeConfig(base); got != theme {
		t.Errorf("loaded %+v; want %+v", got, theme)
	}
}
