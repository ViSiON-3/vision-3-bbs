package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
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

// loadKeys loads a new keystore for each name.
func loadKeys(t *testing.T, names ...string) []*keystore.Keystore {
	t.Helper()
	keys := make([]*keystore.Keystore, 0, len(names))
	for _, name := range names {
		ks, _, err := keystore.Load(filepath.Join(t.TempDir(), name+".key"))
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, ks)
	}
	return keys
}

// chainFrom signs the chain through signers to the key to.
func chainFrom(t *testing.T, to string, signers ...*keystore.Keystore) []protocol.CoordHandover {
	t.Helper()
	chain, err := signChain(signers, "testnet", to, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return chain
}

// matchChain takes the chain the hub needs, and refuses every other, naming
// the one it needs. A chain that only leaves out a recorded key passes with
// -omit-lost-keys; one the hub would refuse never does.
func TestMatchChain(t *testing.T) {
	keys := loadKeys(t, "a", "b", "c", "d", "x")
	a, b, c, d, x := keys[0], keys[1], keys[2], keys[3], keys[4]
	key := func(ks *keystore.Keystore) string { return ks.PubKeyBase64() }
	link := func(from, to *keystore.Keystore) protocol.HandoverLink {
		return protocol.HandoverLink{OldKeyB64: key(from), NewKeyB64: key(to)}
	}
	// Hub on C, NAL re-signed by C, A and B before it.
	resigned := &protocol.SignerHistory{NALKeyB64: key(c), KeyInForceB64: key(c), PrevSigners: []string{key(a), key(b)}}
	// Hub on C, NAL still B's, A before it.
	unsigned := &protocol.SignerHistory{NALKeyB64: key(b), KeyInForceB64: key(b), PrevSigners: []string{key(a)}}
	// Hub on A, its NAL never re-signed.
	plain := &protocol.SignerHistory{NALKeyB64: key(a), KeyInForceB64: key(a), PrevSigners: []string{}}
	// Hub on C, B before it, and its NAL C's.
	earlier := &protocol.SignerHistory{NALKeyB64: key(c), KeyInForceB64: key(c), PrevSigners: []string{key(b)}}
	// Hub went back to A, a key that signed the NAL.
	wentBack := &protocol.SignerHistory{NALKeyB64: key(b), KeyInForceB64: key(b), PrevSigners: []string{key(a)}}
	// Hub on C, the chain A to B to C held.
	held := &protocol.SignerHistory{NALKeyB64: key(c), KeyInForceB64: key(c), PrevSigners: []string{},
		Handovers: []protocol.HandoverLink{link(a, b), link(b, c)}}

	// Hub on D, its NAL still A's, the handover A to B held.
	heldSwitched := &protocol.SignerHistory{NALKeyB64: key(a), KeyInForceB64: key(b), PrevSigners: []string{},
		Handovers: []protocol.HandoverLink{link(a, b)}}
	// Hub ran A, B, then A again, re-signing each time.
	returned := &protocol.SignerHistory{NALKeyB64: key(a), KeyInForceB64: key(a), PrevSigners: []string{key(a), key(b)}}

	cases := []struct {
		name     string
		history  *protocol.SignerHistory
		hubKey   *keystore.Keystore
		chain    []protocol.CoordHandover
		omitLost bool
		wantErr  string // in the error; "" for none
	}{
		{"the chain needed", resigned, c, chainFrom(t, key(c), a, b), false, ""},
		{"no -via when one is needed", resigned, c, chainFrom(t, key(c), a), false, "-omit-lost-keys"},
		{"no -via, key lost", resigned, c, chainFrom(t, key(c), a), true, ""},
		{"starts late", resigned, c, chainFrom(t, key(c), b), false, key(a)},
		{"starts late, key lost", resigned, c, chainFrom(t, key(c), b), true, ""},
		{"-via out of order", resigned, c, chainFrom(t, key(c), b, a), false, "would refuse"},
		{"-via out of order, omit is no help", resigned, c, chainFrom(t, key(c), b, a), true, "would refuse"},
		{"-via through a third key", resigned, c, chainFrom(t, key(c), a, x), true, "would refuse"},
		{"NAL's key, the chain needed", unsigned, c, chainFrom(t, key(c), a, b), false, ""},
		{"past the NAL's key", unsigned, c, chainFrom(t, key(c), a), true, "would refuse"},
		{"plain handover", plain, a, chainFrom(t, key(b), a), false, ""},
		{"planned chain to come", plain, a, chainFrom(t, key(c), a, b), false, ""},
		{"onward, earlier signers unbridged", earlier, c, chainFrom(t, key(d), c), false, key(b)},
		{"onward, earlier signers lost", earlier, c, chainFrom(t, key(d), c), true, ""},
		{"hub went back to a used key", wentBack, a, chainFrom(t, key(a), b), false, "already signed"},
		{"nothing to hand over", &protocol.SignerHistory{NALKeyB64: key(c), KeyInForceB64: key(c)}, c, chainFrom(t, key(c), a), false, "key in force"},
		{"held, reposted", held, c, chainFrom(t, key(c), a, b), false, ""},
		{"held, one link reposted", held, c, chainFrom(t, key(c), b), false, ""},
		{"held, on from the key in force", held, c, chainFrom(t, key(d), c), false, ""},
		{"held, repeat then on", held, c, chainFrom(t, key(d), b, c), false, ""},
		{"held, not from the key in force", held, c, chainFrom(t, key(d), a), false, "key in force"},
		{"held, on then back to a used key", held, c, chainFrom(t, key(a), c, d), false, "already used"},
		{"held, hub switched, through a third key", heldSwitched, d, chainFrom(t, key(d), b, c), false, "hub's key"},
		{"held, hub switched, straight to it", heldSwitched, d, chainFrom(t, key(d), b), false, ""},
		{"onward through a recorded key, omit is no help", resigned, c, chainFrom(t, key(d), c, a), true, "already used"},
		{"went back, onward leaves a key unbridged", returned, a, chainFrom(t, key(c), a), false, "restart the hub"},
		{"went back, onward, key lost", returned, a, chainFrom(t, key(c), a), true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			note, err := matchChain(tc.history, key(tc.hubKey), tc.chain, tc.omitLost)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("refused: %v", err)
			case tc.wantErr == "" && note == "":
				t.Error("no note for the operator")
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Errorf("err = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// The needed chain names each keystore in the order to pass it.
func TestDescribeChain(t *testing.T) {
	got := describeChain([]string{"A", "B", "C"}, "D")
	for _, want := range []string{"-keystore  the keystore of A", "-via", "B\n", "C\n", "-new-key   D"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q does not contain %q", got, want)
		}
	}
	if strings.Index(got, "B\n") > strings.Index(got, "C\n") {
		t.Errorf("%q has -via out of order", got)
	}
	if got := describeChain([]string{"A"}, "D"); strings.Contains(got, "-via") {
		t.Errorf("%q names -via with nothing between", got)
	}
}

// switchedHubServer serves a real hub switched through keys with no handover,
// each switch re-signing the NAL, with sender subscribed.
func switchedHubServer(t *testing.T, sender *keystore.Keystore, keys ...*keystore.Keystore) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	var h *hub.Hub
	open := func(ks *keystore.Keystore) {
		t.Helper()
		var err error
		h, err = hub.New(hub.Config{ListenAddr: ":0", DataDir: dir, Keystore: ks, AutoApprove: true,
			Networks: []hub.NetworkConfig{{Name: "testnet"}}})
		if err != nil {
			t.Fatalf("create hub: %v", err)
		}
	}
	open(keys[0])
	n := &protocol.NAL{V3NetNAL: "1.0", Network: "testnet", Areas: []protocol.Area{}}
	if err := nal.Sign(n, keys[0]); err != nil {
		t.Fatal(err)
	}
	if err := h.NALStore().Put("testnet", n); err != nil {
		t.Fatal(err)
	}
	for _, ks := range keys[1:] {
		_ = h.Close()
		open(ks)
		n, err := h.NALStore().Get("testnet")
		if err != nil || n == nil {
			t.Fatalf("get NAL: %v", err)
		}
		if err := h.SignNAL(n, nil); err != nil {
			t.Fatal(err)
		}
		if err := h.NALStore().Put("testnet", n); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = h.Close() })
	ts := httptest.NewServer(h.Mux())
	t.Cleanup(ts.Close)
	sub, _ := json.Marshal(protocol.SubscribeRequest{Network: "testnet", NodeID: sender.NodeID(),
		PubKeyB64: sender.PubKeyBase64(), BBSName: "Sender", BBSHost: "sender.example.net"})
	resp, err := http.Post(ts.URL+"/v3net/v1/subscribe", "application/json", bytes.NewReader(sub))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subscribe: status %d", resp.StatusCode)
	}
	return ts
}

// Against a real hub switched from A to B to C: the chain needed passes and
// is printed, and one missing B is refused, naming B.
func TestCheckHistoryAgainstHub(t *testing.T) {
	keys := loadKeys(t, "a", "b", "c")
	a, b, c := keys[0], keys[1], keys[2]
	ts := switchedHubServer(t, a, a, b, c)
	to := c.PubKeyBase64()

	var out bytes.Buffer
	if _, err := checkHistory(&out, ts.Client(), ts.URL, chainFrom(t, to, a, b), to, a, false); err != nil {
		t.Fatalf("the chain needed: %v", err)
	}
	if !strings.Contains(out.String(), "matches") || !strings.Contains(out.String(), b.PubKeyBase64()) {
		t.Errorf("output %q, want the match and the chain", out.String())
	}
	_, err := checkHistory(&out, ts.Client(), ts.URL, chainFrom(t, to, a), to, a, false)
	if err == nil || !strings.Contains(err.Error(), b.PubKeyBase64()) {
		t.Errorf("A to C with B recorded: %v, want a refusal naming B", err)
	}
}

// A reply that does not verify under the hub key the operator named never
// drives the decision, whatever it says: a body changed in transit, a reply
// signed by another key (a hub or proxy that serves that key at /info too), a
// signature stripped, or an old reply to an earlier request. Each would have
// passed the chain if believed, and each stops the tool instead.
func TestCheckHistoryRefusesUnverifiedReply(t *testing.T) {
	keys := loadKeys(t, "a", "b", "c", "evil")
	a, b, c, evil := keys[0], keys[1], keys[2], keys[3]
	to := c.PubKeyBase64()
	// A forged history under which the wrong chain A to C matches.
	forged, _ := json.Marshal(protocol.SignerHistory{Network: "testnet", NALKeyB64: to, KeyInForceB64: to, PrevSigners: []string{a.PubKeyBase64()}})
	hubTS := switchedHubServer(t, a, a, b, c)

	// proxy passes requests to the hub, then rewrites the reply with edit.
	proxy := func(edit func(r *http.Request, h http.Header, body []byte) []byte) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			req, _ := http.NewRequest(r.Method, hubTS.URL+r.URL.Path, nil)
			req.Header = r.Header.Clone()
			resp, err := hubTS.Client().Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			out := edit(r, resp.Header, body)
			for k, v := range resp.Header {
				w.Header()[k] = v
			}
			w.Header().Del("Content-Length")
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(out)
		}))
	}

	var captured struct {
		sig  string
		body []byte
	}
	cases := []struct {
		name string
		edit func(r *http.Request, h http.Header, body []byte) []byte
	}{
		{"body changed", func(_ *http.Request, _ http.Header, _ []byte) []byte { return forged }},
		{"signed by another key", func(r *http.Request, h http.Header, _ []byte) []byte {
			sig, err := protocol.SignResponse(evil, r.Header.Get("X-V3Net-Signature"), r.Header.Get(protocol.HeaderNonce), http.StatusOK, forged)
			if err != nil {
				t.Error(err)
			}
			h.Set(protocol.HeaderHubSignature, sig)
			return forged
		}},
		{"signature stripped", func(_ *http.Request, h http.Header, _ []byte) []byte {
			h.Del(protocol.HeaderHubSignature)
			return forged
		}},
		{"an earlier reply replayed", func(_ *http.Request, h http.Header, body []byte) []byte {
			if captured.sig == "" {
				captured.sig, captured.body = h.Get(protocol.HeaderHubSignature), body
				return body
			}
			h.Set(protocol.HeaderHubSignature, captured.sig)
			return captured.body
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := proxy(tc.edit)
			defer ts.Close()
			if tc.name == "an earlier reply replayed" {
				// The same request in the same second has the same signature,
				// so only the nonce can tell this reply from the last.
				if _, err := checkHistory(io.Discard, ts.Client(), ts.URL, chainFrom(t, to, a, b), to, a, false); err != nil {
					t.Fatalf("the first, genuine reply: %v", err)
				}
			}
			var out bytes.Buffer
			chain := chainFrom(t, to, a)
			if tc.name == "an earlier reply replayed" {
				chain = chainFrom(t, to, a, b)
			}
			_, err := checkHistory(&out, ts.Client(), ts.URL, chain, to, a, false)
			if err == nil || !strings.Contains(err.Error(), "refusing to post") {
				t.Errorf("err = %v, want a refusal to post; output %q", err, out.String())
			}
		})
	}
}

