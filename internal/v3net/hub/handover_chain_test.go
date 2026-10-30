package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// chainOf returns the signed handovers keys[0]->keys[1]->... for testnet.
func chainOf(t *testing.T, keys ...*keystore.Keystore) []protocol.CoordHandover {
	t.Helper()
	var hs []protocol.CoordHandover
	for i := 0; i+1 < len(keys); i++ {
		hs = append(hs, *signHandover(t, "testnet", keys[i], keys[i+1]))
	}
	return hs
}

// switchedHub runs a hub on dir with each of keys in turn, the first
// seeding the NAL and registering poster, each later one re-signing the
// NAL with a hub-side change and no handover, as the lost-key path does. It
// returns the hub running the last key and its server.
func switchedHub(t *testing.T, dir string, poster *keystore.Keystore, keys ...*keystore.Keystore) (*Hub, *httptest.Server) {
	t.Helper()
	h := openTestHubAt(t, dir, keys[0])
	seedTestNAL(t, h, keys[0])
	ts := httptest.NewServer(h.newMux())
	registerLeaf(t, ts, poster)
	for i, ks := range keys[1:] {
		ts.Close()
		_ = h.Close()
		h = openTestHubAt(t, dir, ks)
		ts = httptest.NewServer(h.newMux())
		desc := "switch " + string(rune('1'+i))
		if err := h.updateNALArea("testnet", "gen.general", func(a *protocol.Area) { a.Description = desc }); err != nil {
			t.Fatalf("updateNALArea after switching to key %d: %v", i+2, err)
		}
	}
	t.Cleanup(func() {
		ts.Close()
		_ = h.Close()
	})
	return h, ts
}

