// Package protocol defines the V3Net wire types, constants, and validation.
package protocol

import (
	"fmt"
	"regexp"
	"time"
	"unicode"
	"unicode/utf8"
)

// ProtocolVersion is the current V3Net protocol version.
const ProtocolVersion = "1.0"

// MaxBodyBytes is the maximum allowed message body size.
const MaxBodyBytes = 32768

// Message represents a V3Net networked message in wire format.
type Message struct {
	V3Net       string         `json:"v3net"`
	Network     string         `json:"network"`
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
	Tearline    string         `json:"tearline,omitempty"`
	Attributes  uint32         `json:"attributes"`
	Kludges     map[string]any `json:"kludges"`
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
	if !uuidRe.MatchString(m.MsgUUID) {
		return fmt.Errorf("invalid msg_uuid: %q", m.MsgUUID)
	}
	if !uuidRe.MatchString(m.ThreadUUID) {
		return fmt.Errorf("invalid thread_uuid: %q", m.ThreadUUID)
	}
	if m.ParentUUID != nil && !uuidRe.MatchString(*m.ParentUUID) {
		return fmt.Errorf("invalid parent_uuid: %q", *m.ParentUUID)
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
	if len(m.Body) == 0 {
		return fmt.Errorf("body must not be empty")
	}
	return nil
}

// IsTruncated returns true if the body exceeds MaxBodyBytes.
func (m *Message) IsTruncated() bool {
	return len(m.Body) > MaxBodyBytes
}

// Truncate truncates the body to MaxBodyBytes at a valid UTF-8 rune boundary
// and sets the v3net_truncated kludge.
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
