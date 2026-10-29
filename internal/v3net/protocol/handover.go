package protocol

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// A coordinator handover is the outgoing coordinator's signed statement
// that a network's NAL is signed by a new key from now on. A leaf that pins
// the old key and verifies the statement against that pin moves its pin to
// the new key without the sysop (see leaf/pin.go). The hub keeps every
// handover it accepted and serves them with the NAL, so a leaf that was
// offline through several handovers can follow the chain from its pin.

// CoordHandoverDomain prefixes the canonical JSON a handover signature
// covers, keeping it apart from every other signature the protocol uses.
// The version suffix changes if the signed fields ever do.
const CoordHandoverDomain = "V3NET-COORD-HANDOVER-v1\n"

// Coordinator handover errors.
var (
	// ErrHandoverKey is returned when old_key or new_key is not a valid
	// base64 ed25519 public key, or the two are the same key.
	ErrHandoverKey = errors.New("protocol: invalid handover key")
	// ErrHandoverIssued is returned when issued is not an RFC 3339 time.
	ErrHandoverIssued = errors.New("protocol: invalid handover issued time")
	// ErrHandoverSignature is returned when signature_b64 is missing, not
	// valid base64, or does not verify under old_key.
	ErrHandoverSignature = errors.New("protocol: handover signature verification failed")
)

// CoordHandover hands a network's coordinator role from OldKeyB64 to
// NewKeyB64. It is signed by the old key.
type CoordHandover struct {
	Network   string `json:"network"`
	OldKeyB64 string `json:"old_key"` // standard base64 ed25519 public key
	NewKeyB64 string `json:"new_key"` // standard base64 ed25519 public key
	Issued    string `json:"issued"`  // RFC 3339 UTC
	// SignatureB64 is the old key's ed25519 signature over SignedPayload,
	// in standard base64.
	SignatureB64 string `json:"signature_b64"`
}

// HandoverSigner is the outgoing coordinator's key. *keystore.Keystore
// satisfies it.
type HandoverSigner interface {
	PubKeyBase64() string
	SignRaw(payload []byte) ([]byte, error)
}

// canonicalHandover is the signed form of a CoordHandover, with its fields
// in alphabetical order of their JSON keys like canonicalMessage.
type canonicalHandover struct {
	Issued    string `json:"issued"`
	Network   string `json:"network"`
	NewKeyB64 string `json:"new_key"`
	OldKeyB64 string `json:"old_key"`
}

// SignedPayload returns the exact bytes a handover signature covers:
// CoordHandoverDomain followed by the compact JSON of issued, network,
// new_key and old_key, keys sorted and HTML escaping off.
func (h *CoordHandover) SignedPayload() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(CoordHandoverDomain)
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(canonicalHandover{
		Issued:    h.Issued,
		Network:   h.Network,
		NewKeyB64: h.NewKeyB64,
		OldKeyB64: h.OldKeyB64,
	}); err != nil {
		return nil, fmt.Errorf("protocol: marshal handover payload: %w", err)
	}
	// Encode appends a newline; the payload ends at the closing brace.
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// NewCoordHandover returns a handover of network from s's key to newKey,
// issued at the given time and signed by s.
func NewCoordHandover(s HandoverSigner, network, newKey string, issued time.Time) (*CoordHandover, error) {
	h := &CoordHandover{
		Network:   network,
		OldKeyB64: s.PubKeyBase64(),
		NewKeyB64: newKey,
		Issued:    issued.UTC().Format(time.RFC3339),
	}
	if err := h.checkFields(); err != nil {
		return nil, err
	}
	payload, err := h.SignedPayload()
	if err != nil {
		return nil, err
	}
	sig, err := s.SignRaw(payload)
	if err != nil {
		return nil, fmt.Errorf("protocol: sign handover: %w", err)
	}
	h.SignatureB64 = base64.StdEncoding.EncodeToString(sig)
	return h, nil
}

// Verify checks that both keys are valid and different, issued is an
// RFC 3339 time and the signature verifies under old_key. It does not check
// that old_key is anyone's coordinator key; the reader decides that.
func (h *CoordHandover) Verify() error {
	if err := h.checkFields(); err != nil {
		return err
	}
	old, _ := decodePubKey(h.OldKeyB64)
	sig, err := base64.StdEncoding.DecodeString(h.SignatureB64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrHandoverSignature
	}
	payload, err := h.SignedPayload()
	if err != nil {
		return err
	}
	if !ed25519.Verify(old, payload, sig) {
		return ErrHandoverSignature
	}
	return nil
}

// checkFields validates everything but the signature.
func (h *CoordHandover) checkFields() error {
	if _, err := decodePubKey(h.OldKeyB64); err != nil {
		return fmt.Errorf("%w: old_key", ErrHandoverKey)
	}
	if _, err := decodePubKey(h.NewKeyB64); err != nil {
		return fmt.Errorf("%w: new_key", ErrHandoverKey)
	}
	if h.OldKeyB64 == h.NewKeyB64 {
		return fmt.Errorf("%w: old_key and new_key are the same", ErrHandoverKey)
	}
	if _, err := time.Parse(time.RFC3339, h.Issued); err != nil {
		return ErrHandoverIssued
	}
	return nil
}

// decodePubKey decodes a standard base64 ed25519 public key.
func decodePubKey(b64 string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("invalid ed25519 public key")
	}
	return ed25519.PublicKey(b), nil
}
