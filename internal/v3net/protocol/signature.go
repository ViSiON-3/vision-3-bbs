package protocol

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
)

// MessageSignatureDomain prefixes the canonical JSON that a message
// signature covers. It separates message signatures from NAL signatures
// (bare canonical JSON) and request signatures (method\npath\n...), so a
// signature made for one can never be replayed as another. The version
// suffix changes if the signed field set ever does.
const MessageSignatureDomain = "V3NET-MSG-SIG-v1\n"

// Message signature errors.
var (
	// ErrMessageUnsigned is returned by VerifySignature for a message that
	// carries neither origin_pubkey_b64 nor signature_b64.
	ErrMessageUnsigned = errors.New("protocol: message is unsigned")
	// ErrMessageOriginPubKey is returned when origin_pubkey_b64 is not a
	// valid base64 ed25519 public key.
	ErrMessageOriginPubKey = errors.New("protocol: invalid origin_pubkey_b64")
	// ErrMessageOriginMismatch is returned when origin_node is not the node
	// ID derived from origin_pubkey_b64.
	ErrMessageOriginMismatch = errors.New("protocol: origin_node does not match origin_pubkey_b64")
	// ErrMessageSignature is returned when signature_b64 is missing, not
	// valid base64, or does not verify over the canonical payload.
	ErrMessageSignature = errors.New("protocol: message signature verification failed")
)

// MessageSigner is the key a message is signed with. *keystore.Keystore
// satisfies it.
type MessageSigner interface {
	NodeID() string
	PubKeyBase64() string
	SignRaw(payload []byte) ([]byte, error)
}

// canonicalMessage is the signed form of a Message. Its fields are declared
// in alphabetical order of their JSON keys, so encoding/json emits sorted
// keys, the same approach the NAL signature uses (internal/v3net/nal).
//
// Only fields the author sets and nobody may change are covered. Kludges are
// left out: they are free-form metadata a relay may annotate (including the
// v3net_truncated marker), and the hub's own row id and received_at
// timestamp never appear on the wire message at all.
type canonicalMessage struct {
	AreaTag         string  `json:"area_tag"`
	Attributes      uint32  `json:"attributes"`
	Body            string  `json:"body"`
	DateUTC         string  `json:"date_utc"`
	From            string  `json:"from"`
	MsgUUID         string  `json:"msg_uuid"`
	Network         string  `json:"network"`
	Origin          string  `json:"origin"`
	OriginBoard     string  `json:"origin_board"`
	OriginNode      string  `json:"origin_node"`
	OriginPubKeyB64 string  `json:"origin_pubkey_b64"`
	ParentUUID      *string `json:"parent_uuid"`
	Subject         string  `json:"subject"`
	Tearline        string  `json:"tearline"`
	ThreadUUID      string  `json:"thread_uuid"`
	To              string  `json:"to"`
	V3Net           string  `json:"v3net"`
}

