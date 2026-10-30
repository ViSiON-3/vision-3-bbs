package v3net_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	v3net "github.com/ViSiON-3/vision-3-bbs/internal/v3net"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/leaf"
)

// unsavedFixture is a service with two leaves, built from a real v3net.json,
// whose first leaf holds a hubSigned record and a coordinator pin it has not
// stored.
type unsavedFixture struct {
	t    *testing.T
	svc  *v3net.Service
	mm   *message.MessageManager
	dir  string
	cfgs []config.V3NetLeafConfig
}

const unsavedPin = "pin-not-stored"

func newUnsavedFixture(t *testing.T) *unsavedFixture {
	t.Helper()
	svc, mm := newReloadFixture(t, "ALPHA", "BETA", "GAMMA")
	f := &unsavedFixture{
		t: t, svc: svc, mm: mm,
		dir:  filepath.Dir(svc.ConfigPath()),
		cfgs: []config.V3NetLeafConfig{leafCfg("net-a", "ALPHA"), leafCfg("net-b", "BETA")},
	}
	f.write()
	svc.ConfigureLeaves(f.cfgs, mm)
	f.leaf().RestoreUnsaved(leaf.UnsavedState{Coord: unsavedPin, HubSigned: true})
	return f
}

// leaf returns the current leaf for net-a.
func (f *unsavedFixture) leaf() *leaf.Leaf {
	f.t.Helper()
	for _, l := range f.svc.Leaves() {
		if l.Network() == "net-a" {
			return l
		}
	}
	f.t.Fatal("no leaf for net-a")
	return nil
}

// write saves f.cfgs to v3net.json.
func (f *unsavedFixture) write() {
	f.t.Helper()
	if err := config.SaveV3NetConfig(f.dir, config.V3NetConfig{Leaves: f.cfgs}); err != nil {
		f.t.Fatal(err)
	}
}

// reload hands the service the leaves as the file now holds them.
func (f *unsavedFixture) reload() {
	f.t.Helper()
	fresh, err := config.LoadV3NetConfig(f.dir)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.svc.ReloadLeaves(fresh.Leaves, f.mm, nil); err != nil {
		f.t.Fatal(err)
	}
}

func (f *unsavedFixture) wantKept(when string) {
	f.t.Helper()
	got := f.leaf().UnsavedState()
	if got.Coord != unsavedPin || !got.HubSigned || !f.leaf().HubSigned() || f.leaf().PinnedKey(leaf.KeyCoordinator) != unsavedPin {
		f.t.Errorf("%s: unsaved %+v, hubSigned=%v, pin %q; want the record and pin kept unsaved",
			when, got, f.leaf().HubSigned(), f.leaf().PinnedKey(leaf.KeyCoordinator))
	}
}

// An edit to another leaf's keys or to the hub settings, applied in place,
// leaves what a leaf has not stored alone (#798).
func TestReloadKeepsUnsavedOnInPlaceChange(t *testing.T) {
	f := newUnsavedFixture(t)
	first := f.leaf()

	f.cfgs[1].HubPubKey = "other-leaf-key"
	f.write()
	f.reload()
	if f.leaf() != first {
		t.Fatal("a keys-only change rebuilt the leaf")
	}
	f.wantKept("another leaf's key changed")

	if err := config.SaveV3NetConfig(f.dir, config.V3NetConfig{
		Leaves: f.cfgs, Hub: config.V3NetHubConfig{Port: 9999},
	}); err != nil {
		t.Fatal(err)
	}
	f.reload()
	if f.leaf() != first {
		t.Fatal("a hub settings change rebuilt the leaf")
	}
	f.wantKept("the hub settings changed")
}

// An edit that rebuilds the leaves, such as another leaf's boards, carries
// what they have not stored into their replacements (#798).
func TestReloadKeepsUnsavedAcrossRebuild(t *testing.T) {
	f := newUnsavedFixture(t)
	first := f.leaf()

	f.cfgs[1].Boards = []string{"GAMMA"}
	f.write()
	f.reload()
	if f.leaf() == first {
		t.Fatal("a boards change did not rebuild the leaf")
	}
	f.wantKept("another leaf's boards changed")

	// Nothing unsaved is invented for the leaf that had none.
	for _, l := range f.svc.Leaves() {
		if l.Network() == "net-b" && l.UnsavedState() != (leaf.UnsavedState{}) {
			t.Errorf("net-b unsaved %+v, want none", l.UnsavedState())
		}
	}
}

