package menu

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// bodyAsUTF8 settles a message body's encoding once, for the whole body, and
// returns it as UTF-8. charset is the message's CHRS kludge
// (DisplayMessage.Charset).
//
// Left to the writer, the decision is made per span between escape sequences
// and per wrapped line, and a CP437 run cut short by a colour change is often
// valid UTF-8 on its own — ▄▒ (DC B1) decodes as U+0731 — so two cells of art
// went out as one unrelated character, or as '?' on a CP437 terminal. Once the
// body is UTF-8 every span agrees, and a CP437 terminal gets the original
// bytes back from the writer.
//
// A body that is valid UTF-8 is taken as UTF-8 whatever CHRS says: labels
// naming a single-byte set on what is really UTF-8 are common (CP850 and
// LATIN-1 on Russian and English text), while a single-byte body that is
// valid UTF-8 throughout is not seen in practice. Otherwise CHRS picks the
// decoding. A UTF-8 label means the body is UTF-8 with stray bytes in it,
// usually a quote of a CP437 message, so only those bytes are read as CP437.
// The Cyrillic sets are decoded by name; their box drawing sits where CP437's
// does, so art drawn in them survives. Every other label is read as CP437:
// art is often labelled with whatever its editor defaulted to, and the other
// sets put letters where CP437 draws blocks and lines.
func bodyAsUTF8(body, charset string) string {
	if utf8.ValidString(body) {
		return body
	}
	switch charset {
	case "UTF-8", "UTF8":
		return mixedUTF8(body)
	case "CP866", "+7_FIDO", "+7":
		return decodeCharmap(body, charmap.CodePage866)
	case "CP1251":
		return decodeCharmap(body, charmap.Windows1251)
	case "KOI8-R":
		return decodeCharmap(body, charmap.KOI8R)
	case "KOI8-U":
		return decodeCharmap(body, charmap.KOI8U)
	}
	return string(ansi.CP437BytesToUTF8([]byte(body)))
}

// mixedUTF8 keeps every valid UTF-8 sequence in s and reads each byte that
// is not part of one as CP437.
func mixedUTF8(s string) string {
	var b strings.Builder
	b.Grow(len(s) + len(s)/4)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			r = ansi.Cp437ToUnicode[s[i]]
		}
		b.WriteRune(r)
		i += size
	}
	return b.String()
}

// decodeCharmap decodes s from a single-byte character set. Every byte has a
// character in the sets bodyAsUTF8 passes, and ASCII, escape sequences
// included, decodes as itself.
func decodeCharmap(s string, cm *charmap.Charmap) string {
	out, err := cm.NewDecoder().String(s)
	if err != nil {
		return string(ansi.CP437BytesToUTF8([]byte(s)))
	}
	return out
}

// messageBodyLines turns a message body, colour pipe codes already expanded,
// into the lines the reader paints, as UTF-8.
func messageBodyLines(body, charset string, termWidth int, mode ansi.OutputMode) []string {
	return messageBodyLinesWithBanners(body, charset, termWidth, mode, nil)
}

// messageBodyLinesWithBanners is messageBodyLines for a board whose quote
// banners are known, so that a banner shaped like a quote line is not reflowed.
func messageBodyLinesWithBanners(body, charset string, termWidth int, mode ansi.OutputMode, banners quoteBannerMatcher) []string {
	body = bodyAsUTF8(body, charset)

	if detectAnsiArtInMessage(body) {
		// Render ANSI art into a virtual buffer, cursor positioning
		// relative to the buffer (0,0) rather than the terminal screen.
		// Text reaching the right margin wraps onto the next row the way a
		// terminal does, deferred so that a row filled exactly to the
		// margin does not gain a blank row after it. Art can turn wrapping
		// off with ESC[?7l, and much of this echo's art turns it on with
		// ESC[?7h.
		//
		// Message art is drawn for 80 columns and much of it relies on
		// wrapping there, so a wider terminal still renders it at 80;
		// wrapping at the terminal's width would shear it.
		return RenderANSIArtToLines(body, min(termWidth, ansi.ArtWidth), 500)
	}

	// Quote lines are reflowed here and word wrap leaves them alone. Word wrap
	// also cannot break a run with no spaces (an ASCII rule, a row of blocks);
	// whatever is still too wide is cut, or it spills onto the row below when
	// drawn.
	body = dropTruncatedEscapes(body)
	body = reflowQuotes(body, termWidth, mode, banners)
	return breakOversizedLines(wrapAnsiString(body, termWidth, mode), termWidth, mode)
}

// dropTruncatedEscapes removes escape sequences that were cut off before their
// final byte, keeping everything else exactly as it arrived. Art can reach us
// cut at 79 bytes mid-sequence, and a body using only colour never goes through
// the ANSI renderer, which discards these itself. Sent to the terminal, the
// fragment is drawn as text or eats whatever follows, depending on the client.
// The lost bytes cannot be recovered; this only stops the damage showing.
func dropTruncatedEscapes(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := i + 1
		if j < len(s) && s[j] == '[' {
			j++
			for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f { // parameters, intermediates
				j++
			}
			if j < len(s) && s[j] >= 0x40 && s[j] <= 0x7e { // final byte: complete
				b.WriteString(s[i : j+1])
				i = j + 1
				continue
			}
			i = j // stop before the byte that ended it, so it is still processed
			continue
		}
		if j >= len(s) || s[j] == '\r' || s[j] == '\n' || s[j] == 0x1b {
			i = j // a bare ESC
			continue
		}
		b.WriteString(s[i : j+1])
		i = j + 1
	}
	return b.String()
}

