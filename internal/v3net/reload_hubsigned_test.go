package v3net_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/leaf"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// A reload applied after a newer one holds an older copy of the file, with a
// lower hubSignedClears. That is no clear, whether the leaf is rebuilt or not, and
// the reload of the file that follows is not one either (#824).
func TestOlderCopyWithLowerClearsKeepsUnsaved(t *testing.T) {
	for _, rebuild := range []bool{false, true} {
		f := newUnsavedFixture(t)
		f.cfgs[0].HubSignedClears = 1
		f.write()
		f.reload()
		if f.leaf().HubSigned() {
			t.Fatalf("rebuild=%v: the clear did not drop the record", rebuild)
		}
		f.leaf().RestoreUnsaved(leaf.UnsavedState{HubSigned: true})

		if rebuild {
			f.cfgs[1].Boards = []string{"GAMMA"}
			f.write()
		}
		copyOfFile := slices.Clone(f.cfgs)
		copyOfFile[0].HubSignedClears = 0
		if err := f.svc.ReloadLeaves(copyOfFile, f.mm, nil); err != nil {
			t.Fatal(err)
		}
		if !f.leaf().HubSigned() {
			t.Errorf("rebuild=%v: an older copy's lower count dropped the record", rebuild)
		}
		f.reload()
		if !f.leaf().HubSigned() {
			t.Errorf("rebuild=%v: the file's count, already applied, dropped the record", rebuild)
		}
	}
}

// A clear written after an in-place reload's copy was read drops a record not yet
// stored, and the reload the clear's own save triggers is not a second one (#824).
func TestInPlaceReloadHonoursClearSinceRead(t *testing.T) {
	f := newUnsavedFixture(t)
	copyOfFile := slices.Clone(f.cfgs)
	f.cfgs[0].HubSignedClears = 1
	f.write()
	if err := f.svc.ReloadLeaves(copyOfFile, f.mm, nil); err != nil {
		t.Fatal(err)
	}
	if f.leaf().HubSigned() {
		t.Fatal("a clear since the copy was read did not drop the record")
	}
	f.leaf().RestoreUnsaved(leaf.UnsavedState{HubSigned: true})
	f.reload()
	if !f.leaf().HubSigned() {
		t.Error("the reload of the same count dropped a new record")
	}
}

// The sysop lowering the count by hand, or removing the field, clears nothing, and
// the next rise from the lower number does, whether the leaf is rebuilt or not (#824).
func TestLoweredClearsIsNoClearAndNextRiseCounts(t *testing.T) {
	for _, rebuild := range []bool{false, true} {
		f := newUnsavedFixture(t)
		f.cfgs[0].HubSignedClears = 3
		f.write()
		f.reload()
		f.leaf().RestoreUnsaved(leaf.UnsavedState{HubSigned: true})

		f.cfgs[0].HubSignedClears = 0
		if rebuild {
			f.cfgs[1].Boards = []string{"GAMMA"}
		}
		f.write()
		f.reload()
		if !f.leaf().HubSigned() {
			t.Errorf("rebuild=%v: removing the count dropped the record", rebuild)
		}

		f.cfgs[0].HubSignedClears = 1
		f.write()
		f.reload()
		if f.leaf().HubSigned() {
			t.Errorf("rebuild=%v: the rise from the lower number did not drop the record", rebuild)
		}
	}
}

// A signature first verifying on a leaf a reload has replaced reaches its
// replacement, which refuses unsigned replies from then on and stores the record.
// It does not when the sysop cleared the record in that reload, or when the
// subscription moved to another hub (#824).
func TestLateVerifiedReplyAfterRebuild(t *testing.T) {
	hubKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "hub.key"))
	if err != nil {
		t.Fatal(err)
	}
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := []byte("[]")
		sig, err := protocol.SignResponse(hubKS, r.Header.Get("X-V3Net-Signature"), r.Header.Get(protocol.HeaderNonce), http.StatusOK, body)
		if err != nil {
			t.Error(err)
		}
		w.Header().Set(protocol.HeaderHubSignature, sig)
		_, _ = w.Write(body)
	}))
	t.Cleanup(hub.Close)

	cases := []struct {
		name string
		edit func(c *config.V3NetLeafConfig)
		want bool
	}{
		{name: "same hub", want: true},
		{name: "cleared in the reload", edit: func(c *config.V3NetLeafConfig) { c.HubSignedClears = 1 }},
		{name: "moved to another hub", edit: func(c *config.V3NetLeafConfig) { c.HubURL = "http://127.0.0.1:2" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newUnsavedFixture(t)
			f.cfgs[1].HubURL, f.cfgs[1].HubPubKey = hub.URL, hubKS.PubKeyBase64()
			f.write()
			f.reload()
			old := f.leafFor("net-b")

			f.cfgs[1].Boards = []string{"GAMMA"}
			if c.edit != nil {
				c.edit(&f.cfgs[1])
			}
			f.write()
			f.reload()
			if f.leafFor("net-b") == old {
				t.Fatal("the reload did not rebuild the leaf")
			}

			_, _ = old.Poll(context.Background())
			if !old.HubSigned() {
				t.Fatal("the hub's signature did not verify on the old leaf")
			}
			if got := f.leafFor("net-b").HubSigned(); got != c.want {
				t.Errorf("replacement hubSigned = %v, want %v", got, c.want)
			}
			fresh, err := config.LoadV3NetConfig(f.dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, lc := range fresh.Leaves {
				if lc.Network == "net-b" && lc.HubSigned != c.want {
					t.Errorf("file hubSigned = %v, want %v", lc.HubSigned, c.want)
				}
			}
		})
	}
}
