package mailer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// newTestRoot builds a BBS root with a fake binkd binary and a binkd.conf.
func newTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	ftnDir := filepath.Join(root, "data", "ftn")
	for _, d := range []string{binDir, ftnDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	// Fake binkd: a shell script that sleeps.
	script := "#!/bin/sh\nsleep 60\n"
	if err := os.WriteFile(filepath.Join(binDir, "binkd"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ftnDir, "binkd.conf"), []byte("iport 24554\nloglevel 4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func testFTNConfig() config.FTNConfig {
	return config.FTNConfig{
		Networks: map[string]config.FTNNetworkConfig{
			"fsxnet": {OwnAddress: "21:4/158", InternalTosserEnabled: true},
		},
		Binkd: config.BinkdServerConfig{
			Enabled: true, Port: 24554, BinaryPath: "bin/binkd", LogLevel: 4, ExportSecs: 300,
		},
	}
}

func TestNewPreflightOK(t *testing.T) {
	root := newTestRoot(t)
	svc, err := New(Config{BBSRoot: root, FTN: testFTNConfig()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestNewPreflightMissingBinary(t *testing.T) {
	root := newTestRoot(t)
	if err := os.Remove(filepath.Join(root, "bin", "binkd")); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{BBSRoot: root, FTN: testFTNConfig()}); err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestNewPreflightMissingConf(t *testing.T) {
	root := newTestRoot(t)
	if err := os.Remove(filepath.Join(root, "data", "ftn", "binkd.conf")); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{BBSRoot: root, FTN: testFTNConfig()}); err == nil {
		t.Fatal("expected error for missing binkd.conf")
	}
}

func TestNewPreflightUnreadableConf(t *testing.T) {
	// A conf that exists but cannot be read is a different failure from a
	// missing one: it must not be misreported as "run the FTN Setup Wizard".
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission-based test; skipped on windows and as root")
	}
	root := newTestRoot(t)
	confPath := filepath.Join(root, "data", "ftn", "binkd.conf")
	if err := os.Chmod(confPath, 0000); err != nil {
		t.Fatal(err)
	}
	_, err := New(Config{BBSRoot: root, FTN: testFTNConfig()})
	if err == nil {
		t.Fatal("expected error for unreadable binkd.conf")
	}
	if strings.Contains(err.Error(), "Setup Wizard") {
		t.Fatalf("permission error must not be reported as a missing conf: %v", err)
	}
}

func TestNewPreflightPlaceholderConf(t *testing.T) {
	// A binkd.conf that is still the raw shipped template (placeholder
	// /opt/vision3 paths, never rewritten by the FTN Setup Wizard) makes
	// binkd exit 1 in a restart loop. New must refuse it up front with a
	// clear error instead.
	root := newTestRoot(t)
	conf := "log /opt/vision3/data/logs/binkd.log\ninbound /opt/vision3/data/ftn/secure_in\niport 24554\n"
	if err := os.WriteFile(filepath.Join(root, "data", "ftn", "binkd.conf"), []byte(conf), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{BBSRoot: root, FTN: testFTNConfig()}); err == nil {
		t.Fatal("expected error for template-placeholder binkd.conf")
	}
}

func TestNewPreflightNoAddresses(t *testing.T) {
	root := newTestRoot(t)
	cfg := testFTNConfig()
	cfg.Networks = map[string]config.FTNNetworkConfig{"fsxnet": {}}
	if _, err := New(Config{BBSRoot: root, FTN: cfg}); err == nil {
		t.Fatal("expected error when no network has an own address")
	}
}

func TestNewPreflightBadPort(t *testing.T) {
	root := newTestRoot(t)
	cfg := testFTNConfig()
	cfg.Binkd.Port = 99999
	if _, err := New(Config{BBSRoot: root, FTN: cfg}); err == nil {
		t.Fatal("expected error for out-of-range port")
	}
}

func TestExportLoopDisabledWithoutDeps(t *testing.T) {
	// With no MsgMgr the export loop must return immediately, not tick.
	root := newTestRoot(t)
	svc, err := New(Config{BBSRoot: root, FTN: testFTNConfig()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	done := make(chan struct{})
	go func() { svc.exportLoop(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("exportLoop must return immediately when deps are nil")
	}
}

func TestCloseBeforeStart(t *testing.T) {
	// Close must return promptly even if Start is never called.
	root := newTestRoot(t)
	svc, err := New(Config{BBSRoot: root, FTN: testFTNConfig()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- svc.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close before Start: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close before Start must return promptly")
	}
}

func TestNewValidatesFTNPathsForExport(t *testing.T) {
	// A tosser-enabled network with blank global paths (inbound/outbound/
	// binkd_outbound/temp) must not fail New (binkd can still serve inbound),
	// but export must be disabled up front with one warning instead of
	// repeated tosser init/scan errors every cycle.
	root := newTestRoot(t)
	cfg := testFTNConfig() // InternalTosserEnabled: true, OwnAddress set, all global paths blank
	svc, err := New(Config{BBSRoot: root, FTN: cfg})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !svc.exportDisabled {
		t.Fatal("expected exportDisabled to be true for invalid FTN path config")
	}

	// Give MsgMgr a non-nil sentinel: exportDisabled must be checked first,
	// so exportOnce (and thus MsgMgr) is never reached/used.
	svc.cfg.MsgMgr = &message.MessageManager{}

	done := make(chan struct{})
	go func() { svc.exportLoop(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("exportLoop must return immediately when exportDisabled is set")
	}
}

func TestNewPreflightAbsoluteBinaryPath(t *testing.T) {
	root := newTestRoot(t)
	cfg := testFTNConfig()
	cfg.Binkd.BinaryPath = filepath.Join(root, "bin", "binkd") // absolute
	if _, err := New(Config{BBSRoot: root, FTN: cfg}); err != nil {
		t.Fatalf("absolute binary path must work: %v", err)
	}
}
