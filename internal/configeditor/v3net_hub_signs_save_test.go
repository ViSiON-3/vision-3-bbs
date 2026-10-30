package configeditor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// Saving Hub Signs at N when v3net.json already holds N (the leaf's write
// of it failed) raises that leaf's hubSignedClears and no other's, even with
// another change in the same save, which is what tells a running BBS the leaf
// should drop the record it could not write. A save that does not set Hub
// Signs leaves an unchanged v3net.json untouched.
func TestSaveHubSignsRaisesClears(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configDir, "v3net.json")
	cfg := config.V3NetConfig{
		Enabled:      true,
		KeystorePath: filepath.Join(root, "data", "v3net.key"),
		Leaves: []config.V3NetLeafConfig{
			{HubURL: "http://hub.example", Network: "testnet", Boards: []string{"test.general"}},
			{HubURL: "http://hub.example", Network: "othernet", Boards: []string{"other.general"}},
		},
	}
	if err := config.SaveV3NetConfig(configDir, cfg); err != nil {
		t.Fatal(err)
	}
	m, err := New(configDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	// A save of something else leaves the unchanged file alone.
	m.dirty = true
	if !m.saveAll() {
		t.Fatalf("saveAll: %s", m.message)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Fatal("a save that did not set Hub Signs moved v3net.json's timestamp")
	}

	m.recordEditIdx = 0
	f := findField(t, m.fieldsV3NetLeaf(), "Hub Signs")
	m.textInput.SetValue("N")
	if err := m.applyRecordFieldValue(*f); err != nil {
		t.Fatalf("set Hub Signs: %v", err)
	}
	if !m.dirty {
		t.Fatal("setting Hub Signs did not mark the session dirty")
	}
	m.configs.V3Net.Leaves[1].Origin = "another change"
	if !m.saveAll() {
		t.Fatalf("saveAll: %s", m.message)
	}
	got, err := config.LoadV3NetConfig(configDir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Leaves[0].HubSignedClears != 1 || got.Leaves[1].HubSignedClears != 0 {
		t.Errorf("clears = %d and %d, want 1 and 0", got.Leaves[0].HubSignedClears, got.Leaves[1].HubSignedClears)
	}
	if got.Leaves[1].Origin != "another change" {
		t.Errorf("origin = %q; the other change was not saved", got.Leaves[1].Origin)
	}
}
