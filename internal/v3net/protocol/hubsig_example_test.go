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

// The worked example in docs/sysop/v3net/message-areas.md ("Hub
// Signatures"): a signed reply to a message poll and a signed SSE event. The
// hub signs with the key of the message signature example (RFC 8032 section
// 7.1, test 1) and the leaf with the key of test 2. Ed25519 signatures are
// deterministic, so these values never change. TestHubSignatureDocExample
// keeps the doc and the code in step.
const (
	// exampleLeafSeedHex is the secret key of RFC 8032 section 7.1, test 2.
	exampleLeafSeedHex   = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"
	exampleLeafPubKeyB64 = "PUAXw+hDiVqStwqnTRt+vJyYLM8uxJaMwM1V8Sr0Zgw="
	exampleLeafNodeID    = "39f713d0a644253f"
	emptyBodySHA256      = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	examplePollPath    = "/v3net/v1/felonynet/messages?since=&limit=100&identity=origin&format=page"
	examplePollDate    = "Thu, 29 Oct 2026 12:00:00 GMT"
	examplePollNonce   = "AAECAwQFBgcICQoLDA0ODw"
	examplePollReqSig  = "ZGIM6oH1yXiQb76W2fM1Oskcq9quIqOfcEz87Ts45d3D1_y-PA9kTApaeS-dfN2d4FMMbVlL8Itr9YWJ5RbLAw"
	examplePollBody    = `{"messages":[],"has_more":false}`
	examplePollBodySHA = "f4528e8588f80a1c2a47d6468ec1ce76305c0780fccee566e4d2e13012026a65"
	exampleRespPayload = "V3NET-RESP-SIG-v1\n" + examplePollReqSig + "\n" + examplePollNonce + "\n200\n" + examplePollBodySHA
	exampleRespSig     = "Q3GGVka76e7yLCKTmyvRnwLePhlmTNoyusbsiV1UKLd8wnf8HA5CYCe6h2xE1ZN5cjoQ2L9iWGuII-bQUvklAA"

	exampleEventsPath   = "/v3net/v1/felonynet/events"
	exampleEventsDate   = "Thu, 29 Oct 2026 12:00:05 GMT"
	exampleEventsNonce  = "EBESExQVFhcYGRobHB0eHw"
	exampleEventsReqSig = "5CtxwqDTTWItRJcSnHzbftIFXtSKt0NGKhaOqmbqn2R0OuYfSM91EwZiBZaAjXoU7VM6piLzZulxtkXQvFHEDg"
	exampleEventData    = `{"handle":"Sysop","node":"39f713d0a644253f","timestamp":"2026-10-29T12:00:07Z"}`
	exampleEventPayload = "V3NET-EVENT-SIG-v1\nfelonynet\n" + exampleEventsReqSig + "\n" + exampleEventsNonce + "\n1\nlogon\n" + exampleEventData
	exampleEventSig     = "6KGM84CZziZt-xRrnWzvbgMaxJdJOFCMNLEHUJ4qnPRiY1uJDDJPzOH9SXrhP9UFO5svy6I0AFVktMkVxp9vAg"
	exampleEventSSE     = "event: logon\nid: 1\nsig: " + exampleEventSig + "\ndata: " + exampleEventData + "\n"
)