// The same reload that leaves the rest alone replaces the field the sysop
// edited, whether the leaf is rebuilt or not.
func TestReloadReplacesEditedUnsavedPin(t *testing.T) {
	for _, rebuild := range []bool{false, true} {
		f := newUnsavedFixture(t)
		f.cfgs[0].CoordinatorPubKey = "sysop-key"
		if rebuild {
			f.cfgs[1].Boards = []string{"GAMMA"}
		}
		f.write()
		f.reload()
		l := f.leaf()
		if got := l.PinnedKey(leaf.KeyCoordinator); got != "sysop-key" {
			t.Errorf("rebuild=%v: pin %q, want the sysop's key", rebuild, got)
		}
		if got := l.UnsavedState(); got.Coord != "" || !got.HubSigned {
			t.Errorf("rebuild=%v: unsaved %+v, want the pin replaced and the record kept", rebuild, got)
		}
	}
}

// The sysop setting Hub Signs to the N the file holds raises hubSignedClears.
// That drops the record, not the pin, whether the leaf is rebuilt or not, and
// whatever else the same save changed (#804).
func TestReloadOfRaisedClearsDropsUnsavedHubSigned(t *testing.T) {
	cases := []struct {
		name    string
		rebuild bool
		other   func(f *unsavedFixture)
	}{
		{name: "alone"},
		{name: "with another leaf's key", other: func(f *unsavedFixture) { f.cfgs[1].HubPubKey = "other-leaf-key" }},
		{name: "with another leaf's boards", rebuild: true, other: func(f *unsavedFixture) { f.cfgs[1].Boards = []string{"GAMMA"} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newUnsavedFixture(t)
			first := f.leaf()
			f.cfgs[0].HubSignedClears++
			if c.other != nil {
				c.other(f)
			}
			f.write()
			f.reload()
			if rebuilt := f.leaf() != first; rebuilt != c.rebuild {
				t.Fatalf("rebuilt = %v, want %v", rebuilt, c.rebuild)
			}
			got := f.leaf().UnsavedState()
			if got.HubSigned || f.leaf().HubSigned() {
				t.Errorf("unsaved %+v, hubSigned=%v; want the record dropped", got, f.leaf().HubSigned())
			}
			if got.Coord != unsavedPin {
				t.Errorf("unsaved %+v; want the pin kept", got)
			}
		})
	}
}

// A clear still reaches the leaf when it was saved while another change was
// queued, so the leaf first sees both in one reload, and later reloads of the
// same count are not clears again (#804).
func TestReloadOfQueuedClearsDropsOnce(t *testing.T) {
	f := newUnsavedFixture(t)
	f.cfgs[0].HubSignedClears = 1
	f.write()
	f.cfgs[1].HubPubKey = "queued-key"
	f.write()
	f.reload()
	if f.leaf().HubSigned() {
		t.Fatal("the clear saved before a queued change did not drop the record")
	}

	f.leaf().RestoreUnsaved(leaf.UnsavedState{HubSigned: true})
	f.reload()
	if !f.leaf().HubSigned() {
		t.Error("a second reload of the same count dropped a new record")
	}
}

// A clear belongs to one leaf: another leaf holding a record keeps it (#804).
func TestRaisedClearsDropsOnlyThatLeaf(t *testing.T) {
	f := newUnsavedFixture(t)
	f.cfgs[1].HubSignedClears = 1
	f.write()
	f.reload()
	f.wantKept("another leaf's Hub Signs was cleared")
}

// A reload without a raised count says nothing about the record, however the
// file's timestamp moved (#804).
func TestReloadWithoutRaisedClearsKeepsUnsaved(t *testing.T) {
	f := newUnsavedFixture(t)
	f.reload()
	f.wantKept("nothing changed")
	f.write() // the same contents, with a new timestamp
	f.reload()
	f.wantKept("the file was saved as it was")
}

// A reload carrying the pin the leaf itself stored, while a later handover
// pin waits to be stored, keeps that pin, rebuilt or not; a sysop's edit, even
// to a key the leaf passed through earlier, replaces it (#805).
func TestReloadOfOwnStoredPin(t *testing.T) {
	const earlier, stored, sysop = "key-earlier", "key-stored", "key-sysop"
	cases := []struct {
		name string
		file string
		kept bool
	}{
		{"the leaf's own store", stored, true},
		{"an edit to another key", sysop, false},
		{"an edit to a key the leaf passed through", earlier, false},
		{"a blanked pin", "", false},
	}
	for _, rebuild := range []bool{false, true} {
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				f := newUnsavedFixture(t)
				// The leaf has applied a copy older than its own store.
				f.cfgs[0].CoordinatorPubKey = "key-applied"
				f.write()
				f.reload()
				f.leaf().RestoreUnsaved(leaf.UnsavedState{Coord: unsavedPin, CoordMovedFrom: stored, HubSigned: true})

				f.cfgs[0].CoordinatorPubKey = c.file
				if rebuild {
					f.cfgs[1].Boards = []string{"GAMMA"}
				}
				f.write()
				f.reload()

				got := f.leaf().UnsavedState()
				if c.kept {
					f.wantKept("the leaf's own store reached the reload")
					if got.CoordMovedFrom != stored {
						t.Errorf("rebuild=%v: unsaved %+v, want it still moving from the stored pin", rebuild, got)
					}
					return
				}
				if pin := f.leaf().PinnedKey(leaf.KeyCoordinator); pin == unsavedPin || got.Coord != "" {
					t.Errorf("rebuild=%v: pin %q, unsaved %+v; want the sysop's edit in force", rebuild, pin, got)
				}
			})
		}
	}
}

