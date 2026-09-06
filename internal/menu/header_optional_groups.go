package menu

import (
	"bytes"
	"regexp"
)

// Optional groups in message header templates.
//
// Same delimiters as menu prompts (see placeholder_groups.go) so template
// authors learn one syntax, but the behaviour has to differ. A prompt is
// flowing text, so dropping a group there removes it and the rest of the line
// closes up. A header is not flowing: it draws boxes, pads values to fixed
// columns, and several templates position the cursor absolutely. Removing
// characters would pull a border out of alignment.
//
// So here a dropped group is replaced by spaces occupying the width it would
// have rendered at. Nothing shifts; the decoration simply goes blank.
//
//	"|{ Note: @U#####@|}"   ->  "               "   when the note is empty
//	                        ->  " Note: sysop   "   when it is not
const (
	hdrGroupOpen  = "|{"
	hdrGroupClose = "|}"
)

// ansiEscapeRe matches the CSI sequences used for colour and positioning, which
// occupy no columns and so contribute nothing to a group's rendered width.
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// expandHeaderOptionalGroups resolves |{...|} groups in a header template
// against the substitution map, before placeholders themselves are expanded.
//
// A group is blanked when it contains at least one known code and every one of
// them is empty. A group with no known code is left in place — there is nothing
// conditional about it, and dropping it would silently eat part of the layout.
// An unmatched opener is left alone so a malformed template degrades to visible
// text rather than blanking the rest of the header.
func expandHeaderOptionalGroups(template []byte, substitutions map[byte]string) []byte {
	if !bytes.Contains(template, []byte(hdrGroupOpen)) {
		return template
	}

	var out bytes.Buffer
	out.Grow(len(template))
	rest := template

	for {
		start := bytes.Index(rest, []byte(hdrGroupOpen))
		if start < 0 {
			out.Write(rest)
			break
		}
		after := rest[start+len(hdrGroupOpen):]
		end := bytes.Index(after, []byte(hdrGroupClose))
		if end < 0 {
			out.Write(rest) // unmatched opener: emit verbatim
			break
		}

		inner := after[:end]
		out.Write(rest[:start])
		if groupValuesAllEmpty(inner, substitutions) {
			out.Write(bytes.Repeat([]byte{' '}, renderedGroupWidth(inner)))
		} else {
			out.Write(inner)
		}
		rest = after[end+len(hdrGroupClose):]
	}

	return out.Bytes()
}

// groupValuesAllEmpty reports whether inner holds at least one placeholder this
// substitution map knows about and every one of them resolves to blank.
// Whitespace counts as blank, so a value padded to a fixed width does not keep
// a group alive.
//
// A placeholder the map does not know about keeps the group, rather than being
// ignored. Not every recognised code draws from the substitution map: @G@ is
// gap fill, expanded later against the terminal width, so skipping it would let
// a group blank away a gap fill that had nothing to do with the empty value.
// More generally, "this group holds something we cannot evaluate" is not a
// basis for deciding it renders as nothing.
func groupValuesAllEmpty(inner []byte, substitutions map[byte]string) bool {
	matches := parsePlaceholders(inner)
	found := false
	for _, m := range matches {
		if len(m.Code) == 0 {
			continue
		}
		value, known := substitutions[m.Code[0]]
		if !known {
			return false
		}
		found = true
		if len(bytes.TrimSpace([]byte(value))) > 0 {
			return false
		}
	}
	return found
}

// renderedGroupWidth is the column count a group occupies once its (empty)
// placeholders are substituted: literal text plus the reserved width of any
// padded placeholder. A placeholder with no explicit width collapses to
// nothing, since its value is empty.
func renderedGroupWidth(inner []byte) int {
	width := 0
	consumed := 0

	for _, m := range parsePlaceholders(inner) {
		// Literal text before this placeholder, minus zero-width escapes.
		width += visibleLen(inner[consumed:m.StartPos])
		if m.Width > 0 {
			width += m.Width
		}
		consumed = m.EndPos
	}
	width += visibleLen(inner[consumed:])
	return width
}

// visibleLen counts the columns a byte slice occupies, ignoring ANSI escapes.
func visibleLen(b []byte) int {
	return len(ansiEscapeRe.ReplaceAll(b, nil))
}
