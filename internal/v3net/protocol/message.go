// Package protocol defines the V3Net wire types, constants, and validation.
package protocol

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ProtocolVersion is the current V3Net protocol version.
const ProtocolVersion = "1.0"

// MaxBodyBytes is the maximum allowed message body size.
const MaxBodyBytes = 32768

// Length limits for the text fields that describe where a message came from,
// in characters (a byte that is not valid UTF-8 counts as one). The tearline
// and origin each become a line of the stored body, so they are held to the 79
// columns of an FTN line; the board name matches from and to.
const (
	MaxOriginBoardLen = 64
	MaxTearlineLen    = 79
	MaxOriginLen      = 79
)

// IdentityOrigin is the value of the identity query parameter a leaf sends
// on every message poll to say it identifies a message by network, origin
// node and UUID. Without it the hub serves only the earliest message with
// each UUID, for leaves that identify a message by UUID alone.
const IdentityOrigin = "origin"

// PollFormatPage is the value of the format query parameter a leaf sends on
// a message poll to have the page returned as a MessagePage, with has_more
// in the body, which the hub's response signature covers. Without it the
// hub returns a bare JSON array and the X-V3Net-Has-More header, which no
// signature covers; a hub that predates the parameter does that too.
const PollFormatPage = "page"

// MessagePage is a page of polled messages in the PollFormatPage form.
type MessagePage struct {
	Messages []Message `json:"messages"`
	// HasMore means another page follows the last message in Messages.
	HasMore bool `json:"has_more"`
}

// Message represents a V3Net networked message in wire format.
type Message struct {
	V3Net       string         `json:"v3net"`
	Network     string         `json:"network"`
	AreaTag     string         `json:"area_tag"`
	MsgUUID     string         `json:"msg_uuid"`
	ThreadUUID  string         `json:"thread_uuid"`
	ParentUUID  *string        `json:"parent_uuid"`
	OriginNode  string         `json:"origin_node"`
	OriginBoard string         `json:"origin_board"`
	From        string         `json:"from"`
	To          string         `json:"to"`
	Subject     string         `json:"subject"`
	DateUTC     string         `json:"date_utc"`
	Body        string         `json:"body"`
	Tearline    string         `json:"tearline,omitempty"` // Software tearline (e.g. "--- ViSiON/3 v0.8.0/Linux")
	Origin      string         `json:"origin,omitempty"`   // User-defined origin line (e.g. "My Cool BBS - bbs.example.com")
	Attributes  uint32         `json:"attributes"`
	Kludges     map[string]any `json:"kludges"`
	// OriginPubKeyB64 is the author node's ed25519 public key (base64). Its
	// derived node ID must equal OriginNode. Empty on legacy unsigned messages.
	OriginPubKeyB64 string `json:"origin_pubkey_b64,omitempty"`
	// SignatureB64 is the author node's ed25519 signature (base64) over
	// SignedPayload. Empty on legacy unsigned messages. See Sign and
	// VerifySignature.
	SignatureB64 string `json:"signature_b64,omitempty"`
}

