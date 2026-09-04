package menu

import "strings"

// Optional-group delimiters for menu prompts. A group is dropped in full when
// every placeholder inside it resolves to an empty value, so a template can
// carry decoration — parentheses, a label, a separator — that disappears along
// with the value rather than being left stranded.
//
//	"|10|UH |12from |GL |{(|04|UN|12)|}|07"
//
// renders as "Felonius from ViSiON/3 (SysOp)" when the user has a note, and
// "Felonius from ViSiON/3 " when they do not — instead of a bare "()".
//
// "{" and "}" are not valid pipe-code characters, so these cannot collide with
// |00-|15 colors or the |CL / |CR / |PP style commands.
const (
	optGroupOpen  = "|{"
	optGroupClose = "|}"
)

// expandOptionalGroups resolves |{...|} groups in s against the placeholder
// map, returning the prompt with each group either unwrapped or removed.
//
// A group is removed when it contains at least one known placeholder and all
// of them are empty. A group containing no known placeholder is unwrapped and
// kept — there is nothing conditional about it, so dropping it would silently
// eat literal text. Groups do not nest; an unmatched opener is left as-is so a
// malformed template degrades to visible text rather than swallowing the rest
// of the prompt.
func expandOptionalGroups(s string, placeholders map[string]string) string {
	if !strings.Contains(s, optGroupOpen) {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))

	for {
		start := strings.Index(s, optGroupOpen)
		if start < 0 {
			b.WriteString(s)
			break
		}
		end := strings.Index(s[start+len(optGroupOpen):], optGroupClose)
		if end < 0 {
			// Unmatched opener: emit the rest verbatim.
			b.WriteString(s)
			break
		}

		inner := s[start+len(optGroupOpen) : start+len(optGroupOpen)+end]
		b.WriteString(s[:start])
		if !groupIsEmpty(inner, placeholders) {
			b.WriteString(inner)
		}
		s = s[start+len(optGroupOpen)+end+len(optGroupClose):]
	}

	return b.String()
}

// groupIsEmpty reports whether inner holds at least one known placeholder and
// every one of them resolves to blank. Whitespace counts as blank so a value
// padded to a fixed width does not keep a group alive.
func groupIsEmpty(inner string, placeholders map[string]string) bool {
	found := false
	for key, val := range placeholders {
		if !strings.Contains(inner, key) {
			continue
		}
		// A longer key that also contains this one (|CAN vs |CA) means the
		// match may belong to the longer placeholder; that is harmless here,
		// since both must be empty for the group to drop.
		found = true
		if strings.TrimSpace(val) != "" {
			return false
		}
	}
	return found
}
