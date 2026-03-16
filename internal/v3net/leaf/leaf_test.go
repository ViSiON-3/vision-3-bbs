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

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/dedup"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// mockJAMWriter records messages written to it.
type mockJAMWriter struct {
	mu       sync.Mutex
	messages []protocol.Message
	nextNum  int64
}

func (m *mockJAMWriter) WriteMessage(msg protocol.Message) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextNum++
	m.messages = append(m.messages, msg)
	return m.nextNum, nil
}

func (m *mockJAMWriter) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.messages)
}

// mockHub simulates a V3Net hub for testing.
type mockHub struct {
	ks       *keystore.Keystore
	messages []protocol.Message
}

func newMockHub(t *testing.T, messages []protocol.Message) (*httptest.Server, *keystore.Keystore) {
	t.Helper()
	hubKS, err := keystore.Load(filepath.Join(t.TempDir(), "hub.key"))
	if err != nil {
		t.Fatalf("load hub keystore: %v", err)
	}

	mh := &mockHub{ks: hubKS, messages: messages}
	ts := httptest.NewServer(http.HandlerFunc(mh.handler))
	t.Cleanup(ts.Close)
	return ts, hubKS
}

func (mh *mockHub) handler(w http.ResponseWriter, r *http.Request) {
	// Skip auth validation in mock — just serve messages.
	switch {
	case r.Method == http.MethodGet && contains(r.URL.Path, "/messages"):
		data, _ := json.Marshal(mh.messages)
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)

	case r.Method == http.MethodPost && contains(r.URL.Path, "/messages"):
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"msg_uuid":"test"}`))

	case r.Method == http.MethodGet && contains(r.URL.Path, "/events"):
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher.Flush()
		// Keep connection open until client disconnects.
		<-r.Context().Done()

	default:
		http.NotFound(w, r)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func setupLeaf(t *testing.T, hubURL string, writer JAMWriter) (*Leaf, *dedup.Index) {
	t.Helper()
	dir := t.TempDir()

	ks, err := keystore.Load(filepath.Join(dir, "leaf.key"))
	if err != nil {
		t.Fatalf("load keystore: %v", err)
	}

	ix, err := dedup.Open(filepath.Join(dir, "dedup.sqlite"))
	if err != nil {
		t.Fatalf("open dedup: %v", err)
	}
	t.Cleanup(func() { ix.Close() })

	cfg := Config{
		HubURL:       hubURL,
		Network:      "testnet",
		PollInterval: 100 * time.Millisecond,
		Keystore:     ks,
		DedupIndex:   ix,
		JAMWriter:    writer,
	}

	return New(cfg), ix
}

func testMessage(uuid string) protocol.Message {
	return protocol.Message{
		V3Net:       "1.0",
		Network:     "testnet",
		MsgUUID:     uuid,
		ThreadUUID:  uuid,
		OriginNode:  "test.example.net",
		OriginBoard: "General",
		From:        "Tester",
		To:          "All",
		Subject:     "Test",
		DateUTC:     "2026-03-16T04:20:00Z",
		Body:        "Hello!",
		Kludges:     map[string]any{},
	}
}

func TestPoll_ReceivesMessages(t *testing.T) {
	msgs := []protocol.Message{
		testMessage("550e8400-e29b-41d4-a716-446655440000"),
		testMessage("550e8400-e29b-41d4-a716-446655440001"),
	}
	ts, _ := newMockHub(t, msgs)
	writer := &mockJAMWriter{}
	l, _ := setupLeaf(t, ts.URL, writer)

	ctx := context.Background()
	count, err := l.poll(ctx)
	if err != nil {
		t.Fatalf("poll error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 messages, got %d", count)
	}
	if writer.count() != 2 {
		t.Errorf("expected 2 JAM writes, got %d", writer.count())
	}
}

func TestPoll_SkipsDuplicates(t *testing.T) {
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	msgs := []protocol.Message{testMessage(uuid)}
	ts, _ := newMockHub(t, msgs)
	writer := &mockJAMWriter{}
	l, ix := setupLeaf(t, ts.URL, writer)

	// Pre-mark as seen.
	if err := ix.MarkSeen(uuid, "testnet", nil); err != nil {
		t.Fatalf("mark seen: %v", err)
	}

	ctx := context.Background()
	count, err := l.poll(ctx)
	if err != nil {
		t.Fatalf("poll error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 new messages (duplicate), got %d", count)
	}
	if writer.count() != 0 {
		t.Errorf("expected 0 JAM writes for duplicate, got %d", writer.count())
	}
}

func TestSendMessage_Signs(t *testing.T) {
	var receivedNodeID string
	var receivedSig string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedNodeID = r.Header.Get("X-V3Net-Node-ID")
		receivedSig = r.Header.Get("X-V3Net-Signature")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"msg_uuid":"test"}`)
	}))
	defer ts.Close()

	writer := &mockJAMWriter{}
	l, _ := setupLeaf(t, ts.URL, writer)

	msg := testMessage("550e8400-e29b-41d4-a716-446655440000")
	if err := l.SendMessage(msg); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if receivedNodeID == "" {
		t.Error("expected X-V3Net-Node-ID header")
	}
	if receivedSig == "" {
		t.Error("expected X-V3Net-Signature header")
	}
}

func TestSSE_ReconnectsOnDisconnect(t *testing.T) {
	// Shorten backoff for testing.
	origBase := sseBackoffBase
	origMax := sseBackoffMax
	sseBackoffBase = 50 * time.Millisecond
	sseBackoffMax = 200 * time.Millisecond
	t.Cleanup(func() {
		sseBackoffBase = origBase
		sseBackoffMax = origMax
	})

	connectCount := 0
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if containsStr(r.URL.Path, "/events") {
			mu.Lock()
			connectCount++
			count := connectCount
			mu.Unlock()

			flusher, ok := w.(http.Flusher)
			if !ok {
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			flusher.Flush()

			if count == 1 {
				// First connection: close immediately to trigger reconnect.
				return
			}
			// Second connection: keep alive until client disconnects.
			<-r.Context().Done()
			return
		}
		http.NotFound(w, r)
	}))

	writer := &mockJAMWriter{}
	l, _ := setupLeaf(t, ts.URL, writer)

	ctx, cancel := context.WithCancel(context.Background())

	go l.runSSE(ctx)

	// Wait for at least 2 connections (initial + reconnect).
	deadline := time.After(2500 * time.Millisecond)
	for {
		mu.Lock()
		c := connectCount
		mu.Unlock()
		if c >= 2 {
			// Cancel first so the SSE goroutine exits and releases the connection.
			cancel()
			ts.Close()
			return
		}
		select {
		case <-deadline:
			cancel()
			ts.Close()
			t.Fatalf("expected at least 2 SSE connections, got %d", c)
		case <-time.After(50 * time.Millisecond):
		}
	}
}
