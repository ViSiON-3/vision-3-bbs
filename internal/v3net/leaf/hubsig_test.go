package leaf

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// hubProxy sits between a leaf and a real hub, counting the signed requests
// that reach the hub and optionally rewriting response bodies or stripping
// the hub's signature header.
type hubProxy struct {
	ts     *httptest.Server
	signed atomic.Int64
	events atomic.Int64
	// stripSig removes X-V3Net-Hub-Signature from every response.
	stripSig atomic.Bool

	mu      sync.Mutex
	rewrite func(path string, body []byte) []byte
}

func newHubProxy(t *testing.T, hubURL string) *hubProxy {
	t.Helper()
	target, err := url.Parse(hubURL)
	if err != nil {
		t.Fatal(err)
	}
	p := &hubProxy{}
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.ModifyResponse = func(resp *http.Response) error {
		if p.stripSig.Load() {
			resp.Header.Del(protocol.HeaderHubSignature)
		}
		p.mu.Lock()
		rw := p.rewrite
		p.mu.Unlock()
		if rw == nil || strings.HasSuffix(resp.Request.URL.Path, "/events") {
			return nil
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return err
		}
		body = rw(resp.Request.URL.Path, body)
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
		resp.Header.Del("Content-Length")
		return nil
	}
	p.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-V3Net-Signature") != "" {
			p.signed.Add(1)
			if strings.HasSuffix(r.URL.Path, "/events") {
				p.events.Add(1)
			}
		}
		rp.ServeHTTP(w, r)
	}))
	t.Cleanup(p.ts.Close)
	return p
}

func (p *hubProxy) setRewrite(fn func(path string, body []byte) []byte) {
	p.mu.Lock()
	p.rewrite = fn
	p.mu.Unlock()
}

// proxiedLeaf returns a leaf subscribed to a real hub through a hubProxy,
// with its hub key pinned to hubPin, and the hub's own key.
func proxiedLeaf(t *testing.T, hubPin func(hubKS *keystore.Keystore) string) (*Leaf, *keyRecorder, *hubProxy, *keystore.Keystore) {
	t.Helper()
	ts, _, hubKS := newTestHub(t, true)
	p := newHubProxy(t, ts.URL)
	l := subscribedLeaf(t, p.ts, "Leaf BBS")
	rec := &keyRecorder{}
	rec.attach(l)
	l.pins.hub = hubPin(hubKS)
	return l, rec, p, hubKS
}

// waitFor polls cond until it holds or 5s pass.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Responses from a hub whose key is pinned are verified and accepted.
func TestSignedHubResponsesVerify(t *testing.T) {
	l, rec, _, _ := proxiedLeaf(t, (*keystore.Keystore).PubKeyBase64)
	if _, err := l.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if err := l.SendLogon("tester"); err != nil {
		t.Fatalf("SendLogon: %v", err)
	}
	if l.HubPaused() || l.unsignedHubLogged.Load() {
		t.Errorf("paused=%v unsignedLogged=%v for a signing hub with the right pin", l.HubPaused(), l.unsignedHubLogged.Load())
	}
	if _, changed := rec.counts(); changed != 0 {
		t.Errorf("key changes reported: %+v", rec.changed)
	}
}

// A response altered on the way is refused. The hub's /info still serves
// the pinned key, so nothing is reported and syncing carries on.
func TestTamperedResponseRefused(t *testing.T) {
	l, rec, p, _ := proxiedLeaf(t, (*keystore.Keystore).PubKeyBase64)
	p.setRewrite(func(path string, body []byte) []byte {
		if strings.HasSuffix(path, "/messages") {
			return append(body, ' ')
		}
		return body
	})
	if _, err := l.Poll(context.Background()); !errors.Is(err, ErrHubSignature) {
		t.Fatalf("Poll of a tampered page = %v, want ErrHubSignature", err)
	}
	if l.HubPaused() {
		t.Error("still paused although /info serves the pinned key")
	}
	if _, changed := rec.counts(); changed != 0 {
		t.Errorf("key changes reported: %+v", rec.changed)
	}
	p.setRewrite(nil)
	if _, err := l.Poll(context.Background()); err != nil {
		t.Errorf("Poll after tampering stopped: %v", err)
	}
}

