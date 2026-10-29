package leaf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
	for n := 1; n <= 5; n++ {
		if seen, _ := ix.Seen(sigUUID(n)); seen {
			t.Errorf("dropped message %s was marked seen", sigUUID(n))
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
