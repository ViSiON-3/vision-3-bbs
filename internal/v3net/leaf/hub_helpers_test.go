package leaf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/hub"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// newTestHub starts a real in-process hub serving "testnet", so leaf calls are
// checked against the hub's actual behaviour rather than a canned reply.
func newTestHub(t *testing.T, autoApprove bool) (*httptest.Server, *hub.Hub, *keystore.Keystore) {
	t.Helper()
	dir := t.TempDir()
	ks, _, err := keystore.Load(filepath.Join(dir, "hub.key"))
	if err != nil {
		t.Fatalf("load hub keystore: %v", err)
	}
	h, err := hub.New(hub.Config{
		ListenAddr:             ":0",
		DataDir:                dir,
		Keystore:               ks,
		AutoApprove:            autoApprove,
		RequireSignedSubscribe: true,
		AutoApproveAreas:       true,
		Networks:               []hub.NetworkConfig{{Name: "testnet", Description: "Test network"}},
	})
	if err != nil {
		t.Fatalf("create hub: %v", err)
	}
	ts := httptest.NewServer(h.Mux())
	t.Cleanup(func() {
		ts.Close()
		_ = h.Close()
	})
	return ts, h, ks
}

// seedHubNAL publishes a hub-signed NAL with one open area per tag.
func seedHubNAL(t *testing.T, h *hub.Hub, ks *keystore.Keystore, tags ...string) {
	t.Helper()
	n := &protocol.NAL{
		V3NetNAL:       "1.0",
		Network:        "testnet",
		CoordNodeID:    ks.NodeID(),
		CoordPubKeyB64: ks.PubKeyBase64(),
		Areas:          []protocol.Area{},
	}
	for _, tag := range tags {
		n.Areas = append(n.Areas, protocol.Area{
			Tag:              tag,
			Name:             tag,
			Language:         "en",
			ManagerNodeID:    ks.NodeID(),
			ManagerPubKeyB64: ks.PubKeyBase64(),
			Access:           protocol.AreaAccess{Mode: protocol.AccessModeOpen},
			Policy:           protocol.AreaPolicy{MaxBodyBytes: protocol.MaxBodyBytes, AllowANSI: true},
		})
	}
	if err := nal.Sign(n, ks); err != nil {
		t.Fatalf("sign NAL: %v", err)
	}
	if err := h.NALStore().Put("testnet", n); err != nil {
		t.Fatalf("put NAL: %v", err)
	}
}

// subscribedLeaf creates a leaf named bbsName and registers it with the hub.
func subscribedLeaf(t *testing.T, ts *httptest.Server, bbsName string, areaTags ...string) *Leaf {
	t.Helper()
	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	l.cfg.BBSName = bbsName
	l.cfg.BBSHost = bbsName + ".example.net"
	l.cfg.AreaTags = areaTags
	if err := l.subscribe(context.Background()); err != nil {
		t.Fatalf("subscribe %s: %v", bbsName, err)
	}
	t.Cleanup(l.Close)
	return l
}

// probeHandle is the logon handle watchEvents uses to detect a live stream.
const probeHandle = "sse-probe"

// watchEvents runs the leaf's SSE loop and returns the events it receives.
// It returns only once the stream is delivering, which it establishes by
// sending logon probes until one comes back; later probe echoes may still
// arrive, so callers match on the events they expect.
func watchEvents(t *testing.T, l *Leaf) <-chan protocol.Event {
	t.Helper()
	events := make(chan protocol.Event, 256)
	l.SetOnEvent(func(ev protocol.Event) {
		select {
		case events <- ev:
		default:
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.RunSSE(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := l.SendLogon(probeHandle); err != nil {
			t.Fatalf("send SSE probe: %v", err)
		}
		retry := time.After(100 * time.Millisecond)
		for waiting := true; waiting; {
			select {
			case ev := <-events:
				if ev.Type == protocol.EventLogon {
					return events
				}
			case <-retry:
				waiting = false
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("SSE stream never delivered a probe event")
		}
	}
}

// awaitEvent returns the payload of the next event of the given type that
// satisfies ok, skipping everything else.
func awaitEvent[T any](t *testing.T, events <-chan protocol.Event, eventType string, ok func(T) bool) T {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Type != eventType {
				continue
			}
			var payload T
			if err := json.Unmarshal(ev.Data, &payload); err != nil {
				t.Fatalf("decode %s event: %v", eventType, err)
			}
			if ok == nil || ok(payload) {
				return payload
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %s event", eventType)
		}
	}
}

// awaitChat returns the next chat event of the given type from a session.
func awaitChat(t *testing.T, s *ChatSession, eventType chat.ChatEventType) chat.ChatEvent {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev, open := <-s.Events():
			if !open {
				t.Fatal("session events channel closed while waiting")
			}
			if ev.Type == eventType {
				return ev
			}
		case <-timeout:
			t.Fatalf("timed out waiting for chat event type %v", eventType)
		}
	}
}

// cannedHub answers every request with the given status and body.
func cannedHub(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	return ts
}

// deadHubURL returns the URL of a hub that has already shut down.
func deadHubURL(t *testing.T) string {
	t.Helper()
	ts := httptest.NewServer(http.NotFoundHandler())
	ts.Close()
	return ts.URL
}
