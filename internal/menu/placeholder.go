package menu

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// PlaceholderMatch represents a parsed placeholder from a template.
type PlaceholderMatch struct {
	Code      string         // Single letter (T, F, S, etc.)
	Width     int            // 0 = no width constraint
	MaxWidth  int            // 0 = no limit; otherwise truncate to this width without padding
	AutoWidth bool           // true = use auto-calculated width from context
	Align     ansi.Alignment // AlignLeft (default), AlignRight, AlignCenter
	FullMatch string         // Complete matched text "@T###@"
	StartPos  int            // Byte offset in template
	EndPos    int            // End byte offset
}

// Regex compiled once for performance.
// Matches: @CODE@, @CODE:20@, @CODE###@, @CODE*@, @CODE<20@, or @CODE|MODIFIER...@
// Groups: 1=code letter, 2=modifier(opt), 3=digits-after-modifier(opt),
//
//	4=:WIDTH (optional), 5=### (optional), 6=* (optional), 7=<MAXWIDTH (optional)
//
// G = gap fill: fills remaining line width with ─ (CP437 0xC4) characters.
var placeholderRegex = regexp.MustCompile(`@([BTFSUL#NDWPEOMAZCXGVK])(?:\|([LRC])(\d+)?)?(?::(\d+)|([#]+)|(\*)|<(\d+))?@`)

// parsePlaceholders extracts all @CODE@ patterns from template bytes.
func parsePlaceholders(template []byte) []PlaceholderMatch {
	matches := placeholderRegex.FindAllSubmatchIndex(template, -1)
	result := make([]PlaceholderMatch, 0, len(matches))

	for _, match := range matches {
		// match[0], match[1]   = full match start/end
		// match[2], match[3]   = code letter start/end
		// match[4], match[5]   = modifier L/R/C start/end (or -1 if not present)
		// match[6], match[7]   = digits after modifier (or -1 if not present)
		// match[8], match[9]   = :WIDTH start/end (or -1 if not present)
		// match[10], match[11] = ### start/end (or -1 if not present)
		// match[12], match[13] = * start/end (or -1 if not present)
		// match[14], match[15] = <MAXWIDTH digits start/end (or -1 if not present)

		code := string(template[match[2]:match[3]])
		fullMatch := string(template[match[0]:match[1]])

		// Parse alignment modifier
		align := ansi.AlignLeft
		if match[4] != -1 {
			align = ansi.ParseAlignment(string(template[match[4]:match[5]]))
		}

		// Calculate width: digits-after-modifier > colon-width > visual-hash-width
		width := 0
		autoWidth := false
		if match[6] != -1 && match[6] < match[7] {
			// Digits immediately after modifier (e.g. @T|R8@)
			widthStr := string(template[match[6]:match[7]])
			width, _ = strconv.Atoi(widthStr)
		} else if match[8] != -1 && match[8] < match[9] {
			// Parameter width :20 (regex captures digits only, not colon)
			widthStr := string(template[match[8]:match[9]])
			width, _ = strconv.Atoi(widthStr)
		} else if match[10] != -1 && match[10] < match[11] {
			// Visual width — the full placeholder token (e.g. @T###@) occupies N columns
			// in the ANSI art template, so the substituted value must also be N bytes wide
			// to preserve alignment. Width = total token length including delimiters.
			width = match[1] - match[0]
		} else if match[12] != -1 {
			// Auto-width: width determined at render time from context
			autoWidth = true
		}

		// Max width (@T<40@): truncate long values but never pad short ones,
		// so the field keeps its natural length and whatever follows it flows on.
		maxWidth := 0
		if match[14] != -1 && match[14] < match[15] {
			maxWidth, _ = strconv.Atoi(string(template[match[14]:match[15]]))
		}

		result = append(result, PlaceholderMatch{
			Code:      code,
			Width:     width,
			MaxWidth:  maxWidth,
			AutoWidth: autoWidth,
			Align:     align,
			FullMatch: fullMatch,
			StartPos:  match[0],
			EndPos:    match[1],
		})
	}

	return result
}

// gapFillMarker is an internal marker that replaces @G@ during the first pass.
// Chosen to be unlikely to appear in real template content.
const gapFillMarker = "\x00GAP_FILL\x00"