// A hub signing with a key other than the pin pauses polling, SSE and
// sending, reports the hub's key as pending, and resumes once the sysop
// accepts that key.
func TestHubKeyChangePausesUntilAccepted(t *testing.T) {
	oldKey := newCoordKey(t).PubKeyBase64()
	l, rec, p, hubKS := proxiedLeaf(t, func(*keystore.Keystore) string { return oldKey })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := l.Poll(ctx); !errors.Is(err, ErrHubSignature) {
		t.Fatalf("Poll = %v, want ErrHubSignature", err)
	}
	if !l.HubPaused() {
		t.Fatal("not paused after the hub signed with another key")
	}
	if _, changed := rec.counts(); changed != 1 || rec.changed[0].Kind != KeyHub ||
		rec.changed[0].Offered != hubKS.PubKeyBase64() || rec.changed[0].Pinned != oldKey {
		t.Fatalf("key changes = %+v, want the hub's key offered once", rec.changed)
	}
	if got := l.PendingKey(KeyHub); got != hubKS.PubKeyBase64() {
		t.Errorf("pending hub key = %q", got)
	}

	// Nothing reaches the hub while paused, and the SSE loop holds off.
	sseDone := make(chan struct{})
	go func() {
		defer close(sseDone)
		l.RunSSE(ctx)
	}()
	before := p.signed.Load()
	if _, err := l.Poll(ctx); !errors.Is(err, ErrHubPaused) {
		t.Errorf("Poll while paused = %v, want ErrHubPaused", err)
	}
	if err := l.SendLogon("tester"); !errors.Is(err, ErrHubPaused) {
		t.Errorf("SendLogon while paused = %v, want ErrHubPaused", err)
	}
	time.Sleep(200 * time.Millisecond)
	if n := p.signed.Load() - before; n != 0 {
		t.Errorf("%d signed requests reached the hub while paused", n)
	}

	// The sysop accepts the new key.
	events := make(chan protocol.Event, 64)
	l.SetOnEvent(func(ev protocol.Event) {
		select {
		case events <- ev:
		default:
		}
	})
	l.UpdatePins(ctx, Pins{Hub: hubKS.PubKeyBase64(), PendingHub: hubKS.PubKeyBase64()})
	waitFor(t, "resume", func() bool { return !l.HubPaused() })
	if _, err := l.Poll(ctx); err != nil {
		t.Errorf("Poll after acceptance: %v", err)
	}
	waitFor(t, "SSE reconnect", func() bool { return p.events.Load() > 0 })
	deadline := time.Now().Add(5 * time.Second)
	for got := false; !got; {
		if time.Now().After(deadline) {
			t.Fatal("no signed SSE event delivered after acceptance")
		}
		if err := l.SendLogon(probeHandle); err != nil {
			t.Fatalf("SendLogon after acceptance: %v", err)
		}
		select {
		case ev := <-events:
			got = ev.Type == protocol.EventLogon
		case <-time.After(100 * time.Millisecond):
		}
	}
	if p := l.PendingKey(KeyHub); p != "" {
		t.Errorf("pending hub key %q left after acceptance", p)
	}
	cancel()
	<-sseDone
}

// A pause drops an SSE stream that is already open.
func TestPauseDropsOpenStream(t *testing.T) {
	l, _, p, _ := proxiedLeaf(t, (*keystore.Keystore).PubKeyBase64)
	done := make(chan error, 1)
	go func() { done <- l.connectSSE(context.Background(), false) }()
	waitFor(t, "SSE connect", func() bool { return p.events.Load() > 0 })
	l.pauseHub("test")
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("connectSSE = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream still open after pause")
	}
}

// A hub that predates signing (no signature header) is accepted
// unverified, even with a hub key pinned, and noted once.
func TestUnsignedHubTolerated(t *testing.T) {
	ts, _ := newMockHub(t, []protocol.Message{testMessage("550e8400-e29b-41d4-a716-446655440000")})
	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	l.pins.hub = newCoordKey(t).PubKeyBase64()

	if n, err := l.Poll(context.Background()); err != nil || n != 1 {
		t.Fatalf("Poll = %d, %v; want 1 message", n, err)
	}
	if err := l.SendMessage(ownMessage(l, "550e8400-e29b-41d4-a716-446655440001")); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if l.HubPaused() {
		t.Error("paused by an unsigned hub")
	}
	if !l.unsignedHubLogged.Load() {
		t.Error("unsigned hub not noted")
	}
}

