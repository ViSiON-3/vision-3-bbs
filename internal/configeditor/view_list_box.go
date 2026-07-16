package configeditor

import "strings"

// listBox renders the shared bordered-list-box scaffold used by the FTN and
// V3Net browser screens: a background-filled screen with a centered box
// containing a title, optional column header, a scrollable highlighted list,
// and a help bar. All visual parameters are passed explicitly so each screen
// keeps its exact byte output.
type listBox struct {
	b         strings.Builder
	width     int // terminal width
	boxW      int // inner box width (between the │ borders)
	padL      int // background columns left of the box
	padR      int // background columns right of the box
	bottomPad int // background rows below the box (vertical centering)
	bgLine    string
}

// newListBox writes the global header plus top padding and returns a renderer
// for a centered box of inner width boxW. fixedRows is the number of
// non-padding screen rows (header, box, footer) used for vertical centering.
func (m Model) newListBox(boxW, fixedRows int) *listBox {
	lb := &listBox{
		width:  m.width,
		boxW:   boxW,
		bgLine: bgFillStyle.Render(strings.Repeat("░", m.width)),
	}
	extraV := maxInt(0, m.height-fixedRows)
	topPad := extraV / 2
	lb.bottomPad = extraV - topPad
	lb.padL = maxInt(0, (m.width-boxW-2)/2)
	lb.padR = maxInt(0, m.width-lb.padL-boxW-2)

	lb.b.WriteString(m.globalHeaderLine())
	lb.b.WriteByte('\n')
	lb.bgRows(topPad)
	return lb
}

// line writes a full-width screen line followed by a newline.
func (lb *listBox) line(s string) {
	lb.b.WriteString(s)
	lb.b.WriteByte('\n')
}

// pad surrounds box content with the background fill on both sides.
func (lb *listBox) pad(s string) string {
	return bgFillStyle.Render(strings.Repeat("░", lb.padL)) + s +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, lb.padR)))
}

// row writes styled inner content wrapped in │ │ side borders.
func (lb *listBox) row(content string) {
	lb.line(lb.pad(menuBorderStyle.Render("│") + content + menuBorderStyle.Render("│")))
}

// topBorder / bottomBorder write the horizontal box borders.
func (lb *listBox) topBorder() {
	lb.line(lb.pad(menuBorderStyle.Render("┌" + strings.Repeat("─", lb.boxW) + "┐")))
}

func (lb *listBox) bottomBorder() {
	lb.line(lb.pad(menuBorderStyle.Render("└" + strings.Repeat("─", lb.boxW) + "┘")))
}

// title writes a centered box title row.
func (lb *listBox) title(t string) {
	lb.row(menuHeaderStyle.Render(centerText(t, lb.boxW)))
}

// colHeader writes a left-aligned column header row.
func (lb *listBox) colHeader(h string) {
	lb.row(menuHeaderStyle.Render(padRight(h, lb.boxW)))
}

// separator writes a horizontal rule row inside the box.
func (lb *listBox) separator() {
	lb.row(separatorStyle.Render(strings.Repeat("─", lb.boxW)))
}

// emptyRows writes n blank list rows.
func (lb *listBox) emptyRows(n int) {
	for i := 0; i < n; i++ {
		lb.row(menuItemStyle.Render(strings.Repeat(" ", lb.boxW)))
	}
}

// bgRows writes n full-width background lines.
func (lb *listBox) bgRows(n int) {
	for i := 0; i < n; i++ {
		lb.line(lb.bgLine)
	}
}

// list writes the scrollable list window. format returns the pre-formatted
// content for item i (padded/truncated to boxW here); the cursor row is
// highlighted.
func (lb *listBox) list(visible, scroll, cursor, total int, format func(i int) string) {
	for i := 0; i < visible; i++ {
		visIdx := scroll + i
		var content string
		if visIdx >= 0 && visIdx < total {
			content = format(visIdx)
		}
		if content == "" {
			content = strings.Repeat(" ", lb.boxW)
		}
		if len(content) < lb.boxW {
			content += strings.Repeat(" ", lb.boxW-len(content))
		} else if len(content) > lb.boxW {
			content = content[:lb.boxW]
		}
		if visIdx == cursor {
			lb.row(menuHighlightStyle.Render(content))
		} else {
			lb.row(menuItemStyle.Render(content))
		}
	}
}

// messageRow writes the flash-message line below the box, or a plain
// background line when the message is empty.
func (lb *listBox) messageRow(msg string) {
	if msg == "" {
		lb.line(lb.bgLine)
		return
	}
	lb.line(bgFillStyle.Render(strings.Repeat("░", lb.padL)) +
		flashMessageStyle.Render(" "+padRight(msg, lb.boxW)) +
		bgFillStyle.Render(strings.Repeat("░", maxInt(0, lb.padR+1))))
}

// errorRow returns the styled inner content for an error status row,
// truncating long messages to fit the box.
func (lb *listBox) errorRow(errMsg string) string {
	errText := " " + errMsg
	if len([]rune(errText)) > lb.boxW {
		errText = string([]rune(errText)[:lb.boxW-3]) + "..."
	}
	return flashMessageStyle.Render(padRight(errText, lb.boxW))
}

// statusScreen finishes the screen in a loading/error state: one status row,
// blank rows in place of the list, the bottom border, trailing background
// lines, and a help bar. trailingBG is the number of background lines written
// after the bottom padding (screens differ here).
func (lb *listBox) statusScreen(statusRow string, listVisible, trailingBG int, help string) string {
	lb.row(statusRow)
	lb.emptyRows(listVisible + 1)
	lb.bottomBorder()
	lb.bgRows(lb.bottomPad + trailingBG)
	return lb.finish(help)
}

// finish appends the help bar (no trailing newline) and returns the screen.
func (lb *listBox) finish(help string) string {
	lb.b.WriteString(helpBarStyle.Render(centerText(help, lb.width)))
	return lb.b.String()
}