// processPlaceholderTemplate replaces @CODE@ placeholders with values from substitutions map.
// Supports four formats:
//   - @T@ - Insert value as-is
//   - @T:20@ - Explicit width (parameter-based)
//   - @T###########@ - Visual width (width = total placeholder length including delimiters)
//   - @T*@ - Auto-width (width from autoWidths map, calculated from context)
//   - @T<40@ - Max width (truncate to 40, never pad)
//
// Special code @G@ (gap fill): fills remaining line width with ─ (CP437 0xC4).
// Width is determined by: @G:80@ (explicit target), @G*@ (auto-width from map),
// or @G@ (default 80). The fill count = target_width - visible_chars_on_line.
//
// autoWidths is optional (nil = no auto-width support). When provided, @CODE*@ placeholders
// look up their width from this map.
func processPlaceholderTemplate(template []byte, substitutions map[byte]string, autoWidths map[byte]int, cp437Values bool) []byte {
	matches := parsePlaceholders(template)
	if len(matches) == 0 {
		return template // No placeholders
	}

	// Track whether we have any gap fill placeholders
	hasGapFill := false

	// Build result by copying template and replacing placeholders
	result := make([]byte, 0, len(template)*2)
	lastEnd := 0

	for _, match := range matches {
		// Copy template bytes before this placeholder
		result = append(result, template[lastEnd:match.StartPos]...)

		if match.Code == "G" {
			// Gap fill: determine target width and insert marker for second pass
			hasGapFill = true
			targetWidth := 79 // default (79 avoids auto-wrap at column 80)
			if match.AutoWidth && autoWidths != nil {
				if w, ok := autoWidths['G']; ok && w > 0 {
					targetWidth = w
				}
			} else if match.Width > 0 {
				targetWidth = match.Width
			}
			// Encode target width into marker
			marker := gapFillMarker + strconv.Itoa(targetWidth) + gapFillMarker
			result = append(result, []byte(marker)...)
		} else {
			// Get substitution value (map key is byte)
			value := ""
			if len(match.Code) > 0 {
				if val, ok := substitutions[match.Code[0]]; ok {
					value = val
				}
			}

			// Apply width constraint if specified (with alignment)
			if match.AutoWidth && autoWidths != nil {
				if w, ok := autoWidths[match.Code[0]]; ok && w > 0 {
					value = fitPlaceholderValue(value, w, match.Align, cp437Values)
				}
			} else if match.Width > 0 {
				value = fitPlaceholderValue(value, match.Width, match.Align, cp437Values)
			} else if match.MaxWidth > 0 {
				value = truncatePlaceholderValue(value, match.MaxWidth, cp437Values)
			}

			// Append processed value
			result = append(result, []byte(value)...)
		}

		lastEnd = match.EndPos
	}

	// Append remaining template after last placeholder
	result = append(result, template[lastEnd:]...)

	// Second pass: resolve gap fill markers
	if hasGapFill {
		result = resolveGapFills(result, cp437Values)
	}

	return result
}

// resolveGapFills replaces gap fill markers with ─ characters to fill lines to target width.
// Each marker encodes its target width. The fill count is calculated per-line:
// fill = targetWidth - visibleCharsOnLine (excluding the marker itself).
func resolveGapFills(data []byte, cp437Values bool) []byte {
	markerBytes := []byte(gapFillMarker)

	// Process line by line to calculate per-line visible widths
	lines := bytes.Split(data, []byte("\n"))
	for i, line := range lines {
		// Check if line contains a gap fill marker
		startIdx := bytes.Index(line, markerBytes)
		if startIdx == -1 {
			continue
		}

		// Find the full marker: \x00GAP_FILL\x00<width>\x00GAP_FILL\x00
		afterFirst := startIdx + len(markerBytes)
		endIdx := bytes.Index(line[afterFirst:], markerBytes)
		if endIdx == -1 {
			continue
		}
		endIdx += afterFirst + len(markerBytes)

		// Extract target width
		widthStr := string(line[afterFirst : afterFirst+endIdx-afterFirst-len(markerBytes)])
		targetWidth, err := strconv.Atoi(widthStr)
		if err != nil || targetWidth <= 0 {
			targetWidth = 80
		}

		// Calculate visible width of line WITHOUT the marker
		lineWithout := make([]byte, 0, len(line))
		lineWithout = append(lineWithout, line[:startIdx]...)
		lineWithout = append(lineWithout, line[endIdx:]...)
		// Strip \r for width calculation
		visibleWidth := placeholderCells(strings.TrimRight(string(lineWithout), "\r"), cp437Values)

		// Calculate fill count
		fillCount := targetWidth - visibleWidth
		if fillCount < 0 {
			fillCount = 0
		}

		// Build fill string: CP437 horizontal line character (0xC4)
		fill := bytes.Repeat([]byte{0xC4}, fillCount)

		// Replace marker with fill
		newLine := make([]byte, 0, len(line))
		newLine = append(newLine, line[:startIdx]...)
		newLine = append(newLine, fill...)
		newLine = append(newLine, line[endIdx:]...)
		lines[i] = newLine
	}

	return bytes.Join(lines, []byte("\n"))
}