// A hub with no history to give is not a failure: the tool says it could not
// check and the post goes ahead. That is an older hub, signed or not, a sender
// the hub does not know, a hub that cannot be reached, or a reply it signed
// but the tool cannot read.
func TestCheckHistoryCarriesOnWithoutHistory(t *testing.T) {
	keys := loadKeys(t, "a", "c", "stranger")
	a, c, stranger := keys[0], keys[1], keys[2]
	to := c.PubKeyBase64()
	signed := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			sig, err := protocol.SignResponse(c, r.Header.Get("X-V3Net-Signature"), r.Header.Get(protocol.HeaderNonce), status, []byte(body))
			if err != nil {
				t.Error(err)
			}
			w.Header().Set(protocol.HeaderHubSignature, sig)
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}
	}
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"older hub, signed 404", signed(http.StatusNotFound, "404 page not found\n"), "answered 404"},
		{"older hub, unsigned 404", http.NotFound, "answered 404"},
		{"signed 401", signed(http.StatusUnauthorized, `{"error":"unknown or inactive node"}`), "answered 401"},
		{"signed 200 it cannot read", signed(http.StatusOK, "not json"), "could not be read"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(tc.handler)
			defer ts.Close()
			var out bytes.Buffer
			if _, err := checkHistory(&out, ts.Client(), ts.URL, chainFrom(t, to, a), to, a, false); err != nil {
				t.Fatalf("checkHistory: %v", err)
			}
			if !strings.Contains(out.String(), "Could not check") || !strings.Contains(out.String(), tc.want) {
				t.Errorf("output %q, want it to say it could not check and %q", out.String(), tc.want)
			}
		})
	}

	t.Run("hub unreachable", func(t *testing.T) {
		ts := httptest.NewServer(http.NotFoundHandler())
		url, client := ts.URL, ts.Client()
		ts.Close()
		var out bytes.Buffer
		if _, err := checkHistory(&out, client, url, chainFrom(t, to, a), to, a, false); err != nil {
			t.Fatalf("checkHistory: %v", err)
		}
		if !strings.Contains(out.String(), "Could not check") {
			t.Errorf("output %q", out.String())
		}
	})

	t.Run("real hub, sender unknown", func(t *testing.T) {
		hubTS := switchedHubServer(t, a, a, c)
		var out bytes.Buffer
		if _, err := checkHistory(&out, hubTS.Client(), hubTS.URL, chainFrom(t, to, a), to, stranger, false); err != nil {
			t.Fatalf("checkHistory: %v", err)
		}
		if !strings.Contains(out.String(), "answered 401") {
			t.Errorf("output %q", out.String())
		}
	})

	t.Run("a bad signature on a refusal still stops the tool", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(protocol.HeaderHubSignature, "bm90LWEtc2lnbmF0dXJl")
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()
		_, err := checkHistory(io.Discard, ts.Client(), ts.URL, chainFrom(t, to, a), to, a, false)
		if err == nil || !strings.Contains(err.Error(), "does not verify") {
			t.Errorf("err = %v, want it to say the reply does not verify", err)
		}
	})
}

