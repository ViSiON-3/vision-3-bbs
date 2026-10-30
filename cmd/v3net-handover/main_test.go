package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/hub"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// publish claims a recovery only when the hub confirms it: a late post
// succeeds when the hub answers 200 and then serves the NAL signed by the
// new key with the handover; a 500, or a 200 while the NAL is still the old
// key's, sends the operator to the hub log and to run the command again.
func TestPublishReportsOutcome(t *testing.T) {
	oldKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "old.key"))
	if err != nil {
		t.Fatal(err)
	}
	newKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "new.key"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := protocol.NewCoordHandover(oldKS, "testnet", newKS.PubKeyBase64(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(h)
	signedBy := func(ks *keystore.Keystore) *protocol.NAL {
		n := &protocol.NAL{V3NetNAL: "1.0", Network: "testnet", Areas: []protocol.Area{}}
		if err := nal.Sign(n, ks); err != nil {
			t.Fatal(err)
		}
		n.Handovers = []protocol.CoordHandover{*h}
		return n
	}

	cases := []struct {
		name    string
		late    bool
		authSet bool
		status  int
		served  *protocol.NAL
		ok      bool
		want    string // in the output
	}{
		{"before the switch", false, false, http.StatusOK, signedBy(oldKS), true, "Restart the hub with the new key"},
		{"late, re-signed", true, false, http.StatusOK, signedBy(newKS), true, "now serves the NAL signed by the new key"},
		{"late, re-sign failed", true, false, http.StatusInternalServerError, signedBy(oldKS), false, "Check the hub log, fix the cause, then run this command again"},
		{"late, 200 but old NAL", true, false, http.StatusOK, signedBy(oldKS), false, "still serves a NAL signed by " + oldKS.PubKeyBase64()},
		{"old node not registered", true, false, http.StatusUnauthorized, signedBy(oldKS), false, "-auth-keystore"},
		{"other node not registered", true, true, http.StatusUnauthorized, signedBy(oldKS), false, "Response: 401"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/v3net/v1/testnet/nal/handover":
					if r.Header.Get("X-V3Net-Node-ID") != oldKS.NodeID() {
						t.Errorf("request sent as %s, want the old key's node", r.Header.Get("X-V3Net-Node-ID"))
					}
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(`{}`))
				case r.Method == http.MethodGet && r.URL.Path == "/v3net/v1/testnet/nal":
					_ = json.NewEncoder(w).Encode(tc.served)
				default:
					http.NotFound(w, r)
				}
			}))
			defer ts.Close()
			var out, errOut bytes.Buffer
			ok := publish(&out, &errOut, ts.Client(), ts.URL, []protocol.CoordHandover{*h}, body, oldKS, tc.authSet, tc.late)
			all := out.String() + errOut.String()
			if ok != tc.ok || !strings.Contains(all, tc.want) {
				t.Errorf("publish = %v, output %q; want %v and %q", ok, all, tc.ok, tc.want)
			}
			if !tc.ok && strings.Contains(out.String(), "Handover published") {
				t.Errorf("reported success on failure: %q", out.String())
			}
		})
	}
}

// signChain signs each link with the key it hands over from, and chainBody
// keeps a single handover as the object every hub takes.
func TestSignChain(t *testing.T) {
	var keys []*keystore.Keystore
	for _, name := range []string{"a", "b", "c"} {
		ks, _, err := keystore.Load(filepath.Join(t.TempDir(), name+".key"))
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, ks)
	}
	to := keys[2].PubKeyBase64()
	chain, err := signChain(keys[:2], "testnet", to, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(chain) != 2 || chain[0].OldKeyB64 != keys[0].PubKeyBase64() || chain[0].NewKeyB64 != keys[1].PubKeyBase64() ||
		chain[1].OldKeyB64 != keys[1].PubKeyBase64() || chain[1].NewKeyB64 != to {
		t.Fatalf("chain = %+v, want A to B to C", chain)
	}
	if _, err := nal.FollowHandovers("testnet", keys[0].PubKeyBase64(), to, chain); err != nil {
		t.Errorf("chain does not verify from A to C: %v", err)
	}
	if body, err := chainBody(chain); err != nil || !bytes.HasPrefix(body, []byte("[")) {
		t.Errorf("chain body = %s, %v; want an array", body, err)
	}
	if body, err := chainBody(chain[:1]); err != nil || !bytes.HasPrefix(body, []byte("{")) {
		t.Errorf("single handover body = %s, %v; want an object", body, err)
	}
}