// fakeSSEHub serves /info with its key and an SSE stream whose events are
// written by write, given the opening request's signature and nonce.
func fakeSSEHub(t *testing.T, hubKS *keystore.Keystore, write func(w io.Writer, req reqBinding)) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/info") {
			_ = json.NewEncoder(w).Encode(protocol.NetworkInfo{
				Name: "testnet", HubNodeID: hubKS.NodeID(), HubPubKeyB64: hubKS.PubKeyBase64(),
			})
			return
		}
		req := reqBinding{sig: r.Header.Get("X-V3Net-Signature"), nonce: r.Header.Get(protocol.HeaderNonce)}
		sig, _ := protocol.SignResponse(hubKS, req.sig, req.nonce, http.StatusOK, nil)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set(protocol.HeaderHubSignature, sig)
		w.WriteHeader(http.StatusOK)
		write(w, req)
		w.(http.Flusher).Flush()
	}))
	t.Cleanup(ts.Close)
	return ts
}

// signedEvent formats one event as the hub sends it.
func signedEvent(t *testing.T, hubKS *keystore.Keystore, req reqBinding, seq uint64, typ, data string) string {
	t.Helper()
	sig, err := protocol.SignEvent(hubKS, "testnet", req.sig, req.nonce, seq, typ, []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("event: %s\nid: %d\nsig: %s\ndata: %s\n\n", typ, seq, sig, data)
}

// Within a signed stream an event replayed, skipped ahead, or lifted from
// another stream (including an identical connect in the same second, which
// differs only by its nonce) drops the stream before it is dispatched.
func TestSSERejectsReplayedEvents(t *testing.T) {
	hubKS := newCoordKey(t)
	cases := map[string]func(w io.Writer, req reqBinding){
		"replayed": func(w io.Writer, req reqBinding) {
			ev := signedEvent(t, hubKS, req, 1, "ping", "{}")
			_, _ = io.WriteString(w, ev+ev)
		},
		"skipped": func(w io.Writer, req reqBinding) {
			_, _ = io.WriteString(w, signedEvent(t, hubKS, req, 1, "ping", "{}")+signedEvent(t, hubKS, req, 3, "ping", "{}"))
		},
		"other stream": func(w io.Writer, req reqBinding) {
			other := reqBinding{sig: "another-request", nonce: req.nonce}
			_, _ = io.WriteString(w, signedEvent(t, hubKS, req, 1, "ping", "{}")+signedEvent(t, hubKS, other, 2, "ping", "{}"))
		},
		"same-second stream": func(w io.Writer, req reqBinding) {
			other := reqBinding{sig: req.sig, nonce: "another-nonce"}
			_, _ = io.WriteString(w, signedEvent(t, hubKS, req, 1, "ping", "{}")+signedEvent(t, hubKS, other, 2, "ping", "{}"))
		},
		"unsigned event": func(w io.Writer, req reqBinding) {
			_, _ = io.WriteString(w, signedEvent(t, hubKS, req, 1, "ping", "{}")+"event: ping\ndata: {}\n\n")
		},
	}
	for name, write := range cases {
		t.Run(name, func(t *testing.T) {
			ts := fakeSSEHub(t, hubKS, write)
			l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
			l.pins.hub = hubKS.PubKeyBase64()
			var got atomic.Int64
			l.SetOnEvent(func(protocol.Event) { got.Add(1) })

			err := l.connectSSE(context.Background(), false)
			if !errors.Is(err, ErrHubSignature) {
				t.Fatalf("connectSSE = %v, want ErrHubSignature", err)
			}
			if n := got.Load(); n != 1 {
				t.Errorf("%d events dispatched, want only the first", n)
			}
			// /info still serves the pinned key, so the pause lifts.
			if l.HubPaused() {
				t.Error("still paused although /info serves the pinned key")
			}
		})
	}
}

// legacySSEEvents is the event loop of a leaf from before hub signing,
// copied as it was, collecting events instead of dispatching them.
func legacySSEEvents(body io.Reader, out chan<- protocol.Event) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20) // 1MB max token size
	var eventType string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if eventType != "" {
				out <- protocol.Event{
					Type: eventType,
					Data: json.RawMessage(data),
				}
			}
			eventType = ""
			continue
		}
	}
}