// SignedPayload returns the exact bytes a message signature covers:
// MessageSignatureDomain followed by the compact JSON of the immutable
// fields with keys in sorted order and HTML escaping off. signature_b64 is
// never part of it.
func (m *Message) SignedPayload() ([]byte, error) {
	c := canonicalMessage{
		AreaTag:         m.AreaTag,
		Attributes:      m.Attributes,
		Body:            m.Body,
		DateUTC:         m.DateUTC,
		From:            m.From,
		MsgUUID:         m.MsgUUID,
		Network:         m.Network,
		Origin:          m.Origin,
		OriginBoard:     m.OriginBoard,
		OriginNode:      m.OriginNode,
		OriginPubKeyB64: m.OriginPubKeyB64,
		ParentUUID:      m.ParentUUID,
		Subject:         m.Subject,
		Tearline:        m.Tearline,
		ThreadUUID:      m.ThreadUUID,
		To:              m.To,
		V3Net:           m.V3Net,
	}
	var buf bytes.Buffer
	buf.WriteString(MessageSignatureDomain)
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(c); err != nil {
		return nil, fmt.Errorf("protocol: marshal signed payload: %w", err)
	}
	// Encode appends a newline; the payload ends at the closing brace.
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// IsSigned reports whether the message claims an author signature, that is
// it carries origin_pubkey_b64 or signature_b64. A message that claims one
// must pass VerifySignature; it is never treated as unsigned.
func (m *Message) IsSigned() bool {
	return m.OriginPubKeyB64 != "" || m.SignatureB64 != ""
}

// NormalizeUTF8 replaces each byte of invalid UTF-8 in the message's signed
// text fields with U+FFFD. encoding/json makes the same replacement when the
// message is sent, so the hub and every leaf decode exactly these strings;
// signing the raw bytes instead (raw CP437 text, say) would sign something
// no reader ever sees. Valid text is left as it is.
func (m *Message) NormalizeUTF8() {
	for _, f := range []*string{
		&m.V3Net, &m.Network, &m.AreaTag, &m.MsgUUID, &m.ThreadUUID, &m.OriginNode,
		&m.OriginBoard, &m.From, &m.To, &m.Subject, &m.DateUTC, &m.Body, &m.Tearline, &m.Origin,
	} {
		*f = wireUTF8(*f)
	}
	if m.ParentUUID != nil {
		p := wireUTF8(*m.ParentUUID)
		m.ParentUUID = &p
	}
}

// wireUTF8 returns s with every byte that is not part of valid UTF-8
// replaced by U+FFFD, one per byte, matching encoding/json's encoder (unlike
// strings.ToValidUTF8, which collapses a run of bad bytes into one).
func wireUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteRune(utf8.RuneError)
		} else {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// Sign sets OriginPubKeyB64 to the signer's key and SignatureB64 to its
// signature over SignedPayload. It first applies NormalizeUTF8, so the
// signature covers the text as it travels. The message must otherwise be
// final: its origin_node must be the signer's node ID and its body must be
// within MaxBodyBytes after normalising (call NormalizeUTF8, then Truncate,
// first), since any later change breaks the signature.
func (m *Message) Sign(s MessageSigner) error {
	m.NormalizeUTF8()
	if m.OriginNode != s.NodeID() {
		return fmt.Errorf("protocol: sign: origin_node %q is not the signing node %q", m.OriginNode, s.NodeID())
	}
	if m.NeedsTruncation() {
		return fmt.Errorf("protocol: sign: body exceeds %d bytes; truncate before signing", MaxBodyBytes)
	}
	m.OriginPubKeyB64 = s.PubKeyBase64()
	m.SignatureB64 = ""
	payload, err := m.SignedPayload()
	if err != nil {
		return err
	}
	sig, err := s.SignRaw(payload)
	if err != nil {
		return fmt.Errorf("protocol: sign: %w", err)
	}
	m.SignatureB64 = base64.StdEncoding.EncodeToString(sig)
	return nil
}

// VerifySignature checks the author signature: origin_pubkey_b64 is a valid
// ed25519 key, origin_node is the node ID derived from it, and signature_b64
// verifies over SignedPayload. It returns ErrMessageUnsigned for an unsigned
// message; callers decide whether that is acceptable. It does not check that
// the key belongs to any particular subscriber.
func (m *Message) VerifySignature() error {
	if !m.IsSigned() {
		return ErrMessageUnsigned
	}
	pub, err := m.OriginPubKey()
	if err != nil {
		return err
	}
	if keystore.NodeIDFromPubKey(pub) != m.OriginNode {
		return ErrMessageOriginMismatch
	}
	sig, err := base64.StdEncoding.DecodeString(m.SignatureB64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrMessageSignature
	}
	payload, err := m.SignedPayload()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, payload, sig) {
		return ErrMessageSignature
	}
	return nil
}

// OriginPubKey decodes origin_pubkey_b64. It returns ErrMessageOriginPubKey
// if the field is empty, not base64, or not an ed25519 public key.
func (m *Message) OriginPubKey() (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(m.OriginPubKeyB64)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, ErrMessageOriginPubKey
	}
	return ed25519.PublicKey(b), nil
}