// End to end against a hub switched from A to B to C with no handover, each
// switch re-signing the NAL: the tool, given A's keystore, B's through -via
// and C's key, posts the chain in one request, and confirms the hub serves
// the NAL signed by C with a chain from A.
func TestPublishChainToHub(t *testing.T) {
	dir := t.TempDir()
	var keys []*keystore.Keystore
	for _, name := range []string{"a", "b", "c"} {
		ks, _, err := keystore.Load(filepath.Join(dir, name+".key"))
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, ks)
	}
	var current atomic.Pointer[hub.Hub]
	open := func(ks *keystore.Keystore) *hub.Hub {
		t.Helper()
		h, err := hub.New(hub.Config{ListenAddr: ":0", DataDir: dir, Keystore: ks, AutoApprove: true,
			Networks: []hub.NetworkConfig{{Name: "testnet"}}})
		if err != nil {
			t.Fatalf("create hub: %v", err)
		}
		current.Store(h)
		return h
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current.Load().Mux().ServeHTTP(w, r)
	}))
	defer ts.Close()

	h := open(keys[0])
	n := &protocol.NAL{V3NetNAL: "1.0", Network: "testnet", Areas: []protocol.Area{}}
	if err := nal.Sign(n, keys[0]); err != nil {
		t.Fatal(err)
	}
	if err := h.NALStore().Put("testnet", n); err != nil {
		t.Fatal(err)
	}
	sub, _ := json.Marshal(protocol.SubscribeRequest{Network: "testnet", NodeID: keys[0].NodeID(),
		PubKeyB64: keys[0].PubKeyBase64(), BBSName: "Old Hub", BBSHost: "old.example.net"})
	resp, err := http.Post(ts.URL+"/v3net/v1/subscribe", "application/json", bytes.NewReader(sub))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subscribe the old key's node: status %d", resp.StatusCode)
	}
	for _, ks := range keys[1:] {
		_ = h.Close()
		h = open(ks)
		n, err := h.NALStore().Get("testnet")
		if err != nil || n == nil {
			t.Fatalf("get NAL: %v", err)
		}
		if err := h.SignNAL(n); err != nil {
			t.Fatal(err)
		}
		if err := h.NALStore().Put("testnet", n); err != nil {
			t.Fatal(err)
		}
	}
	defer h.Close()

	to := keys[2].PubKeyBase64()
	chain, err := signChain(keys[:2], "testnet", to, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	body, err := chainBody(chain)
	if err != nil {
		t.Fatal(err)
	}
	late, err := checkHubKey(ts.Client(), ts.URL, "testnet", keys[0].PubKeyBase64(), to)
	if err != nil || !late {
		t.Fatalf("checkHubKey = %v, %v; want late", late, err)
	}
	var out, errOut bytes.Buffer
	if !publish(&out, &errOut, ts.Client(), ts.URL, chain, body, keys[0], false, true) {
		t.Fatalf("publish failed: %s%s", out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "now serves the NAL signed by the new key") {
		t.Errorf("output %q, want the recovery confirmed", out.String())
	}
}

// checkHubKey tells a hub on the old key (post as usual) from one already
// on the new key (post, late) and refuses one on any other key or none.
func TestCheckHubKey(t *testing.T) {
	keys := map[string]*keystore.Keystore{}
	for _, name := range []string{"old", "new", "other"} {
		ks, _, err := keystore.Load(filepath.Join(t.TempDir(), name+".key"))
		if err != nil {
			t.Fatal(err)
		}
		keys[name] = ks
	}
	h, err := protocol.NewCoordHandover(keys["old"], "testnet", keys["new"].PubKeyBase64(), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	var served string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3net/v1/testnet/info" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(protocol.NetworkInfo{Name: "testnet", HubPubKeyB64: served})
	}))
	defer ts.Close()

	cases := []struct {
		name     string
		hubKey   string
		network  string
		wantLate bool
		wantErr  bool
	}{
		{"old key", keys["old"].PubKeyBase64(), "testnet", false, false},
		{"new key", keys["new"].PubKeyBase64(), "testnet", true, false},
		{"other key", keys["other"].PubKeyBase64(), "testnet", false, true},
		{"no key", "", "testnet", false, true},
		{"unknown network", keys["old"].PubKeyBase64(), "othernet", false, true},
	}
	for _, tc := range cases {
		served = tc.hubKey
		late, err := checkHubKey(ts.Client(), ts.URL, tc.network, h.OldKeyB64, h.NewKeyB64)
		if late != tc.wantLate || (err != nil) != tc.wantErr {
			t.Errorf("%s: late %v, err %v; want late %v, error %v", tc.name, late, err, tc.wantLate, tc.wantErr)
		}
	}
}
