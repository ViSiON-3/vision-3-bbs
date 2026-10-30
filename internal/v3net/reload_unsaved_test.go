package v3net_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	v3net "github.com/ViSiON-3/vision-3-bbs/internal/v3net"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/leaf"
)

// unsavedFixture is a service with two leaves, built from a real v3net.json,
// whose first leaf holds a hubSigned record and a coordinator pin it has not
// stored. The file gets an old timestamp, so a touch visibly moves it.
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
	f.write(time.Now().Add(-time.Hour))
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

// write saves f.cfgs to v3net.json with the timestamp at.
func (f *unsavedFixture) write(at time.Time) {
	f.t.Helper()
	if err := config.SaveV3NetConfig(f.dir, config.V3NetConfig{Leaves: f.cfgs}); err != nil {
		f.t.Fatal(err)
	}
	f.touch(at)
}

func (f *unsavedFixture) touch(at time.Time) {
	f.t.Helper()
	if err := os.Chtimes(filepath.Join(f.dir, "v3net.json"), at, at); err != nil {
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
	f.write(time.Now())
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
	f.write(time.Now())
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
		f.write(time.Now())
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

// The sysop saving Hub Signs at the N the file holds moves only the file's
// timestamp (config.TouchConfigFile). That still drops the record, not the
// pin (#787).
func TestReloadOfTouchedFileDropsUnsavedHubSigned(t *testing.T) {
	f := newUnsavedFixture(t)
	f.touch(time.Now())
	f.reload()
	got := f.leaf().UnsavedState()
	if got.HubSigned || f.leaf().HubSigned() {
		t.Errorf("unsaved %+v, hubSigned=%v; want the record dropped", got, f.leaf().HubSigned())
	}
	if got.Coord != unsavedPin {
		t.Errorf("unsaved %+v; want the pin kept", got)
	}
}

// A touch that comes with a rebuild drops the record there too.
func TestRebuildOfTouchedFileDropsUnsavedHubSigned(t *testing.T) {
	f := newUnsavedFixture(t)
	first := f.leaf()
	fresh := append([]config.V3NetLeafConfig(nil), f.cfgs...)
	fresh[1].Boards = []string{"GAMMA"}
	f.touch(time.Now())
	if err := f.svc.ReloadLeaves(fresh, f.mm, nil); err != nil {
		t.Fatal(err)
	}
	if f.leaf() == first {
		t.Fatal("a boards change did not rebuild the leaf")
	}
	if f.leaf().HubSigned() {
		t.Error("the rebuilt leaf kept the record after a touched file")
	}
}

// A reload the file did not cause (reload.force, the area browser) leaves
// the timestamp alone, so it is not a sysop's save of Hub Signs.
func TestReloadOfUntouchedFileKeepsUnsaved(t *testing.T) {
	f := newUnsavedFixture(t)
	f.reload()
	f.wantKept("the file was not touched")

	// Having been touched once does not make later reloads count again.
	f.touch(time.Now())
	f.reload()
	f.leaf().RestoreUnsaved(leaf.UnsavedState{HubSigned: true})
	f.reload()
	if !f.leaf().HubSigned() {
		t.Error("a second reload of the same touch dropped the record")
	}
}