// The hub records each key that signed the NAL before it re-signed without
// a handover, oldest first. A key recorded again moves to the end, and only
// the newest maxPrevSigners are kept.
func TestPrevSignerHistory(t *testing.T) {
	h, _ := setupTestHub(t)
	keys := make([]string, maxPrevSigners+2)
	for i := range keys {
		keys[i] = newKey(t, "k.key").PubKeyBase64()
	}
	for _, k := range keys[:3] {
		if err := h.nalStore.AddPrevSigner("testnet", k); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.nalStore.AddPrevSigner("testnet", keys[0]); err != nil {
		t.Fatal(err)
	}
	got, err := h.nalStore.PrevSigners("testnet")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{keys[1], keys[2], keys[0]}; !equalKeys(got, want) {
		t.Errorf("previous signers = %q, want %q", got, want)
	}
	if other, err := h.nalStore.PrevSigners("othernet"); err != nil || len(other) != 0 {
		t.Errorf("another network's signers = %q, %v; want none", other, err)
	}

	for _, k := range keys[3:] {
		if err := h.nalStore.AddPrevSigner("testnet", k); err != nil {
			t.Fatal(err)
		}
	}
	got, err = h.nalStore.PrevSigners("testnet")
	if err != nil {
		t.Fatal(err)
	}
	want := append([]string{keys[2], keys[0]}, keys[3:]...)
	want = want[len(want)-maxPrevSigners:]
	if !equalKeys(got, want) {
		t.Errorf("previous signers after %d more = %q, want the newest %d: %q", len(keys)-3, got, maxPrevSigners, want)
	}
}

func equalKeys(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A database from before the history, with one previous signer per network
// in coord_prev_signers, is migrated when the hub opens it: the key becomes
// the only entry of the history, the old table goes, and a late handover
// from that key is still accepted.
func TestPrevSignerMigrated(t *testing.T) {
	dir := t.TempDir()
	oldKS, newKS, poster := newKey(t, "old.key"), newKey(t, "new.key"), newKey(t, "poster.key")
	h, ts := switchedHub(t, dir, poster, oldKS, newKS)
	ts.Close()
	// Put the record back the way the previous release kept it.
	for _, stmt := range []string{
		`DROP TABLE coord_signer_history`,
		`CREATE TABLE coord_prev_signers (network TEXT PRIMARY KEY, key_b64 TEXT NOT NULL, recorded_at DATETIME DEFAULT (datetime('now')))`,
	} {
		if _, err := h.db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.db.Exec(`INSERT INTO coord_prev_signers (network, key_b64) VALUES ('testnet', ?)`, oldKS.PubKeyBase64()); err != nil {
		t.Fatal(err)
	}
	_ = h.Close()

	h = openTestHubAt(t, dir, newKS)
	defer h.Close()
	ts = httptest.NewServer(h.newMux())
	defer ts.Close()
	if prev, err := h.nalStore.PrevSigners("testnet"); err != nil || !equalKeys(prev, []string{oldKS.PubKeyBase64()}) {
		t.Fatalf("previous signers after the migration = %q, %v; want the old key", prev, err)
	}
	var name string
	if err := h.db.QueryRow(`SELECT name FROM sqlite_master WHERE name = 'coord_prev_signers'`).Scan(&name); err == nil {
		t.Error("the old table is still there")
	}
	if got := postJSON(t, poster, ts.URL+"/v3net/v1/testnet/nal/handover", signHandover(t, "testnet", oldKS, newKS)); got != http.StatusOK {
		t.Errorf("late handover from the migrated key: status %d, want 200", got)
	}
	// Opening a migrated database again changes nothing.
	_ = h.Close()
	h = openTestHubAt(t, dir, newKS)
	defer h.Close()
	if prev, err := h.nalStore.PrevSigners("testnet"); err != nil || len(prev) != 1 {
		t.Errorf("previous signers after reopening = %q, %v", prev, err)
	}
}

// A hub switched from A to B to C, re-signing the NAL each time with no
// handover, takes the chain A to B to C posted as one request: every link
// is signed by the key before it and starts at a recorded signer, in the
// order they were recorded, and the chain ends at the hub's key. The NAL is
// served with both handovers, so leaves on A and on B follow it, and the
// areas A's node managed move to C. Chains that skip the order, pass
// through a third key, end elsewhere or are malformed are refused, and
// nothing is stored.
func TestLateHandoverChainBridgesSwitches(t *testing.T) {
	dir := t.TempDir()
	a, b, c := newKey(t, "a.key"), newKey(t, "b.key"), newKey(t, "c.key")
	x, poster := newKey(t, "x.key"), newKey(t, "poster.key")
	h, ts := switchedHub(t, dir, poster, a, b, c)
	if prev, err := h.nalStore.PrevSigners("testnet"); err != nil || !equalKeys(prev, []string{a.PubKeyBase64(), b.PubKeyBase64()}) {
		t.Fatalf("previous signers = %q, %v; want A then B", prev, err)
	}
	url := ts.URL + "/v3net/v1/testnet/nal/handover"

	tooLong := make([]*keystore.Keystore, nal.MaxHandoverSteps+2)
	for i := range tooLong {
		tooLong[i] = newKey(t, "long.key")
	}
	broken := append(chainOf(t, a, b), chainOf(t, a, c)...)
	looped := append(chainOf(t, a, b, a), chainOf(t, a, c)...)
	refused := []struct {
		name string
		body any
		want int
	}{
		{"A to B alone (not to the hub key)", chainOf(t, a, b)[0], http.StatusConflict},
		{"B to A to C (out of recorded order)", chainOf(t, b, a, c), http.StatusConflict},
		{"A to X to C (through a third key)", chainOf(t, a, x, c), http.StatusConflict},
		{"A to B to X (ends at a third key)", chainOf(t, a, b, x), http.StatusConflict},
		{"X to C (from a key never recorded)", chainOf(t, x, c), http.StatusConflict},
		{"links that do not join", broken, http.StatusBadRequest},
		{"a key visited twice", looped, http.StatusBadRequest},
		{"longer than a leaf follows", chainOf(t, tooLong...), http.StatusBadRequest},
		{"empty", []protocol.CoordHandover{}, http.StatusBadRequest},
	}
	for _, tc := range refused {
		if got := postJSON(t, poster, url, tc.body); got != tc.want {
			t.Errorf("%s: status %d, want %d", tc.name, got, tc.want)
		}
	}
	tampered := chainOf(t, a, b, c)
	tampered[1].Issued = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if got := postJSON(t, poster, url, tampered); got != http.StatusUnprocessableEntity {
		t.Errorf("chain with a forged link: status %d, want 422", got)
	}
	if hs, err := h.nalStore.Handovers("testnet"); err != nil || len(hs) != 0 {
		t.Fatalf("stored handovers = %d, %v; want none after the refusals", len(hs), err)
	}

	chain := chainOf(t, a, b, c)
	for i := 0; i < 2; i++ {
		if got := postJSON(t, poster, url, chain); got != http.StatusOK {
			t.Fatalf("POST A->B->C #%d: status %d", i+1, got)
		}
	}
	// One link of the held chain, signed again, is a repeat too.
	if got := postJSON(t, poster, url, chainOf(t, b, c)[0]); got != http.StatusOK {
		t.Errorf("B to C again: status %d, want 200", got)
	}
	var served protocol.NAL
	if err := json.Unmarshal(getNALBody(t, ts), &served); err != nil {
		t.Fatal(err)
	}
	if served.CoordPubKeyB64 != c.PubKeyBase64() || nal.Verify(&served) != nil {
		t.Errorf("served NAL signed by %s; want C's, verified", served.CoordPubKeyB64)
	}
	if len(served.Handovers) != 2 || served.Handovers[0] != chain[0] || served.Handovers[1] != chain[1] {
		t.Errorf("served handovers = %+v, want the chain posted", served.Handovers)
	}
	for name, from := range map[string]*keystore.Keystore{"A": a, "B": b} {
		if _, err := nal.FollowHandovers("testnet", from.PubKeyBase64(), c.PubKeyBase64(), served.Handovers); err != nil {
			t.Errorf("no chain from %s to the served NAL: %v", name, err)
		}
	}
	if area := served.FindArea("gen.general"); area == nil || area.ManagerNodeID != c.NodeID() {
		t.Errorf("gen.general managed by %+v, want C's node", area)
	}
	// Once handovers are held, the recorded signers no longer count, and
	// the hub changes the NAL as usual.
	if got := postJSON(t, poster, url, chainOf(t, b, x)[0]); got != http.StatusConflict {
		t.Errorf("B to X after the chain: status %d, want 409", got)
	}
	if err := h.updateNALArea("testnet", "gen.general", func(a *protocol.Area) { a.Description = "after" }); err != nil {
		t.Errorf("updateNALArea after the chain: %v", err)
	}
}

// The hub ran A, was switched to B and re-signed the NAL, then switched to
// C without re-signing, so the NAL is still B's. The chain A to B to C is
// taken all the same: A is a recorded signer and B the NAL's own key, in
// that order. The hub then re-signs the NAL with C, moving the areas of
// A's and B's nodes. A to C, which skips the NAL's key and so could never
// re-sign it, is refused.
func TestLateHandoverChainToUnsignedSwitch(t *testing.T) {
	dir := t.TempDir()
	a, b, c, poster := newKey(t, "a.key"), newKey(t, "b.key"), newKey(t, "c.key"), newKey(t, "poster.key")
	h, ts := switchedHub(t, dir, poster, a, b)
	// B's node manages an area of its own.
	n, err := h.nalStore.Get("testnet")
	if err != nil {
		t.Fatal(err)
	}
	n.Areas = append(n.Areas, protocol.Area{Tag: "gen.b", Name: "B", Language: "en",
		ManagerNodeID: b.NodeID(), ManagerPubKeyB64: b.PubKeyBase64(),
		Access: protocol.AreaAccess{Mode: protocol.AccessModeOpen}, Policy: protocol.AreaPolicy{MaxBodyBytes: 65536}})
	if err := h.SignNAL(n); err != nil {
		t.Fatal(err)
	}
	if err := h.nalStore.Put("testnet", n); err != nil {
		t.Fatal(err)
	}
	ts.Close()
	_ = h.Close()
	h = openTestHubAt(t, dir, c)
	defer h.Close()
	ts = httptest.NewServer(h.newMux())
	defer ts.Close()
	url := ts.URL + "/v3net/v1/testnet/nal/handover"

	if got := postJSON(t, poster, url, chainOf(t, a, c)); got != http.StatusConflict {
		t.Errorf("A to C past the NAL's key: status %d, want 409", got)
	}
	if got := postJSON(t, poster, url, chainOf(t, a, b, c)); got != http.StatusOK {
		t.Fatalf("A to B to C: status %d, want 200", got)
	}
	var served protocol.NAL
	if err := json.Unmarshal(getNALBody(t, ts), &served); err != nil {
		t.Fatal(err)
	}
	if served.CoordPubKeyB64 != c.PubKeyBase64() || nal.Verify(&served) != nil {
		t.Errorf("served NAL signed by %s; want C's, verified", served.CoordPubKeyB64)
	}
	if _, err := nal.FollowHandovers("testnet", a.PubKeyBase64(), c.PubKeyBase64(), served.Handovers); err != nil {
		t.Errorf("no chain from A to the served NAL: %v", err)
	}
	for _, tag := range []string{"gen.general", "gen.b"} {
		if area := served.FindArea(tag); area == nil || area.ManagerNodeID != c.NodeID() {
			t.Errorf("%s managed by %+v, want C's node", tag, area)
		}
	}
}

// Posted one link at a time, the chain cannot be completed: B to C is a
// valid late handover on its own and is held, and from then on A's link is
// refused, since a held handover ends the recorded signers' say. Posting
// the whole chain after that is refused for the same reason. That is why a
// late chain goes in one request.
func TestLateHandoverChainPostedInParts(t *testing.T) {
	dir := t.TempDir()
	a, b, c, poster := newKey(t, "a.key"), newKey(t, "b.key"), newKey(t, "c.key"), newKey(t, "poster.key")
	h, ts := switchedHub(t, dir, poster, a, b, c)
	url := ts.URL + "/v3net/v1/testnet/nal/handover"
	if got := postJSON(t, poster, url, chainOf(t, b, c)[0]); got != http.StatusOK {
		t.Fatalf("B to C: status %d, want 200", got)
	}
	if got := postJSON(t, poster, url, chainOf(t, a, b)[0]); got != http.StatusConflict {
		t.Errorf("A to B after B to C: status %d, want 409", got)
	}
	if got := postJSON(t, poster, url, chainOf(t, a, b, c)); got != http.StatusConflict {
		t.Errorf("A to B to C after B to C: status %d, want 409", got)
	}
	if hs, err := h.nalStore.Handovers("testnet"); err != nil || len(hs) != 1 {
		t.Errorf("stored handovers = %d, %v; want only B to C", len(hs), err)
	}
}

// A chain posted to a hub that has not switched is checked link by link,
// as if each were posted after the one before: A to B to C on a hub still
// running A is held whole, while one whose second link is not signed by
// the key the first handed over to never gets that far.
func TestHandoverChainInForce(t *testing.T) {
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()
	seedTestNAL(t, h, hubKS)
	b, c, poster := newKey(t, "b.key"), newKey(t, "c.key"), newKey(t, "poster.key")
	registerLeaf(t, ts, poster)
	url := ts.URL + "/v3net/v1/testnet/nal/handover"

	if got := postJSON(t, poster, url, chainOf(t, b, c)); got != http.StatusConflict {
		t.Errorf("B to C, not signed by the key in force: status %d, want 409", got)
	}
	if got := postJSON(t, poster, url, chainOf(t, hubKS, b, hubKS)); got != http.StatusBadRequest {
		t.Errorf("A to B to A: status %d, want 400", got)
	}
	if got := postJSON(t, poster, url, chainOf(t, hubKS, b, c)); got != http.StatusOK {
		t.Fatalf("A to B to C: status %d, want 200", got)
	}
	n, err := h.nalStore.Get("testnet")
	if err != nil {
		t.Fatal(err)
	}
	hs, err := h.nalStore.Handovers("testnet")
	if err != nil || len(hs) != 2 {
		t.Fatalf("stored handovers = %d, %v; want 2", len(hs), err)
	}
	if got := coordKeyInForce(n, hs); got != c.PubKeyBase64() {
		t.Errorf("key in force = %s, want C", got)
	}
	if got := postJSON(t, poster, url, chainOf(t, c, b)[0]); got != http.StatusConflict {
		t.Errorf("C back to B: status %d, want 409", got)
	}
}
