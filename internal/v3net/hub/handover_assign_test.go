package hub

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// A hub-side NAL change that overtakes a failed adopt moves the retired key's
// areas in that update (see SignNAL), but not one whose manager the caller says
// was set, even to the retired key's node. The choice also survives a restart,
// which finds nothing left to adopt.
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
				if err := h.setNALAreaManager("testnet", "gen.other", retired.assign); err != nil {
					t.Fatal(err)
				}
			},
			wantCoord: "hub", wantOther: "retired", wantIDOnly: "hub",
		},
		{
			name: "assigned to the retired node that already manages it",
			change: func(t *testing.T, h *Hub, retired, _ managerOf) {
				if err := h.setNALAreaManager("testnet", "gen.coord", retired.assign); err != nil {
					t.Fatal(err)
				}
			},
			wantCoord: "retired", wantOther: "other", wantIDOnly: "hub",
		},
		{
			name: "a retired area given to another node",
			change: func(t *testing.T, h *Hub, _, kept managerOf) {
				if err := h.setNALAreaManager("testnet", "gen.coord", kept.assign); err != nil {
					t.Fatal(err)
				}
			},
			wantCoord: "other", wantOther: "other", wantIDOnly: "hub",
		},
		{
			name: "only the key of a retired area filled in",
			change: func(t *testing.T, h *Hub, retired, _ managerOf) {
				if err := h.setNALAreaManager("testnet", "gen.idonly", retired.assign); err != nil {
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
				if err := h.SignNAL(n, map[string]bool{"gen.new": true}); err != nil {
					t.Fatal(err)
				}
				if err := h.nalStore.Put("testnet", n); err != nil {
					t.Fatal(err)
				}
			},
			wantCoord: "hub", wantOther: "other", wantIDOnly: "hub", wantNew: "retired",
		},
		{
			name: "a new area for the retired node that no one chose",
			change: func(t *testing.T, h *Hub, retired, _ managerOf) {
				n, err := h.nalStore.Get("testnet")
				if err != nil {
					t.Fatal(err)
				}
				a := protocol.Area{Tag: "gen.new", Name: "New", Language: "en",
					Access: protocol.AreaAccess{Mode: protocol.AccessModeOpen}, Policy: protocol.AreaPolicy{MaxBodyBytes: 65536}}
				retired.assign(&a)
				n.Areas = append(n.Areas, a)
				if err := h.SignNAL(n, nil); err != nil {
					t.Fatal(err)
				}
				if err := h.nalStore.Put("testnet", n); err != nil {
					t.Fatal(err)
				}
			},
			wantCoord: "hub", wantOther: "other", wantIDOnly: "hub", wantNew: "hub",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, h, ts, oldKS, newKS, other := overtakenAdopt(t)
			defer ts.Close()

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

// overtakenAdopt returns a hub on the new key with a handover it holds but
// failed to adopt, so the stored NAL (see seedManagedNAL) is still the old
// key's, and its server. oldKS and other are active subscribers, and oldKS,
// which signed the stored NAL, is the coordinator the hub answers until the
// NAL is re-signed.
func overtakenAdopt(t *testing.T) (dir string, h *Hub, ts *httptest.Server, oldKS, newKS, other *keystore.Keystore) {
	t.Helper()
	dir = t.TempDir()
	oldKS, newKS, other = newKey(t, "old.key"), newKey(t, "new.key"), newKey(t, "other.key")
	poster := newKey(t, "poster.key")
	h = openTestHubAt(t, dir, oldKS)
	seedManagedNAL(t, h, oldKS, other)
	ts = httptest.NewServer(h.newMux())
	for _, ks := range []*keystore.Keystore{poster, oldKS, other} {
		registerLeaf(t, ts, ks)
	}
	ts.Close()
	_ = h.Close()

	// The adopt fails, so the handover is held and the NAL stays the old key's.
	h = openTestHubAt(t, dir, newKS)
	ts = httptest.NewServer(h.newMux())
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
	return dir, h, ts, oldKS, newKS, other
}

// The requests themselves say whether a manager was chosen. Naming a manager
// is a choice, even of the retired node, and even when it manages the area
// already. Approving a proposal without naming one is not: the proposing node
// manages the area by default, so the retired node's proposal moves to the hub
// with its other areas, whether a coordinator or auto-approval approved it.
func TestManagerRequestsDuringOvertakenAdopt(t *testing.T) {
	approve := func(t *testing.T, ts *httptest.Server, coord *keystore.Keystore, id string, req protocol.ProposalApproveRequest) {
		t.Helper()
		url := ts.URL + "/v3net/v1/testnet/areas/proposals/" + id + "/approve"
		if got := postJSON(t, coord, url, req); got != http.StatusOK {
			t.Fatalf("approve: status %d", got)
		}
	}
	propose := func(t *testing.T, h *Hub, from *keystore.Keystore) string {
		t.Helper()
		id, err := h.proposals.Add("testnet", "gen.new", "New", "", "en", protocol.AccessModeOpen, from.NodeID(), true)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	cases := []struct {
		name string
		do   func(t *testing.T, h *Hub, ts *httptest.Server, oldKS, other *keystore.Keystore)
		// The node of each area of interest: "hub", "retired" or "other".
		wantCoord, wantNew string
	}{
		{
			name: "manager request for the retired node that already manages the area",
			do: func(t *testing.T, _ *Hub, ts *httptest.Server, oldKS, _ *keystore.Keystore) {
				url := ts.URL + "/v3net/v1/testnet/areas/gen.coord/manager"
				if got := postJSON(t, oldKS, url, protocol.AreaManagerRequest{ManagerNodeID: oldKS.NodeID()}); got != http.StatusOK {
					t.Fatalf("set manager: status %d", got)
				}
			},
			wantCoord: "retired",
		},
		{
			name: "the retired node's proposal approved without a manager",
			do: func(t *testing.T, h *Hub, ts *httptest.Server, oldKS, _ *keystore.Keystore) {
				approve(t, ts, oldKS, propose(t, h, oldKS), protocol.ProposalApproveRequest{})
			},
			wantCoord: "hub", wantNew: "hub",
		},
		{
			name: "the retired node's proposal approved naming it as manager",
			do: func(t *testing.T, h *Hub, ts *httptest.Server, oldKS, _ *keystore.Keystore) {
				approve(t, ts, oldKS, propose(t, h, oldKS), protocol.ProposalApproveRequest{ManagerNodeID: oldKS.NodeID()})
			},
			wantCoord: "hub", wantNew: "retired",
		},
		{
			name: "another node's proposal approved naming the retired node",
			do: func(t *testing.T, h *Hub, ts *httptest.Server, oldKS, other *keystore.Keystore) {
				approve(t, ts, oldKS, propose(t, h, other), protocol.ProposalApproveRequest{ManagerNodeID: oldKS.NodeID()})
			},
			wantCoord: "hub", wantNew: "retired",
		},
		{
			name: "another node's proposal approved without a manager",
			do: func(t *testing.T, h *Hub, ts *httptest.Server, oldKS, other *keystore.Keystore) {
				approve(t, ts, oldKS, propose(t, h, other), protocol.ProposalApproveRequest{})
			},
			wantCoord: "hub", wantNew: "other",
		},
		{
			name: "the retired node's proposal auto-approved",
			do: func(t *testing.T, h *Hub, ts *httptest.Server, oldKS, _ *keystore.Keystore) {
				h.cfg.AutoApproveAreas = true
				url := ts.URL + "/v3net/v1/testnet/areas/propose"
				req := protocol.AreaProposalRequest{Tag: "gen.new", Name: "New", Language: "en", AccessMode: protocol.AccessModeOpen}
				if got := postJSON(t, oldKS, url, req); got != http.StatusOK {
					t.Fatalf("propose: status %d", got)
				}
			},
			wantCoord: "hub", wantNew: "hub",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, h, ts, oldKS, newKS, other := overtakenAdopt(t)
			defer ts.Close()

			tc.do(t, h, ts, oldKS, other)

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
				// The other areas move or stay as they would for any change.
				want := map[string]string{"gen.coord": tc.wantCoord, "gen.other": "other", "gen.idonly": "hub"}
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
