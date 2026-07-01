package qwkapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestLoadOrCreateCert_GeneratesAndReloads(t *testing.T) {
	dir := t.TempDir()
	cfg := config.QWKAPIConfig{Host: "127.0.0.1"}

	cert1, fp1, err := loadOrCreateCert(cfg, dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(cert1.Certificate) == 0 || fp1 == "" {
		t.Fatal("expected a generated cert and fingerprint")
	}
	// Files persisted with a locked-down key.
	keyInfo, err := os.Stat(filepath.Join(dir, "qwkapi_key.pem"))
	if err != nil {
		t.Fatalf("key file missing: %v", err)
	}
	if keyInfo.Mode().Perm() != 0o600 {
		t.Errorf("key mode = %o, want 600", keyInfo.Mode().Perm())
	}

	// Second call loads the same cert (same fingerprint).
	_, fp2, err := loadOrCreateCert(cfg, dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if fp1 != fp2 {
		t.Errorf("fingerprint changed on reload: %s != %s", fp1, fp2)
	}
}

func TestLoadOrCreateCert_ExplicitFiles(t *testing.T) {
	dir := t.TempDir()
	// Generate a pair via the auto path, then point cfg at those files.
	if _, _, err := loadOrCreateCert(config.QWKAPIConfig{Host: "127.0.0.1"}, dir); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg := config.QWKAPIConfig{
		CertFile: filepath.Join(dir, "qwkapi_cert.pem"),
		KeyFile:  filepath.Join(dir, "qwkapi_key.pem"),
	}
	if _, fp, err := loadOrCreateCert(cfg, t.TempDir()); err != nil || fp == "" {
		t.Fatalf("explicit files: fp=%q err=%v", fp, err)
	}
}

func TestLoadOrCreateCert_OneOfCertKeyFailsClosed(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := loadOrCreateCert(config.QWKAPIConfig{CertFile: "/x/cert.pem"}, dir); err == nil {
		t.Error("expected error when only CertFile is set")
	}
	if _, _, err := loadOrCreateCert(config.QWKAPIConfig{KeyFile: "/x/key.pem"}, dir); err == nil {
		t.Error("expected error when only KeyFile is set")
	}
}
