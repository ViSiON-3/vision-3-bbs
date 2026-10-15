package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// UpdateV3NetConfig edits the file as it is now: a change written since the
// caller loaded it survives.
func TestUpdateV3NetConfigKeepsOtherChanges(t *testing.T) {
	dir := t.TempDir()
	if err := SaveV3NetConfig(dir, V3NetConfig{Enabled: true, Leaves: []V3NetLeafConfig{{Network: "a", Boards: []string{"X"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := UpdateV3NetConfig(dir, func(c *V3NetConfig) error {
		c.Leaves = append(c.Leaves, V3NetLeafConfig{Network: "b"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadV3NetConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || len(got.Leaves) != 2 {
		t.Errorf("got %+v", got)
	}

	path := filepath.Join(dir, "v3net.json")
	before, _ := os.Stat(path)
	err = UpdateV3NetConfig(dir, func(*V3NetConfig) error { return ErrNoChange })
	if !errors.Is(err, ErrNoChange) {
		t.Errorf("err = %v, want ErrNoChange", err)
	}
	after, _ := os.Stat(path)
	if !os.SameFile(before, after) {
		t.Error("ErrNoChange still rewrote the file")
	}
}

func TestUpdateServerConfigChangesOneSetting(t *testing.T) {
	dir := t.TempDir()
	cfg := defaultServerConfig()
	cfg.BoardName = "Disk Board"
	if err := SaveServerConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateServerConfig(dir, func(c *ServerConfig) error {
		c.AllowNewUsers = !c.AllowNewUsers
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadServerConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.BoardName != "Disk Board" || got.AllowNewUsers == cfg.AllowNewUsers {
		t.Errorf("board=%q allowNew=%v", got.BoardName, got.AllowNewUsers)
	}
}
