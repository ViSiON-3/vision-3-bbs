package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// countPosts passes requests to next and counts the POSTs.
func countPosts(next http.Handler, posts *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
		}
		next.ServeHTTP(w, r)
	}))
}

// passTo returns a handler that forwards a request to target.
func passTo(t *testing.T, target *httptest.Server) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := http.NewRequest(r.Method, target.URL+r.URL.Path, r.Body)
		req.Header = r.Header.Clone()
		resp, err := target.Client().Do(req)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})
}

// A dry run against a real hub makes the same checks as a real run and reports
// its verdict, and never posts: the chain needed passes, and a chain that
// would be refused is refused, naming the chain needed.
func TestDryRunChecksAgainstHub(t *testing.T) {
	keys := loadKeys(t, "a", "b", "c", "x")
	a, b, c, x := keys[0], keys[1], keys[2], keys[3]
	hubTS := switchedHubServer(t, a, a, b, c)
	var posts atomic.Int32
	ts := countPosts(passTo(t, hubTS), &posts)
	defer ts.Close()
	to := c.PubKeyBase64()

	cases := []struct {
		name     string
		chain    []protocol.CoordHandover
		omitLost bool
		ok       bool
		want     []string
	}{
		{"the chain needed", chainFrom(t, to, a, b), false, true, []string{"already runs the new key", "matches", "would post this chain"}},
		{"leaves out a recorded key", chainFrom(t, to, a), false, false, []string{"would stop here", "-omit-lost-keys", b.PubKeyBase64()}},
		{"leaves out a lost key", chainFrom(t, to, a), true, true, []string{"leaving out 1", "would post this chain"}},
		{"a chain the hub would refuse", chainFrom(t, to, b, a), true, false, []string{"would stop here", "would refuse"}},
		{"a hub on neither key", chainFrom(t, x.PubKeyBase64(), a), false, false, []string{"would stop here", "neither"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			ok := dryRunCheck(&out, &errOut, ts.Client(), ts.URL, tc.chain, a, tc.omitLost)
			all := out.String() + errOut.String()
			if ok != tc.ok {
				t.Errorf("dryRunCheck = %v, want %v; output %q", ok, tc.ok, all)
			}
			for _, want := range tc.want {
				if !strings.Contains(all, want) {
					t.Errorf("output %q does not contain %q", all, want)
				}
			}
			if strings.Contains(all, "Posting") {
				t.Errorf("output %q says it is posting", all)
			}
			if !tc.ok && strings.Contains(all, "would post") {
				t.Errorf("output %q passes a chain it refuses", all)
			}
		})
	}
	if got := posts.Load(); got != 0 {
		t.Errorf("the dry runs posted %d times", got)
	}
}

