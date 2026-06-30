package config

import "strings"

// NormalizeQWKID reduces a string to a valid QWK BBS ID: ASCII letters and
// digits only, upper-cased, at most 8 characters. It returns "" when nothing
// valid remains; callers decide the fallback (e.g. derive from another field or
// use "BBS").
func NormalizeQWKID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			if b.Len() >= 8 {
				break
			}
		}
	}
	return strings.ToUpper(b.String())
}
