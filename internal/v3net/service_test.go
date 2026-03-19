package v3net_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
)

func TestHubAutoInit_DataDirCreated(t *testing.T) {
	dir := t.TempDir()
	hubDataDir := filepath.Join(dir, "data", "v3net_hub")
	keystorePath := filepath.Join(dir, "v3net.key")

	if _, _, err := keystore.Load(keystorePath); err != nil {
		t.Fatal(err)
	}

	cfg := config.V3NetConfig{
		Enabled:      true,
		KeystorePath: keystorePath,
		DedupDBPath:  filepath.Join(dir, "dedup.sqlite"),
		ConfigPath:   dir,
		Hub: config.V3NetHubConfig{
			Enabled:  true,
			DataDir:  hubDataDir,
			Networks: []config.V3NetHubNetwork{{Name: "testnet"}},
		},
	}

	svc, err := v3net.New(cfg)
	if err != nil {
		t.Fatalf("v3net.New: %v", err)
	}
	defer svc.Close()

	if _, err := os.Stat(hubDataDir); os.IsNotExist(err) {
		t.Error("expected hub data dir to be created, but it does not exist")
	}
}

func TestHubAutoInit_SelfRegistered(t *testing.T) {
	dir := t.TempDir()
	keystorePath := filepath.Join(dir, "v3net.key")

	ks, _, err := keystore.Load(keystorePath)
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.V3NetConfig{
		Enabled:      true,
		KeystorePath: keystorePath,
		DedupDBPath:  filepath.Join(dir, "dedup.sqlite"),
		ConfigPath:   dir,
		Hub: config.V3NetHubConfig{
			Enabled:  true,
			DataDir:  dir,
			Networks: []config.V3NetHubNetwork{{Name: "testnet"}},
		},
	}

	svc, err := v3net.New(cfg)
	if err != nil {
		t.Fatalf("v3net.New: %v", err)
	}
	defer svc.Close()

	// The hub's own node must be an active subscriber.
	sub := svc.Hub().Subscribers().Get(ks.NodeID(), "testnet")
	if sub == nil {
		t.Fatal("expected hub node to be registered as subscriber, got nil")
	}
	if sub.Status != "active" {
		t.Errorf("expected status=active, got %q", sub.Status)
	}
}

func TestHubAutoInit_NALSeeded(t *testing.T) {
	dir := t.TempDir()
	keystorePath := filepath.Join(dir, "v3net.key")

	if _, _, err := keystore.Load(keystorePath); err != nil {
		t.Fatal(err)
	}

	cfg := config.V3NetConfig{
		Enabled:      true,
		KeystorePath: keystorePath,
		DedupDBPath:  filepath.Join(dir, "dedup.sqlite"),
		ConfigPath:   dir,
		Hub: config.V3NetHubConfig{
			Enabled: true,
			DataDir: dir,
			Networks: []config.V3NetHubNetwork{{Name: "testnet"}},
			InitialAreas: []config.V3NetHubArea{
				{Tag: "test.general", Name: "General"},
			},
		},
	}

	svc, err := v3net.New(cfg)
	if err != nil {
		t.Fatalf("v3net.New: %v", err)
	}
	defer svc.Close()

	n, err := svc.Hub().NALStore().Get("testnet")
	if err != nil {
		t.Fatalf("get NAL: %v", err)
	}
	if n == nil {
		t.Fatal("expected NAL to be seeded, got nil")
	}
	if len(n.Areas) != 1 || n.Areas[0].Tag != "test.general" {
		t.Errorf("unexpected areas: %+v", n.Areas)
	}
}

func TestHubAutoInit_NALSeedIdempotent(t *testing.T) {
	dir := t.TempDir()
	keystorePath := filepath.Join(dir, "v3net.key")

	if _, _, err := keystore.Load(keystorePath); err != nil {
		t.Fatal(err)
	}

	cfg := config.V3NetConfig{
		Enabled:      true,
		KeystorePath: keystorePath,
		DedupDBPath:  filepath.Join(dir, "dedup.sqlite"),
		ConfigPath:   dir,
		Hub: config.V3NetHubConfig{
			Enabled: true,
			DataDir: dir,
			Networks: []config.V3NetHubNetwork{{Name: "testnet"}},
			InitialAreas: []config.V3NetHubArea{
				{Tag: "test.general", Name: "General"},
			},
		},
	}

	// First init — seeds NAL.
	svc1, err := v3net.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	svc1.Close()

	// Second init with same initialAreas — must not error and must not overwrite NAL.
	svc2, err := v3net.New(cfg)
	if err != nil {
		t.Fatalf("second v3net.New: %v", err)
	}
	defer svc2.Close()

	n, _ := svc2.Hub().NALStore().Get("testnet")
	if n == nil || len(n.Areas) != 1 {
		t.Error("expected original seeded NAL to remain after second init")
	}
}