// A dry run with nothing to check against still works, and says the chain was
// not checked: a hub that cannot be reached, as when signing offline, and a
// hub with no history to give. Neither reads as a pass.
func TestDryRunCouldNotCheck(t *testing.T) {
	keys := loadKeys(t, "a", "c")
	a, c := keys[0], keys[1]
	to := c.PubKeyBase64()
	chain := chainFrom(t, to, a)

	check := func(t *testing.T, ok bool, out, errOut *bytes.Buffer, want string) {
		t.Helper()
		all := out.String() + errOut.String()
		if !ok {
			t.Errorf("dryRunCheck = false, output %q", all)
		}
		for _, s := range []string{"Could not check", want} {
			if !strings.Contains(out.String(), s) {
				t.Errorf("output %q does not contain %q", all, s)
			}
		}
		if strings.Contains(all, "checks pass") || strings.Contains(all, "Posting") {
			t.Errorf("output %q claims a pass or a post", all)
		}
	}

	t.Run("hub unreachable", func(t *testing.T) {
		ts := httptest.NewServer(http.NotFoundHandler())
		url, client := ts.URL, ts.Client()
		ts.Close()
		var out, errOut bytes.Buffer
		ok := dryRunCheck(&out, &errOut, client, url, chain, a, false)
		check(t, ok, &out, &errOut, "could not be reached")
	})

	t.Run("older hub, no signer history", func(t *testing.T) {
		var posts atomic.Int32
		ts := countPosts(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/info") {
				_ = json.NewEncoder(w).Encode(protocol.NetworkInfo{Name: "testnet", HubPubKeyB64: a.PubKeyBase64()})
				return
			}
			http.NotFound(w, r)
		}), &posts)
		defer ts.Close()
		var out, errOut bytes.Buffer
		ok := dryRunCheck(&out, &errOut, ts.Client(), ts.URL, chain, a, false)
		check(t, ok, &out, &errOut, "answered 404")
		if got := posts.Load(); got != 0 {
			t.Errorf("the dry run posted %d times", got)
		}
	})

	// The hub answered, so this is not offline use: a real run stops, and so
	// does the verdict.
	t.Run("a hub that will not say which key it runs", func(t *testing.T) {
		ts := httptest.NewServer(http.NotFoundHandler())
		defer ts.Close()
		var out, errOut bytes.Buffer
		if dryRunCheck(&out, &errOut, ts.Client(), ts.URL, chain, a, false) {
			t.Errorf("dryRunCheck = true, output %q", out.String()+errOut.String())
		}
	})

	t.Run("a redirect loop at the hub's key check", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, r.URL.Path, http.StatusFound)
		}))
		defer ts.Close()
		var out, errOut bytes.Buffer
		if dryRunCheck(&out, &errOut, ts.Client(), ts.URL, chain, a, false) {
			t.Errorf("dryRunCheck = true, output %q", out.String()+errOut.String())
		}
		if !strings.Contains(errOut.String(), "would stop here") || strings.Contains(out.String(), "Could not check") {
			t.Errorf("output %q %q, want a real run's stop, not offline use", out.String(), errOut.String())
		}
	})
}

// A dry run believes the signer history only as a real run does: a reply that
// does not verify stops it, whatever it says.
func TestDryRunRefusesUnverifiedHistory(t *testing.T) {
	keys := loadKeys(t, "a", "c")
	a, c := keys[0], keys[1]
	to := c.PubKeyBase64()
	forged, _ := json.Marshal(protocol.SignerHistory{Network: "testnet", NALKeyB64: to, KeyInForceB64: to, PrevSigners: []string{a.PubKeyBase64()}})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/info") {
			_ = json.NewEncoder(w).Encode(protocol.NetworkInfo{Name: "testnet", HubPubKeyB64: to})
			return
		}
		_, _ = w.Write(forged)
	}))
	defer ts.Close()
	var out, errOut bytes.Buffer
	if dryRunCheck(&out, &errOut, ts.Client(), ts.URL, chainFrom(t, to, a), a, false) {
		t.Errorf("dryRunCheck passed a forged history: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "unsigned") {
		t.Errorf("output %q, want it to say the reply is unsigned", errOut.String())
	}
}

