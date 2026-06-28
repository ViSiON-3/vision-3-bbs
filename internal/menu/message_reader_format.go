package menu

import (
	"fmt"
	"strings"
)

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
	return quotePrefixRe.MatchString(trimmed)
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
	// Must contain ANSI codes
	if !strings.Contains(text, "\x1b[") {
		return false
	}

	// Check for common ANSI art indicators:
	// 1. Home cursor without row/col (ESC[H)
	// 2. Explicit cursor positioning (ESC[f)
	// 3. Cursor movement with digits (ESC[5A, ESC[10;20H, etc.)
	return strings.Contains(text, "\x1b[H") ||
		strings.Contains(text, "\x1b[f") ||
		hasANSICursorMovement(text)
}

func formatMessageBody(body, originAddr string, includeOrigin bool) string {
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
		isTear := isTearLine(line)
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
			if !prevWasQuote && len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}

			out = append(out, fmt.Sprintf("|14%s|07", line))

			nextLine := ""
			if i+1 < len(lines) {
				nextLine = lines[i+1]
			}
			nextIsQuote := i+1 < len(lines) && isQuoteLine(nextLine)
			nextIsOriginBlock := i+1 < len(lines) && (isTearLine(nextLine) || isOriginLine(nextLine))
			if !nextIsQuote && strings.TrimSpace(nextLine) != "" && !nextIsOriginBlock {
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
