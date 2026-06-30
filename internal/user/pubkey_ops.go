package user

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

// PublicKeyInfo is a display-friendly summary of one registered WFC public key.
type PublicKeyInfo struct {
	Type        string // e.g. "ssh-ed25519"
	Comment     string // trailing comment on the authorized_keys line, if any
	Fingerprint string // ssh.FingerprintSHA256, e.g. "SHA256:abc…"
}

// NormalizeAuthorizedKey parses one OpenSSH authorized_keys line and returns
// its canonical stored form (key type + base64 key + optional comment), a
// display summary, and any error. Malformed or empty input is rejected.
func NormalizeAuthorizedKey(line string) (string, PublicKeyInfo, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", PublicKeyInfo{}, fmt.Errorf("empty public key")
	}
	pub, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return "", PublicKeyInfo{}, fmt.Errorf("not a valid OpenSSH public key: %w", err)
	}
	normalized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if comment != "" {
		normalized += " " + comment
	}
	return normalized, PublicKeyInfo{
		Type:        pub.Type(),
		Comment:     comment,
		Fingerprint: ssh.FingerprintSHA256(pub),
	}, nil
}

// keyMarshal returns the wire bytes of a stored line's key, for dedup/matching.
func keyMarshal(line string) ([]byte, bool) {
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return nil, false
	}
	return pub.Marshal(), true
}

// AddPublicKey validates line, dedupes against u.PublicKeys by wire bytes
// (ignoring comment, matching how auth compares keys), appends the normalized
// form, and returns the new key's info.
func (u *User) AddPublicKey(line string) (PublicKeyInfo, error) {
	normalized, info, err := NormalizeAuthorizedKey(line)
	if err != nil {
		return PublicKeyInfo{}, err
	}
	newBytes, _ := keyMarshal(normalized)
	for _, existing := range u.PublicKeys {
		if b, ok := keyMarshal(existing); ok && string(b) == string(newBytes) {
			return PublicKeyInfo{}, fmt.Errorf("key already registered (%s)", info.Fingerprint)
		}
	}
	u.PublicKeys = append(u.PublicKeys, normalized)
	return info, nil
}

// RemovePublicKey removes the key identified by ref: a full SHA256 fingerprint,
// a unique fingerprint prefix, or a 1-based index as shown by ListPublicKeys.
func (u *User) RemovePublicKey(ref string) (PublicKeyInfo, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return PublicKeyInfo{}, fmt.Errorf("no key reference given")
	}
	if idx, err := strconv.Atoi(ref); err == nil {
		// Count only parseable (visible) keys so idx matches ListPublicKeys display.
		visible := 0
		for i, line := range u.PublicKeys {
			_, info, err2 := NormalizeAuthorizedKey(line)
			if err2 != nil {
				continue
			}
			visible++
			if visible == idx {
				u.PublicKeys = append(u.PublicKeys[:i], u.PublicKeys[i+1:]...)
				return info, nil
			}
		}
		if idx < 1 {
			return PublicKeyInfo{}, fmt.Errorf("key index %d out of range (have %d)", idx, visible)
		}
		return PublicKeyInfo{}, fmt.Errorf("key index %d out of range (have %d)", idx, visible)
	}
	matchIdx := -1
	var matchInfo PublicKeyInfo
	for i, line := range u.PublicKeys {
		_, info, err := NormalizeAuthorizedKey(line)
		if err != nil {
			continue
		}
		if info.Fingerprint == ref || strings.HasPrefix(info.Fingerprint, ref) {
			if matchIdx != -1 {
				return PublicKeyInfo{}, fmt.Errorf("ambiguous key reference %q", ref)
			}
			matchIdx, matchInfo = i, info
		}
	}
	if matchIdx == -1 {
		return PublicKeyInfo{}, fmt.Errorf("no key matching %q", ref)
	}
	u.PublicKeys = append(u.PublicKeys[:matchIdx], u.PublicKeys[matchIdx+1:]...)
	return matchInfo, nil
}

// ListPublicKeys returns display summaries for u.PublicKeys and the count of
// stored lines that failed to parse (so a corrupt entry is surfaced).
func (u *User) ListPublicKeys() ([]PublicKeyInfo, int) {
	var out []PublicKeyInfo
	unparseable := 0
	for _, line := range u.PublicKeys {
		_, info, err := NormalizeAuthorizedKey(line)
		if err != nil {
			unparseable++
			continue
		}
		out = append(out, info)
	}
	return out, unparseable
}
