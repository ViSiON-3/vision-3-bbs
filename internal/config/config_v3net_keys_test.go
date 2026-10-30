package config

import (
	"reflect"
	"slices"
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

// The BBS recording that the old hub signs is per-hub state too, so it
// does not keep the old leaf and does not carry over to the new hub.
func TestV3NetSaveHubURLChangeBeatsHubSigned(t *testing.T) {
	leaves, conflicts := saveV3NetHubRace(t, hubMoveBase(), func(c *V3NetConfig) {
		c.Leaves[0].HubSigned = true
	}, moveHub)
	if len(conflicts) != 0 {
		t.Errorf("conflicts = %+v", conflicts)
	}
	if len(leaves) != 2 || leaves[0].HubURL != "https://new.example" || leaves[1].Network != "netb" {
		t.Fatalf("leaves = %+v, want neta at the new hub and netb", leaves)
	}
	if leaves[0].HubSigned {
		t.Errorf("neta = %+v; hubSigned for the old hub must not carry over", leaves[0])
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

// Leaves sharing a network and hub URL merge as a single list, so a hub
// move must not touch them first: here it would set both of this side's
// unchanged duplicates to one of them, which reads as an edit and brings
// the old leaves back over the move made on disk.
func TestV3NetSaveHubMoveLeavesDuplicateLeavesAlone(t *testing.T) {
	base := V3NetConfig{Leaves: []V3NetLeafConfig{
		{HubURL: "https://old.example", Network: "neta", CoordinatorPubKey: "A"},
		{HubURL: "https://old.example", Network: "neta", CoordinatorPubKey: "B"},
	}}
	leaves, conflicts := saveV3NetHubRace(t, base, func(c *V3NetConfig) {
		c.Leaves = []V3NetLeafConfig{{HubURL: "https://new.example", Network: "neta", CoordinatorPubKey: "A"}}
	}, func(*V3NetConfig) {})
	if len(conflicts) != 0 {
		t.Errorf("conflicts = %+v", conflicts)
	}
	if len(leaves) != 1 || leaves[0].HubURL != "https://new.example" {
		t.Errorf("leaves = %+v, want the move made on disk", leaves)
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

// Two editor sessions move one network to different hubs: a hub URL
// changed on both sides, so this save's hub is kept, the other move is
// reported, the network is listed once, and the other session's other edits
// survive. Its hub state does not, being about a hub this leaf does not use
// (#791, #796).
func TestV3NetSaveHubMovedBothSides(t *testing.T) {
	leaves, conflicts := saveV3NetHubRace(t, hubMoveBase(), func(c *V3NetConfig) {
		c.Leaves[0].HubURL = "https://other.example"
		c.Leaves[0].Boards = []string{"A", "A2"}
		c.Leaves[0].PollInterval = "9m"
		c.Leaves[0].RequireSignedHub = true
		c.Leaves[0].CoordinatorPubKey = "other-c"
		c.Leaves[0].HubPubKey = "other-h"
		c.Leaves[0].PendingHubPubKey = "other-p"
		c.Leaves[0].HubSigned = true
	}, func(c *V3NetConfig) {
		moveHub(c)
		c.Leaves[0].Origin = "ours"
	})
	if len(leaves) != 2 || leaves[0].HubURL != "https://new.example" || leaves[1].Network != "netb" {
		t.Fatalf("leaves = %+v, want neta at this save's hub and netb", leaves)
	}
	l := leaves[0]
	if !slices.Equal(l.Boards, []string{"A", "A2"}) || l.PollInterval != "9m" || !l.RequireSignedHub || l.Origin != "ours" {
		t.Errorf("neta = %+v; want both sessions' edits", l)
	}
	if l.CoordinatorPubKey != "C" || l.HubPubKey != "" || l.PendingHubPubKey != "" || l.HubSigned {
		t.Errorf("neta = %+v; want the other hub's state left out", l)
	}
	if len(conflicts) != 1 || conflicts[0].Field != "hubUrl" ||
		!strings.Contains(conflicts[0].What, "https://other.example") {
		t.Errorf("conflicts = %+v, want one naming the other session's hub", conflicts)
	}
}

// Beside the hub URL, a field the two sessions changed differently keeps
// this save's value and is reported, as in a leaf that did not move. A blank
// here is a change like any other, and the same change on both sides is no
// conflict (#796).
func TestV3NetSaveHubMovedBothSidesFieldConflicts(t *testing.T) {
	base := hubMoveBase()
	base.Leaves[0].Origin = "base"
	base.Leaves[0].PollInterval = "5m"
	leaves, conflicts := saveV3NetHubRace(t, base, func(c *V3NetConfig) {
		c.Leaves[0].HubURL = "https://other.example"
		c.Leaves[0].Boards = []string{"THEIRS"}
		c.Leaves[0].Origin = "theirs"
		c.Leaves[0].PollInterval = "9m"
	}, func(c *V3NetConfig) {
		moveHub(c)
		c.Leaves[0].Boards = []string{"OURS"}
		c.Leaves[0].Origin = ""
		c.Leaves[0].PollInterval = "9m"
	})
	if len(leaves) != 2 {
		t.Fatalf("leaves = %+v", leaves)
	}
	l := leaves[0]
	if l.HubURL != "https://new.example" || !slices.Equal(l.Boards, []string{"OURS"}) || l.Origin != "" || l.PollInterval != "9m" {
		t.Errorf("neta = %+v; want this save's values", l)
	}
	var fields []string
	for _, c := range conflicts {
		fields = append(fields, c.Field)
	}
	if want := []string{"hubUrl", "boards", "origin"}; !slices.Equal(fields, want) {
		t.Errorf("conflict fields = %v, want %v (%+v)", fields, want, conflicts)
	}
}

// SaveFile still compares the result with theirs as read, so settling must
// leave the versions it is passed as they were.
func TestV3NetHubMovedBothSidesKeepsInputs(t *testing.T) {
	base := hubMoveBase()
	mk := func(hub, poll string) V3NetConfig {
		c, err := configmerge.Clone(base)
		if err != nil {
			t.Fatal(err)
		}
		c.Leaves[0].HubURL = hub
		c.Leaves[0].PollInterval = poll
		return c
	}
	ours, theirs := mk("https://new.example", ""), mk("https://other.example", "9m")
	wantOurs, wantTheirs := mk("https://new.example", ""), mk("https://other.example", "9m")
	gotOurs, gotTheirs, _ := settleV3NetHubMoves(base, ours, theirs)
	if !reflect.DeepEqual(ours, wantOurs) || !reflect.DeepEqual(theirs, wantTheirs) {
		t.Errorf("inputs modified: ours = %+v, theirs = %+v", ours, theirs)
	}
	if gotOurs.Leaves[0].PollInterval != "9m" || len(gotTheirs.Leaves) != 1 {
		t.Errorf("ours = %+v, theirs = %+v; want the edit moved into ours and the leaf out of theirs", gotOurs, gotTheirs)
	}
}

// v3netLeafOnHubOf copies exactly the hub URL and the hub state.
func TestV3NetLeafOnHubOf(t *testing.T) {
	l := V3NetLeafConfig{HubURL: "l", Network: "n", Origin: "o", CoordinatorPubKey: "l", HubSigned: true}
	got := v3netLeafOnHubOf(l, V3NetLeafConfig{HubURL: "h", Network: "x", Origin: "x", HubPubKey: "h"})
	want := V3NetLeafConfig{HubURL: "h", Network: "n", Origin: "o", HubPubKey: "h"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// Both sessions moving the network to the same hub agree, so there is one
// leaf and nothing to report.
func TestV3NetSaveHubMovedBothSidesSameHub(t *testing.T) {
	leaves, conflicts := saveV3NetHubRace(t, hubMoveBase(), moveHub, moveHub)
	if len(conflicts) != 0 {
		t.Errorf("conflicts = %+v", conflicts)
	}
	if len(leaves) != 2 || leaves[0].HubURL != "https://new.example" || leaves[1].Network != "netb" {
		t.Errorf("leaves = %+v, want neta at the new hub and netb", leaves)
	}
}

// A second leaf for a network is a subscription through a second hub. When
// one side moves the network and the other adds such a leaf, the sides did
// not change the same thing: the move and the new subscription are both
// kept, unreported, whichever side made which.
func TestV3NetSaveHubMoveBesideSecondHub(t *testing.T) {
	addSecond := func(c *V3NetConfig) {
		c.Leaves = append(c.Leaves, V3NetLeafConfig{HubURL: "https://second.example", Network: "neta"})
	}
	hubs := func(leaves []V3NetLeafConfig) []string {
		var out []string
		for _, l := range leaves {
			if l.Network == "neta" {
				out = append(out, l.HubURL)
			}
		}
		slices.Sort(out)
		return out
	}
	want := []string{"https://new.example", "https://second.example"}
	for _, tc := range []struct {
		name       string
		disk, edit func(*V3NetConfig)
	}{
		{"moved here", addSecond, moveHub},
		{"moved elsewhere", moveHub, addSecond},
	} {
		leaves, conflicts := saveV3NetHubRace(t, hubMoveBase(), tc.disk, tc.edit)
		if len(conflicts) != 0 {
			t.Errorf("%s: conflicts = %+v", tc.name, conflicts)
		}
		if got := hubs(leaves); !slices.Equal(got, want) {
			t.Errorf("%s: neta hubs = %v, want %v", tc.name, got, want)
		}
	}
}
