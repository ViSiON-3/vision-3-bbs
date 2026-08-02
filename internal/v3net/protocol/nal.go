package protocol

import (
	"fmt"
	"regexp"
	"strings"
)

// AreaTagRegexp validates area tags: {prefix}.{name} where prefix is 1-8
// lowercase alphanumeric chars and name is 1-24 lowercase alphanumeric/dash chars.
var AreaTagRegexp = regexp.MustCompile(`^[a-z0-9]{1,8}\.[a-z0-9-]{1,24}$`)

// NAL is the Network Area List — the signed canonical list of areas
// that a network carries. The coordinator hub owns and signs it.
type NAL struct {
	V3NetNAL       string `json:"v3net_nal"`
	Network        string `json:"network"`
	CoordNodeID    string `json:"coordinator_node_id"`
	CoordPubKeyB64 string `json:"coordinator_pubkey_b64"`
	Updated        string `json:"updated"`       // YYYY-MM-DD UTC
	SignatureB64   string `json:"signature_b64"` // ed25519 over canonical payload
	Areas          []Area `json:"areas"`
}

// Area defines a single message area within a network.
type Area struct {
	Tag              string     `json:"tag"`
	Name             string     `json:"name"`
	Description      string     `json:"description"`
	Language         string     `json:"language"`
	Moderated        bool       `json:"moderated"`
	ManagerNodeID    string     `json:"manager_node_id"`
	ManagerPubKeyB64 string     `json:"manager_pubkey_b64"`
	Access           AreaAccess `json:"access"`
	Policy           AreaPolicy `json:"policy"`
}

// AreaAccess controls which leaf nodes may subscribe to an area.
type AreaAccess struct {
	// Mode: "open", "approval", or "closed".
	Mode string `json:"mode"`

	// AllowList is the set of node_ids explicitly permitted.
	AllowList []string `json:"allow_list,omitempty"`

	// DenyList is always enforced regardless of Mode.
	DenyList []string `json:"deny_list,omitempty"`
}

// AreaPolicy defines per-area content policies.
type AreaPolicy struct {
	MaxBodyBytes    int  `json:"max_body_bytes"`
	AllowANSI       bool `json:"allow_ansi"`
	RequireTearline bool `json:"require_tearline"`
}

// Access mode constants.
const (
	AccessModeOpen     = "open"
	AccessModeApproval = "approval"
	AccessModeClosed   = "closed"
)

// ValidateAreaTag checks that a tag matches the required format. Errors are
// worded for sysops typing tags into config forms, not for developers: the
// offending value is not echoed (it is visible in the form field) and every
// message fits a 59-character status row (the narrowest screen showing them).
func ValidateAreaTag(tag string) error {
	if AreaTagRegexp.MatchString(tag) {
		return nil
	}
	prefix, name, found := strings.Cut(tag, ".")
	switch {
	case !found:
		return fmt.Errorf(`area tag missing a period - tags look like "fel.general"`)
	case len(prefix) > 8 && prefixCharsValid(prefix):
		return fmt.Errorf("area tag prefix too long - max 8 letters/digits")
	case len(name) > 24 && nameCharsValid(name):
		return fmt.Errorf("area tag name too long - max 24 letters/digits/hyphens")
	default:
		return fmt.Errorf(`area tag must be lowercase "prefix.name" like "fel.general"`)
	}
}

// prefixCharsValid reports whether every prefix character is allowed
// (lowercase letters and digits), regardless of length.
func prefixCharsValid(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// nameCharsValid reports whether every name character is allowed
// (lowercase letters, digits, hyphens), regardless of length.
func nameCharsValid(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

// ValidateAccessMode checks that a mode string is one of the valid access modes.
func ValidateAccessMode(mode string) error {
	switch mode {
	case AccessModeOpen, AccessModeApproval, AccessModeClosed:
		return nil
	default:
		return fmt.Errorf("invalid access mode %q: must be open, approval, or closed", mode)
	}
}

// FindArea returns the area with the given tag, or nil if not found.
func (n *NAL) FindArea(tag string) *Area {
	for i := range n.Areas {
		if n.Areas[i].Tag == tag {
			return &n.Areas[i]
		}
	}
	return nil
}
