package hub

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func setupTestHub(t *testing.T) (*Hub, *keystore.Keystore) {
	t.Helper()
	dir := t.TempDir()

	ks, _, err := keystore.Load(filepath.Join(dir, "hub.key"))
	if err != nil {
		t.Fatalf("load hub keystore: %v", err)
	}

	cfg := Config{
		ListenAddr:  ":0",
		DataDir:     dir,
		Keystore:    ks,
		AutoApprove: true,
		Networks: []NetworkConfig{
			{Name: "testnet", Description: "Test network"},
		},
	}

	h, err := New(cfg)
	if err != nil {
		t.Fatalf("create hub: %v", err)
	}
	t.Cleanup(func() { h.Close() })
	return h, ks
}

func registerLeaf(t *testing.T, ts *httptest.Server, leafKS *keystore.Keystore) {
	t.Helper()
	body := fmt.Sprintf(`{
		"network": "testnet",
		"node_id": %q,
		"pubkey_b64": %q,
		"bbs_name": "Test BBS",
		"bbs_host": "test.example.net"
	}`, leafKS.NodeID(), leafKS.PubKeyBase64())

	resp, err := http.Post(ts.URL+"/v3net/v1/subscribe", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subscribe status: %d", resp.StatusCode)
	}
}

func signedRequest(t *testing.T, ks *keystore.Keystore, method, url, body string) *http.Request {
	t.Helper()
	bodyHash := sha256.Sum256([]byte(body))
	bodySHA := hex.EncodeToString(bodyHash[:])
	dateStr := time.Now().UTC().Format(http.TimeFormat)

	// Extract path from full URL.
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	path := req.URL.Path
	if req.URL.RawQuery != "" {
		path += "?" + req.URL.RawQuery
	}

	sig, err := ks.Sign(method, path, dateStr, bodySHA)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Date", dateStr)
	req.Header.Set("X-V3Net-Node-ID", ks.NodeID())
	req.Header.Set("X-V3Net-Signature", sig)
	return req
}

func TestPublicEndpoints(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	// GET /v3net/v1/networks
	resp, err := http.Get(ts.URL + "/v3net/v1/networks")
	if err != nil {
		t.Fatalf("GET networks: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var summaries []protocol.NetworkSummary
	if err := json.NewDecoder(resp.Body).Decode(&summaries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Name != "testnet" {
		t.Errorf("unexpected networks: %+v", summaries)
	}

	// GET /v3net/v1/testnet/info
	resp2, err := http.Get(ts.URL + "/v3net/v1/testnet/info")
	if err != nil {
		t.Fatalf("GET info: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}
}

func TestUnauthenticatedRequestReturns401(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v3net/v1/testnet/messages")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestPostAndGetMessage(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	// Register a leaf node.
	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerLeaf(t, ts, leafKS)

	// POST a message.
	msg := protocol.Message{
		V3Net:       "1.0",
		Network:     "testnet",
		MsgUUID:     "550e8400-e29b-41d4-a716-446655440000",
		ThreadUUID:  "550e8400-e29b-41d4-a716-446655440000",
		OriginNode:  "test.example.net",
		OriginBoard: "General",
		From:        "Tester",
		To:          "All",
		Subject:     "Test message",
		DateUTC:     "2026-03-16T04:20:00Z",
		Body:        "Hello V3Net!",
		Kludges:     map[string]any{},
	}
	msgJSON, _ := json.Marshal(msg)

	req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/messages", string(msgJSON))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var body bytes.Buffer
		body.ReadFrom(resp.Body)
		t.Fatalf("POST status: %d, body: %s", resp.StatusCode, body.String())
	}

	// GET messages.
	getReq := signedRequest(t, leafKS, "GET", ts.URL+"/v3net/v1/testnet/messages", "")
	resp2, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("GET status: %d", resp2.StatusCode)
	}

	var messages []protocol.Message
	if err := json.NewDecoder(resp2.Body).Decode(&messages); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].MsgUUID != msg.MsgUUID {
		t.Errorf("wrong message UUID: %s", messages[0].MsgUUID)
	}
}

func TestDuplicatePostReturns200(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerLeaf(t, ts, leafKS)

	msgJSON := `{
		"v3net": "1.0",
		"network": "testnet",
		"msg_uuid": "550e8400-e29b-41d4-a716-446655440000",
		"thread_uuid": "550e8400-e29b-41d4-a716-446655440000",
		"origin_node": "test.example.net",
		"origin_board": "General",
		"from": "Tester",
		"to": "All",
		"subject": "Test",
		"date_utc": "2026-03-16T04:20:00Z",
		"body": "Hello!",
		"kludges": {}
	}`

	// Post twice.
	for i := 0; i < 2; i++ {
		req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/messages", msgJSON)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST %d status: %d", i, resp.StatusCode)
		}
	}

	// Verify only one message stored.
	getReq := signedRequest(t, leafKS, "GET", ts.URL+"/v3net/v1/testnet/messages", "")
	resp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET messages: %v", err)
	}
	defer resp.Body.Close()

	var messages []json.RawMessage
	json.NewDecoder(resp.Body).Decode(&messages)
	if len(messages) != 1 {
		t.Errorf("expected 1 message after duplicate post, got %d", len(messages))
	}
}

