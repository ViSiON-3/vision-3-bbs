package ftn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const settingsConf = `# binkd.conf
sysname "Test BBS"
loglevel 4
iport 24554
node 21:1/100@fsxnet host:24554 secret
`

func writeConf(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "binkd.conf")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSyncBinkdSettingsUpdatesLines(t *testing.T) {
	path := writeConf(t, settingsConf)
	if err := SyncBinkdSettings(path, 24555, 6); err != nil {
		t.Fatalf("SyncBinkdSettings: %v", err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	if !strings.Contains(s, "iport 24555\n") {
		t.Errorf("iport not updated:\n%s", s)
	}
	if !strings.Contains(s, "loglevel 6\n") {
		t.Errorf("loglevel not updated:\n%s", s)
	}
	if !strings.Contains(s, "node 21:1/100@fsxnet host:24554 secret") {
		t.Errorf("node line must be untouched:\n%s", s)
	}
}

func TestSyncBinkdSettingsNoChangeLeavesFile(t *testing.T) {
	path := writeConf(t, settingsConf)
	before, _ := os.Stat(path)
	if err := SyncBinkdSettings(path, 24554, 4); err != nil {
		t.Fatalf("SyncBinkdSettings: %v", err)
	}
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("file must not be rewritten when values already match")
	}
}

func TestSyncBinkdSettingsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "binkd.conf")
	if err := SyncBinkdSettings(path, 24554, 4); err != nil {
		t.Fatalf("missing file must be a no-op, got: %v", err)
	}
}

func TestSyncBinkdSettingsNonPositiveIgnored(t *testing.T) {
	path := writeConf(t, settingsConf)
	if err := SyncBinkdSettings(path, 0, 0); err != nil {
		t.Fatalf("SyncBinkdSettings: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "iport 24554\n") || !strings.Contains(string(got), "loglevel 4\n") {
		t.Errorf("zero values must leave lines untouched:\n%s", got)
	}
}
