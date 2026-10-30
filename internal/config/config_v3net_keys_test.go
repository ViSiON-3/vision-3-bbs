package config

import (
	"reflect"
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

// Blanking a pinned key in the editor is how the sysop asks for a re-pin.
// The blank must win over a key the BBS pinned meanwhile, even though the
// omitempty field vanishes from the editor's JSON.
func TestV3NetSaveKeepsBlankedPin(t *testing.T) {
	dir := t.TempDir()
	base := V3NetConfig{Leaves: []V3NetLeafConfig{
		{HubURL: "https://a.example", Network: "neta", CoordinatorPubKey: "A", PendingHubPubKey: "P"},
	}}
	if err := SaveV3NetConfig(dir, base); err != nil {
		t.Fatal(err)
	}
	if err := UpdateV3NetConfig(dir, func(c *V3NetConfig) error {
		c.Leaves[0].CoordinatorPubKey = "B"
		c.Leaves[0].PendingHubPubKey = "Q"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	ours, err := configmerge.Clone(base)
	if err != nil {
		t.Fatal(err)
	}
	ours.Leaves[0].CoordinatorPubKey = ""
	ours.Leaves[0].PendingHubPubKey = ""
	if _, _, err := configmerge.SaveFile(V3NetConfigFile(dir), base, ours); err != nil {
		t.Fatal(err)
	}
	got, err := LoadV3NetConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Leaves) != 1 {
		t.Fatalf("leaves = %+v", got.Leaves)
	}
	if l := got.Leaves[0]; l.CoordinatorPubKey != "" || l.PendingHubPubKey != "" {
		t.Errorf("leaf = %+v; the editor's blanked keys must win", l)
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

// saveV3NetHubRace writes base, applies disk as a change made elsewhere,
// then saves ours against base as the config editor does, and returns the
// leaves on disk and the conflicts reported.
func saveV3NetHubRace(t *testing.T, base V3NetConfig, disk func(*V3NetConfig), edit func(*V3NetConfig)) ([]V3NetLeafConfig, []configmerge.Conflict) {
	t.Helper()
	dir := t.TempDir()
	if err := SaveV3NetConfig(dir, base); err != nil {
		t.Fatal(err)
	}
	if err := UpdateV3NetConfig(dir, func(c *V3NetConfig) error { disk(c); return nil }); err != nil {
		t.Fatal(err)
	}
	ours, err := configmerge.Clone(base)
	if err != nil {
		t.Fatal(err)
	}
	edit(&ours)
	_, res, err := configmerge.SaveFile(V3NetConfigFile(dir), base, ours)
	if err != nil {
		t.Fatal(err)
	}
	got, err := LoadV3NetConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	return got.Leaves, res.Conflicts
}

func hubMoveBase() V3NetConfig {
	return V3NetConfig{Leaves: []V3NetLeafConfig{
		{HubURL: "https://old.example", Network: "neta", Boards: []string{"A"}, CoordinatorPubKey: "C"},
		{HubURL: "https://b.example", Network: "netb", Boards: []string{"B"}},
	}}
}

// moveHub changes neta's hub URL as the leaf edit form does, clearing the
// hub key pinned for the old hub.
func moveHub(c *V3NetConfig) {
	c.Leaves[0].HubURL = "https://new.example"
	c.Leaves[0].HubPubKey = ""
}

// The editor changes a leaf's hub URL while the BBS pins a key on the old
// leaf: the old leaf goes, without a conflict, rather than listing the
// network twice (#765).
func TestV3NetSaveHubURLChangeBeatsKeyPin(t *testing.T) {
	leaves, conflicts := saveV3NetHubRace(t, hubMoveBase(), func(c *V3NetConfig) {
		c.Leaves[0].HubPubKey = "pinned-old"
		c.Leaves[0].PendingCoordinatorPubKey = "offered-old"
	}, moveHub)
	if len(conflicts) != 0 {
		t.Errorf("conflicts = %+v", conflicts)
	}
	if len(leaves) != 2 || leaves[0].HubURL != "https://new.example" || leaves[1].Network != "netb" {
		t.Fatalf("leaves = %+v, want neta at the new hub and netb", leaves)
	}
	if l := leaves[0]; l.HubPubKey != "" || l.PendingCoordinatorPubKey != "" || l.CoordinatorPubKey != "C" {
		t.Errorf("neta = %+v; the old hub's pins must not carry over", l)
	}
}

// An edit elsewhere to anything but the old leaf's keys is not the BBS
// pinning a key, so it is kept and reported as before.
func TestV3NetSaveHubURLChangeKeepsOtherEdits(t *testing.T) {
	leaves, conflicts := saveV3NetHubRace(t, hubMoveBase(), func(c *V3NetConfig) {
		c.Leaves[0].HubPubKey = "pinned-old"
		c.Leaves[0].Boards = []string{"A", "A2"}
	}, moveHub)
	if len(conflicts) != 1 {
		t.Errorf("conflicts = %+v, want one for the old leaf", conflicts)
	}
	if len(leaves) != 3 {
		t.Errorf("leaves = %+v, want the edited old leaf kept beside the new one", leaves)
	}
}

// Removing a leaf without adding another for its network is not a hub
// change: a key pinned on it meanwhile keeps it, reported, as before.
func TestV3NetSaveRemovedLeafKeepsKeyPin(t *testing.T) {
	leaves, conflicts := saveV3NetHubRace(t, hubMoveBase(), func(c *V3NetConfig) {
		c.Leaves[0].HubPubKey = "pinned-old"
	}, func(c *V3NetConfig) {
		c.Leaves = c.Leaves[1:]
	})
	if len(conflicts) != 1 {
		t.Errorf("conflicts = %+v, want one for the removed leaf", conflicts)
	}
	if len(leaves) != 2 || leaves[1].HubPubKey != "pinned-old" {
		t.Errorf("leaves = %+v, want the pinned leaf kept", leaves)
	}
}

// A misspelt name in v3netLeafHubState would silently stop that field
// counting as hub state.
func TestV3NetLeafHubStateNamesFields(t *testing.T) {
	tags := make(map[string]bool)
	rt := reflect.TypeOf(V3NetLeafConfig{})
	for i := 0; i < rt.NumField(); i++ {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		tags[name] = true
	}
	for _, k := range v3netLeafHubState {
		if !tags[k] {
			t.Errorf("v3netLeafHubState names %q, which is not a V3NetLeafConfig field", k)
		}
	}
}

// The reverse: another editor session moved the leaf to a new hub while
// this one changed only the old leaf's keys. The move wins the same way;
// an edit to another field of the old leaf is kept and reported.
func TestV3NetSaveHubURLChangedElsewhere(t *testing.T) {
	leaves, conflicts := saveV3NetHubRace(t, hubMoveBase(), moveHub, func(c *V3NetConfig) {
		c.Leaves[0].CoordinatorPubKey = "accepted"
	})
	if len(conflicts) != 0 || len(leaves) != 2 || leaves[0].HubURL != "https://new.example" {
		t.Errorf("leaves = %+v, conflicts = %+v; want only the moved leaf", leaves, conflicts)
	}

	leaves, conflicts = saveV3NetHubRace(t, hubMoveBase(), moveHub, func(c *V3NetConfig) {
		c.Leaves[0].PollInterval = "10m"
	})
	if len(conflicts) != 1 || len(leaves) != 3 {
		t.Errorf("leaves = %+v, conflicts = %+v; want the edited old leaf kept and reported", leaves, conflicts)
	}
}
