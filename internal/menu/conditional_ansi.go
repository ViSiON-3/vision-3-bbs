package menu

import (
	"bytes"
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Conditional display regions in menu ANSI art: {{acs}}text{{/}} shows text
// only to users passing the ACS expression; others see equal-width blank
// space so the art's geometry is preserved. See
// docs/sysop/menus/menu-system.md ("Conditional Regions").

const (
	condOpen  = "{{"
	condClose = "{{/}}"
)

// knownLiteralTokens are {{...}} tokens substituted elsewhere in the display
// pipeline; the conditional-region parser must leave them untouched.
var knownLiteralTokens = map[string]bool{
	"PENDING_VALIDATIONS": true,
}

func isUpper(b byte) bool { return b >= 'A' && b <= 'Z' }
func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// pipeSequence classifies the pipe code at the start of s (s[0] == '|'),
// mirroring the renderer's longest-match-first parsing. It returns the byte
// length of the zero-width code and whether it must be kept verbatim (the
// |CR line break, which affects vertical geometry). n == 0 means s does not
// start a recognized code and the '|' is visible content — notably a
// multi-letter |TOKEN placeholder, which is blanked at full marker width
// rather than partially consumed.
func pipeSequence(s []byte) (n int, keep bool) {
	if len(s) >= 4 {
		if s[1] == '{' && (s[2] == 'P' || s[2] == 'O') && s[3] == '}' {
			return 4, false // |{P} / |{O} login position markers
		}
		if s[1] == 'B' && s[2] == '1' && s[3] >= '0' && s[3] <= '5' {
			return 4, false // |B10..|B15 bright backgrounds
		}
	}
	if len(s) >= 3 {
		switch string(s[:3]) {
		case "|CR":
			return 3, true // line break: preserve vertical geometry
		case "|CL", "|DE", "|PP":
			return 3, false
		}
		if isDigit(s[1]) && isDigit(s[2]) {
			if v := int(s[1]-'0')*10 + int(s[2]-'0'); v <= 23 {
				return 3, false // |00..|23 colors and reset
			}
			return 0, false // not a color code: visible
		}
		if s[1] == 'B' && isDigit(s[2]) {
			return 3, false // |B0..|B9 backgrounds
		}
	}
	if len(s) >= 2 && s[1] == 'P' {
		return 2, false // |P save cursor (renderer matches it before placeholders)
	}
	// Coordinate placeholders: |X or |XY uppercase — but three or more
	// uppercase letters is a |TOKEN placeholder, left visible.
	if len(s) >= 2 && isUpper(s[1]) {
		if len(s) >= 3 && isUpper(s[2]) {
			if len(s) >= 4 && isUpper(s[3]) {
				return 0, false
			}
			return 3, false
		}
		return 2, false
	}
	return 0, false
}

// applyConditionalRegions resolves {{condition}}...{{/}} regions in raw menu
// ANSI content against the viewing user. A condition is resolved first
// against the caller-supplied keywords map (e.g. "SPONSOR"), then as an ACS
// expression. Safe for a nil user and a nil keywords map (all gated regions
// hide, matching CheckUserACS).
func applyConditionalRegions(content []byte, u *user.User, keywords map[string]bool) []byte {
	if !bytes.Contains(content, []byte(condOpen)) {
		return content
	}
	var out bytes.Buffer
	out.Grow(len(content))
	i := 0
	for i < len(content) {
		open := bytes.Index(content[i:], []byte(condOpen))
		if open < 0 {
			out.Write(content[i:])
			break
		}
		open += i
		out.Write(content[i:open])

		braceEnd := bytes.Index(content[open:], []byte("}}"))
		if braceEnd < 0 {
			// No closing braces anywhere: literal text.
			out.Write(content[open:])
			break
		}
		inner := string(content[open+len(condOpen) : open+braceEnd])
		markerEnd := open + braceEnd + 2

		// A real ACS condition never spans lines; decorative {{ in ASCII art
		// whose nearest }} is on a later line stays literal.
		if strings.ContainsAny(inner, "\r\n") {
			out.Write([]byte(condOpen))
			i = open + len(condOpen)
			continue
		}

		if knownLiteralTokens[inner] {
			out.Write(content[open:markerEnd])
			i = markerEnd
			continue
		}
		if inner == "/" {
			slog.Warn("stray {{/}} without matching open marker in menu ANSI")
			i = markerEnd
			continue
		}

		// inner is an ACS condition opening a region.
		region := content[markerEnd:]
		next := len(content)
		if closeIdx := bytes.Index(region, []byte(condClose)); closeIdx >= 0 {
			region = region[:closeIdx]
			next = markerEnd + closeIdx + len(condClose)
		} else {
			slog.Warn("unclosed conditional region in menu ANSI, applying to end of file", "acs", inner)
		}
		show, isKeyword := keywords[strings.ToUpper(inner)]
		if !isKeyword {
			show = CheckUserACS(inner, u)
		}
		if show {
			out.Write(region)
		} else {
			out.Write(blankRegion(region))
		}
		i = next
	}
	return out.Bytes()
}

// blankRegion replaces a hidden region with whitespace of identical rendered
// geometry: zero-width content (CSI escapes, pipe/tilde codes and markers)
// is removed, line breaks — including the |CR pipe command — are preserved,
// and everything else, including multi-letter |TOKEN placeholders, is
// blanked at its own width.
func blankRegion(region []byte) []byte {
	var out bytes.Buffer
	out.Grow(len(region))
	i := 0
	for i < len(region) {
		b := region[i]
		switch {
		case b == '\r' || b == '\n':
			out.WriteByte(b)
			i++
		case b == 0x1b: // CSI escape sequence: zero-width, remove
			j := i + 1
			if j < len(region) && region[j] == '[' {
				j++
				for j < len(region) && !isCSIFinal(region[j]) {
					j++
				}
				if j < len(region) {
					j++ // consume the final byte
				}
			}
			i = j
		case b == '|':
			n, keep := pipeSequence(region[i:])
			switch {
			case n == 0:
				out.WriteByte(' ') // literal '|' is visible
				i++
			case keep:
				out.Write(region[i : i+n])
				i += n
			default:
				i += n
			}
		case b == '~' && i+2 < len(region) && isUpper(region[i+1]) && isUpper(region[i+2]):
			i += 3 // ~XY coordinate marker (letters only): zero-width, remove
		default:
			out.WriteByte(' ')
			i++
		}
	}
	return out.Bytes()
}

func isCSIFinal(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}