// A leaf that predates hub signing still reads the signed stream: the id
// and sig fields come before data, which is where it dispatches.
func TestLegacyLeafReadsSignedStream(t *testing.T) {
	ts, _, _ := newTestHub(t, true)
	l := subscribedLeaf(t, ts, "Old BBS")
	resp, _, err := l.signedGetSSE(context.Background(), "/v3net/v1/testnet/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get(protocol.HeaderHubSignature) == "" {
		t.Fatal("stream is not signed; the test would prove nothing")
	}
	events := make(chan protocol.Event, 16)
	go legacySSEEvents(resp.Body, events)

	if err := l.SendLogon("old-timer"); err != nil {
		t.Fatalf("SendLogon: %v", err)
	}
	payload := awaitEvent(t, events, protocol.EventLogon, func(p protocol.LogonPayload) bool { return p.Handle == "old-timer" })
	if payload.Handle != "old-timer" {
		t.Errorf("logon payload = %+v", payload)
	}
}

// rawSSEHub serves an unsigned SSE stream with the given body.
func rawSSEHub(t *testing.T, body string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// An event is buffered until its blank line, so its field count and size
// are capped; a stream over either cap is dropped, signed or not. The caps
// apply per event: many normal events in a row are fine.
func TestSSEEventLimits(t *testing.T) {
	line := "data: " + strings.Repeat("x", 128<<10) + "\n" // 128KB, well under the line cap
	hubKS := newCoordKey(t)
	cases := map[string]struct {
		hub     func(t *testing.T) *httptest.Server
		wantErr error
		events  int64
	}{
		"too many fields": {
			hub: func(t *testing.T) *httptest.Server {
				return rawSSEHub(t, "event: ping\n"+strings.Repeat("data: {}\n", maxSSEEventFields)+"\n")
			},
			wantErr: errSSEEventTooLarge,
		},
		"too many bytes": {
			hub:     func(t *testing.T) *httptest.Server { return rawSSEHub(t, "event: ping\n"+strings.Repeat(line, 9)+"\n") },
			wantErr: errSSEEventTooLarge,
		},
		"too many bytes, signed": {
			hub: func(t *testing.T) *httptest.Server {
				return fakeSSEHub(t, hubKS, func(w io.Writer, req reqBinding) {
					_, _ = io.WriteString(w, signedEvent(t, hubKS, req, 1, "ping", "{}")+"event: ping\nid: 2\n"+strings.Repeat(line, 9)+"\n")
				})
			},
			wantErr: errSSEEventTooLarge,
			events:  1,
		},
		"limits reset per event": {
			hub: func(t *testing.T) *httptest.Server {
				ev := "event: ping\n" + strings.Repeat(line, 7) + "\n" // 896KB, 8 fields
				return rawSSEHub(t, strings.Repeat(ev, 4))
			},
			events: 4,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ts := tc.hub(t)
			l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
			l.pins.hub = hubKS.PubKeyBase64()
			var got atomic.Int64
			l.SetOnEvent(func(protocol.Event) { got.Add(1) })

			err := l.connectSSE(context.Background(), false)
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("connectSSE = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && (err == nil || !strings.Contains(err.Error(), "stream ended")) {
				t.Fatalf("connectSSE = %v, want the stream to end normally", err)
			}
			if n := got.Load(); n != tc.events {
				t.Errorf("%d events dispatched, want %d", n, tc.events)
			}
			if l.HubPaused() {
				t.Error("an oversized event paused syncing")
			}
		})
	}
}

