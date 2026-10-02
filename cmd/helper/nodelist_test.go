package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The sysop's ftn_networks.json wins over the built-in registry.
func TestRegistryNodelistURL(t *testing.T) {
	dir := t.TempDir()
	if url, err := registryNodelistURL(dir, 1); err != nil || url == "" {
		t.Errorf("zone 1 from the built-in registry = %q, %v", url, err)
	}
	if _, err := registryNodelistURL(dir, 64999); err == nil {
		t.Error("a zone with no registry entry returned a URL")
	}
	override := `[{"zone": 1, "name": "FidoNet", "nodelist_url": "https://mirror.example/nodelist.zip"}]`
	if err := os.WriteFile(filepath.Join(dir, "ftn_networks.json"), []byte(override), 0644); err != nil {
		t.Fatal(err)
	}
	if url, err := registryNodelistURL(dir, 1); err != nil || url != "https://mirror.example/nodelist.zip" {
		t.Errorf("zone 1 with an override = %q, %v", url, err)
	}
}
