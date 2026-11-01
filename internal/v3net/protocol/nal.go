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

	// Handovers are the coordinator handovers the hub holds for the
	// network, oldest first, so a leaf pinned to an earlier coordinator key
	// can follow them to the key that signed this NAL. The hub attaches
	// them when it serves the NAL; each carries its own signature, so they
	// are left out of the NAL's canonical form, and older leaves ignore the
	// field. Empty when the network never changed coordinator key.
	Handovers []CoordHandover `json:"coordinator_handovers,omitempty"`
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

	// Added is when the area first appeared in this network's NAL, as an
	// RFC 3339 UTC time. The hub stamps it when it stores a NAL (see
	// hub.NALStore.Put), so it is not covered by the coordinator's signature
	// and is left out of the canonical form: signing it would make every
	// older leaf, which drops the field, fail to verify the NAL. Empty for
	// areas that predate the field.
	Added string `json:"added,omitempty"`
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

// ValidateAreaText checks the text a node proposes for an area. Every board that
// lists the area shows it, so it cannot hold control characters or pipes, which
// BBS screens read as codes. Boards still clean what they display, because a NAL
// can hold areas created before this check.
func ValidateAreaText(field, value string) error {
	if hasScreenCodes(value) {
		return fmt.Errorf("area %s cannot contain pipes or control characters", field)
	}
	return nil
}

// ValidateNALAreas checks the areas of a NAL a coordinator posts: every tag
// must be well formed, and no area's name, description or language may hold
// pipes or control characters (see ValidateAreaText). An area that stored, the
// NAL the hub holds now, already carries with the same tag and the same text is
// let through as it is, so text from before these checks does not lock a
// coordinator out of every other change. stored may be nil.
func ValidateNALAreas(areas []Area, stored *NAL) error {
	held := map[string]Area{}
	if stored != nil {
		for _, a := range stored.Areas {
			held[a.Tag] = a
		}
	}
	for i, a := range areas {
		if old, ok := held[a.Tag]; ok && old.Name == a.Name && old.Description == a.Description && old.Language == a.Language {
			continue
		}
		if !AreaTagRegexp.MatchString(a.Tag) {
			return fmt.Errorf("area %d: invalid tag", i+1)
		}
		for _, f := range []struct{ field, value string }{
			{"name", a.Name}, {"description", a.Description}, {"language", a.Language},
		} {
			if err := ValidateAreaText(f.field, f.value); err != nil {
				return fmt.Errorf("area %s: %w", a.Tag, err)
			}
		}
	}
	return nil
}

// hasScreenCodes reports whether s holds a pipe, which BBS screens read as the
// start of a code, or a control character (C0, DEL or C1).
func hasScreenCodes(s string) bool {
	for _, r := range s {
		if r == '|' || r < 0x20 || (r >= 0x7F && r <= 0x9F) {
			return true
		}
	}
	return false
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
