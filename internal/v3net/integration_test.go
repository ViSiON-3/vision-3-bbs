package v3net_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/dedup"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/hub"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/leaf"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// testJAMWriter records messages written to it for verification.
type testJAMWriter struct {
	mu       sync.Mutex
	messages []protocol.Message
	nextNum  int64
}

func (w *testJAMWriter) WriteMessage(msg protocol.Message) (int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.nextNum++
	w.messages = append(w.messages, msg)
	return w.nextNum, nil
}

func (w *testJAMWriter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.messages)
}

func (w *testJAMWriter) get(i int) *protocol.Message {
	w.mu.Lock()
	defer w.mu.Unlock()
	if i < 0 || i >= len(w.messages) {
		return nil
	}
	return &w.messages[i]
}

// setupIntegration creates a hub with httptest.NewServer, a registered leaf,
// and returns all the pieces needed for end-to-end testing.
func setupIntegration(t *testing.T) (
	h *hub.Hub,
	ts *httptest.Server,
	l *leaf.Leaf,
	leafKS *keystore.Keystore,
	dedupIx *dedup.Index,
	writer *testJAMWriter,
) {
	t.Helper()
	dir := t.TempDir()

	// Create hub.
	hubKS, _, err := keystore.Load(filepath.Join(dir, "hub.key"))
	if err != nil {
		t.Fatalf("load hub keystore: %v", err)
	}

	hubCfg := hub.Config{
		ListenAddr:  ":0",
		DataDir:     dir,
		Keystore:    hubKS,
		AutoApprove: true,
		Networks: []hub.NetworkConfig{
			{Name: "testnet", Description: "Integration test network"},
		},
	}

	h, err = hub.New(hubCfg)
	if err != nil {
		t.Fatalf("create hub: %v", err)
	}
	t.Cleanup(func() { h.Close() })

	ts = httptest.NewServer(h.Mux())
	t.Cleanup(ts.Close)

	// Create leaf keystore and register with hub.
	leafKS, _, err = keystore.Load(filepath.Join(dir, "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}

	registerBody, _ := json.Marshal(protocol.SubscribeRequest{
		Network:   "testnet",
		NodeID:    leafKS.NodeID(),
		PubKeyB64: leafKS.PubKeyBase64(),
		BBSName:   "Integration Test BBS",
		BBSHost:   "test.example.net",
	})
	resp, err := ts.Client().Post(ts.URL+"/v3net/v1/subscribe", "application/json",
		bytes.NewReader(registerBody))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("subscribe status: %d", resp.StatusCode)
	}

	// Create dedup index and JAM writer.
	dedupIx, err = dedup.Open(filepath.Join(dir, "dedup.sqlite"))
	if err != nil {
		t.Fatalf("open dedup: %v", err)
	}
	t.Cleanup(func() { dedupIx.Close() })

	writer = &testJAMWriter{}

	leafCfg := leaf.Config{
		HubURL:       ts.URL,
		Network:      "testnet",
		PollInterval: 50 * time.Millisecond,
		Keystore:     leafKS,
		DedupIndex:   dedupIx,
		JAMWriter:    writer,
	}

	l = leaf.New(leafCfg)
	return
}

func TestIntegration_PostAndPoll(t *testing.T) {
	_, _, l, _, _, writer := setupIntegration(t)

	// Leaf sends a message to the hub.
	msg := protocol.Message{
		V3Net:       "1.0",
		Network:     "testnet",
		MsgUUID:     "550e8400-e29b-41d4-a716-446655440000",
		ThreadUUID:  "550e8400-e29b-41d4-a716-446655440000",
		OriginNode:  "test.example.net",
		OriginBoard: "General",
		From:        "Tester",
		To:          "All",
		Subject:     "Integration test message",
		DateUTC:     "2026-03-16T04:20:00Z",
		Body:        "Hello from the integration test!",
		Kludges:     map[string]any{},
	}

	if err := l.SendMessage(msg); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// Leaf polls and should receive the message back.
	ctx := context.Background()
	count, err := l.Poll(ctx)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 message from poll, got %d", count)
	}
	if writer.count() != 1 {
		t.Fatalf("expected 1 JAM write, got %d", writer.count())
	}

	got := writer.get(0)
	if got.MsgUUID != msg.MsgUUID {
		t.Errorf("wrong UUID: got %s, want %s", got.MsgUUID, msg.MsgUUID)
	}
	if got.Subject != msg.Subject {
		t.Errorf("wrong subject: got %q, want %q", got.Subject, msg.Subject)
	}
}

