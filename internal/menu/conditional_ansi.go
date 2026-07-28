package menu

import (
	"bytes"
	"log/slog"
	"regexp"
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

// zeroWidthRegex matches content that occupies no screen cells when the ANSI
// processor renders it: CSI escape sequences, |XX pipe/coordinate codes
// (including 3-char |B10..|B15 backgrounds), and ~XX coordinate markers.
// Removed (not spaced) when blanking so the blank area matches the region's
// rendered width.
var zeroWidthRegex = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]|\|(?:B1[0-5]|[A-Z0-9]{2})|~[A-Z0-9]{2}`)

// applyConditionalRegions resolves {{acs}}...{{/}} regions in raw menu ANSI
// content against the viewing user. Safe for a nil user (all gated regions
// hide, matching CheckUserACS).
func applyConditionalRegions(content []byte, u *user.User) []byte {
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
		if CheckUserACS(inner, u) {
			out.Write(region)
		} else {
			out.Write(blankRegion(region))
		}
		i = next
	}
	return out.Bytes()
}

// blankRegion replaces a hidden region with whitespace of identical rendered
// geometry: zero-width sequences removed, line breaks kept, all else spaced.
func blankRegion(region []byte) []byte {
	region = zeroWidthRegex.ReplaceAll(region, nil)
	blanked := make([]byte, len(region))
	for i, b := range region {
		if b == '\r' || b == '\n' {
			blanked[i] = b
		} else {
			blanked[i] = ' '
		}
	}
	return blanked
}
