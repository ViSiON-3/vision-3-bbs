package config

import (
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/configmerge"
)

// An editor that loaded v3net.json before the BBS pinned a key, and then
// saves an unrelated subscription edit, must not erase the pin; a key field
// the editor itself changed still wins.
func TestV3NetSaveKeepsKeysPinnedMeanwhile(t *testing.T) {
	dir := t.TempDir()
	base := V3NetConfig{Leaves: []V3NetLeafConfig{
		{HubURL: "https://a.example", Network: "neta", Boards: []string{"A"}},
		{HubURL: "https://b.example", Network: "netb", Boards: []string{"B"}, CoordinatorPubKey: "old"},
	}}
	if err := SaveV3NetConfig(dir, base); err != nil {
		t.Fatal(err)
	}

	// The BBS pins neta's keys and records a pending key for netb.
	if err := UpdateV3NetConfig(dir, func(c *V3NetConfig) error {
		c.Leaves[0].CoordinatorPubKey = "pinned-a"
		c.Leaves[0].HubPubKey = "hub-a"
		c.Leaves[1].PendingCoordinatorPubKey = "offered-b"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Meanwhile the editor, still holding base, edits neta's poll interval
	// and accepts a new key for netb by pasting it in.
	ours, err := configmerge.Clone(base)
	if err != nil {
		t.Fatal(err)
	}
	ours.Leaves[0].PollInterval = "10m"
	ours.Leaves[1].CoordinatorPubKey = "accepted-b"
	ours.Leaves[1].PendingCoordinatorPubKey = ""

	if _, _, err := configmerge.SaveFile(V3NetConfigFile(dir), base, ours); err != nil {
		t.Fatal(err)
	}
	got, err := LoadV3NetConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, b := got.Leaves[0], got.Leaves[1]
	if a.PollInterval != "10m" || a.CoordinatorPubKey != "pinned-a" || a.HubPubKey != "hub-a" {
		t.Errorf("neta = %+v; want the editor's poll interval and the BBS's pins", a)
	}
	if b.CoordinatorPubKey != "accepted-b" {
		t.Errorf("netb pin = %q; the editor's accepted key must win", b.CoordinatorPubKey)
	}
	if b.PendingCoordinatorPubKey != "offered-b" {
		// The editor left pending as it was in base (""), so the disk value
		// is kept; the leaf clears it once the accepted key verifies.
		t.Errorf("netb pending = %q, want offered-b", b.PendingCoordinatorPubKey)
	}
}

// Leaves added or removed on disk while the editor holds an old copy are
// kept that way when it saves, and its own leaf edits still win (#763).
func TestV3NetSaveKeepsLeavesChangedMeanwhile(t *testing.T) {
	dir := t.TempDir()
	base := V3NetConfig{Leaves: []V3NetLeafConfig{
		{HubURL: "https://a.example", Network: "neta", Boards: []string{"A"}},
		{HubURL: "https://b.example", Network: "netb", Boards: []string{"B"}},
	}}
	if err := SaveV3NetConfig(dir, base); err != nil {
		t.Fatal(err)
	}

	// Another editor session drops netb and subscribes to netc.
	if err := UpdateV3NetConfig(dir, func(c *V3NetConfig) error {
		c.Leaves = []V3NetLeafConfig{
			c.Leaves[0],
			{HubURL: "https://c.example", Network: "netc", Boards: []string{"C"}},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// This editor, still holding base, changes neta's boards and adds netd.
	ours, err := configmerge.Clone(base)
	if err != nil {
		t.Fatal(err)
	}
	ours.Leaves[0].Boards = []string{"A", "A2"}
	ours.Leaves = append(ours.Leaves, V3NetLeafConfig{HubURL: "https://d.example", Network: "netd"})

	if _, res, err := configmerge.SaveFile(V3NetConfigFile(dir), base, ours); err != nil {
		t.Fatal(err)
	} else if len(res.Conflicts) != 0 {
		t.Errorf("conflicts = %+v", res.Conflicts)
	}
	got, err := LoadV3NetConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	var nets []string
	for _, l := range got.Leaves {
		nets = append(nets, l.Network)
	}
	if strings.Join(nets, ",") != "neta,netd,netc" {
		t.Errorf("leaves = %v, want neta, netd, netc", nets)
	}
	if b := got.Leaves[0].Boards; len(b) != 2 || b[1] != "A2" {
		t.Errorf("neta boards = %v, want the editor's", b)
	}
}