// publish believes the reply to its post only if it verifies. Whatever a reply
// that does not verify says, the outcome is reported as unknown, with the way
// to find out; a good signature gets the usual outcome, for a refusal too. An
// unsigned reply is an older hub's only if the hub is not known to sign.
func TestPublishVerifiesResponse(t *testing.T) {
	keys := loadKeys(t, "old", "new", "evil")
	oldKS, newKS, evil := keys[0], keys[1], keys[2]
	chain := chainFrom(t, newKS.PubKeyBase64(), oldKS)
	body, err := chainBody(chain)
	if err != nil {
		t.Fatal(err)
	}

	const okBody = `{"ok":true}`
	sign := func(t *testing.T, ks *keystore.Keystore, r *http.Request, nonce string, status int, data string) string {
		t.Helper()
		sig, err := protocol.SignResponse(ks, r.Header.Get("X-V3Net-Signature"), nonce, status, []byte(data))
		if err != nil {
			t.Fatal(err)
		}
		return sig
	}
	cases := []struct {
		name     string
		hubSigns bool
		reply    func(t *testing.T, w http.ResponseWriter, r *http.Request)
		ok       bool
		unknown  bool
		want     string
	}{
		{"signed 200", true, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
			w.Header().Set(protocol.HeaderHubSignature, sign(t, oldKS, r, r.Header.Get(protocol.HeaderNonce), 200, okBody))
			_, _ = w.Write([]byte(okBody))
		}, true, false, "Handover published"},
		{"signed refusal", true, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
			w.Header().Set(protocol.HeaderHubSignature, sign(t, oldKS, r, r.Header.Get(protocol.HeaderNonce), 409, `{"error":"no"}`))
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"no"}`))
		}, false, false, "Response: 409"},
		{"200 signed by another key", true, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
			w.Header().Set(protocol.HeaderHubSignature, sign(t, evil, r, r.Header.Get(protocol.HeaderNonce), 200, okBody))
			_, _ = w.Write([]byte(okBody))
		}, false, true, "does not verify"},
		{"200 with the body changed", true, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
			w.Header().Set(protocol.HeaderHubSignature, sign(t, oldKS, r, r.Header.Get(protocol.HeaderNonce), 200, `{"error":"no"}`))
			_, _ = w.Write([]byte(okBody))
		}, false, true, "does not verify"},
		{"200 with the status changed", true, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
			w.Header().Set(protocol.HeaderHubSignature, sign(t, oldKS, r, r.Header.Get(protocol.HeaderNonce), 409, okBody))
			_, _ = w.Write([]byte(okBody))
		}, false, true, "does not verify"},
		{"200 replayed from another request", true, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
			w.Header().Set(protocol.HeaderHubSignature, sign(t, oldKS, r, "an-older-nonce", 200, okBody))
			_, _ = w.Write([]byte(okBody))
		}, false, true, "does not verify"},
		{"forged refusal", true, func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(protocol.HeaderHubSignature, "bm90LWEtc2lnbmF0dXJl")
			w.WriteHeader(http.StatusConflict)
		}, false, true, "does not verify"},
		{"body claiming success", true, func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(protocol.HeaderHubSignature, "bm90LWEtc2lnbmF0dXJl")
			_, _ = w.Write([]byte("\nHandover published. All is well."))
		}, false, true, "does not verify"},
		{"unsigned 200 from a hub that signs", true, func(_ *testing.T, w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(okBody))
		}, false, true, "unsigned"},
		{"unsigned 502 from a hub that signs", true, func(_ *testing.T, w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}, false, true, "unsigned"},
		{"older hub, unsigned 200", false, func(_ *testing.T, w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(okBody))
		}, true, false, "did not sign"},
		{"older hub, unsigned refusal", false, func(_ *testing.T, w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusConflict)
		}, false, false, "Response: 409"},
		{"bad signature from a hub not known to sign", false, func(_ *testing.T, w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(protocol.HeaderHubSignature, "bm90LWEtc2lnbmF0dXJl")
			_, _ = w.Write([]byte(okBody))
		}, false, true, "does not verify"},
		{"no reply at all", true, func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
		}, false, true, "POST handover"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get(protocol.HeaderNonce) == "" {
					t.Error("the post carries no nonce")
				}
				tc.reply(t, w, r)
			}))
			defer ts.Close()
			var out, errOut bytes.Buffer
			ok := publish(&out, &errOut, ts.Client(), ts.URL, chain, body, oldKS, false, false, oldKS.PubKeyBase64(), tc.hubSigns)
			all := out.String() + errOut.String()
			if ok != tc.ok {
				t.Errorf("publish = %v, want %v; output %q", ok, tc.ok, all)
			}
			if !strings.Contains(all, tc.want) {
				t.Errorf("output %q does not contain %q", all, tc.want)
			}
			if said := strings.Contains(errOut.String(), "may or may not") && strings.Contains(errOut.String(), "-dry-run"); said != tc.unknown {
				t.Errorf("reported an unknown outcome: %v, want %v; output %q", said, tc.unknown, all)
			}
			// Nothing on its own line may read as the tool's own success.
			if !tc.ok {
				for _, line := range strings.Split(all, "\n") {
					if strings.HasPrefix(line, "Handover published") {
						t.Errorf("reported success on %q", all)
					}
				}
			}
			if tc.unknown && strings.Contains(out.String(), "Response:") {
				t.Errorf("printed an unverified response as a response: %q", out.String())
			}
		})
	}
}

// Against a real hub: a post verifies under the key the hub runs, whether that
// is the old key or the new one, and a reply that has been tampered with after
// the hub took the handover reports an unknown outcome. The dry run the
// operator is then told to use confirms the post arrived.
func TestPublishVerifiesRealHub(t *testing.T) {
	keys := loadKeys(t, "a", "b", "c")
	a, b, c := keys[0], keys[1], keys[2]

	t.Run("the hub still on the old key", func(t *testing.T) {
		ts := switchedHubServer(t, a, a)
		chain := chainFrom(t, b.PubKeyBase64(), a)
		body, _ := chainBody(chain)
		var out, errOut bytes.Buffer
		if !publish(&out, &errOut, ts.Client(), ts.URL, chain, body, a, false, false, a.PubKeyBase64(), true) {
			t.Fatalf("publish failed: %s%s", out.String(), errOut.String())
		}
		if strings.Contains(out.String(), "did not sign") {
			t.Errorf("a real hub's reply read as unsigned: %q", out.String())
		}
	})

	t.Run("tampered reply, then the dry run", func(t *testing.T) {
		hubTS := switchedHubServer(t, a, a, c)
		var strip atomic.Bool
		forward := passTo(t, hubTS)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && strip.Load() {
				forward.ServeHTTP(&forgedStatus{ResponseWriter: w, status: http.StatusConflict}, r)
				return
			}
			forward.ServeHTTP(w, r)
		}))
		defer ts.Close()
		to := c.PubKeyBase64()
		chain := chainFrom(t, to, a)
		body, _ := chainBody(chain)

		var before bytes.Buffer
		if !dryRunCheck(&before, io.Discard, ts.Client(), ts.URL, chain, a, false) || strings.Contains(before.String(), "already holds") {
			t.Fatalf("before the post: %q", before.String())
		}

		strip.Store(true)
		var out, errOut bytes.Buffer
		if publish(&out, &errOut, ts.Client(), ts.URL, chain, body, a, false, true, to, true) {
			t.Fatalf("publish believed a forged status: %s%s", out.String(), errOut.String())
		}
		if !strings.Contains(errOut.String(), "may or may not") {
			t.Errorf("output %q, want an unknown outcome", errOut.String())
		}

		var after, afterErr bytes.Buffer
		if !dryRunCheck(&after, &afterErr, ts.Client(), ts.URL, chain, a, false) || !strings.Contains(after.String(), "already holds") {
			t.Errorf("after the post: %q %q; want the dry run to confirm it arrived", after.String(), afterErr.String())
		}
		n, err := nal.Fetch(t.Context(), ts.URL+"/v3net/v1/testnet/nal", ts.Client())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := nal.FollowHandovers("testnet", a.PubKeyBase64(), to, n.Handovers); err != nil {
			t.Errorf("the served NAL carries no chain from A; the post should have taken effect: %v", err)
		}
	})
}

// forgedStatus passes a reply on with another status, as something between the
// tool and the hub might.
type forgedStatus struct {
	http.ResponseWriter
	status int
}

func (f *forgedStatus) WriteHeader(int) { f.ResponseWriter.WriteHeader(f.status) }