// A reload that read the file before the leaf's store, and applies after, does
// not install the older pin, rebuilt or not; an edit written after that read
// wins, even one back to an earlier key (#817).
func TestReloadOfCopyReadBeforeStore(t *testing.T) {
	const older, stored, sysop = "key-older", "key-stored", "key-sysop"
	for _, rebuild := range []bool{false, true} {
		cases := []struct {
			name      string
			file      string // what the file holds when the reload applies
			pin       string // the pin the leaf then has
			pending   string
			copyHolds string // the pin in the copy the reload read
		}{
			{name: "the leaf's store", file: stored, pin: stored, copyHolds: older},
			{name: "the first-use store", file: stored, pin: stored, copyHolds: ""},
			{name: "an edit since the read", file: sysop, pin: sysop, pending: "pending-sysop", copyHolds: older},
			{name: "an edit back since the read", file: older, pin: older, copyHolds: stored},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				f := newUnsavedFixture(t)
				f.cfgs[0].CoordinatorPubKey = c.copyHolds
				f.cfgs[0].PendingCoordinatorPubKey = "pending-in-the-copy"
				copyOfFile := slices.Clone(f.cfgs)

				// The leaf has stored its pin and applied that reload.
				f.cfgs[0].CoordinatorPubKey = stored
				f.cfgs[0].PendingCoordinatorPubKey = ""
				f.write()
				f.reload()
				if got := f.leaf().PinnedKey(leaf.KeyCoordinator); got != stored {
					t.Fatalf("pin %q before the late reload, want %q", got, stored)
				}

				f.cfgs[0].CoordinatorPubKey = c.file
				f.cfgs[0].PendingCoordinatorPubKey = c.pending
				f.write()
				if rebuild {
					copyOfFile[1].Boards = []string{"GAMMA"}
				}
				first := f.leaf()
				if err := f.svc.ReloadLeaves(copyOfFile, f.mm, nil); err != nil {
					t.Fatal(err)
				}
				if rebuilt := f.leaf() != first; rebuilt != rebuild {
					t.Fatalf("rebuilt = %v, want %v", rebuilt, rebuild)
				}
				if got := f.leaf().PinnedKey(leaf.KeyCoordinator); got != c.pin {
					t.Errorf("rebuild=%v: pin %q, want %q", rebuild, got, c.pin)
				}
				if got := f.leaf().PendingKey(leaf.KeyCoordinator); got != c.pending {
					t.Errorf("rebuild=%v: pending %q, want %q", rebuild, got, c.pending)
				}
				if got := f.leaf().UnsavedState().Coord; got != "" {
					t.Errorf("rebuild=%v: unsaved pin %q, want none", rebuild, got)
				}
			})
		}
	}
}

// After a late reload kept the pin the file held, a sysop's edit back to the
// late copy's pin is still an edit, rebuilt or not (#817).
func TestReloadEditBackToLateCopysPin(t *testing.T) {
	const older, stored = "key-older", "key-stored"
	for _, rebuild := range []bool{false, true} {
		f := newUnsavedFixture(t)
		f.cfgs[0].CoordinatorPubKey = older
		copyOfFile := slices.Clone(f.cfgs)
		f.cfgs[0].CoordinatorPubKey = stored
		f.write()
		f.reload()

		if rebuild {
			copyOfFile[1].Boards = []string{"GAMMA"}
			f.cfgs[1].Boards = []string{"GAMMA"}
			f.write()
		}
		if err := f.svc.ReloadLeaves(copyOfFile, f.mm, nil); err != nil {
			t.Fatal(err)
		}
		f.leaf().RestoreUnsaved(leaf.UnsavedState{Coord: unsavedPin, CoordMovedFrom: stored})

		f.cfgs[0].CoordinatorPubKey = older
		f.write()
		f.reload()
		if got := f.leaf().PinnedKey(leaf.KeyCoordinator); got != older {
			t.Errorf("rebuild=%v: pin %q, want the sysop's %q", rebuild, got, older)
		}
		if got := f.leaf().UnsavedState().Coord; got != "" {
			t.Errorf("rebuild=%v: unsaved pin %q, want it replaced", rebuild, got)
		}
	}
}
