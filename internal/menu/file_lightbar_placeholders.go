package menu

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// File-list template placeholders and FILE_ID.DIZ formatting.
// fileListPlaceholderRegex matches @FPAGE@, @FTOTAL@, @FCONFPATH@ with optional alignment and width.
// Modifier: | (0x7C) or │ (CP437 0xB3, common in ANSI art) followed by L/R/C — matches message-header format.
// Groups: 1=code (FPAGE|FTOTAL|FCONFPATH), 2=modifier (L|R|C), 3=:N digits, 4=# sequence
var fileListPlaceholderRegex = regexp.MustCompile(`@(FPAGE|FTOTAL|FCONFPATH)(?:[\x7C\xB3]([LRC]))?(?::(\d+)|(#+))?@`)

// processFileListPlaceholders replaces file-list-specific pipe codes and @-placeholders
// with current page, total pages, total file count, and conference path. Use in FILELIST.TOP and FILELIST.BOT.
// Pipe codes: |FPAGE ("Page X of Y"), |FTOTAL (total file count), |FCONFPATH (Conference > File Area).
// Placeholders support alignment modifiers: @FPAGE|R###@, @FTOTAL|C:5@, @FCONFPATH|R############@
// fconfpath is the pre-formatted "Conference > Area" string with pipe codes (from resolveFileConferencePath).
func processFileListPlaceholders(data []byte, currentPage, totalPages, totalFiles int, fconfpath string) []byte {
	s := string(data)
	pageStr := fmt.Sprintf("Page %d of %d", currentPage, totalPages)
	totalStr := strconv.Itoa(totalFiles)

	// Process @-placeholders FIRST so |FPAGE inside @FPAGE|R#####@ isn't consumed by pipe codes.
	// @CODE@ placeholders with optional alignment modifier (|L, |R, |C) and width (:N or ###)
	// For ###: width = total placeholder length (entire token) so replacement preserves ANSI layout.
	// E.g. @FPAGE|R###########@ is 20 cols — output is padded/truncated to 20 visible chars.
	s = fileListPlaceholderRegex.ReplaceAllStringFunc(s, func(match string) string {
		subs := fileListPlaceholderRegex.FindStringSubmatch(match)
		if len(subs) < 2 {
			return match
		}
		code := subs[1]
		modifier := ""
		if len(subs) > 2 {
			modifier = subs[2]
		}
		width := 0
		if len(subs) > 3 && subs[3] != "" {
			width, _ = strconv.Atoi(subs[3])
		} else if len(subs) > 4 && subs[4] != "" {
			// Visual width: entire placeholder length (matches message-header / editor placeholder behavior)
			width = len(match)
		}
		align := ansi.AlignLeft
		if modifier != "" {
			align = ansi.ParseAlignment(modifier)
		}

		var value string
		switch code {
		case "FPAGE":
			value = pageStr
		case "FTOTAL":
			value = totalStr
		case "FCONFPATH":
			value = fconfpath
		default:
			return match
		}
		if width <= 0 {
			return value
		}
		return ansi.ApplyWidthConstraintAligned(value, width, align)
	})

	// Pipe codes AFTER @-placeholders so |FPAGE inside @FPAGE|R#####@ isn't destroyed.
	s = strings.ReplaceAll(s, "|FPAGE", pageStr)
	s = strings.ReplaceAll(s, "|FTOTAL", totalStr)
	s = strings.ReplaceAll(s, "|FCONFPATH", fconfpath)

	return []byte(s)
}

const (
	dizMaxWidth = 45
	dizMaxLines = 22
)

// formatDIZLines splits FILE_ID.DIZ content into display-ready lines.
// Each line is truncated to maxWidth visible characters (ANSI-aware).
// Returns at most maxLines lines, with trailing blank lines trimmed.
func formatDIZLines(content string, maxWidth, maxLines int) []string {
	if content == "" {
		return nil
	}

	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	rawLines := strings.Split(content, "\n")

	var lines []string
	for _, line := range rawLines {
		if len(lines) >= maxLines {
			break
		}
		line = strings.TrimRight(line, " \t")
		if ansi.VisibleLength(line) > maxWidth {
			line = ansi.TruncateVisible(line, maxWidth)
		}
		lines = append(lines, line)
	}

	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}
