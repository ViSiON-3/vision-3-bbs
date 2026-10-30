package hub

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// A hub-side NAL change that overtakes a failed adopt moves the retired key's
// areas in that update (see SignNAL), but not one whose manager the change
// itself sets, even to the retired key's node. The choice also survives a
// restart, which finds nothing left to adopt.
func TestSignNALKeepsManagerTheChangeSets(t *testing.T) {
	cases := []struct {
		name   string
		change func(t *testing.T, h *Hub, retired, kept managerOf)
		// The managers the stored NAL should end up with.
		wantCoord, wantOther, wantIDOnly string
		wantNew                          string // "" when the change adds no area
	}{
		{
			name: "assigned to the retired node",
			change: func(t *testing.T, h *Hub, retired, _ managerOf) {
				if err := h.updateNALArea("testnet", "gen.other", retired.assign); err != nil {
					t.Fatal(err)
				}
			},
			wantCoord: "hub", wantOther: "retired", wantIDOnly: "hub",
		},
		{
			name: "a retired area given to another node",
			change: func(t *testing.T, h *Hub, _, kept managerOf) {
				if err := h.updateNALArea("testnet", "gen.coord", kept.assign); err != nil {
					t.Fatal(err)
				}
			},
			wantCoord: "other", wantOther: "other", wantIDOnly: "hub",
		},
		{
			name: "only the key of a retired area filled in",
			change: func(t *testing.T, h *Hub, retired, _ managerOf) {
				if err := h.updateNALArea("testnet", "gen.idonly", retired.assign); err != nil {
					t.Fatal(err)
				}
			},
			wantCoord: "hub", wantOther: "other", wantIDOnly: "retired",
		},
		{
			name: "a change that leaves managers alone",
			change: func(t *testing.T, h *Hub, _, _ managerOf) {
				if err := h.updateNALArea("testnet", "gen.other", func(a *protocol.Area) { a.Description = "edited" }); err != nil {
					t.Fatal(err)
				}
			},
			wantCoord: "hub", wantOther: "other", wantIDOnly: "hub",
		},
		{
			name: "a new area for the retired node",
			change: func(t *testing.T, h *Hub, retired, _ managerOf) {
				n, err := h.nalStore.Get("testnet")
				if err != nil {
					t.Fatal(err)
				}
				a := protocol.Area{Tag: "gen.new", Name: "New", Language: "en",
					Access: protocol.AreaAccess{Mode: protocol.AccessModeOpen}, Policy: protocol.AreaPolicy{MaxBodyBytes: 65536}}
				retired.assign(&a)
				n.Areas = append(n.Areas, a)
				if err := h.SignNAL(n); err != nil {
					t.Fatal(err)
				}
				if err := h.nalStore.Put("testnet", n); err != nil {
					t.Fatal(err)
				}
			},
			wantCoord: "hub", wantOther: "other", wantIDOnly: "hub", wantNew: "retired",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			oldKS, newKS, other, poster := newKey(t, "old.key"), newKey(t, "new.key"), newKey(t, "other.key"), newKey(t, "poster.key")
			h := openTestHubAt(t, dir, oldKS)
			seedManagedNAL(t, h, oldKS, other)
			ts := httptest.NewServer(h.newMux())
			registerLeaf(t, ts, poster)
			ts.Close()
			_ = h.Close()

			// The adopt fails, so the handover is held and the NAL stays the
			// old key's.
			h = openTestHubAt(t, dir, newKS)
			ts = httptest.NewServer(h.newMux())
			defer ts.Close()
			if _, err := h.db.Exec(`CREATE TRIGGER fail_nal_update BEFORE UPDATE ON network_nal
				BEGIN SELECT RAISE(ABORT, 'injected'); END`); err != nil {
				t.Fatal(err)
			}
			url := ts.URL + "/v3net/v1/testnet/nal/handover"
			if got := postJSON(t, poster, url, signHandover(t, "testnet", oldKS, newKS)); got != http.StatusInternalServerError {
				t.Fatalf("POST with the store failing: status %d, want 500", got)
			}
			if _, err := h.db.Exec(`DROP TRIGGER fail_nal_update`); err != nil {
				t.Fatal(err)
			}

			tc.change(t, h, managerOf{oldKS.NodeID(), oldKS.PubKeyBase64()}, managerOf{other.NodeID(), other.PubKeyBase64()})

			names := map[string]string{"hub": newKS.NodeID(), "retired": oldKS.NodeID(), "other": other.NodeID()}
			check := func(h *Hub) {
				t.Helper()
				n, err := h.nalStore.Get("testnet")
				if err != nil || n == nil {
					t.Fatalf("get NAL: %v", err)
				}
				if n.CoordPubKeyB64 != newKS.PubKeyBase64() || nal.Verify(n) != nil {
					t.Errorf("NAL signed by %s; want the new key's, verified", n.CoordPubKeyB64)
				}
				want := map[string]string{"gen.coord": tc.wantCoord, "gen.other": tc.wantOther, "gen.idonly": tc.wantIDOnly}
				if tc.wantNew != "" {
					want["gen.new"] = tc.wantNew
				}
				for tag, who := range want {
					if a := n.FindArea(tag); a == nil || a.ManagerNodeID != names[who] {
						t.Errorf("%s managed by %+v, want %s's node", tag, a, who)
					}
				}
			}
			check(h)
			_ = h.Close()
			h = openTestHubAt(t, dir, newKS)
			defer h.Close()
			check(h)
		})
	}
}

// managerOf names a node for a test to make an area's manager.
type managerOf struct{ nodeID, key string }

// assign makes m the manager of a.
func (m managerOf) assign(a *protocol.Area) {
	a.ManagerNodeID, a.ManagerPubKeyB64 = m.nodeID, m.key
}
