package protocol_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func loadKey(t *testing.T, name string) *keystore.Keystore {
	t.Helper()
	ks, _, err := keystore.Load(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatalf("load keystore: %v", err)
	}
	return ks
}

func signedMessage(t *testing.T, ks *keystore.Keystore) protocol.Message {
	t.Helper()
	parent := "550e8400-e29b-41d4-a716-446655440009"
	m := protocol.Message{
		V3Net:       protocol.ProtocolVersion,
		Network:     "testnet",
		AreaTag:     "gen.general",
		MsgUUID:     "550e8400-e29b-41d4-a716-446655440000",
		ThreadUUID:  "550e8400-e29b-41d4-a716-446655440000",
		ParentUUID:  &parent,
		OriginNode:  ks.NodeID(),
		OriginBoard: "General",
		From:        "Tester",
		To:          "All",
		Subject:     "Signed <hello> & welcome",
		DateUTC:     "2026-03-16T04:20:00Z",
		Body:        "Hello V3Net! ünïcödé",
		Tearline:    "--- ViSiON/3",
		Origin:      "Test BBS",
		Attributes:  1,
		Kludges:     map[string]any{"chrs": "UTF-8"},
	}
	if err := m.Sign(ks); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return m
}

func TestMessageSign_RoundTripsThroughJSON(t *testing.T) {
	ks := loadKey(t, "a.key")
	m := signedMessage(t, ks)
	if m.OriginPubKeyB64 != ks.PubKeyBase64() || m.SignatureB64 == "" {
		t.Fatalf("Sign did not set key and signature: %+v", m)
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var got protocol.Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if err := got.VerifySignature(); err != nil {
		t.Fatalf("VerifySignature after JSON round trip: %v", err)
	}
}

func TestMessageVerify_DetectsChangesAfterSigning(t *testing.T) {
	ks := loadKey(t, "a.key")
	other := "550e8400-e29b-41d4-a716-44665544000f"
	cases := []struct {
		name   string
		change func(*protocol.Message)
	}{
		{"body", func(m *protocol.Message) { m.Body += " (edited by hub)" }},
		{"subject", func(m *protocol.Message) { m.Subject = "Altered" }},
		{"from", func(m *protocol.Message) { m.From = "Sysop" }},
		{"to", func(m *protocol.Message) { m.To = "Someone" }},
		{"date", func(m *protocol.Message) { m.DateUTC = "2026-03-17T04:20:00Z" }},
		{"area", func(m *protocol.Message) { m.AreaTag = "gen.other" }},
		{"network", func(m *protocol.Message) { m.Network = "othernet" }},
		{"msg uuid", func(m *protocol.Message) { m.MsgUUID = other }},
		{"thread uuid", func(m *protocol.Message) { m.ThreadUUID = other }},
		{"parent uuid", func(m *protocol.Message) { m.ParentUUID = nil }},
		{"origin board", func(m *protocol.Message) { m.OriginBoard = "Elsewhere" }},
		{"tearline", func(m *protocol.Message) { m.Tearline = "--- Other" }},
		{"origin", func(m *protocol.Message) { m.Origin = "Other BBS" }},
		{"attributes", func(m *protocol.Message) { m.Attributes = 0 }},
		{"version", func(m *protocol.Message) { m.V3Net = "2.0" }},
		{"truncated", func(m *protocol.Message) { m.Body = m.Body[:5] }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := signedMessage(t, ks)
			tc.change(&m)
			if err := m.VerifySignature(); !errors.Is(err, protocol.ErrMessageSignature) {
				t.Fatalf("VerifySignature = %v, want ErrMessageSignature", err)
			}
		})
	}
}

func TestMessageSign_InvalidUTF8MatchesWire(t *testing.T) {
	ks := loadKey(t, "a.key")
	m := protocol.Message{
		V3Net: protocol.ProtocolVersion, Network: "testnet", AreaTag: "gen.general",
		MsgUUID:    "550e8400-e29b-41d4-a716-446655440000",
		ThreadUUID: "550e8400-e29b-41d4-a716-446655440000",
		OriginNode: ks.NodeID(), OriginBoard: "General",
		From: "Ren\x82e", To: "All", Subject: "\xc9\xcd\xbb box",
		DateUTC: "2026-03-16T04:20:00Z", Body: "raw \x80\x81\xfe\xff bytes \xe2\x82",
	}
	raw := m // what encoding/json sent before messages were signed
	if err := m.Sign(ks); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var got protocol.Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if err := got.VerifySignature(); err != nil {
		t.Fatalf("VerifySignature after JSON round trip: %v", err)
	}

	// Readers see the same text as when the raw bytes went out unsigned.
	rawData, _ := json.Marshal(raw)
	var legacy protocol.Message
	if err := json.Unmarshal(rawData, &legacy); err != nil {
		t.Fatal(err)
	}
	if got.Body != legacy.Body || got.Subject != legacy.Subject || got.From != legacy.From {
		t.Errorf("signed text %q/%q/%q differs from unsigned wire text %q/%q/%q",
			got.From, got.Subject, got.Body, legacy.From, legacy.Subject, legacy.Body)
	}
}