func TestSSEReceivesNewMessageEvent(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerLeaf(t, ts, leafKS)

	// Subscribe to the broadcaster directly to avoid HTTP SSE timing issues.
	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	// Post a message to trigger a new_message event.
	msgJSON := `{
		"v3net": "1.0",
		"network": "testnet",
		"msg_uuid": "660e8400-e29b-41d4-a716-446655440001",
		"thread_uuid": "660e8400-e29b-41d4-a716-446655440001",
		"origin_node": "test.example.net",
		"origin_board": "General",
		"from": "Tester",
		"to": "All",
		"subject": "SSE Test",
		"date_utc": "2026-03-16T04:20:00Z",
		"body": "Testing SSE",
		"kludges": {}
	}`
	postReq := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/messages", msgJSON)
	postResp, err := http.DefaultClient.Do(postReq)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	postResp.Body.Close()

	select {
	case ev := <-ch:
		if ev.Type != "new_message" {
			t.Errorf("expected new_message event, got %q", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for SSE event")
	}
}

func TestSSEHTTPStream(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerLeaf(t, ts, leafKS)

	// Connect SSE via HTTP.
	sseReq := signedRequest(t, leafKS, "GET", ts.URL+"/v3net/v1/testnet/events", "")
	sseResp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}
	defer sseResp.Body.Close()

	if sseResp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status: %d", sseResp.StatusCode)
	}
	if ct := sseResp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", ct)
	}

	// Publish an event directly and read it from the HTTP stream.
	ev, _ := protocol.NewEvent(protocol.EventChat, protocol.ChatPayload{
		From: "Test", Node: "test", Text: "hello", Timestamp: "2026-03-16T00:00:00Z",
	})
	h.broadcaster.Publish("testnet", ev)

	done := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(sseResp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				done <- strings.TrimPrefix(line, "event: ")
				return
			}
		}
	}()

	select {
	case eventType := <-done:
		if eventType != "chat" {
			t.Errorf("expected chat event, got %q", eventType)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for SSE event over HTTP")
	}
}

func TestPresence_LogonLogoff(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerLeaf(t, ts, leafKS)

	// Subscribe to broadcaster to receive events.
	ch, cancel := h.broadcaster.Subscribe("testnet")
	defer cancel()

	// Send logon.
	logonJSON := `{"type":"logon","handle":"Darkstar"}`
	req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/presence", logonJSON)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST logon: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logon status: %d", resp.StatusCode)
	}

	select {
	case ev := <-ch:
		if ev.Type != "logon" {
			t.Errorf("expected logon event, got %q", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for logon event")
	}

	// Send logoff.
	logoffJSON := `{"type":"logoff","handle":"Darkstar"}`
	req2 := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/presence", logoffJSON)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("POST logoff: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("logoff status: %d", resp2.StatusCode)
	}

	select {
	case ev := <-ch:
		if ev.Type != "logoff" {
			t.Errorf("expected logoff event, got %q", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for logoff event")
	}
}

func TestPresence_InvalidType(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerLeaf(t, ts, leafKS)

	badJSON := `{"type":"invalid","handle":"Test"}`
	req := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/presence", badJSON)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid type, got %d", resp.StatusCode)
	}
}

func TestChat_RateLimited(t *testing.T) {
	h, _ := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	defer ts.Close()

	leafKS, _, err := keystore.Load(filepath.Join(t.TempDir(), "leaf.key"))
	if err != nil {
		t.Fatalf("load leaf keystore: %v", err)
	}
	registerLeaf(t, ts, leafKS)

	chatJSON := `{"from":"Tester","text":"hello"}`

	// First request should succeed.
	req1 := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/chat", chatJSON)
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("POST chat 1: %v", err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first chat expected 200, got %d", resp1.StatusCode)
	}

	// Second request immediately should be rate limited.
	req2 := signedRequest(t, leafKS, "POST", ts.URL+"/v3net/v1/testnet/chat", chatJSON)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("POST chat 2: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusTooManyRequests {
		t.Errorf("second chat expected 429, got %d", resp2.StatusCode)
	}
}
