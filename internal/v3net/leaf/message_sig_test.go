package leaf

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func sigUUID(n int) string {
	return fmt.Sprintf("b%07x-e29b-41d4-a716-446655440000", n)
}

func TestPoll_VerifiesAuthorSignatures(t *testing.T) {
	author, _, err := keystore.Load(filepath.Join(t.TempDir(), "author.key"))
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := keystore.Load(filepath.Join(t.TempDir(), "other.key"))
	if err != nil {
		t.Fatal(err)
	}
	signed := func(n int) protocol.Message {
		m := testMessage(sigUUID(n))
		m.OriginNode = author.NodeID()
		if err := m.Sign(author); err != nil {
			t.Fatal(err)
		}
		return m
	}

	// What a malicious or broken hub might relay.
	bodyEdited := signed(1)
	bodyEdited.Body = "the hub rewrote this"
	subjectEdited := signed(2)
	subjectEdited.Subject = "Hub subject"
	wrongOrigin := signed(3)
	wrongOrigin.OriginNode = other.NodeID()
	impersonated := signed(4) // other's key and node, author's signature
	impersonated.OriginNode = other.NodeID()
	impersonated.OriginPubKeyB64 = other.PubKeyBase64()
	keyStripped := signed(5)
	keyStripped.OriginPubKeyB64 = ""

	good := signed(6)
	unsigned := testMessage(sigUUID(7))

	ts, _ := newMockHub(t, []protocol.Message{
		bodyEdited, subjectEdited, wrongOrigin, impersonated, keyStripped, good, unsigned,
	})
	writer := &mockJAMWriter{}
	l, ix := setupLeaf(t, ts.URL, writer)

	count, err := l.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if count != 2 || len(writer.messages) != 2 {
		t.Fatalf("Poll imported %d (wrote %d), want 2", count, len(writer.messages))
	}
	if writer.messages[0].MsgUUID != good.MsgUUID || writer.messages[1].MsgUUID != unsigned.MsgUUID {
		t.Errorf("imported %s and %s, want the signed and the unsigned message",
			writer.messages[0].MsgUUID, writer.messages[1].MsgUUID)
	}
	for _, m := range []protocol.Message{bodyEdited, subjectEdited, wrongOrigin, impersonated, keyStripped} {
		if seen, _ := ix.Seen(m.Network, m.OriginNode, m.MsgUUID); seen {
			t.Errorf("dropped message %s was marked seen", m.MsgUUID)
		}
	}
}

