package protocol_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// A response signature verifies for the request, status and body it was
// made for, and for nothing else.
func TestResponseSignature(t *testing.T) {
	hub := loadKey(t, "hub.key")
	other := loadKey(t, "other.key")
	const reqSig = "request-signature-a"
	body := []byte(`[{"msg_uuid":"x"}]`)

	sig, err := protocol.SignResponse(hub, reqSig, 200, body)
	if err != nil {
		t.Fatalf("SignResponse: %v", err)
	}
	if err := protocol.VerifyResponse(hub.PubKey(), reqSig, 200, body, sig); err != nil {
		t.Fatalf("VerifyResponse round trip: %v", err)
	}

	cases := []struct {
		name   string
		reqSig string
		status int
		body   []byte
		sig    string
	}{
		{name: "tampered body", reqSig: reqSig, status: 200, body: []byte(`[]`), sig: sig},
		{name: "tampered status", reqSig: reqSig, status: 401, body: body, sig: sig},
		{name: "replayed for another request", reqSig: "request-signature-b", status: 200, body: body, sig: sig},
		{name: "bad base64", reqSig: reqSig, status: 200, body: body, sig: "!!" + sig},
		{name: "truncated", reqSig: reqSig, status: 200, body: body, sig: sig[:20]},
	}
	for _, tc := range cases {
		if err := protocol.VerifyResponse(hub.PubKey(), tc.reqSig, tc.status, tc.body, tc.sig); !errors.Is(err, protocol.ErrHubSignature) {
			t.Errorf("%s: VerifyResponse = %v, want ErrHubSignature", tc.name, err)
		}
	}
	if err := protocol.VerifyResponse(other.PubKey(), reqSig, 200, body, sig); !errors.Is(err, protocol.ErrHubSignature) {
		t.Errorf("other key: VerifyResponse = %v, want ErrHubSignature", err)
	}
}

// The response payload is the documented string, so other implementations
// can build it from the docs.
func TestResponseSignedPayloadFormat(t *testing.T) {
	got := string(protocol.ResponseSignedPayload("abc", 204, nil))
	want := "V3NET-RESP-SIG-v1\nabc\n204\ne3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Errorf("payload = %q, want %q", got, want)
	}
	got = string(protocol.EventSignedPayload("testnet", "abc", 7, "ping", []byte(`{}`)))
	want = "V3NET-EVENT-SIG-v1\ntestnet\nabc\n7\nping\n{}"
	if got != want {
		t.Errorf("event payload = %q, want %q", got, want)
	}
}

// An event signature is bound to its network, stream, sequence number, type
// and data; changing any of them, or replaying it into another stream or
// position, fails.
func TestEventSignature(t *testing.T) {
	hub := loadKey(t, "hub.key")
	const reqSig = "stream-a"
	data := []byte(`{"from":"x"}`)

	sig, err := protocol.SignEvent(hub, "testnet", reqSig, 3, "chat", data)
	if err != nil {
		t.Fatalf("SignEvent: %v", err)
	}
	if err := protocol.VerifyEvent(hub.PubKey(), "testnet", reqSig, 3, "chat", data, sig); err != nil {
		t.Fatalf("VerifyEvent round trip: %v", err)
	}
	if strings.ContainsAny(sig, "+/=") {
		t.Errorf("signature %q is not unpadded base64url", sig)
	}

	bad := map[string]error{
		"other network":  protocol.VerifyEvent(hub.PubKey(), "othernet", reqSig, 3, "chat", data, sig),
		"other stream":   protocol.VerifyEvent(hub.PubKey(), "testnet", "stream-b", 3, "chat", data, sig),
		"replayed later": protocol.VerifyEvent(hub.PubKey(), "testnet", reqSig, 4, "chat", data, sig),
		"other type":     protocol.VerifyEvent(hub.PubKey(), "testnet", reqSig, 3, "logon", data, sig),
		"tampered data":  protocol.VerifyEvent(hub.PubKey(), "testnet", reqSig, 3, "chat", []byte(`{"from":"y"}`), sig),
	}
	for name, err := range bad {
		if !errors.Is(err, protocol.ErrHubSignature) {
			t.Errorf("%s: VerifyEvent = %v, want ErrHubSignature", name, err)
		}
	}
}
