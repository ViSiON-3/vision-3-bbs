package configeditor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// Saving Hub Signs at N when v3net.json already holds N (the leaf's write
// of it failed) leaves the file's contents alone but still moves its
// timestamp, which is what makes a running BBS reload it and the leaf drop
// the record it could not write. A save that does not set Hub Signs leaves
// an unchanged v3net.json untouched.
func TestSaveHubSignsBumpsUnchangedV3NetConfig(t *testing.T) {
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
		Leaves:       []config.V3NetLeafConfig{{HubURL: "http://hub.example", Network: "testnet", Boards: []string{"test.general"}}},
	}
	if err := config.SaveV3NetConfig(configDir, cfg); err != nil {
		t.Fatal(err)
	}
	m, err := New(configDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-time.Hour)
	setOld := func() {
		t.Helper()
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	modTime := func() time.Time {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return info.ModTime()
	}

	// A save of something else leaves the unchanged file alone.
	setOld()
	m.dirty = true
	if !m.saveAll() {
		t.Fatalf("saveAll: %s", m.message)
	}
	if !modTime().Equal(old) {
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
	if !m.saveAll() {
		t.Fatalf("saveAll: %s", m.message)
	}
	if !modTime().After(old) {
		t.Error("saving Hub Signs at N did not move v3net.json's timestamp")
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != string(before) {
		t.Errorf("v3net.json rewritten (err %v):\n%s\nwant\n%s", err, after, before)
	}

	// The next save, with Hub Signs not set again, leaves it alone.
	setOld()
	m.dirty = true
	if !m.saveAll() {
		t.Fatalf("saveAll: %s", m.message)
	}
	if !modTime().Equal(old) {
		t.Error("a later save moved v3net.json's timestamp again")
	}
}