func TestSendMessage_SignsTruncatedMessage(t *testing.T) {
	var posted protocol.Message
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &posted)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"msg_uuid":"test"}`)
	}))
	defer ts.Close()

	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	msg := ownMessage(l, testUUID1)
	msg.Body = strings.Repeat("y", protocol.MaxBodyBytes+10)
	if err := l.SendMessage(msg); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if posted.OriginPubKeyB64 != l.cfg.Keystore.PubKeyBase64() {
		t.Errorf("origin_pubkey_b64 = %q, want the node key", posted.OriginPubKeyB64)
	}
	if !posted.IsTruncated() || len(posted.Body) > protocol.MaxBodyBytes {
		t.Errorf("body not truncated before sending: %d bytes", len(posted.Body))
	}
	if err := posted.VerifySignature(); err != nil {
		t.Errorf("posted message does not verify: %v", err)
	}
}

func TestSendMessage_RefusesForeignOriginNode(t *testing.T) {
	ts, _ := newMockHub(t, nil)
	l, _ := setupLeaf(t, ts.URL, &mockJAMWriter{})
	msg := testMessage(testUUID1) // origin_node is not this leaf's node ID
	if err := l.SendMessage(msg); err == nil || !strings.Contains(err.Error(), "sign message") {
		t.Errorf("SendMessage with another node's origin_node = %v, want a signing error", err)
	}
}

func TestSendMessage_RawHighBytesVerifyThroughHub(t *testing.T) {
	ts, h, hubKS := newTestHub(t, true)
	seedHubNAL(t, h, hubKS, "gen.general")
	l := subscribedLeaf(t, ts, "leafbbs", "gen.general")

	// Raw CP437 bytes (box drawing, accented letters), which are not UTF-8.
	msg := ownMessage(l, testUUID1)
	msg.Subject = "Caf\x82 \xc9\xcd\xbb"
	msg.Body = "Hello \x80\x81\xfe\xff world\r\n\xb0\xb1\xb2"
	if err := l.SendMessage(msg); err != nil {
		t.Fatalf("SendMessage with raw high bytes: %v", err)
	}

	if _, err := l.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	writer := l.cfg.JAMWriter.(*mockJAMWriter)
	if len(writer.messages) != 1 {
		t.Fatalf("imported %d messages, want 1", len(writer.messages))
	}
	got := writer.messages[0]
	if err := got.VerifySignature(); err != nil {
		t.Errorf("relayed message does not verify: %v", err)
	}
	// Each bad byte becomes one U+FFFD, as encoding/json sent it before
	// messages were signed.
	if want := "Hello ���� world\r\n���"; got.Body != want {
		t.Errorf("body = %q, want %q", got.Body, want)
	}
}

func TestPoll_DropsMessageFromAnotherNetwork(t *testing.T) {
	author, _, err := keystore.Load(filepath.Join(t.TempDir(), "author.key"))
	if err != nil {
		t.Fatal(err)
	}
	// Correctly signed, but for a different network with the same area tag.
	replayed := testMessage(sigUUID(1))
	replayed.Network = "othernet"
	replayed.OriginNode = author.NodeID()
	if err := replayed.Sign(author); err != nil {
		t.Fatal(err)
	}
	unsignedElsewhere := testMessage(sigUUID(2))
	unsignedElsewhere.Network = "othernet"
	ours := testMessage(sigUUID(3))

	ts, _ := newMockHub(t, []protocol.Message{replayed, unsignedElsewhere, ours})
	writer := &mockJAMWriter{}
	l, _ := setupLeaf(t, ts.URL, writer)
	if count, err := l.Poll(context.Background()); err != nil || count != 1 {
		t.Fatalf("Poll = %d, %v; want 1", count, err)
	}
	if writer.messages[0].MsgUUID != ours.MsgUUID {
		t.Errorf("imported %s, want only this network's message", writer.messages[0].MsgUUID)
	}
}

func TestPoll_DropsOversizeSignedMessage(t *testing.T) {
	author, _, err := keystore.Load(filepath.Join(t.TempDir(), "author.key"))
	if err != nil {
		t.Fatal(err)
	}
	// Signed by hand over an oversize body, as a nonconforming hub might
	// relay it; Sign itself refuses.
	big := testMessage(sigUUID(1))
	big.OriginNode = author.NodeID()
	big.OriginPubKeyB64 = author.PubKeyBase64()
	big.Body = strings.Repeat("z", protocol.MaxBodyBytes+50)
	payload, err := big.SignedPayload()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := author.SignRaw(payload)
	if err != nil {
		t.Fatal(err)
	}
	big.SignatureB64 = base64.StdEncoding.EncodeToString(sig)
	if err := big.VerifySignature(); err != nil {
		t.Fatalf("hand-signed message does not verify: %v", err)
	}
	legacy := testMessage(sigUUID(2))
	legacy.Body = big.Body

	ts, _ := newMockHub(t, []protocol.Message{big, legacy})
	writer := &mockJAMWriter{}
	l, _ := setupLeaf(t, ts.URL, writer)
	if count, err := l.Poll(context.Background()); err != nil || count != 1 {
		t.Fatalf("Poll = %d, %v; want 1", count, err)
	}
	got := writer.messages[0]
	if got.MsgUUID != legacy.MsgUUID || !got.IsTruncated() || len(got.Body) > protocol.MaxBodyBytes {
		t.Errorf("imported %s (truncated %v, %d bytes), want only the unsigned message, truncated",
			got.MsgUUID, got.IsTruncated(), len(got.Body))
	}
}

func TestPoll_StopsWhenHubDoesNotAdvance(t *testing.T) {
	// A hub that answers every cursor with the same page and says there is
	// more, as one does when handed a UUID it does not know.
	page := []protocol.Message{testMessage(sigUUID(1)), testMessage(sigUUID(2))}
	var requests int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-V3Net-Has-More", "true")
		_ = json.NewEncoder(w).Encode(page)
	}))
	defer ts.Close()

	writer := &mockJAMWriter{}
	l, _ := setupLeaf(t, ts.URL, writer)
	count, err := l.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if count != 2 || requests > 3 {
		t.Errorf("Poll imported %d over %d requests; want 2 and a bounded number of requests", count, requests)
	}
}

// sinceHub serves the messages after the one named by the since cursor, as
// the hub does, and records each request's cursor.
type sinceHub struct {
	mu       sync.Mutex
	messages []protocol.Message
	cursors  []string
}

func (h *sinceHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	since := r.URL.Query().Get("since")
	page := h.messages
	for i, m := range h.messages {
		if m.MsgUUID == since {
			page = h.messages[i+1:]
			break
		}
	}
	h.cursors = append(h.cursors, since)
	_ = json.NewEncoder(w).Encode(page)
}

// replace swaps in m for the stored message with the same UUID, as a hub
// holds one row per UUID.
func (h *sinceHub) replace(m protocol.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.messages {
		if h.messages[i].MsgUUID == m.MsgUUID {
			h.messages[i] = m
		}
	}
}

func (h *sinceHub) lastCursor() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cursors[len(h.cursors)-1]
}

// captureLogs sends the default logger to a buffer, at debug level, for the
// rest of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// countLogLines counts the lines of logs at level that contain text.
func countLogLines(logs, level, text string) int {
	n := 0
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "level="+level) && strings.Contains(line, text) {
			n++
		}
	}
	return n
}

func TestPoll_RefusedNewestMessageWarnsOnceAndGenuineImports(t *testing.T) {
	author, _, err := keystore.Load(filepath.Join(t.TempDir(), "author.key"))
	if err != nil {
		t.Fatal(err)
	}
	genuine := testMessage(sigUUID(2))
	genuine.OriginNode = author.NodeID()
	if err := genuine.Sign(author); err != nil {
		t.Fatal(err)
	}
	tampered := genuine
	tampered.Body = "a proxy rewrote this"

	h := &sinceHub{messages: []protocol.Message{testMessage(sigUUID(1)), tampered}}
	ts := httptest.NewServer(h)
	defer ts.Close()
	writer := &mockJAMWriter{}
	l, ix := setupLeaf(t, ts.URL, writer)
	logs := captureLogs(t)

	if count, err := l.Poll(context.Background()); err != nil || count != 1 {
		t.Fatalf("first Poll = %d, %v; want 1", count, err)
	}
	// The tampered message is the newest on the hub. Each poll starts at the
	// last seen message and fetches it again, but warns only the first time.
	for i := 0; i < 3; i++ {
		if count, err := l.Poll(context.Background()); err != nil || count != 0 {
			t.Fatalf("repeat Poll = %d, %v; want 0", count, err)
		}
		if cursor := h.lastCursor(); cursor != sigUUID(1) {
			t.Fatalf("poll asked since %q, want the last seen message %s", cursor, sigUUID(1))
		}
	}
	const text = "failed author signature check"
	if n := countLogLines(logs.String(), "WARN", text); n != 1 {
		t.Errorf("signature warning logged %d times over 4 polls, want 1", n)
	}
	if n := countLogLines(logs.String(), "DEBUG", text); n != 3 {
		t.Errorf("signature debug line logged %d times, want 3", n)
	}
	if seen, _ := ix.Seen(tampered.Network, tampered.OriginNode, tampered.MsgUUID); seen {
		t.Fatal("refused message was marked seen")
	}

	// The hub now serves the genuine message under the same UUID.
	h.replace(genuine)
	if count, err := l.Poll(context.Background()); err != nil || count != 1 {
		t.Fatalf("Poll after correction = %d, %v; want 1", count, err)
	}
	if got := writer.messages[len(writer.messages)-1]; got.MsgUUID != genuine.MsgUUID || got.Body != genuine.Body {
		t.Errorf("imported %s %q, want the genuine message", got.MsgUUID, got.Body)
	}
}

func TestRefusedLog_ForgetsOldestWhenFull(t *testing.T) {
	var r refusedLog
	for i := 0; i < refusedCap; i++ {
		if !r.add(sigUUID(i)) {
			t.Fatalf("add(%d) reported a repeat", i)
		}
	}
	if r.add(sigUUID(0)) {
		t.Fatal("a remembered UUID was reported new")
	}
	if !r.add(sigUUID(refusedCap)) {
		t.Fatal("a new UUID was reported as a repeat")
	}
	if len(r.keys) != refusedCap || len(r.order) != refusedCap {
		t.Errorf("log holds %d/%d entries, want %d", len(r.keys), len(r.order), refusedCap)
	}
	// The oldest entry was dropped to make room.
	if !r.add(sigUUID(0)) {
		t.Error("the oldest UUID was not forgotten")
	}
}

func TestPoll_RetriesUnwritableMessage(t *testing.T) {
	h := &sinceHub{messages: []protocol.Message{testMessage(sigUUID(1)), testMessage(sigUUID(2))}}
	ts := httptest.NewServer(h)
	defer ts.Close()
	writer := &failingJAMWriter{failUUID: sigUUID(2)}
	l, _ := setupLeaf(t, ts.URL, writer)

	if count, err := l.Poll(context.Background()); err != nil || count != 1 {
		t.Fatalf("first Poll = %d, %v; want 1", count, err)
	}
	// Once JAM takes it, the next poll goes back for the message it refused.
	writer.failUUID = ""
	if count, err := l.Poll(context.Background()); err != nil || count != 1 {
		t.Fatalf("second Poll = %d, %v; want 1", count, err)
	}
	if cursor := h.lastCursor(); cursor != sigUUID(1) {
		t.Errorf("second poll asked since %q, want the last seen message %s", cursor, sigUUID(1))
	}
}

func TestPoll_RequireSignedMessagesDropsUnsigned(t *testing.T) {
	author, _, err := keystore.Load(filepath.Join(t.TempDir(), "author.key"))
	if err != nil {
		t.Fatal(err)
	}
	signed := testMessage(sigUUID(1))
	signed.OriginNode = author.NodeID()
	if err := signed.Sign(author); err != nil {
		t.Fatal(err)
	}
	unsigned := testMessage(sigUUID(2))

	ts, _ := newMockHub(t, []protocol.Message{signed, unsigned})
	writer := &mockJAMWriter{}
	l, ix := setupLeaf(t, ts.URL, writer)
	l.cfg.RequireSignedMessages = true

	if count, err := l.Poll(context.Background()); err != nil || count != 1 {
		t.Fatalf("Poll = %d, %v; want 1", count, err)
	}
	if writer.messages[0].MsgUUID != signed.MsgUUID {
		t.Errorf("imported %s, want only the signed message", writer.messages[0].MsgUUID)
	}
	if seen, _ := ix.Seen(unsigned.Network, unsigned.OriginNode, unsigned.MsgUUID); seen {
		t.Error("dropped unsigned message was marked seen")
	}
}
