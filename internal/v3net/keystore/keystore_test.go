package keystore

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_GeneratesNewKeypair(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")

	ks, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// File should exist with restricted permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("key file not created: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected file mode 0600, got %o", info.Mode().Perm())
	}

	// Node ID should be 16 hex chars.
	nodeID := ks.NodeID()
	if len(nodeID) != 16 {
		t.Errorf("expected 16-char node ID, got %d: %q", len(nodeID), nodeID)
	}
	if _, err := hex.DecodeString(nodeID); err != nil {
		t.Errorf("node ID is not valid hex: %q", nodeID)
	}

	// PubKeyBase64 should be non-empty.
	if ks.PubKeyBase64() == "" {
		t.Error("PubKeyBase64 is empty")
	}
}

func TestLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")

	ks1, _, err := Load(path)
	if err != nil {
		t.Fatalf("first Load failed: %v", err)
	}

	ks2, _, err := Load(path)
	if err != nil {
		t.Fatalf("second Load failed: %v", err)
	}

	if ks1.NodeID() != ks2.NodeID() {
		t.Errorf("node IDs differ: %q vs %q", ks1.NodeID(), ks2.NodeID())
	}
	if ks1.PubKeyBase64() != ks2.PubKeyBase64() {
		t.Error("public keys differ after round-trip")
	}
}

func TestNodeID_DerivedFromPubKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")

	ks, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	h := sha256.Sum256(ks.PubKey())
	expected := hex.EncodeToString(h[:8])

	if ks.NodeID() != expected {
		t.Errorf("node ID mismatch: got %q, expected %q", ks.NodeID(), expected)
	}
}

func TestSign_VerifiesCorrectly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")

	ks, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	method := "POST"
	reqPath := "/v3net/v1/felonynet/messages"
	dateUTC := "Sun, 16 Mar 2026 04:20:00 GMT"
	bodySHA := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	sig, err := ks.Sign(method, reqPath, dateUTC, bodySHA)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	if !Verify(ks.PubKey(), method, reqPath, dateUTC, bodySHA, sig) {
		t.Error("Verify returned false for valid signature")
	}
}

func TestSign_RejectsTamperedData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")

	ks, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	method := "POST"
	reqPath := "/v3net/v1/felonynet/messages"
	dateUTC := "Sun, 16 Mar 2026 04:20:00 GMT"
	bodySHA := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	sig, err := ks.Sign(method, reqPath, dateUTC, bodySHA)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	// Tamper with method.
	if Verify(ks.PubKey(), "GET", reqPath, dateUTC, bodySHA, sig) {
		t.Error("Verify should reject tampered method")
	}

	// Tamper with path.
	if Verify(ks.PubKey(), method, "/v3net/v1/other/messages", dateUTC, bodySHA, sig) {
		t.Error("Verify should reject tampered path")
	}

	// Tamper with date.
	if Verify(ks.PubKey(), method, reqPath, "Mon, 17 Mar 2026 04:20:00 GMT", bodySHA, sig) {
		t.Error("Verify should reject tampered date")
	}

	// Tamper with body hash.
	if Verify(ks.PubKey(), method, reqPath, dateUTC, "0000000000000000000000000000000000000000000000000000000000000000", sig) {
		t.Error("Verify should reject tampered body hash")
	}
}

func TestVerify_RejectsInvalidSignature(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")

	ks, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if Verify(ks.PubKey(), "GET", "/", "now", "hash", "not-valid-base64-sig!!!") {
		t.Error("Verify should reject invalid base64 signature")
	}
}