func TestIntegration_DedupPreventsDoubleWrite(t *testing.T) {
	_, _, l, _, _, writer := setupIntegration(t)

	msg := protocol.Message{
		V3Net:       "1.0",
		Network:     "testnet",
		MsgUUID:     "660e8400-e29b-41d4-a716-446655440001",
		ThreadUUID:  "660e8400-e29b-41d4-a716-446655440001",
		OriginNode:  "test.example.net",
		OriginBoard: "General",
		From:        "Tester",
		To:          "All",
		Subject:     "Dedup test",
		DateUTC:     "2026-03-16T04:20:00Z",
		Body:        "This should only appear once.",
		Kludges:     map[string]any{},
	}

	if err := l.SendMessage(msg); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	ctx := context.Background()

	// First poll: should write the message.
	count, err := l.Poll(ctx)
	if err != nil {
		t.Fatalf("first poll: %v", err)
	}
	if count != 1 {
		t.Fatalf("first poll: expected 1, got %d", count)
	}

	// Second poll: same message is already in dedup index, should skip.
	count, err = l.Poll(ctx)
	if err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if count != 0 {
		t.Errorf("second poll: expected 0 new messages (dedup), got %d", count)
	}

	// JAM writer should have exactly 1 message, not 2.
	if writer.count() != 1 {
		t.Errorf("expected exactly 1 JAM write after 2 polls, got %d", writer.count())
	}
}

func TestIntegration_SSEReceivesEvents(t *testing.T) {
	_, _, l, _, _, _ := setupIntegration(t)

	events := make(chan protocol.Event, 10)
	l.SetOnEvent(func(ev protocol.Event) {
		events <- ev
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start SSE in a goroutine.
	go l.RunSSE(ctx)

	// Give SSE time to connect.
	time.Sleep(200 * time.Millisecond)

	// Send a message which should trigger a new_message SSE event.
	msg := protocol.Message{
		V3Net:       "1.0",
		Network:     "testnet",
		MsgUUID:     "770e8400-e29b-41d4-a716-446655440002",
		ThreadUUID:  "770e8400-e29b-41d4-a716-446655440002",
		OriginNode:  "test.example.net",
		OriginBoard: "General",
		From:        "Tester",
		To:          "All",
		Subject:     "SSE event test",
		DateUTC:     "2026-03-16T04:20:00Z",
		Body:        "This should trigger an SSE event.",
		Kludges:     map[string]any{},
	}

	if err := l.SendMessage(msg); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	select {
	case ev := <-events:
		if ev.Type != "new_message" {
			t.Errorf("expected new_message event, got %q", ev.Type)
		}
	case <-time.After(3 * time.Second):
		t.Error("timed out waiting for SSE event")
	}
}

func TestIntegration_ChatEvent(t *testing.T) {
	_, _, l, _, _, _ := setupIntegration(t)

	events := make(chan protocol.Event, 10)
	l.SetOnEvent(func(ev protocol.Event) {
		events <- ev
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go l.RunSSE(ctx)
	time.Sleep(200 * time.Millisecond)

	if err := l.SendChat("Hello from integration test!", "Tester"); err != nil {
		t.Fatalf("SendChat: %v", err)
	}

	select {
	case ev := <-events:
		if ev.Type != "chat" {
			t.Errorf("expected chat event, got %q", ev.Type)
		}
	case <-time.After(3 * time.Second):
		t.Error("timed out waiting for chat event")
	}
}

func TestIntegration_PresenceEvents(t *testing.T) {
	_, _, l, _, _, _ := setupIntegration(t)

	events := make(chan protocol.Event, 10)
	l.SetOnEvent(func(ev protocol.Event) {
		events <- ev
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go l.RunSSE(ctx)
	time.Sleep(200 * time.Millisecond)

	// Send logon.
	if err := l.SendLogon("Darkstar"); err != nil {
		t.Fatalf("SendLogon: %v", err)
	}

	select {
	case ev := <-events:
		if ev.Type != "logon" {
			t.Errorf("expected logon event, got %q", ev.Type)
		}
	case <-time.After(3 * time.Second):
		t.Error("timed out waiting for logon event")
	}

	// Send logoff.
	if err := l.SendLogoff("Darkstar"); err != nil {
		t.Fatalf("SendLogoff: %v", err)
	}

	select {
	case ev := <-events:
		if ev.Type != "logoff" {
			t.Errorf("expected logoff event, got %q", ev.Type)
		}
	case <-time.After(3 * time.Second):
		t.Error("timed out waiting for logoff event")
	}
}