// headerTemplateUsesUserNote reports whether a header template shows the user
// note itself: a legacy |U code or an @U@ placeholder in any of its forms
// (@U:20@, @U###@, @U*@, @U<20@, @U|R20@, ...). When it does, the reader
// leaves the note out of the From value so it is not shown twice.
func headerTemplateUsesUserNote(template []byte) bool {
	if bytes.Contains(template, []byte("|U")) {
		return true
	}
	for _, m := range parsePlaceholders(template) {
		if m.Code == "U" {
			return true
		}
	}
	return false
}

// Width helpers for substituted values. The message reader converts values to
// CP437 bytes for CP437 sessions, and there every byte is one cell. The ansi
// helpers decode UTF-8 instead, and plenty of CP437 byte pairs happen to be
// valid UTF-8 (├⌐ is 0xC3 0xA9), so they would count two cells as one and let
// a value run past its width. cp437Values selects byte counting; UTF-8
// sessions keep the rune-aware ansi helpers.

// placeholderCells counts the cells s occupies, ignoring ANSI escapes.
func placeholderCells(s string, cp437Values bool) int {
	if !cp437Values {
		return ansi.VisibleLength(s)
	}
	cells := 0
	for i := 0; i < len(s); i++ {
		if n := placeholderEscapeLen(s[i:]); n > 0 {
			i += n - 1
			continue
		}
		cells++
	}
	return cells
}

// truncatePlaceholderValue cuts s to at most maxCells cells, keeping ANSI
// escapes intact.
func truncatePlaceholderValue(s string, maxCells int, cp437Values bool) string {
	if !cp437Values {
		return ansi.TruncateVisible(s, maxCells)
	}
	var b strings.Builder
	cells := 0
	for i := 0; i < len(s); i++ {
		if n := placeholderEscapeLen(s[i:]); n > 0 {
			b.WriteString(s[i : i+n])
			i += n - 1
			continue
		}
		if cells == maxCells {
			continue // keep scanning so trailing escapes (colour resets) survive
		}
		b.WriteByte(s[i])
		cells++
	}
	return b.String()
}

// fitPlaceholderValue truncates or pads s to exactly width cells.
func fitPlaceholderValue(s string, width int, align ansi.Alignment, cp437Values bool) string {
	if !cp437Values {
		return ansi.ApplyWidthConstraintAligned(s, width, align)
	}
	s = truncatePlaceholderValue(s, width, true)
	pad := width - placeholderCells(s, true)
	if pad <= 0 {
		return s
	}
	switch align {
	case ansi.AlignRight:
		return strings.Repeat(" ", pad) + s
	case ansi.AlignCenter:
		return strings.Repeat(" ", pad/2) + s + strings.Repeat(" ", pad-pad/2)
	default:
		return s + strings.Repeat(" ", pad)
	}
}

// placeholderEscapeLen returns the length of the CSI escape at the start of s,
// or 0 if s does not start with one.
func placeholderEscapeLen(s string) int {
	if len(s) < 2 || s[0] != 0x1b || s[1] != '[' {
		return 0
	}
	for i := 2; i < len(s); i++ {
		if c := s[i]; c >= 0x40 && c <= 0x7e {
			return i + 1
		}
	}
	return len(s)
}