// matchChain and a real hub agree on every chain. Each case drives a hub
// through its steps: "a" runs it on key a and re-signs the NAL, "=a" runs it
// on key a without re-signing, and "a>b>c" posts that chain. The last chain
// is then checked by the tool, with -omit-lost-keys so only the hub's own rules
// are in play, and posted to the hub. needsOmit says the tool also asks for
// the flag without it.
func TestMatchChainAgreesWithHub(t *testing.T) {
	cases := []struct {
		name      string
		steps     []string
		chain     string
		accepted  bool
		needsOmit bool
	}{
		{"plain handover", []string{"a"}, "a>b", true, false},
		{"planned chain", []string{"a"}, "a>b>c", true, false},
		{"not from the key in force", []string{"a"}, "x>b", false, false},
		{"late, NAL still the old key's", []string{"a", "=c"}, "a>c", true, false},
		{"late, re-signed", []string{"a", "c"}, "a>c", true, false},
		{"re-signed twice, the chain needed", []string{"a", "b", "c"}, "a>b>c", true, false},
		{"re-signed twice, skipping B", []string{"a", "b", "c"}, "a>c", true, true},
		{"re-signed twice, starting at B", []string{"a", "b", "c"}, "b>c", true, true},
		{"re-signed twice, out of order", []string{"a", "b", "c"}, "b>a>c", false, false},
		{"re-signed twice, through a third key", []string{"a", "b", "c"}, "a>x>c", false, false},
		{"re-signed twice, ending elsewhere", []string{"a", "b", "c"}, "a>b>x", false, false},
		{"NAL B's, the chain needed", []string{"a", "b", "=c"}, "a>b>c", true, false},
		{"NAL B's, past its key", []string{"a", "b", "=c"}, "a>c", false, false},
		{"NAL B's, starting at it", []string{"a", "b", "=c"}, "b>c", true, true},
		{"onward, earlier signers unbridged", []string{"a", "b", "c"}, "c>d", true, true},
		{"onward through a recorded key", []string{"a", "b", "c"}, "c>a>d", false, false},
		{"held, onward", []string{"a", "b", "c", "a>b>c"}, "c>d", true, false},
		{"held, repeated", []string{"a", "b", "c", "a>b>c"}, "a>b>c", true, false},
		{"held, one link repeated", []string{"a", "b", "c", "a>b>c"}, "b>c", true, false},
		{"held, repeat then onward", []string{"a", "b", "c", "a>b>c"}, "b>c>d", true, false},
		{"held, on then back to a used key", []string{"a", "b", "c", "a>b>c"}, "c>d>a", false, false},
		{"held, not from the key in force", []string{"a", "b", "c", "a>b>c"}, "a>d", false, false},
		{"held, hub switched, through a third key", []string{"a", "a>b", "=d"}, "b>c>d", false, false},
		{"held, hub switched, straight to it", []string{"a", "a>b", "=d"}, "b>d", true, false},
		{"went back, NAL B's, back to the hub", []string{"a", "b", "=a"}, "b>a", false, false},
		{"went back, re-signed, onward", []string{"a", "b", "a"}, "a>c", true, true},
		{"went back, the chain from a new key", []string{"a", "b", "a", "=c"}, "b>a>c", true, false},
		{"went back, new key, skipping B", []string{"a", "b", "a", "=c"}, "a>c", true, true},
		{"a key twice", []string{"a"}, "a>b>a", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			names := []string{"a", "b", "c", "d", "x", "sender"}
			loaded := loadKeys(t, names...)
			keys := make(map[string]*keystore.Keystore, len(names))
			for i, name := range names {
				keys[name] = loaded[i]
			}
			sender := keys["sender"]
			dir := t.TempDir()

			var current atomic.Pointer[hub.Hub]
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				current.Load().Mux().ServeHTTP(w, r)
			}))
			defer ts.Close()
			var hubKey string
			open := func(name string) {
				t.Helper()
				if old := current.Load(); old != nil {
					_ = old.Close()
				}
				h, err := hub.New(hub.Config{ListenAddr: ":0", DataDir: dir, Keystore: keys[name], AutoApprove: true,
					Networks: []hub.NetworkConfig{{Name: "testnet"}}})
				if err != nil {
					t.Fatalf("create hub: %v", err)
				}
				current.Store(h)
				hubKey = keys[name].PubKeyBase64()
			}
			defer func() { _ = current.Load().Close() }()
			signed := func(chain string) []protocol.CoordHandover {
				t.Helper()
				parts := strings.Split(chain, ">")
				signers := make([]*keystore.Keystore, 0, len(parts)-1)
				for _, name := range parts[:len(parts)-1] {
					signers = append(signers, keys[name])
				}
				return chainFrom(t, keys[parts[len(parts)-1]].PubKeyBase64(), signers...)
			}
			post := func(chain []protocol.CoordHandover) int {
				t.Helper()
				body, err := chainBody(chain)
				if err != nil {
					t.Fatal(err)
				}
				path := "/v3net/v1/testnet/nal/handover"
				sum := sha256.Sum256(body)
				date := time.Now().UTC().Format(http.TimeFormat)
				sig, err := sender.Sign(http.MethodPost, path, date, hex.EncodeToString(sum[:]))
				if err != nil {
					t.Fatal(err)
				}
				req, err := http.NewRequest(http.MethodPost, ts.URL+path, bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Date", date)
				req.Header.Set("X-V3Net-Node-ID", sender.NodeID())
				req.Header.Set("X-V3Net-Signature", sig)
				resp, err := ts.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				_ = resp.Body.Close()
				return resp.StatusCode
			}

			for i, step := range tc.steps {
				switch {
				case strings.Contains(step, ">"):
					if got := post(signed(step)); got != http.StatusOK {
						t.Fatalf("step %s: status %d", step, got)
					}
				case strings.HasPrefix(step, "="):
					open(strings.TrimPrefix(step, "="))
				default:
					open(step)
					h := current.Load()
					n, err := h.NALStore().Get("testnet")
					if err != nil {
						t.Fatal(err)
					}
					if i == 0 {
						n = &protocol.NAL{V3NetNAL: "1.0", Network: "testnet", Areas: []protocol.Area{}}
					}
					if err := h.SignNAL(n, nil); err != nil {
						t.Fatalf("step %s: %v", step, err)
					}
					if err := h.NALStore().Put("testnet", n); err != nil {
						t.Fatal(err)
					}
				}
				if i == 0 {
					sub, _ := json.Marshal(protocol.SubscribeRequest{Network: "testnet", NodeID: sender.NodeID(),
						PubKeyB64: sender.PubKeyBase64(), BBSName: "Sender", BBSHost: "sender.example.net"})
					resp, err := http.Post(ts.URL+"/v3net/v1/subscribe", "application/json", bytes.NewReader(sub))
					if err != nil {
						t.Fatal(err)
					}
					_ = resp.Body.Close()
				}
			}

			history, why, _, err := fetchHistory(ts.Client(), ts.URL, "testnet", hubKey, sender)
			if err != nil || history == nil {
				t.Fatalf("fetch history: %v %s", err, why)
			}
			chain := signed(tc.chain)
			_, withOmit := matchChain(history, hubKey, chain, true)
			_, strict := matchChain(history, hubKey, chain, false)

			status := post(chain)
			if (status == http.StatusOK) != tc.accepted {
				t.Errorf("hub answered %d, want accepted = %v", status, tc.accepted)
			}
			if (withOmit == nil) != (status == http.StatusOK) {
				t.Errorf("tool with -omit-lost-keys says %v, hub answered %d", withOmit, status)
			}
			if wantStrict := tc.accepted && !tc.needsOmit; (strict == nil) != wantStrict {
				t.Errorf("tool without -omit-lost-keys says %v, want accepted = %v", strict, wantStrict)
			}
		})
	}
}

// With the hub back on a key it already ran, the chain the tool points to
// works: restarted on the new key, the hub takes it whole.
func TestWentBackAdviceNamesTheChain(t *testing.T) {
	keys := loadKeys(t, "a", "b", "c")
	a, b, c := keys[0].PubKeyBase64(), keys[1].PubKeyBase64(), keys[2].PubKeyBase64()
	history := &protocol.SignerHistory{NALKeyB64: a, KeyInForceB64: a, PrevSigners: []string{a, b}}
	_, err := matchChain(history, a, chainFrom(t, c, keys[0]), false)
	if err == nil {
		t.Fatal("A to C with B unbridged passed without -omit-lost-keys")
	}
	want := describeChain([]string{b, a}, c)
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not contain the chain %q", err, want)
	}
}
