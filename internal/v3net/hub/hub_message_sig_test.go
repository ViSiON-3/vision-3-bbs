package hub

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// sigTestHub is a hub with the test NAL and one leaf subscribed to
// gen.general.
func sigTestHub(t *testing.T) (*Hub, *httptest.Server, *keystore.Keystore) {
	t.Helper()
	h, hubKS := setupTestHub(t)
	ts := httptest.NewServer(h.newMux())
	t.Cleanup(ts.Close)
	seedTestNAL(t, h, hubKS)
	leafKS := loadTestKeystore(t, "leaf.key")
	registerLeafWithAreas(t, ts, leafKS, []string{"gen.general"})
	return h, ts, leafKS
}

// sigTestMessage returns an unsigned message from originNode.
func sigTestMessage(originNode, uuid string) protocol.Message {
	return protocol.Message{
		V3Net:       protocol.ProtocolVersion,
		Network:     "testnet",
		AreaTag:     "gen.general",
		MsgUUID:     uuid,
		ThreadUUID:  uuid,
		OriginNode:  originNode,
		OriginBoard: "General",
		From:        "Tester",
		To:          "All",
		Subject:     "Signed test",
		DateUTC:     "2026-03-16T04:20:00Z",
		Body:        "Hello signed V3Net!",
		Kludges:     map[string]any{},
	}
}

