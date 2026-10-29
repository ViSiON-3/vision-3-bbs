package protocol_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// A response signature verifies for the request, nonce, status and body it
// was made for, and for nothing else.
func TestResponseSignature(t *testing.T) {
	hub := loadKey(t, "hub.key")
	other := loadKey(t, "other.key")
	const reqSig, nonce = "request-signature-a", "nonce-a"
	body := []byte(`[{"msg_uuid":"x"}]`)

	sig, err := protocol.SignResponse(hub, reqSig, nonce, 200, body)
	if err != nil {
		t.Fatalf("SignResponse: %v", err)
	}
	if err := protocol.VerifyResponse(hub.PubKey(), reqSig, nonce, 200, body, sig); err != nil {
		t.Fatalf("VerifyResponse round trip: %v", err)
	}

	cases := []struct {
		name          string
		reqSig, nonce string
		status        int
		body          []byte
		sig           string
	}{
		{name: "tampered body", reqSig: reqSig, nonce: nonce, status: 200, body: []byte(`[]`), sig: sig},
		{name: "tampered status", reqSig: reqSig, nonce: nonce, status: 401, body: body, sig: sig},
		{name: "replayed for another request", reqSig: "request-signature-b", nonce: nonce, status: 200, body: body, sig: sig},
		{name: "replayed for an identical request with another nonce", reqSig: reqSig, nonce: "nonce-b", status: 200, body: body, sig: sig},
		{name: "nonce dropped", reqSig: reqSig, status: 200, body: body, sig: sig},
		{name: "bad base64", reqSig: reqSig, nonce: nonce, status: 200, body: body, sig: "!!" + sig},
		{name: "truncated", reqSig: reqSig, nonce: nonce, status: 200, body: body, sig: sig[:20]},
	}
	for _, tc := range cases {
		if err := protocol.VerifyResponse(hub.PubKey(), tc.reqSig, tc.nonce, tc.status, tc.body, tc.sig); !errors.Is(err, protocol.ErrHubSignature) {
			t.Errorf("%s: VerifyResponse = %v, want ErrHubSignature", tc.name, err)
		}
	}
	if err := protocol.VerifyResponse(other.PubKey(), reqSig, nonce, 200, body, sig); !errors.Is(err, protocol.ErrHubSignature) {
		t.Errorf("other key: VerifyResponse = %v, want ErrHubSignature", err)
	}
}

// The payloads are the documented strings, so other implementations can
// build them from the docs. A request without a nonce (an older leaf) is
// signed with an empty one.
func TestResponseSignedPayloadFormat(t *testing.T) {
	got := string(protocol.ResponseSignedPayload("abc", "n0nce", 204, nil))
	want := "V3NET-RESP-SIG-v1\nabc\nn0nce\n204\ne3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Errorf("payload = %q, want %q", got, want)
	}
	got = string(protocol.ResponseSignedPayload("abc", "", 204, nil))
	want = "V3NET-RESP-SIG-v1\nabc\n\n204\ne3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Errorf("payload without nonce = %q, want %q", got, want)
	}
	got = string(protocol.EventSignedPayload("testnet", "abc", "n0nce", 7, "ping", []byte(`{}`)))
	want = "V3NET-EVENT-SIG-v1\ntestnet\nabc\nn0nce\n7\nping\n{}"
	if got != want {
		t.Errorf("event payload = %q, want %q", got, want)
	}
}

// Nonces are fresh, unpadded base64url, and a hub signs over an oversized
// nonce header as if it were absent.
func TestNonce(t *testing.T) {
	a, err := protocol.NewNonce()
	if err != nil {
		t.Fatal(err)
	}
	b, err := protocol.NewNonce()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || len(a) != 22 || strings.ContainsAny(a, "+/=") {
		t.Errorf("nonces %q, %q: want two distinct 22-char base64url strings", a, b)
	}
	if got := protocol.RequestNonce(a); got != a {
		t.Errorf("RequestNonce(%q) = %q", a, got)
	}
	if got := protocol.RequestNonce(strings.Repeat("x", protocol.MaxNonceLen+1)); got != "" {
		t.Errorf("RequestNonce of an oversized nonce = %q, want empty", got)
	}
}

// An event signature is bound to its network, stream, nonce, sequence
// number, type and data; changing any of them, or replaying it into another
// stream or position, fails.
func TestEventSignature(t *testing.T) {
	hub := loadKey(t, "hub.key")
	const reqSig, nonce = "stream-a", "nonce-a"
	data := []byte(`{"from":"x"}`)

	sig, err := protocol.SignEvent(hub, "testnet", reqSig, nonce, 3, "chat", data)
	if err != nil {
		t.Fatalf("SignEvent: %v", err)
	}
	if err := protocol.VerifyEvent(hub.PubKey(), "testnet", reqSig, nonce, 3, "chat", data, sig); err != nil {
		t.Fatalf("VerifyEvent round trip: %v", err)
	}
	if strings.ContainsAny(sig, "+/=") {
		t.Errorf("signature %q is not unpadded base64url", sig)
	}

	bad := map[string]error{
		"other network":           protocol.VerifyEvent(hub.PubKey(), "othernet", reqSig, nonce, 3, "chat", data, sig),
		"other stream":            protocol.VerifyEvent(hub.PubKey(), "testnet", "stream-b", nonce, 3, "chat", data, sig),
		"same-second other nonce": protocol.VerifyEvent(hub.PubKey(), "testnet", reqSig, "nonce-b", 3, "chat", data, sig),
		"replayed later":          protocol.VerifyEvent(hub.PubKey(), "testnet", reqSig, nonce, 4, "chat", data, sig),
		"other type":              protocol.VerifyEvent(hub.PubKey(), "testnet", reqSig, nonce, 3, "logon", data, sig),
		"tampered data":           protocol.VerifyEvent(hub.PubKey(), "testnet", reqSig, nonce, 3, "chat", []byte(`{"from":"y"}`), sig),
	}
	for name, err := range bad {
		if !errors.Is(err, protocol.ErrHubSignature) {
			t.Errorf("%s: VerifyEvent = %v, want ErrHubSignature", name, err)
		}
	}
}
