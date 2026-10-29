package protocol

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
)

// The hub signs what it sends a leaf with its node key, the key /info
// serves and the leaf pins. A response is bound to the request it answers
// by covering that request's signature and its nonce. The signature alone
// is not enough: Date has one-second resolution and Ed25519 is
// deterministic, so two identical requests in the same second carry the
// same signature, and a response captured for one would verify for the
// other. The leaf therefore sends a fresh random nonce with every request.
// An SSE stream cannot be signed as one body, so each event is signed
// instead, numbered so it cannot be replayed or reordered within its stream.
//
// The nonce is not part of the request signature, whose form older hubs
// verify. Changing it on the way only makes the reply fail to verify.

// Hub signature domains. Like MessageSignatureDomain, each separates its
// signatures from every other kind the protocol uses, and the version suffix
// changes if the signed fields ever do.
const (
	ResponseSignatureDomain = "V3NET-RESP-SIG-v1\n"
	EventSignatureDomain    = "V3NET-EVENT-SIG-v1\n"
)

// HeaderHubSignature carries the hub's signature over a response to a
// signed request.
const HeaderHubSignature = "X-V3Net-Hub-Signature"

// HeaderNonce carries the leaf's per-request nonce, which the hub's
// signatures on the reply cover. Older hubs ignore it; a request from an
// older leaf has none and its reply is signed with an empty nonce.
const HeaderNonce = "X-V3Net-Nonce"

// MaxNonceLen bounds the nonce a hub signs over. A longer header is treated
// as absent (RequestNonce).
const MaxNonceLen = 128

// ErrHubSignature is returned when a hub response or event signature is not
// valid base64url or does not verify against the given key.
var ErrHubSignature = errors.New("protocol: hub signature verification failed")

// HubSigner is the key a hub signs with. *keystore.Keystore satisfies it.
type HubSigner interface {
	SignRaw(payload []byte) ([]byte, error)
}

// NewNonce returns a fresh request nonce: 16 random bytes, unpadded
// base64url.
func NewNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// RequestNonce returns the nonce a hub signs over for a request whose
// X-V3Net-Nonce header is h: h itself, or "" if it is over MaxNonceLen.
func RequestNonce(h string) string {
	if len(h) > MaxNonceLen {
		return ""
	}
	return h
}

// ResponseSignedPayload returns the exact bytes a response signature covers:
//
//	V3NET-RESP-SIG-v1\n{request signature}\n{nonce}\n{status}\n{sha256(body) hex}
//
// where the request signature is the request's X-V3Net-Signature header as
// sent, nonce its X-V3Net-Nonce header (empty if none) and status the
// decimal HTTP status code. A streamed (SSE) response is signed with an
// empty body.
func ResponseSignedPayload(requestSig, nonce string, status int, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte(ResponseSignatureDomain + requestSig + "\n" + nonce + "\n" + strconv.Itoa(status) + "\n" + hex.EncodeToString(sum[:]))
}

// SignResponse returns the base64url (unpadded) signature for
// X-V3Net-Hub-Signature over ResponseSignedPayload.
func SignResponse(s HubSigner, requestSig, nonce string, status int, body []byte) (string, error) {
	return signB64(s, ResponseSignedPayload(requestSig, nonce, status, body))
}

// VerifyResponse checks an X-V3Net-Hub-Signature value against pub. It
// returns ErrHubSignature if it does not verify.
func VerifyResponse(pub ed25519.PublicKey, requestSig, nonce string, status int, body []byte, sigB64 string) error {
	return verifyB64(pub, ResponseSignedPayload(requestSig, nonce, status, body), sigB64)
}

// EventSignedPayload returns the exact bytes an SSE event signature covers:
//
//	V3NET-EVENT-SIG-v1\n{network}\n{request signature}\n{nonce}\n{seq}\n{type}\n{data}
//
// where the request signature and nonce are those of the request that
// opened the stream (the nonce empty if it had none), seq is the event's
// decimal sequence number in that stream (the first event is 1) and data is
// the event's JSON as sent.
func EventSignedPayload(network, requestSig, nonce string, seq uint64, eventType string, data []byte) []byte {
	b := make([]byte, 0, len(EventSignatureDomain)+len(network)+len(requestSig)+len(nonce)+len(eventType)+len(data)+26)
	b = append(b, EventSignatureDomain...)
	b = append(b, network...)
	b = append(b, '\n')
	b = append(b, requestSig...)
	b = append(b, '\n')
	b = append(b, nonce...)
	b = append(b, '\n')
	b = strconv.AppendUint(b, seq, 10)
	b = append(b, '\n')
	b = append(b, eventType...)
	b = append(b, '\n')
	return append(b, data...)
}

// SignEvent returns the base64url (unpadded) signature for an SSE event's
// sig field over EventSignedPayload.
func SignEvent(s HubSigner, network, requestSig, nonce string, seq uint64, eventType string, data []byte) (string, error) {
	return signB64(s, EventSignedPayload(network, requestSig, nonce, seq, eventType, data))
}

// VerifyEvent checks an SSE event's sig field against pub. It returns
// ErrHubSignature if it does not verify.
func VerifyEvent(pub ed25519.PublicKey, network, requestSig, nonce string, seq uint64, eventType string, data []byte, sigB64 string) error {
	return verifyB64(pub, EventSignedPayload(network, requestSig, nonce, seq, eventType, data), sigB64)
}

func signB64(s HubSigner, payload []byte) (string, error) {
	sig, err := s.SignRaw(payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(sig), nil
}

func verifyB64(pub ed25519.PublicKey, payload []byte, sigB64 string) error {
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil || len(sig) != ed25519.SignatureSize || len(pub) != ed25519.PublicKeySize {
		return ErrHubSignature
	}
	if !ed25519.Verify(pub, payload, sig) {
		return ErrHubSignature
	}
	return nil
}
