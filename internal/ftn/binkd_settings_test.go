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
	if err := SyncBinkdSettings(path, 24555, 6, ""); err != nil {
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
	if err := SyncBinkdSettings(path, 24554, 4, ""); err != nil {
		t.Fatalf("SyncBinkdSettings: %v", err)
	}
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("file must not be rewritten when values already match")
	}
}

func TestSyncBinkdSettingsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "binkd.conf")
	if err := SyncBinkdSettings(path, 24554, 4, ""); err != nil {
		t.Fatalf("missing file must be a no-op, got: %v", err)
	}
}

func TestSyncBinkdSettingsNonPositiveIgnored(t *testing.T) {
	path := writeConf(t, settingsConf)
	if err := SyncBinkdSettings(path, 0, 0, ""); err != nil {
		t.Fatalf("SyncBinkdSettings: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "iport 24554\n") || !strings.Contains(string(got), "loglevel 4\n") {
		t.Errorf("zero values must leave lines untouched:\n%s", got)
	}
}

// The drift this guards against shipped: the tosser packed bundles into
// ftn.json's binkd_outbound_path while binkd.conf still pointed elsewhere, so
// binkd reported an empty queue and echomail never left the board.
func TestSyncBinkdSettingsRepointsDomainOutbound(t *testing.T) {
	path := writeConf(t, settingsConf+"domain fsxnet /opt/v3/data/ftn/binkd_outbound 21\ndomain fidonet /opt/v3/data/ftn/binkd_outbound 3\n")
	if err := SyncBinkdSettings(path, 24554, 4, "/opt/v3/data/ftn/out"); err != nil {
		t.Fatalf("SyncBinkdSettings: %v", err)
	}
	got := readConf(t, path)
	for _, want := range []string{
		"domain fsxnet /opt/v3/data/ftn/out 21",
		"domain fidonet /opt/v3/data/ftn/out 3",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "binkd_outbound") {
		t.Errorf("stale outbound path survived:\n%s", got)
	}
}

func TestSyncBinkdSettingsLeavesMatchingDomainAlone(t *testing.T) {
	conf := settingsConf + "domain fsxnet /opt/v3/data/ftn/out 21\n"
	path := writeConf(t, conf)
	if err := SyncBinkdSettings(path, 24554, 4, "/opt/v3/data/ftn/out"); err != nil {
		t.Fatalf("SyncBinkdSettings: %v", err)
	}
	if got := readConf(t, path); got != conf {
		t.Errorf("file rewritten despite no change:\ngot:\n%s\nwant:\n%s", got, conf)
	}
}

// An empty outboundPath means "not configured"; blanking the sysop's domain
// lines would leave binkd with no outbound at all.
func TestSyncBinkdSettingsEmptyOutboundLeavesDomains(t *testing.T) {
	conf := settingsConf + "domain fsxnet /somewhere/else 21\n"
	path := writeConf(t, conf)
	if err := SyncBinkdSettings(path, 24554, 4, ""); err != nil {
		t.Fatalf("SyncBinkdSettings: %v", err)
	}
	if got := readConf(t, path); got != conf {
		t.Errorf("domain line touched with no configured path:\n%s", got)
	}
}
