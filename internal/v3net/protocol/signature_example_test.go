package protocol_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// The worked example in docs/sysop/v3net/message-areas.md ("Message
// Signatures"). Ed25519 signatures are deterministic, so the same key and
// message always give these bytes. TestMessageSignatureDocExample keeps the
// doc and the code in step.
const (
	// exampleSeedHex is the secret key of RFC 8032 section 7.1, test 1.
	exampleSeedHex   = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
	examplePubKeyB64 = "11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="
	exampleNodeID    = "21fe31dfa154a261"
	examplePayload   = "V3NET-MSG-SIG-v1\n" +
		`{"area_tag":"fel.general","attributes":0,"body":"Café opens at 8.\r\nSee you there!",` +
		`"date_utc":"2026-10-29T12:00:00Z","from":"Sysop","msg_uuid":"6f9619ff-8b86-4d01-b42d-00cf4fc964ff",` +
		`"network":"felonynet","origin":"Example BBS","origin_board":"General","origin_node":"21fe31dfa154a261",` +
		`"origin_pubkey_b64":"11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo=","parent_uuid":null,` +
		`"subject":"Q&A <tonight>","tearline":"--- ViSiON/3","thread_uuid":"6f9619ff-8b86-4d01-b42d-00cf4fc964ff",` +
		`"to":"All","v3net":"1.0"}`
	examplePayloadSHA256 = "3d4746d10764ef86e059b25669c591e01e62a7b4911f17dccfdbf5acfcba5c60"
	exampleSigB64        = "buk/YQJzkqsR2N2RMdVMjKBgpXP6MYvDIFEXjQQ+NnlXYPS7+J5BrJF3YBWGmu/CG4DtSinfXoic8U1oZz6MCA=="
)

// seedSigner signs with a fixed ed25519 key.
type seedSigner struct{ priv ed25519.PrivateKey }

func (s seedSigner) pub() ed25519.PublicKey { return s.priv.Public().(ed25519.PublicKey) }
func (s seedSigner) NodeID() string         { return keystore.NodeIDFromPubKey(s.pub()) }
func (s seedSigner) PubKeyBase64() string   { return base64.StdEncoding.EncodeToString(s.pub()) }
func (s seedSigner) SignRaw(p []byte) ([]byte, error) {
	return ed25519.Sign(s.priv, p), nil
}

func exampleMessage(nodeID string) protocol.Message {
	return protocol.Message{
		V3Net:       protocol.ProtocolVersion,
		Network:     "felonynet",
		AreaTag:     "fel.general",
		MsgUUID:     "6f9619ff-8b86-4d01-b42d-00cf4fc964ff",
		ThreadUUID:  "6f9619ff-8b86-4d01-b42d-00cf4fc964ff",
		OriginNode:  nodeID,
		OriginBoard: "General",
		From:        "Sysop",
		To:          "All",
		Subject:     "Q&A <tonight>",
		DateUTC:     "2026-10-29T12:00:00Z",
		Body:        "Café opens at 8.\r\nSee you there!",
		Tearline:    "--- ViSiON/3",
		Origin:      "Example BBS",
		Kludges:     map[string]any{"chrs": "UTF-8"},
	}
}

func TestMessageSignatureDocExample(t *testing.T) {
	seed, err := hex.DecodeString(exampleSeedHex)
	if err != nil {
		t.Fatal(err)
	}
	s := seedSigner{priv: ed25519.NewKeyFromSeed(seed)}
	m := exampleMessage(s.NodeID())
	if err := m.Sign(s); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	payload, err := m.SignedPayload()
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(payload)
	got := map[string]string{
		"public key":     s.PubKeyBase64(),
		"node ID":        s.NodeID(),
		"payload":        string(payload),
		"payload SHA256": hex.EncodeToString(sum[:]),
		"signature":      m.SignatureB64,
	}
	want := map[string]string{
		"public key":     examplePubKeyB64,
		"node ID":        exampleNodeID,
		"payload":        examplePayload,
		"payload SHA256": examplePayloadSHA256,
		"signature":      exampleSigB64,
	}
	for k := range want {
		if got[k] != want[k] {
			t.Errorf("%s = %q, want %q", k, got[k], want[k])
		}
	}
	if err := m.VerifySignature(); err != nil {
		t.Errorf("example does not verify: %v", err)
	}

	doc, err := os.ReadFile("../../../docs/sysop/v3net/message-areas.md")
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range want {
		if !strings.Contains(string(doc), v) {
			t.Errorf("message-areas.md does not show the example %s %q", k, v)
		}
	}
	if !strings.Contains(string(doc), exampleSeedHex) {
		t.Error("message-areas.md does not show the example seed")
	}
}
