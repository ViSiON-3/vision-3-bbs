package menu

import (
	"sort"
	"strings"
)

// Optional-group delimiters for menu prompts. A group is dropped in full when
// every placeholder inside it resolves to an empty value, so a template can
// carry decoration — parentheses, a label, a separator — that disappears along
// with the value rather than being left stranded.
//
//	"|10|UH |12from |GL |{(|04|UN|12)|}|07"
//
// renders as "Felonius from ViSiON/3 (SysOp)" when the user has a note, and
// "Felonius from ViSiON/3 " when they do not — instead of a bare "()".
const (
	optGroupOpen  = "|{"
	optGroupClose = "|}"
)

// isCoordMarker reports whether s starts with a |{P} or |{O} login position
// marker. Those share the "|{" prefix with an optional group but are not one,
// so the scanner must step over them. The shape mirrors pipeSequence in
// conditional_ansi.go, which is the renderer's own definition.
func isCoordMarker(s string) bool {
	return len(s) >= 4 && s[0] == '|' && s[1] == '{' &&
		(s[2] == 'P' || s[2] == 'O') && s[3] == '}'
}

// findGroupOpen returns the index of the next real optional-group opener in s,
// skipping |{P} / |{O} coordinate markers, or -1 if there is none.
func findGroupOpen(s string) int {
	for i := 0; ; {
		j := strings.Index(s[i:], optGroupOpen)
		if j < 0 {
			return -1
		}
		at := i + j
		if isCoordMarker(s[at:]) {
			i = at + 4 // step over the whole marker
			continue
		}
		return at
	}
}

// expandOptionalGroups resolves |{...|} groups in s against the placeholder
// map, returning the prompt with each group either unwrapped or removed.
//
// A group is removed when it contains at least one known placeholder and all
// of them are empty. A group containing no known placeholder is unwrapped and
// kept — there is nothing conditional about it, so dropping it would silently
// eat literal text. Groups do not nest; an unmatched opener is left as-is so a
// malformed template degrades to visible text rather than swallowing the rest
// of the prompt.
//
// |{P} and |{O} login position markers are stepped over rather than treated as
// openers; without that, a prompt containing both a marker and a real group
// would pair the marker's "|{" with the group's "|}" and delete everything in
// between.
func expandOptionalGroups(s string, placeholders map[string]string) string {
	if !strings.Contains(s, optGroupOpen) {
		return s
	}

	keys := placeholderKeysLongestFirst(placeholders)

	var b strings.Builder
	b.Grow(len(s))

	for {
		start := findGroupOpen(s)
		if start < 0 {
			b.WriteString(s)
			break
		}
		rest := s[start+len(optGroupOpen):]
		end := strings.Index(rest, optGroupClose)
		if end < 0 {
			// Unmatched opener: emit the rest verbatim.
			b.WriteString(s)
			break
		}

		inner := rest[:end]
		b.WriteString(s[:start])
		if !groupIsEmpty(inner, placeholders, keys) {
			b.WriteString(inner)
		}
		s = rest[end+len(optGroupClose):]
	}

	return b.String()
}

// placeholderKeysLongestFirst orders placeholder names longest-first so that
// scanning matches |CAN before |CA, mirroring how substitution itself resolves
// prefix collisions.
func placeholderKeysLongestFirst(placeholders map[string]string) []string {
	keys := make([]string, 0, len(placeholders))
	for k := range placeholders {
		keys = append(keys, k)
	}
	sort.SliceStable(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	return keys
}

// groupIsEmpty reports whether inner holds at least one known placeholder and
// every one of them resolves to blank. Whitespace counts as blank so a value
// padded to a fixed width does not keep a group alive.
//
// Matching walks the string longest-key-first and consumes each match, so a
// group containing |CAN is judged on |CAN alone and not also on the |CA that
// shares its prefix.
func groupIsEmpty(inner string, placeholders map[string]string, keys []string) bool {
	found := false
	for i := 0; i < len(inner); {
		matched := false
		for _, k := range keys {
			if strings.HasPrefix(inner[i:], k) {
				found = true
				if strings.TrimSpace(placeholders[k]) != "" {
					return false
				}
				i += len(k)
				matched = true
				break
			}
		}
		if !matched {
			i++
		}
	}
	return found
}
