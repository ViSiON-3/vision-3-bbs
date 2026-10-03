package menu

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"golang.org/x/term"
)

// keyReader is the part of editor.InputHandler the kludge view needs.
type keyReader interface {
	ReadKey() (int, error)
}

// showKludgeView displays a message's control information (see
// message.MessageManager.GetMessageKludges) a page at a time, for a sysop
// working out how it arrived. Space, Enter, PgDn or Down shows the next page;
// PgUp or Up the previous; Q or Esc returns to the message. Leaving from the
// last page needs no key beyond the one that dismisses it.
//
// The kludge text is the sender's and may contain '|', so it is written as is
// rather than through pipe-code substitution.
func showKludgeView(ih keyReader, terminal *term.Terminal, outputMode ansi.OutputMode,
	msgNum int, kludges []string, termWidth, termHeight int) error {
	width := max(termWidth, 40) - 1 // stay clear of the last column
	rows := wrapKludgeLines(kludges, width, outputMode)
	if len(rows) == 0 {
		rows = []string{"(no control information)"}
	}

	perPage := max(termHeight-3, 1) // title, rule, footer
	pages := (len(rows) + perPage - 1) / perPage
	page := 0
	for {
		var b strings.Builder
		b.WriteString(ansi.ClearScreen())
		// One row only, or a narrow terminal wraps it and the page overruns
		// the footer: drop the page counter, then clip the title, to fit.
		title := fmt.Sprintf("Message #%d control information", msgNum)
		counter := ""
		if pages > 1 {
			counter = fmt.Sprintf("  (page %d of %d)", page+1, pages)
		}
		if len(title)+len(counter) > width {
			counter = ""
		}
		if len(title) > width {
			title = title[:width]
		}
		b.WriteString("\x1b[1;37m" + title + "\x1b[1;30m" + counter + "\x1b[0;37m")
		b.WriteString("\r\n\x1b[1;30m" + strings.Repeat("-", min(width, 79)) + "\x1b[0;37m\r\n")
		end := min((page+1)*perPage, len(rows))
		for _, row := range rows[page*perPage : end] {
			b.WriteString(kludgeRowColour(row) + "\r\n")
		}
		b.WriteString(ansi.MoveCursor(termHeight, 1) + "\x1b[1;30m")
		if page < pages-1 {
			b.WriteString("Space/Enter more, Up/PgUp back, Q quit")
		} else {
			b.WriteString("Press any key to return")
		}
		b.WriteString("\x1b[0m")
		terminalio.WriteProcessedBytes(terminal, []byte(b.String()), outputMode)

		key, err := ih.ReadKey()
		if err != nil {
			return err
		}
		switch {
		case key == 'q' || key == 'Q' || key == editor.KeyEsc:
			return nil
		case key == editor.KeyPageUp || key == editor.KeyArrowUp:
			if page > 0 {
				page--
			}
		case page < pages-1:
			page++
		default:
			return nil
		}
	}
}

// kludgeRowColour colours a row's label, the text up to the first ": " or
// the first word of a continuation-free kludge, so the values stand out.
func kludgeRowColour(row string) string {
	if strings.HasPrefix(row, " ") {
		return "\x1b[0;37m" + row // continuation of a wrapped row
	}
	if i := strings.Index(row, ": "); i > 0 && i < 16 {
		return "\x1b[0;36m" + row[:i+1] + "\x1b[0;37m" + row[i+1:]
	}
	return "\x1b[0;37m" + row
}

// wrapKludgeLines fits each line to width columns, breaking long ones
// (SEEN-BY, PATH) at spaces and indenting the continuation rows. Columns are
// measured as the writer renders them for mode (see columnWidth): kludge
// values are the sender's raw bytes, CP437 or UTF-8, and a wide UTF-8
// character takes two columns. A run with no space to break at is cut.
func wrapKludgeLines(lines []string, width int, mode ansi.OutputMode) []string {
	const indent = "    "
	var out []string
	for _, line := range lines {
		asUTF8 := utf8.ValidString(line)
		if columnWidth(line, asUTF8, mode) <= width {
			out = append(out, line)
			continue
		}
		inner := max(width-len(indent), 1)
		for i, row := range breakOversizedLines(wrapVisualLine(line, inner, asUTF8, mode), inner, mode) {
			if i > 0 {
				row = indent + strings.TrimLeft(row, " ")
			}
			out = append(out, row)
		}
	}
	return out
}