// A hub that has changed its key refuses the stream with a reply signed by
// the new key. The leaf checks that refusal like any other reply, so it
// pauses and reports the offered key rather than reconnecting for ever.
func TestSSESignedRefusalChecked(t *testing.T) {
	newKey := newCoordKey(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/info") {
			_ = json.NewEncoder(w).Encode(protocol.NetworkInfo{
				Name: "testnet", HubNodeID: newKey.NodeID(), HubPubKeyB64: newKey.PubKeyBase64(),
			})
			return
		}
		body := []byte(`{"error":"unknown or inactive node"}`)
		sig, _ := protocol.SignResponse(newKey, r.Header.Get("X-V3Net-Signature"), r.Header.Get(protocol.HeaderNonce), http.StatusUnauthorized, body)
		w.Header().Set(protocol.HeaderHubSignature, sig)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write(body)
	}))
	t.Cleanup(ts.Close)
	l, rec := pinnedLeaf(t, ts, "")
	l.pins.hub = newCoordKey(t).PubKeyBase64()

	err := l.connectSSE(context.Background(), false)
	if !errors.Is(err, ErrHubSignature) || !strings.Contains(err.Error(), "401") {
		t.Fatalf("connectSSE = %v, want a 401 wrapping ErrHubSignature", err)
	}
	if !l.HubPaused() {
		t.Error("not paused after a refusal signed by another key")
	}
	if _, changed := rec.counts(); changed != 1 || rec.changed[0].Offered != newKey.PubKeyBase64() {
		t.Errorf("key changes = %+v, want the new key offered once", rec.changed)
	}
}

// replayProxy forwards to a hub, but answers a message poll whose request
// signature it has seen before with the reply it recorded for it.
type replayProxy struct {
	mu       sync.Mutex
	recorded map[string]recordedReply
	replays  int
}

type recordedReply struct {
	header http.Header
	status int
	body   []byte
}

func (p *replayProxy) handler(t *testing.T, hubURL string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqSig := r.Header.Get("X-V3Net-Signature")
		isPoll := strings.HasSuffix(r.URL.Path, "/messages") && r.Method == http.MethodGet
		p.mu.Lock()
		rec, seen := p.recorded[reqSig]
		if isPoll && seen {
			p.replays++
		}
		p.mu.Unlock()
		if isPoll && seen {
			for k, v := range rec.header {
				w.Header()[k] = v
			}
			w.WriteHeader(rec.status)
			_, _ = w.Write(rec.body)
			return
		}

		out, err := http.NewRequestWithContext(r.Context(), r.Method, hubURL+r.URL.RequestURI(), r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		out.Header = r.Header.Clone()
		resp, err := http.DefaultClient.Do(out)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if isPoll {
			p.mu.Lock()
			p.recorded[reqSig] = recordedReply{header: resp.Header.Clone(), status: resp.StatusCode, body: body}
			p.mu.Unlock()
		}
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
	})
}

// Two identical polls in the same second carry the same request signature.
// A reply captured for the first and replayed for the second is refused,
// because the second request carried a fresh nonce.
func TestSameSecondReplayRejected(t *testing.T) {
	ts, _, hubKS := newTestHub(t, true)
	p := &replayProxy{recorded: map[string]recordedReply{}}
	front := httptest.NewServer(p.handler(t, ts.URL))
	t.Cleanup(front.Close)
	l := subscribedLeaf(t, front, "Leaf BBS")
	l.pins.hub = hubKS.PubKeyBase64()
	ctx := context.Background()

	// Poll back to back until two polls land in the same second.
	var replayErr error
	for i := 0; i < 50; i++ {
		_, err := l.Poll(ctx)
		p.mu.Lock()
		replayed := p.replays > 0
		p.mu.Unlock()
		if replayed {
			replayErr = err
			break
		}
		if err != nil {
			t.Fatalf("Poll %d (forwarded): %v", i, err)
		}
	}
	p.mu.Lock()
	replays := p.replays
	p.mu.Unlock()
	if replays == 0 {
		t.Fatal("no two polls shared a request signature; the replay was never attempted")
	}
	if !errors.Is(replayErr, ErrHubSignature) {
		t.Fatalf("Poll with a replayed reply = %v, want ErrHubSignature", replayErr)
	}
	if l.HubPaused() {
		t.Error("still paused although /info serves the pinned key")
	}
}
