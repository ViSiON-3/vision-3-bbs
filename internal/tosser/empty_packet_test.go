package tosser

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// A zero-byte packet holds no mail. It must be deleted quietly rather than
// reported as a parse error and moved to temp_path, where it looked like lost
// mail to sysops.
func TestTossRemovesEmptyPacket(t *testing.T) {
	env := setupTestEnv(t)
	tsr, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	pktPath := filepath.Join(env.inboundDir, "08ad59a1.pkt")
	if err := os.WriteFile(pktPath, nil, 0644); err != nil {
		t.Fatal(err)
	}

	result := tsr.ProcessInbound()
	if len(result.Errors) != 0 {
		t.Errorf("empty packet reported errors: %v", result.Errors)
	}
	if _, err := os.Stat(pktPath); !os.IsNotExist(err) {
		t.Error("empty packet should be removed from inbound")
	}
	if _, err := os.Stat(filepath.Join(env.tempDir, "08ad59a1.pkt")); !os.IsNotExist(err) {
		t.Error("empty packet should not be moved to temp_path")
	}
}

// An empty packet inside a bundle is dropped while the bundle's real packets
// still import, and the bundle is removed as fully processed.
func TestTossBundleWithEmptyPacket(t *testing.T) {
	env := setupTestEnv(t)
	tsr, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	bundlePath := filepath.Join(env.inboundDir, "0004009e.mo1")
	f, err := os.Create(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	if _, err := zw.Create("empty.pkt"); err != nil {
		t.Fatal(err)
	}
	w, err := zw.Create("real.pkt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(makePktSimple(t, "FSX_TEST", "Sender", "All", "Real", "real mail\r", "21:4/100 E4E40001")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	result := tsr.ProcessInbound()
	if result.MessagesImported != 1 || len(result.Errors) != 0 {
		t.Fatalf("imported %d, errors %v; want 1 imported and no errors", result.MessagesImported, result.Errors)
	}
	if _, err := os.Stat(bundlePath); !os.IsNotExist(err) {
		t.Error("processed bundle should be removed from inbound")
	}
	if _, err := os.Stat(filepath.Join(env.tempDir, "empty.pkt")); !os.IsNotExist(err) {
		t.Error("empty packet from the bundle should not be moved to temp_path")
	}
}
