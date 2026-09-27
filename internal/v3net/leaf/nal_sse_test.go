package leaf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// TestSSE_NALUpdatedTriggersRefetch verifies the nal_updated SSE event is
// wired to the NAL re-fetch handler: when the hub announces a NAL update, the
// leaf must re-fetch and verify the NAL from GET /{network}/nal (per the
// protocol spec, within 60s ±10% jitter — shortened here via nalRefetchBase).
func TestSSE_NALUpdatedTriggersRefetch(t *testing.T) {
	origDelay := nalRefetchBase
	nalRefetchBase = 20 * time.Millisecond
	t.Cleanup(func() { nalRefetchBase = origDelay })

	// Coordinator identity to sign the NAL the mock hub serves.
	coordKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "coord.key"))
	if err != nil {
		t.Fatalf("load coord keystore: %v", err)
	}
	signed := &protocol.NAL{
		V3NetNAL:    "1.0",
		Network:     "testnet",
		CoordNodeID: coordKS.NodeID(),
	}
	if err := nal.Sign(signed, coordKS); err != nil {
		t.Fatalf("sign NAL: %v", err)
	}
	nalJSON, err := json.Marshal(signed)
	if err != nil {
		t.Fatalf("marshal NAL: %v", err)
	}

	var mu sync.Mutex
	nalFetches := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3net/v1/testnet/events":
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Error("no flusher")
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			fmt.Fprint(w, "event: nal_updated\ndata: {\"network\":\"testnet\"}\n\n")
			flusher.Flush()
			<-r.Context().Done()
		case "/v3net/v1/testnet/nal":
			mu.Lock()
			nalFetches++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(nalJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.runSSE(ctx)

	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := nalFetches
		mu.Unlock()
		if n >= 1 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("nal_updated SSE event did not trigger a NAL re-fetch (wiring missing)")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// TestNALUpdatedRefetchBypassesCacheAndNotifies covers a nal_updated that
// arrives while the cached NAL is still fresh: the leaf must still go to the
// hub (the cache's one-hour TTL would otherwise serve the old copy) and hand
// the new NAL to OnNAL.
func TestNALUpdatedRefetchBypassesCacheAndNotifies(t *testing.T) {
	origDelay := nalRefetchBase
	nalRefetchBase = 20 * time.Millisecond
	t.Cleanup(func() { nalRefetchBase = origDelay })

	coordKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "coord.key"))
	if err != nil {
		t.Fatalf("load coord keystore: %v", err)
	}
	signedJSON := func(tags ...string) []byte {
		n := &protocol.NAL{V3NetNAL: "1.0", Network: "testnet", CoordNodeID: coordKS.NodeID()}
		for _, tag := range tags {
			n.Areas = append(n.Areas, protocol.Area{Tag: tag, Name: tag})
		}
		if err := nal.Sign(n, coordKS); err != nil {
			t.Fatalf("sign NAL: %v", err)
		}
		data, _ := json.Marshal(n)
		return data
	}

	var mu sync.Mutex
	current := signedJSON("test.a")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3net/v1/testnet/events":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			fmt.Fprint(w, "event: nal_updated\ndata: {\"network\":\"testnet\"}\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case "/v3net/v1/testnet/nal":
			mu.Lock()
			data := current
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	seen := make(chan int, 4)
	l.cfg.OnNAL = func(n *protocol.NAL) { seen <- len(n.Areas) }

	// Warm the cache with the one-area NAL, as the leaf does after subscribing.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := l.refreshNAL(ctx); err != nil {
		t.Fatalf("initial refresh: %v", err)
	}
	if got := <-seen; got != 1 {
		t.Fatalf("initial OnNAL saw %d areas, want 1", got)
	}

	// The hub adds an area and announces it.
	mu.Lock()
	current = signedJSON("test.a", "test.b")
	mu.Unlock()
	go l.runSSE(ctx)

	select {
	case got := <-seen:
		if got != 2 {
			t.Fatalf("OnNAL after nal_updated saw %d areas, want 2", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nal_updated did not reach OnNAL")
	}
	if cached := l.nalCache.Get("testnet"); cached == nil || len(cached.Areas) != 2 {
		t.Errorf("cache was not updated with the new NAL")
	}
}

// TestSSEReconnectRefetchesNAL covers a hub restart or network drop while the
// BBS stays up: a nal_updated sent during the gap is never delivered, so the
// leaf must check the NAL itself when the stream comes back.
func TestSSEReconnectRefetchesNAL(t *testing.T) {
	origBase := sseBackoffBase
	sseBackoffBase = 10 * time.Millisecond
	t.Cleanup(func() { sseBackoffBase = origBase })

	coordKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "coord.key"))
	if err != nil {
		t.Fatalf("load coord keystore: %v", err)
	}
	n := &protocol.NAL{V3NetNAL: "1.0", Network: "testnet", CoordNodeID: coordKS.NodeID()}
	if err := nal.Sign(n, coordKS); err != nil {
		t.Fatalf("sign NAL: %v", err)
	}
	nalJSON, _ := json.Marshal(n)

	var mu sync.Mutex
	connects := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3net/v1/testnet/events":
			mu.Lock()
			connects++
			first := connects == 1
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			if first {
				return // the stream drops; the leaf reconnects
			}
			<-r.Context().Done()
		case "/v3net/v1/testnet/nal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(nalJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	got := make(chan struct{}, 4)
	l.cfg.OnNAL = func(*protocol.NAL) { got <- struct{}{} }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.runSSE(ctx)

	select {
	case <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("reconnecting the SSE stream did not re-fetch the NAL")
	}
}