func TestHubSignatureDocExample(t *testing.T) {
	hub := seedSignerFromHex(t, exampleSeedHex)
	leaf := seedSignerFromHex(t, exampleLeafSeedHex)

	pollReqSig := requestSig(leaf, examplePollPath, examplePollDate)
	bodySum := sha256.Sum256([]byte(examplePollBody))
	respSig, err := protocol.SignResponse(hub, pollReqSig, examplePollNonce, 200, []byte(examplePollBody))
	if err != nil {
		t.Fatal(err)
	}
	eventsReqSig := requestSig(leaf, exampleEventsPath, exampleEventsDate)
	eventSig, err := protocol.SignEvent(hub, "felonynet", eventsReqSig, exampleEventsNonce, 1, protocol.EventLogon, []byte(exampleEventData))
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]string{
		"hub public key":           hub.PubKeyBase64(),
		"hub node ID":              hub.NodeID(),
		"leaf public key":          leaf.PubKeyBase64(),
		"leaf node ID":             leaf.NodeID(),
		"poll request signature":   pollReqSig,
		"poll body SHA256":         hex.EncodeToString(bodySum[:]),
		"response payload":         string(protocol.ResponseSignedPayload(pollReqSig, examplePollNonce, 200, []byte(examplePollBody))),
		"response signature":       respSig,
		"events request signature": eventsReqSig,
		"event payload":            string(protocol.EventSignedPayload("felonynet", eventsReqSig, exampleEventsNonce, 1, protocol.EventLogon, []byte(exampleEventData))),
		"event signature":          eventSig,
	}
	want := map[string]string{
		"hub public key":           examplePubKeyB64,
		"hub node ID":              exampleNodeID,
		"leaf public key":          exampleLeafPubKeyB64,
		"leaf node ID":             exampleLeafNodeID,
		"poll request signature":   examplePollReqSig,
		"poll body SHA256":         examplePollBodySHA,
		"response payload":         exampleRespPayload,
		"response signature":       exampleRespSig,
		"events request signature": exampleEventsReqSig,
		"event payload":            exampleEventPayload,
		"event signature":          exampleEventSig,
	}
	for k := range want {
		if got[k] != want[k] {
			t.Errorf("%s = %q, want %q", k, got[k], want[k])
		}
	}

	// The request signatures are real ones, as the hub checks them.
	if !keystore.Verify(leaf.pub(), "GET", examplePollPath, examplePollDate, emptyBodySHA256, examplePollReqSig) ||
		!keystore.Verify(leaf.pub(), "GET", exampleEventsPath, exampleEventsDate, emptyBodySHA256, exampleEventsReqSig) {
		t.Error("an example request signature does not verify")
	}
	if err := protocol.VerifyResponse(hub.pub(), examplePollReqSig, examplePollNonce, 200, []byte(examplePollBody), exampleRespSig); err != nil {
		t.Errorf("example response signature does not verify: %v", err)
	}
	if err := protocol.VerifyEvent(hub.pub(), "felonynet", exampleEventsReqSig, exampleEventsNonce, 1, protocol.EventLogon, []byte(exampleEventData), exampleEventSig); err != nil {
		t.Errorf("example event signature does not verify: %v", err)
	}

	raw, err := os.ReadFile("../../../docs/sysop/v3net/message-areas.md")
	if err != nil {
		t.Fatal(err)
	}
	// A Windows checkout may turn the doc's line endings into CRLF.
	doc := strings.ReplaceAll(string(raw), "\r\n", "\n")
	shown := map[string]string{
		"leaf seed":         exampleLeafSeedHex,
		"poll path":         examplePollPath,
		"poll date":         examplePollDate,
		"poll nonce":        examplePollNonce,
		"poll body":         examplePollBody,
		"events path":       exampleEventsPath,
		"events date":       exampleEventsDate,
		"events nonce":      exampleEventsNonce,
		"event as sent":     exampleEventSSE,
		"empty body SHA256": emptyBodySHA256,
	}
	for k, v := range want {
		shown[k] = v
	}
	for k, v := range shown {
		if !strings.Contains(doc, v) {
			t.Errorf("message-areas.md does not show the hub signature example's %s %q", k, v)
		}
	}
}

func seedSignerFromHex(t *testing.T, seedHex string) seedSigner {
	t.Helper()
	seed, err := hex.DecodeString(seedHex)
	if err != nil {
		t.Fatal(err)
	}
	return seedSigner{priv: ed25519.NewKeyFromSeed(seed)}
}

// requestSig is the X-V3Net-Signature a leaf sends with a GET of path at
// date, as keystore.Keystore.Sign makes it.
func requestSig(s seedSigner, path, date string) string {
	sig, _ := s.SignRaw([]byte("GET\n" + path + "\n" + date + "\n" + emptyBodySHA256))
	return base64.RawURLEncoding.EncodeToString(sig)
}