// postAs posts msg to the hub authenticated as ks and returns the status
// and response body.
func postAs(t *testing.T, ts *httptest.Server, ks *keystore.Keystore, msg protocol.Message) (int, string) {
	t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(signedRequest(t, ks, "POST", ts.URL+"/v3net/v1/testnet/messages", string(data)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// storedMessages fetches every message the hub holds for leafKS.
func storedMessages(t *testing.T, ts *httptest.Server, ks *keystore.Keystore) []protocol.Message {
	t.Helper()
	resp, err := http.DefaultClient.Do(signedRequest(t, ks, "GET", ts.URL+"/v3net/v1/testnet/messages", ""))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var msgs []protocol.Message
	if err := json.NewDecoder(resp.Body).Decode(&msgs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return msgs
}

func TestPostMessage_SignedIsStoredIntact(t *testing.T) {
	_, ts, leafKS := sigTestHub(t)
	msg := sigTestMessage(leafKS.NodeID(), "a10e8400-e29b-41d4-a716-446655440001")
	if err := msg.Sign(leafKS); err != nil {
		t.Fatal(err)
	}
	if status, body := postAs(t, ts, leafKS, msg); status != http.StatusOK {
		t.Fatalf("POST signed = %d %s, want 200", status, body)
	}
	msgs := storedMessages(t, ts, leafKS)
	if len(msgs) != 1 {
		t.Fatalf("stored %d messages, want 1", len(msgs))
	}
	if err := msgs[0].VerifySignature(); err != nil {
		t.Fatalf("relayed message no longer verifies: %v", err)
	}
}

func TestPostMessage_RejectsBadSignatures(t *testing.T) {
	h, ts, leafKS := sigTestHub(t)
	otherKS := loadTestKeystore(t, "other.key")
	const uuid = "a20e8400-e29b-41d4-a716-446655440002"

	signedBy := func(ks *keystore.Keystore) protocol.Message {
		m := sigTestMessage(ks.NodeID(), uuid)
		if err := m.Sign(ks); err != nil {
			t.Fatal(err)
		}
		return m
	}

	tampered := signedBy(leafKS)
	tampered.Body = "altered after signing"

	// A well-formed signature by a key that is not the posting subscriber's.
	foreign := signedBy(otherKS)

	// Another node's key claimed under the poster's own origin_node.
	mismatched := signedBy(otherKS)
	mismatched.OriginNode = leafKS.NodeID()

	cases := []struct {
		name string
		msg  protocol.Message
		want string
	}{
		{"altered after signing", tampered, "signature invalid"},
		{"not the subscriber's key", foreign, "not the posting node's registered key"},
		{"origin_node not derived from key", mismatched, "signature invalid"},
	}
	// A bad signature is rejected whether or not unsigned posts are allowed.
	for _, require := range []bool{false, true} {
		h.cfg.RequireSignedMessages = require
		for _, tc := range cases {
			status, body := postAs(t, ts, leafKS, tc.msg)
			if status != http.StatusForbidden || !strings.Contains(body, tc.want) {
				t.Errorf("require=%v %s: POST = %d %s, want 403 %q", require, tc.name, status, body, tc.want)
			}
		}
	}
	if msgs := storedMessages(t, ts, leafKS); len(msgs) != 0 {
		t.Errorf("hub stored %d rejected messages", len(msgs))
	}
}

func TestPostMessage_RequireSignedMessages(t *testing.T) {
	h, ts, leafKS := sigTestHub(t)

	h.cfg.RequireSignedMessages = true
	unsigned := sigTestMessage(leafKS.NodeID(), "a30e8400-e29b-41d4-a716-446655440003")
	if status, body := postAs(t, ts, leafKS, unsigned); status != http.StatusUnauthorized {
		t.Errorf("flag on, unsigned: POST = %d %s, want 401", status, body)
	}
	signed := sigTestMessage(leafKS.NodeID(), "a40e8400-e29b-41d4-a716-446655440004")
	if err := signed.Sign(leafKS); err != nil {
		t.Fatal(err)
	}
	if status, body := postAs(t, ts, leafKS, signed); status != http.StatusOK {
		t.Errorf("flag on, signed: POST = %d %s, want 200", status, body)
	}

	h.cfg.RequireSignedMessages = false
	if status, body := postAs(t, ts, leafKS, unsigned); status != http.StatusOK {
		t.Errorf("flag off, unsigned: POST = %d %s, want 200", status, body)
	}
	if msgs := storedMessages(t, ts, leafKS); len(msgs) != 2 {
		t.Errorf("stored %d messages, want 2", len(msgs))
	}
}

func TestPostMessage_OriginNodeMustBeAuthenticatedNode(t *testing.T) {
	_, ts, leafKS := sigTestHub(t)
	otherKS := loadTestKeystore(t, "other.key")

	spoofed := sigTestMessage(otherKS.NodeID(), "a50e8400-e29b-41d4-a716-446655440005")
	if status, body := postAs(t, ts, leafKS, spoofed); status != http.StatusForbidden ||
		!strings.Contains(body, "origin_node does not match") {
		t.Errorf("unsigned post as another node = %d %s, want 403", status, body)
	}
	own := sigTestMessage(leafKS.NodeID(), "a60e8400-e29b-41d4-a716-446655440006")
	if status, body := postAs(t, ts, leafKS, own); status != http.StatusOK {
		t.Errorf("unsigned post as itself = %d %s, want 200", status, body)
	}
}

func TestPostMessage_OversizeSignedRejectedUnsignedTruncated(t *testing.T) {
	_, ts, leafKS := sigTestHub(t)
	big := strings.Repeat("x", protocol.MaxBodyBytes+100)

	signed := sigTestMessage(leafKS.NodeID(), "a70e8400-e29b-41d4-a716-446655440007")
	signed.OriginPubKeyB64 = leafKS.PubKeyBase64()
	signed.Body = big
	// Sign refuses an oversize body, so sign it by hand the way a broken
	// or hostile leaf could.
	payload, err := signed.SignedPayload()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := leafKS.SignRaw(payload)
	if err != nil {
		t.Fatal(err)
	}
	signed.SignatureB64 = base64.StdEncoding.EncodeToString(sig)
	if err := signed.VerifySignature(); err != nil {
		t.Fatalf("hand-signed message does not verify: %v", err)
	}
	if status, body := postAs(t, ts, leafKS, signed); status != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize signed: POST = %d %s, want 413", status, body)
	}

	unsigned := sigTestMessage(leafKS.NodeID(), "a80e8400-e29b-41d4-a716-446655440008")
	unsigned.Body = big
	if status, body := postAs(t, ts, leafKS, unsigned); status != http.StatusOK {
		t.Fatalf("oversize unsigned: POST = %d %s, want 200", status, body)
	}
	msgs := storedMessages(t, ts, leafKS)
	if len(msgs) != 1 || msgs[0].MsgUUID != unsigned.MsgUUID {
		t.Fatalf("stored %+v, want only the unsigned message", msgs)
	}
	if len(msgs[0].Body) > protocol.MaxBodyBytes || !msgs[0].IsTruncated() {
		t.Errorf("legacy unsigned message not truncated: %d bytes, flag %v", len(msgs[0].Body), msgs[0].IsTruncated())
	}
}