func hasOriginLine(text string) bool {
	if text == "" {
		return false
	}
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "* Origin:") {
			return true
		}
	}
	return false
}

func isQuoteLine(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmed, ">") {
		return true
	}
	prefix, _ := splitReaderQuotePrefix(trimmed)
	return prefix != ""
}

func isTearLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "---")
}

func isOriginLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "* Origin:")
}

// hasANSICursorMovement checks for ANSI cursor movement codes with digits
func hasANSICursorMovement(text string) bool {
	// Look for patterns like ESC[<digits>A/B/C/D or ESC[<digits>;<digits>H/f
	for i := 0; i < len(text)-3; i++ {
		if text[i] == '\x1b' && i+1 < len(text) && text[i+1] == '[' {
			// Found ESC[, now look for cursor codes
			j := i + 2
			hasDigit := false
			for j < len(text) && ((text[j] >= '0' && text[j] <= '9') || text[j] == ';') {
				if text[j] >= '0' && text[j] <= '9' {
					hasDigit = true
				}
				j++
			}
			// Check if followed by cursor movement letter
			if hasDigit && j < len(text) {
				switch text[j] {
				case 'A', 'B', 'C', 'D', 'H', 'f': // Cursor movement or positioning
					return true
				}
			}
		}
	}
	return false
}

// detectAnsiArtInMessage checks if message body contains ANSI art
func detectAnsiArtInMessage(text string) bool {
	// Must contain ANSI codes. ESC 7 / ESC 8 are not CSI sequences, so art
	// that positions with nothing else would otherwise stop here.
	if !strings.Contains(text, "\x1b[") && !strings.Contains(text, "\x1b7") && !strings.Contains(text, "\x1b8") {
		return false
	}

	// Check for common ANSI art indicators:
	// 1. Home cursor without row/col (ESC[H)
	// 2. Explicit cursor positioning (ESC[f)
	// 3. Cursor movement with digits (ESC[5A, ESC[10;20H, etc.)
	// 4. Cursor save/restore (ESC[s / ESC[u, ESC 7 / ESC 8). Art posted from
	//    some systems uses nothing else: it splits each autowrapped row into
	//    short message lines with ESC[s CR ESC[u, which resumes exactly where
	//    the line was cut. Read as text, every piece becomes its own line and
	//    its ESC[u jumps to a stale position once the reader scrolls.
	return strings.Contains(text, "\x1b[H") ||
		strings.Contains(text, "\x1b[f") ||
		strings.Contains(text, "\x1b[s") ||
		strings.Contains(text, "\x1b[u") ||
		strings.Contains(text, "\x1b7") ||
		strings.Contains(text, "\x1b8") ||
		hasANSICursorMovement(text)
}

// formatMessageBody prepares a message body for the reader. Runs of quoted
// lines get a blank line before and after them, except next to a quote banner
// that banners matches: the editor's "Said" / "Done" lines frame the quote.
func formatMessageBody(body, originAddr string, includeOrigin bool, banners quoteBannerMatcher) string {
	// Check if body contains ANSI art BEFORE normalizing line endings
	// ANSI art uses \r for cursor positioning and must NOT be modified
	if detectAnsiArtInMessage(body) {
		// For ANSI art: Return raw body unchanged
		// The ANSI renderer will handle all cursor positioning (\r, \n, ESC codes)
		// Converting \r to \r\n would break layering effects in ANSI art
		if includeOrigin && originAddr != "" && !hasOriginLine(body) {
			return body + "\r\n* Origin: " + originAddr
		}
		return body
	}

	// Regular text: normalize line endings to \n
	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	lines := strings.Split(normalized, "\n")
	if includeOrigin {
		lines = append(lines, fmt.Sprintf("* Origin: %s", originAddr))
	}

	out := make([]string, 0, len(lines))
	prevWasQuote := false
	prevWasTear := false

	for i, rawLine := range lines {
		line := rawLine
		isQuote := isQuoteLine(line)
		// A "--- Name Done ---" quote banner starts like a tearline but is not one.
		isTear := isTearLine(line) && !banners.match(line)
		isOrigin := isOriginLine(line)
		isOriginBlock := isTear || isOrigin

		if isOriginBlock {
			if !prevWasTear && len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			if isTear {
				out = append(out, fmt.Sprintf("|08%s|07", line))
				prevWasTear = true
			} else {
				out = append(out, fmt.Sprintf("|09%s|07", line))
				prevWasTear = false
			}
			prevWasQuote = false
			continue
		}

		if isQuote {
			if !prevWasQuote && len(out) > 0 && out[len(out)-1] != "" && (i == 0 || !banners.match(lines[i-1])) {
				out = append(out, "")
			}

			out = append(out, fmt.Sprintf("|14%s|07", line))

			nextLine := ""
			if i+1 < len(lines) {
				nextLine = lines[i+1]
			}
			nextIsQuote := i+1 < len(lines) && isQuoteLine(nextLine)
			nextIsOriginBlock := i+1 < len(lines) && (isTearLine(nextLine) || isOriginLine(nextLine))
			if !nextIsQuote && strings.TrimSpace(nextLine) != "" && !nextIsOriginBlock && !banners.match(nextLine) {
				out = append(out, "")
			}

			prevWasQuote = true
			prevWasTear = false
			continue
		}

		out = append(out, line)
		prevWasQuote = false
		prevWasTear = false
	}

	return strings.Join(out, "\n")
}
