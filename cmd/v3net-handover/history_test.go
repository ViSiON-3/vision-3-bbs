package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
		{"-via out of order", resigned, c, chainFrom(t, key(c), b, a), false, "does not match"},
		{"-via out of order, omit is no help", resigned, c, chainFrom(t, key(c), b, a), true, "does not match"},
		{"-via through a third key", resigned, c, chainFrom(t, key(c), a, x), true, "does not match"},
		{"NAL's key, the chain needed", unsigned, c, chainFrom(t, key(c), a, b), false, ""},
		{"past the NAL's key", unsigned, c, chainFrom(t, key(c), a), true, "does not match"},
		{"plain handover", plain, a, chainFrom(t, key(b), a), false, ""},
		{"planned chain to come", plain, a, chainFrom(t, key(c), a, b), false, ""},
		{"onward, earlier signers unbridged", earlier, c, chainFrom(t, key(d), c), false, key(b)},
		{"onward, earlier signers lost", earlier, c, chainFrom(t, key(d), c), true, ""},
		{"hub went back to a used key", wentBack, a, chainFrom(t, key(a), b), false, "already signed"},
		{"nothing to hand over", &protocol.SignerHistory{NALKeyB64: key(c), KeyInForceB64: key(c)}, c, chainFrom(t, key(c), a), false, "no handover to take"},
		{"held, reposted", held, c, chainFrom(t, key(c), a, b), false, ""},
		{"held, one link reposted", held, c, chainFrom(t, key(c), b), false, ""},
		{"held, on from the key in force", held, c, chainFrom(t, key(d), c), false, ""},
		{"held, repeat then on", held, c, chainFrom(t, key(d), b, c), false, ""},
		{"held, not from the key in force", held, c, chainFrom(t, key(d), a), false, "key in force"},
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
		if err := h.SignNAL(n); err != nil {
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
	if err := checkHistory(&out, ts.Client(), ts.URL, chainFrom(t, to, a, b), to, a, false); err != nil {
		t.Fatalf("the chain needed: %v", err)
	}
	if !strings.Contains(out.String(), "matches") || !strings.Contains(out.String(), b.PubKeyBase64()) {
		t.Errorf("output %q, want the match and the chain", out.String())
	}
	err := checkHistory(&out, ts.Client(), ts.URL, chainFrom(t, to, a), to, a, false)
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
				if err := checkHistory(io.Discard, ts.Client(), ts.URL, chainFrom(t, to, a, b), to, a, false); err != nil {
					t.Fatalf("the first, genuine reply: %v", err)
				}
			}
			var out bytes.Buffer
			chain := chainFrom(t, to, a)
			if tc.name == "an earlier reply replayed" {
				chain = chainFrom(t, to, a, b)
			}
			err := checkHistory(&out, ts.Client(), ts.URL, chain, to, a, false)
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
			if err := checkHistory(&out, ts.Client(), ts.URL, chainFrom(t, to, a), to, a, false); err != nil {
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
		if err := checkHistory(&out, client, url, chainFrom(t, to, a), to, a, false); err != nil {
			t.Fatalf("checkHistory: %v", err)
		}
		if !strings.Contains(out.String(), "Could not check") {
			t.Errorf("output %q", out.String())
		}
	})

	t.Run("real hub, sender unknown", func(t *testing.T) {
		hubTS := switchedHubServer(t, a, a, c)
		var out bytes.Buffer
		if err := checkHistory(&out, hubTS.Client(), hubTS.URL, chainFrom(t, to, a), to, stranger, false); err != nil {
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
		err := checkHistory(io.Discard, ts.Client(), ts.URL, chainFrom(t, to, a), to, a, false)
		if err == nil || !strings.Contains(err.Error(), "does not verify") {
			t.Errorf("err = %v, want it to say the reply does not verify", err)
		}
	})
}