var (
	networkRe = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
	uuidRe    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

// Validate checks all required fields and returns the first validation error found.
func (m *Message) Validate() error {
	if m.V3Net != ProtocolVersion {
		return fmt.Errorf("unsupported protocol version: %q", m.V3Net)
	}
	if !networkRe.MatchString(m.Network) {
		return fmt.Errorf("invalid network name: %q", m.Network)
	}
	if !AreaTagRegexp.MatchString(m.AreaTag) {
		return fmt.Errorf("invalid area_tag: %q", m.AreaTag)
	}
	if !uuidRe.MatchString(m.MsgUUID) {
		return fmt.Errorf("invalid msg_uuid: %q", m.MsgUUID)
	}
	if !uuidRe.MatchString(m.ThreadUUID) {
		return fmt.Errorf("invalid thread_uuid: %q", m.ThreadUUID)
	}
	if m.ParentUUID != nil && !uuidRe.MatchString(*m.ParentUUID) {
		return fmt.Errorf("invalid parent_uuid: %q", *m.ParentUUID)
	}
	if m.OriginNode == "" {
		return fmt.Errorf("origin_node must not be empty")
	}
	if m.OriginBoard == "" {
		return fmt.Errorf("origin_board must not be empty")
	}
	if _, err := time.Parse(time.RFC3339, m.DateUTC); err != nil {
		return fmt.Errorf("invalid date_utc: %w", err)
	}
	if err := validatePrintableASCII("from", m.From, 1, 64); err != nil {
		return err
	}
	if err := validatePrintableASCII("to", m.To, 1, 64); err != nil {
		return err
	}
	if err := validateStringLength("subject", m.Subject, 1, 128); err != nil {
		return err
	}
	for _, r := range m.Subject {
		if unicode.IsControl(r) {
			return fmt.Errorf("subject contains control character: %U", r)
		}
	}
	for _, f := range []struct {
		name, value string
		max         int
	}{
		{"origin_board", m.OriginBoard, MaxOriginBoardLen},
		{"tearline", m.Tearline, MaxTearlineLen},
		{"origin", m.Origin, MaxOriginLen},
	} {
		if n := utf8.RuneCountInString(f.value); n > f.max {
			return fmt.Errorf("%s must be at most %d characters, got %d", f.name, f.max, n)
		}
		for _, r := range f.value {
			if unicode.IsControl(r) {
				return fmt.Errorf("%s contains control character: %U", f.name, r)
			}
		}
	}
	if len(m.Body) == 0 {
		return fmt.Errorf("body must not be empty")
	}
	return nil
}

// CleanSubject returns subject with each control character turned into a space,
// which is what Validate requires of it.
func CleanSubject(subject string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, subject)
}

// TrimField cuts s to at most max characters, at a character boundary, for a
// field with a length limit such as origin. A byte that is not valid UTF-8
// counts as one character and is kept as it is. A leaf trims these fields on
// messages it polls, and a node on posts it builds, so that a long value costs
// its tail and not the whole message.
func TrimField(s string, max int) string {
	i := 0
	for n := 0; n < max && i < len(s); n++ {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i]
}

// IsTruncated returns true if the message was truncated by Truncate().
func (m *Message) IsTruncated() bool {
	if m.Kludges == nil {
		return false
	}
	v, ok := m.Kludges["v3net_truncated"]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

// NeedsTruncation returns true if the body exceeds MaxBodyBytes.
func (m *Message) NeedsTruncation() bool {
	return len(m.Body) > MaxBodyBytes
}

// Truncate truncates the body to MaxBodyBytes at a valid UTF-8 rune boundary
// and sets the v3net_truncated kludge. The resulting body may be shorter than
// MaxBodyBytes when the boundary falls inside a multi-byte rune.
func (m *Message) Truncate() {
	if len(m.Body) <= MaxBodyBytes {
		return
	}
	// Find the largest byte index <= MaxBodyBytes that falls on a rune boundary.
	cut := 0
	for i := range m.Body {
		if i > MaxBodyBytes {
			break
		}
		cut = i
	}
	m.Body = m.Body[:cut]
	if m.Kludges == nil {
		m.Kludges = make(map[string]any)
	}
	m.Kludges["v3net_truncated"] = true
}

func validatePrintableASCII(field, value string, minLen, maxLen int) error {
	n := utf8.RuneCountInString(value)
	if n < minLen || n > maxLen {
		return fmt.Errorf("%s must be %d–%d characters, got %d", field, minLen, maxLen, n)
	}
	for _, r := range value {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) {
			return fmt.Errorf("%s contains non-printable or non-ASCII character: %U", field, r)
		}
	}
	return nil
}

func validateStringLength(field, value string, minLen, maxLen int) error {
	n := utf8.RuneCountInString(value)
	if n < minLen || n > maxLen {
		return fmt.Errorf("%s must be %d–%d characters, got %d", field, minLen, maxLen, n)
	}
	return nil
}