func TestMessageVerify_KludgesAreNotSigned(t *testing.T) {
	m := signedMessage(t, loadKey(t, "a.key"))
	m.Kludges = map[string]any{"relayed_by": "hub"}
	if err := m.VerifySignature(); err != nil {
		t.Fatalf("VerifySignature after kludge change: %v", err)
	}
}

func TestMessageVerify_OriginNodeBoundToKey(t *testing.T) {
	a, b := loadKey(t, "a.key"), loadKey(t, "b.key")

	// origin_node changed to another node: the key no longer derives it.
	m := signedMessage(t, a)
	m.OriginNode = b.NodeID()
	if err := m.VerifySignature(); !errors.Is(err, protocol.ErrMessageOriginMismatch) {
		t.Errorf("changed origin_node: %v, want ErrMessageOriginMismatch", err)
	}

	// Key and origin_node swapped together to another node's: the signature
	// was made by a's key, so it fails.
	m = signedMessage(t, a)
	m.OriginNode = b.NodeID()
	m.OriginPubKeyB64 = b.PubKeyBase64()
	if err := m.VerifySignature(); !errors.Is(err, protocol.ErrMessageSignature) {
		t.Errorf("swapped key: %v, want ErrMessageSignature", err)
	}

	// Garbage key.
	m = signedMessage(t, a)
	m.OriginPubKeyB64 = "not base64!"
	if err := m.VerifySignature(); !errors.Is(err, protocol.ErrMessageOriginPubKey) {
		t.Errorf("bad key: %v, want ErrMessageOriginPubKey", err)
	}

	// Signature present with the key stripped is still a signed message.
	m = signedMessage(t, a)
	m.OriginPubKeyB64 = ""
	if !m.IsSigned() {
		t.Error("message with a signature but no key reported unsigned")
	}
	if err := m.VerifySignature(); !errors.Is(err, protocol.ErrMessageOriginPubKey) {
		t.Errorf("stripped key: %v, want ErrMessageOriginPubKey", err)
	}

	// Key present with the signature stripped.
	m = signedMessage(t, a)
	m.SignatureB64 = ""
	if err := m.VerifySignature(); !errors.Is(err, protocol.ErrMessageSignature) {
		t.Errorf("stripped signature: %v, want ErrMessageSignature", err)
	}
}

func TestMessageVerify_Unsigned(t *testing.T) {
	m := protocol.Message{OriginNode: "abc"}
	if m.IsSigned() {
		t.Error("unsigned message reported signed")
	}
	if err := m.VerifySignature(); !errors.Is(err, protocol.ErrMessageUnsigned) {
		t.Errorf("VerifySignature = %v, want ErrMessageUnsigned", err)
	}
	data, _ := json.Marshal(m)
	if strings.Contains(string(data), "signature_b64") || strings.Contains(string(data), "origin_pubkey_b64") {
		t.Errorf("unsigned message JSON carries signature fields: %s", data)
	}
}

func TestMessageSign_Refuses(t *testing.T) {
	a, b := loadKey(t, "a.key"), loadKey(t, "b.key")

	m := signedMessage(t, a)
	if err := m.Sign(b); err == nil {
		t.Error("Sign by a node other than origin_node succeeded")
	}

	m = signedMessage(t, a)
	m.Body = strings.Repeat("x", protocol.MaxBodyBytes+1)
	if err := m.Sign(a); err == nil {
		t.Error("Sign of an oversize body succeeded")
	}
	m.Truncate()
	if err := m.Sign(a); err != nil {
		t.Fatalf("Sign after Truncate: %v", err)
	}
	if err := m.VerifySignature(); err != nil {
		t.Fatalf("VerifySignature after Truncate+Sign: %v", err)
	}
}

func TestMessageSignedPayload_DomainSeparated(t *testing.T) {
	m := signedMessage(t, loadKey(t, "a.key"))
	p, err := m.SignedPayload()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(p, []byte(protocol.MessageSignatureDomain)) {
		t.Fatalf("payload lacks domain prefix: %q", p)
	}
	rest := p[len(protocol.MessageSignatureDomain):]
	var fields map[string]any
	if err := json.Unmarshal(rest, &fields); err != nil {
		t.Fatalf("payload after prefix is not JSON: %v", err)
	}
	if _, ok := fields["signature_b64"]; ok {
		t.Error("payload covers signature_b64")
	}
	if _, ok := fields["kludges"]; ok {
		t.Error("payload covers kludges")
	}
	// Keys are sorted and HTML is not escaped, so other implementations can
	// rebuild the same bytes.
	if !strings.HasPrefix(string(rest), `{"area_tag":`) || !strings.HasSuffix(string(rest), `"v3net":"1.0"}`) {
		t.Errorf("payload keys not in sorted order: %s", rest)
	}
	if !strings.Contains(string(rest), `"subject":"Signed <hello> & welcome"`) {
		t.Errorf("payload escapes HTML: %s", rest)
	}
}
