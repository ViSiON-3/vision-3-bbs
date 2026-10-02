package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPageSysopDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadServerConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PageSysopTimeoutSeconds != 60 || cfg.PageSysopCooldownSeconds != 300 {
		t.Fatalf("defaults %d/%d", cfg.PageSysopTimeoutSeconds, cfg.PageSysopCooldownSeconds)
	}
}

func TestChatThemeDefaults(t *testing.T) {
	th, _ := LoadThemeConfig(t.TempDir())
	if th.ChatSysopColor == 0 || th.ChatUserColor == 0 {
		t.Fatalf("theme defaults %+v", th)
	}
}

func TestPageSysopStringFallbacks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "strings.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadStrings(dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]string{
		"PageSysopReasonPrompt": s.PageSysopReasonPrompt, "PageSysopPaging": s.PageSysopPaging,
		"PageSysopUnavailable": s.PageSysopUnavailable, "PageSysopCooldown": s.PageSysopCooldown,
		"SysopChatHeader": s.SysopChatHeader, "SysopChatBack": s.SysopChatBack,
	} {
		if v == "" {
			t.Errorf("%s empty", name)
		}
	}
}