// signedTestNAL returns a signed, empty testnet NAL as JSON.
func signedTestNAL(t *testing.T) []byte {
	t.Helper()
	coordKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "coord.key"))
	if err != nil {
		t.Fatalf("load coord keystore: %v", err)
	}
	n := &protocol.NAL{V3NetNAL: "1.0", Network: "testnet", CoordNodeID: coordKS.NodeID()}
	if err := nal.Sign(n, coordKS); err != nil {
		t.Fatalf("sign NAL: %v", err)
	}
	data, _ := json.Marshal(n)
	return data
}

// TestNALUpdatedBurstFetchesOnce covers a hub announcing several changes in
// quick succession: one re-fetch covers them all, instead of one goroutine per
// event racing to store its NAL last.
func TestNALUpdatedBurstFetchesOnce(t *testing.T) {
	origDelay := nalRefetchBase
	nalRefetchBase = 50 * time.Millisecond
	t.Cleanup(func() { nalRefetchBase = origDelay })

	nalJSON := signedTestNAL(t)
	var mu sync.Mutex
	fetches := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3net/v1/testnet/events":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			for range 3 {
				fmt.Fprint(w, "event: nal_updated\ndata: {\"network\":\"testnet\"}\n\n")
			}
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case "/v3net/v1/testnet/nal":
			mu.Lock()
			fetches++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(nalJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	got := make(chan struct{}, 4)
	l.cfg.OnNAL = func(*protocol.NAL) { got <- struct{}{} }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.runSSE(ctx)

	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("nal_updated did not reach OnNAL")
	}
	time.Sleep(4 * nalRefetchBase) // room for any extra re-fetch to show up
	mu.Lock()
	defer mu.Unlock()
	if fetches != 1 {
		t.Errorf("three nal_updated events caused %d NAL fetches, want 1", fetches)
	}
}

// TestSSEReconnectRetriesFailedNALFetch covers the hub's NAL endpoint failing
// just as the stream comes back: the leaf keeps trying rather than waiting for
// a change announcement that may already have been missed.
func TestSSEReconnectRetriesFailedNALFetch(t *testing.T) {
	origBase := sseBackoffBase
	sseBackoffBase = 10 * time.Millisecond
	t.Cleanup(func() { sseBackoffBase = origBase })

	nalJSON := signedTestNAL(t)
	var mu sync.Mutex
	connects, nalCalls := 0, 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3net/v1/testnet/events":
			mu.Lock()
			connects++
			first := connects == 1
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			if first {
				return
			}
			<-r.Context().Done()
		case "/v3net/v1/testnet/nal":
			mu.Lock()
			nalCalls++
			fail := nalCalls <= 2
			mu.Unlock()
			if fail {
				http.Error(w, "busy", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(nalJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	got := make(chan struct{}, 4)
	l.cfg.OnNAL = func(*protocol.NAL) { got <- struct{}{} }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.runSSE(ctx)

	select {
	case <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("a failed NAL fetch after reconnect was not retried")
	}
}
