package protocol

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
)

// Signed network chat (#1008). A hub relays chat lines, joins and private
// messages, and until now told boards who sent each one: so a dishonest hub
// could seat a board of its own invention in a room. A board now signs what
// its callers send, and the hub passes the signed object on with the
// message, so a board holding a board roster (protocol.CentralRoster) can
// show chat only from boards on it, checked against the key the roster
// names, as it does for Central listings and alerts.
//
// Room leaves and topic changes stay as the hub reports them: a hub makes a
// leave itself when a board's stream drops, and a topic names a handle the
// hub has already seen join.

// ChatSignedVersion is the value of ChatSigned.V3NetChat this code writes.
const ChatSignedVersion = "1.0"

// ChatSignedSignatureDomain prefixes the canonical JSON a chat signature
// covers.
const ChatSignedSignatureDomain = "V3NET-CHAT-v1\n"

// Kinds of signed chat, ChatSigned.Kind.
const (
	ChatSignedLine    = "line"
	ChatSignedJoin    = "join"
	ChatSignedPrivate = "private"
)

// MaxChatSignedAge is how old a live signed line, join or private message
// may be before a board ignores it as replayed or held up. Room history,
// which is old by nature, is not held to it.
const MaxChatSignedAge = 10 * time.Minute

// ErrChatSignature is returned by ChatSigned.Verify when the signature does
// not decode or verify.
var ErrChatSignature = errors.New("protocol: chat signature verification failed")

// ChatSigned is one signed chat action: a room line, a room join, or a
// private message. The board writes and signs it; every field but
// SignatureB64 is covered. ID is random per action, so a board can drop a
// replay; IssuedAt is when it was sent (RFC 3339 UTC).
type ChatSigned struct {
	V3NetChat       string `json:"v3net_chat"`
	Kind            string `json:"kind"`
	ID              string `json:"id"`
	Network         string `json:"network"`
	NodeID          string `json:"node_id"`
	OriginPubKeyB64 string `json:"origin_pubkey_b64"`
	Handle          string `json:"handle"`
	Room            string `json:"room,omitempty"`
	ToHandle        string `json:"to_handle,omitempty"`
	ToNode          string `json:"to_node,omitempty"`
	Text            string `json:"text,omitempty"`
	IssuedAt        string `json:"issued_at"`
	SignatureB64    string `json:"signature_b64,omitempty"`
}

type canonicalChatSigned struct {
	Handle          string `json:"handle"`
	ID              string `json:"id"`
	IssuedAt        string `json:"issued_at"`
	Kind            string `json:"kind"`
	Network         string `json:"network"`
	NodeID          string `json:"node_id"`
	OriginPubKeyB64 string `json:"origin_pubkey_b64"`
	Room            string `json:"room"`
	Text            string `json:"text"`
	ToHandle        string `json:"to_handle"`
	ToNode          string `json:"to_node"`
	V3NetChat       string `json:"v3net_chat"`
}

// SignedPayload returns the exact bytes the signature covers.
func (c *ChatSigned) SignedPayload() ([]byte, error) {
	canon := canonicalChatSigned{
		Handle: c.Handle, ID: c.ID, IssuedAt: c.IssuedAt, Kind: c.Kind, Network: c.Network, NodeID: c.NodeID,
		OriginPubKeyB64: c.OriginPubKeyB64, Room: c.Room, Text: c.Text, ToHandle: c.ToHandle, ToNode: c.ToNode,
		V3NetChat: c.V3NetChat,
	}
	var buf bytes.Buffer
	buf.WriteString(ChatSignedSignatureDomain)
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(canon); err != nil {
		return nil, fmt.Errorf("protocol: marshal chat payload: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// NewChatSigned returns kind, by handle on network, issued now with a fresh
// ID, and signs it as s.
func NewChatSigned(s MessageSigner, kind, network, handle string, fill func(c *ChatSigned)) (*ChatSigned, error) {
	id, err := NewCentralAlertID()
	if err != nil {
		return nil, err
	}
	c := &ChatSigned{V3NetChat: ChatSignedVersion, Kind: kind, ID: id, Network: network, Handle: handle,
		IssuedAt: time.Now().UTC().Format(time.RFC3339)}
	if fill != nil {
		fill(c)
	}
	return c, c.Sign(s)
}

// Sign sets the signer's key and node ID and signs, after normalising the
// text to valid UTF-8.
func (c *ChatSigned) Sign(s MessageSigner) error {
	for _, f := range []*string{&c.Kind, &c.ID, &c.Network, &c.Handle, &c.Room, &c.ToHandle, &c.ToNode, &c.Text, &c.IssuedAt} {
		*f = wireUTF8(*f)
	}
	c.NodeID = s.NodeID()
	c.OriginPubKeyB64 = s.PubKeyBase64()
	c.SignatureB64 = ""
	payload, err := c.SignedPayload()
	if err != nil {
		return err
	}
	sig, err := s.SignRaw(payload)
	if err != nil {
		return fmt.Errorf("protocol: sign chat: %w", err)
	}
	c.SignatureB64 = base64.StdEncoding.EncodeToString(sig)
	return nil
}

// Verify checks the board's signature: origin_pubkey_b64 is an ed25519
// key, node_id derives from it, and signature_b64 verifies.
func (c *ChatSigned) Verify() error {
	pub, err := decodeEd25519Key(c.OriginPubKeyB64)
	if err != nil {
		return err
	}
	if keystore.NodeIDFromPubKey(pub) != c.NodeID {
		return ErrCentralNodeMismatch
	}
	sig, err := base64.StdEncoding.DecodeString(c.SignatureB64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrChatSignature
	}
	payload, err := c.SignedPayload()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, payload, sig) {
		return ErrChatSignature
	}
	return nil
}

// Validate checks the form for network: version, kind and the fields it
// needs, an ID, and an issue time.
func (c *ChatSigned) Validate(network string) error {
	if c.V3NetChat != ChatSignedVersion {
		return fmt.Errorf("unsupported v3net_chat version %q", c.V3NetChat)
	}
	if c.Network != network {
		return fmt.Errorf("network %q does not match %q", c.Network, network)
	}
	if !centralAlertIDRe.MatchString(c.ID) {
		return errors.New("id must be 16 to 32 base64url characters")
	}
	if strings.TrimSpace(c.Handle) == "" {
		return errors.New("handle is required")
	}
	if _, err := time.Parse(time.RFC3339, c.IssuedAt); err != nil {
		return errors.New("issued_at must be RFC 3339")
	}
	switch c.Kind {
	case ChatSignedLine, ChatSignedJoin:
		if c.Room == "" || c.ToHandle != "" || c.ToNode != "" {
			return fmt.Errorf("a %s names a room and no recipient", c.Kind)
		}
		if c.Kind == ChatSignedJoin && c.Text != "" {
			return errors.New("a join carries no text")
		}
	case ChatSignedPrivate:
		if c.ToHandle == "" || c.Room != "" {
			return errors.New("a private message names a recipient and no room")
		}
	default:
		return fmt.Errorf("unknown chat kind %q", c.Kind)
	}
	return nil
}

// Issued returns issued_at, or the zero time.
func (c *ChatSigned) Issued() time.Time {
	t, _ := time.Parse(time.RFC3339, c.IssuedAt)
	return t
}
