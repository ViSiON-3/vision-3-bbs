package editor

import (
	"fmt"
	"strings"
	"unicode"
)

// The split-pane quote picker used by CTRL-Q.
//
// The editing area is divided into a compose pane (the message being written,
// updated live as lines are quoted), a divider row carrying the key legend, and
// a source pane holding a lightbar over the message being replied to. Both panes
// paint only within the rows the editor already owns, so the header template and
// the footer are never scrolled off — the failure mode of the old inline list.
//
// Selecting a line inserts it immediately, which means the compose pane always
// shows exactly what will be saved. Backspace un-quotes the most recent line.

// Quote pane colors.
const (
	qpGutter    = "\x1b[0;34m"    // line numbers: dim blue
	qpText      = "\x1b[0;37m"    // unquoted source text
	qpUsed      = "\x1b[1;30m"    // already-quoted source text: dark gray
	qpBar       = "\x1b[1;37;44m" // lightbar: bright white on blue
	qpBarGutter = "\x1b[1;36;44m" // lightbar gutter
	qpRule      = "\x1b[1;34m"    // divider rule
	qpLabel     = "\x1b[0;37m"    // divider label text
	qpName      = "\x1b[1;37m"    // divider: name being quoted
	qpKey       = "\x1b[1;36m"    // divider: key names in the legend
	qpReset     = "\x1b[0m"
)

// quoteEntry records one quoted source line so Backspace can undo it.
type quoteEntry struct {
	srcIdx   int // index into quoteSession.src
	bufLines int // buffer lines the source line expanded to after wrapping
}

// quoteSession holds the state of one CTRL-Q interaction.
type quoteSession struct {
	ch *CommandHandler

	src    []string // source lines, cruft filtered out
	quoted []int    // how many times src[i] has been quoted into the message
	prefix string   // per-line quote prefix, e.g. "Bu> "

	sel int // selected source index (0-based)
	top int // first visible source index (0-based)

	stack      []quoteEntry
	blockStart int // buffer line holding the "Said" banner (0 = no block yet)
	insertAt   int // buffer line the next quoted line is inserted at
	origLine   int // cursor line CTRL-Q was pressed on

	composeTop   int  // first buffer line shown in the compose pane
	composeFocus bool // true = arrows scroll the compose pane instead of moving the bar

	// Geometry, all 1-based terminal rows.
	composeY, composeRows int
	dividerY              int
	quoteY, quoteRows     int
	width                 int
}

// runQuoteMode drives the split-pane picker and returns the cursor position the
// editor should resume at.
func (ch *CommandHandler) runQuoteMode(inputHandler *InputHandler, currentLine int) (int, int) {
	src := prepareQuoteSource(ch.quoteData.Lines)
	if len(src) == 0 {
		// Everything was trailer/blank — quote the raw body rather than nothing.
		src = ch.quoteData.Lines
	}

	qs := &quoteSession{
		ch:       ch,
		src:      src,
		quoted:   make([]int, len(src)),
		prefix:   ch.quotePrefix(),
		origLine: currentLine,
	}
	qs.layout()
	// Open the compose pane with some of the message above the insertion point,
	// so the quote lands in visible context rather than at the top of the pane.
	qs.composeTop = currentLine - qs.composeRows/2
	if qs.composeTop < 1 {
		qs.composeTop = 1
	}

	ch.screen.WriteDirect("\x1b[?25l") // hide cursor: the lightbar is the pointer
	defer ch.screen.WriteDirect("\x1b[?25h")

	qs.drawAll()

	for {
		key, err := inputHandler.ReadKeyTranslated()
		if err != nil {
			break
		}

		if qs.composeFocus {
			if qs.handleComposeKey(key) {
				break
			}
			continue
		}

		// ReadKeyTranslated folds the arrow and paging keys into the WordStar
		// control codes, so only the control codes are dispatched here.
		switch key {
		case KeyCtrlE:
			qs.moveTo(qs.sel - 1)
		case KeyCtrlX:
			qs.moveTo(qs.sel + 1)
		case KeyCtrlR:
			qs.moveTo(qs.sel - qs.quoteRows)
		case KeyCtrlC:
			qs.moveTo(qs.sel + qs.quoteRows)
		case KeyCtrlW:
			qs.moveTo(0)
		case KeyCtrlP:
			qs.moveTo(len(qs.src) - 1)
		case ' ', KeyEnter:
			qs.quoteSelected()
		case KeyBackspace, KeyCtrlY:
			qs.undoLast()
		case KeyTab:
			qs.composeFocus = true
			qs.drawDivider()
		case KeyEsc, KeyCtrlQ:
			return qs.finish()
		}
	}

	return qs.finish()
}

// handleComposeKey scrolls the compose pane while it holds focus.
// Returns true when the session should end.
func (qs *quoteSession) handleComposeKey(key int) bool {
	switch key {
	case KeyCtrlE:
		if qs.composeTop > 1 {
			qs.composeTop--
			qs.drawCompose(false)
		}
	case KeyCtrlX:
		if qs.composeTop < qs.ch.buffer.GetLineCount() {
			qs.composeTop++
			qs.drawCompose(false)
		}
	case KeyCtrlR:
		qs.composeTop -= qs.composeRows
		if qs.composeTop < 1 {
			qs.composeTop = 1
		}
		qs.drawCompose(false)
	case KeyCtrlC:
		qs.composeTop += qs.composeRows
		if qs.composeTop > qs.ch.buffer.GetLineCount() {
			qs.composeTop = qs.ch.buffer.GetLineCount()
		}
		qs.drawCompose(false)
	case KeyTab:
		qs.composeFocus = false
		qs.drawCompose(true) // re-follow the insertion point
		qs.drawDivider()
	case KeyEsc, KeyCtrlQ:
		return true
	}
	return false
}

// finish returns the cursor position for the editor to resume at: the line just
// after the quote block, or the original line when nothing was quoted.
func (qs *quoteSession) finish() (int, int) {
	if qs.blockStart == 0 {
		return qs.origLine, 1
	}
	line := qs.insertAt + 1 // insertAt holds the "Done" banner
	if line > qs.ch.buffer.GetLineCount() {
		line = qs.ch.buffer.GetLineCount()
	}
	return line, 1
}

// layout splits the editing area into compose pane, divider and source pane.
func (qs *quoteSession) layout() {
	s := qs.ch.screen
	start := s.GetEditingStartY()
	total := s.GetScreenLines()

	quoteRows := (total - 1) / 2
	if quoteRows < 1 {
		quoteRows = 1
	}
	composeRows := total - 1 - quoteRows
	if composeRows < 1 {
		composeRows = 1
		quoteRows = total - 2
	}

	qs.composeY = start
	qs.composeRows = composeRows
	qs.dividerY = start + composeRows
	qs.quoteY = qs.dividerY + 1
	qs.quoteRows = quoteRows
	// Draw one column short of the terminal: writing the last column risks an
	// autowrap on real clients, and ClearEOL would erase it again anyway.
	qs.width = s.termWidth - 1
	if qs.width < 20 {
		qs.width = 79
	}
}

func (qs *quoteSession) drawAll() {
	qs.drawCompose(true)
	qs.drawDivider()
	qs.drawQuotePane()
}

// moveTo moves the lightbar, clamping to the source and scrolling the pane.
func (qs *quoteSession) moveTo(idx int) {
	if idx < 0 {
		idx = 0
	}
	if idx > len(qs.src)-1 {
		idx = len(qs.src) - 1
	}
	if idx == qs.sel {
		return
	}
	qs.sel = idx
	qs.scrollToSelection()
	qs.drawQuotePane()
}

func (qs *quoteSession) scrollToSelection() {
	if qs.sel < qs.top {
		qs.top = qs.sel
	}
	if qs.sel > qs.top+qs.quoteRows-1 {
		qs.top = qs.sel - qs.quoteRows + 1
	}
	if qs.top < 0 {
		qs.top = 0
	}
}

// quoteSelected inserts the highlighted source line into the message and steps
// the lightbar to the next line, so holding SPACE walks a paragraph in.
func (qs *quoteSession) quoteSelected() {
	if qs.sel < 0 || qs.sel >= len(qs.src) {
		return
	}
	if !qs.ensureBlock() {
		qs.notifyFull()
		return
	}

	text := qs.ch.cleanQuoteLine(qs.src[qs.sel])
	wrapped := wrapQuoted(qs.prefix, text, MaxLineLength)

	written := 0
	for _, line := range wrapped {
		if !qs.insertBufferLine(qs.insertAt, line) {
			break
		}
		qs.insertAt++
		written++
	}
	if written == 0 {
		qs.notifyFull()
		return
	}

	qs.quoted[qs.sel]++
	qs.stack = append(qs.stack, quoteEntry{srcIdx: qs.sel, bufLines: written})

	if qs.sel < len(qs.src)-1 {
		qs.sel++
		qs.scrollToSelection()
	}
	qs.drawCompose(true)
	qs.drawQuotePane()
	if written < len(wrapped) {
		qs.notifyFull()
	}
}

// undoLast removes the most recently quoted line, and the banners with it when
// the block becomes empty.
func (qs *quoteSession) undoLast() {
	if len(qs.stack) == 0 {
		return
	}
	entry := qs.stack[len(qs.stack)-1]
	qs.stack = qs.stack[:len(qs.stack)-1]

	for i := 0; i < entry.bufLines; i++ {
		qs.ch.buffer.DeleteLine(qs.insertAt - 1)
		qs.insertAt--
	}
	qs.quoted[entry.srcIdx]--
	qs.sel = entry.srcIdx
	qs.scrollToSelection()

	if len(qs.stack) == 0 {
		// Only the two banners are left — drop the block entirely.
		qs.ch.buffer.DeleteLine(qs.blockStart) // "Said"
		qs.ch.buffer.DeleteLine(qs.blockStart) // "Done" shifted up into its place
		qs.blockStart = 0
		qs.insertAt = 0
	}

	qs.drawCompose(true)
	qs.drawQuotePane()
}

// ensureBlock lazily opens the "Said" / "Done" banner pair at the cursor line so
// an aborted quote leaves no empty block behind.
func (qs *quoteSession) ensureBlock() bool {
	if qs.blockStart != 0 {
		return true
	}
	top := qs.ch.processForBuffer(qs.ch.processQuoteCodes(qs.ch.quoteTopText()))
	bottom := qs.ch.processForBuffer(qs.ch.processQuoteCodes(qs.ch.quoteBottomText()))

	at := qs.origLine
	if at < 1 {
		at = 1
	}
	if !qs.insertBufferLine(at, top) {
		return false
	}
	if !qs.insertBufferLine(at+1, bottom) {
		qs.ch.buffer.DeleteLine(at)
		return false
	}
	qs.blockStart = at
	qs.insertAt = at + 1 // the "Done" banner; quoted lines push it down
	return true
}

// insertBufferLine inserts content as a new hard-broken line, never overwriting
// text the user already typed.
func (qs *quoteSession) insertBufferLine(at int, content string) bool {
	if at < 1 || at > MaxLines {
		return false
	}
	if at > qs.ch.buffer.GetLineCount() {
		// Appending past the end: grow the buffer one line at a time.
		if qs.ch.buffer.GetLineCount() >= MaxLines {
			return false
		}
		qs.ch.buffer.SetLine(at, content)
		qs.ch.buffer.SetHardNewline(at, true)
		return true
	}
	if !qs.ch.buffer.InsertLine(at) {
		return false
	}
	qs.ch.buffer.SetLine(at, content)
	qs.ch.buffer.SetHardNewline(at, true)
	return true
}

func (qs *quoteSession) notifyFull() {
	s := qs.ch.screen
	s.GoXY(1, s.PromptRow())
	s.ClearEOL()
	s.WriteDirectProcessed("|12Message is full — no room for more quoted lines.")
}

// drawCompose paints the message pane. When follow is true the view scrolls to
// keep the insertion point visible.
func (qs *quoteSession) drawCompose(follow bool) {
	s := qs.ch.screen
	focus := qs.origLine
	if qs.blockStart != 0 {
		focus = qs.insertAt
	}
	if follow {
		if focus < qs.composeTop {
			qs.composeTop = focus
		}
		if focus > qs.composeTop+qs.composeRows-1 {
			qs.composeTop = focus - qs.composeRows + 1
		}
	}
	if qs.composeTop < 1 {
		qs.composeTop = 1
	}

	lineCount := qs.ch.buffer.GetLineCount()
	for i := 0; i < qs.composeRows; i++ {
		s.GoXY(1, qs.composeY+i)
		s.WriteDirect(qpReset)
		if n := qs.composeTop + i; n <= lineCount {
			s.WriteDirect(truncRunes(qs.ch.buffer.GetLine(n), qs.width))
		}
		s.ClearEOL()
	}
	s.WriteDirect(qpReset)
}

// drawDivider paints the rule between the panes, carrying the key legend. The
// legend is trimmed from the right when the terminal is too narrow for it all.
func (qs *quoteSession) drawDivider() {
	s := qs.ch.screen

	name := qs.ch.quoteAuthor()
	label := " Quoting " + name + " "
	hints := []struct{ key, text string }{
		{"Up/Dn", "Pick"},
		{"SPACE", "Add"},
		{"BKSP", "Undo"},
		{"TAB", "Pane"},
		{"ESC", "Done"},
	}
	if qs.composeFocus {
		label = " Message "
		hints = []struct{ key, text string }{
			{"Up/Dn", "Scroll"},
			{"TAB", "Back"},
			{"ESC", "Done"},
		}
	}

	// Drop legend entries from the right until label + legend fit with a rule.
	legendWidth := func(n int) int {
		w := 0
		for i := 0; i < n; i++ {
			if i > 0 {
				w += 2
			}
			w += len(hints[i].key) + 1 + len(hints[i].text)
		}
		if w > 0 {
			w += 2 // padding either side
		}
		return w
	}
	labelRunes := runeLen(label)
	if labelRunes > qs.width-8 {
		label = truncRunes(label, qs.width-8)
		labelRunes = runeLen(label)
	}
	shown := len(hints)
	for shown > 0 && labelRunes+legendWidth(shown)+4 > qs.width {
		shown--
	}
	fill := qs.width - labelRunes - legendWidth(shown) - 2
	if fill < 0 {
		fill = 0
	}

	styledLabel := qpLabel + label
	if name != "" {
		styledLabel = qpLabel + strings.Replace(label, name, qpName+name+qpLabel, 1)
	}

	s.GoXY(1, qs.dividerY)
	s.WriteDirect(qpReset + qpRule + "─")
	s.WriteDirect(styledLabel)
	s.WriteDirect(qpRule + strings.Repeat("─", fill))
	if shown > 0 {
		s.WriteDirect(" ")
		for i := 0; i < shown; i++ {
			if i > 0 {
				s.WriteDirect("  ")
			}
			s.WriteDirect(qpKey + hints[i].key + qpLabel + " " + hints[i].text)
		}
		s.WriteDirect(" ")
	}
	s.WriteDirect(qpRule + "─")
	s.ClearEOL()
	s.WriteDirect(qpReset)
}

// drawQuotePane paints the source lines with the lightbar over the selection.
// Already-quoted lines are dimmed so progress through the message is visible.
func (qs *quoteSession) drawQuotePane() {
	s := qs.ch.screen
	const gutter = 4 // "NNN "
	textWidth := qs.width - gutter
	if textWidth < 1 {
		textWidth = 1
	}

	for i := 0; i < qs.quoteRows; i++ {
		idx := qs.top + i
		s.GoXY(1, qs.quoteY+i)
		s.WriteDirect(qpReset)

		if idx >= len(qs.src) {
			s.ClearEOL()
			continue
		}

		text := truncRunes(qs.ch.cleanQuoteLine(qs.src[idx]), textWidth)
		num := fmt.Sprintf("%3d ", idx+1)

		switch {
		case idx == qs.sel:
			// Paint the full row so the bar reads as one continuous block.
			s.WriteDirect(qpBarGutter + num + qpBar + padRunes(text, textWidth))
		case qs.quoted[idx] > 0:
			s.WriteDirect(qpGutter + num + qpUsed + text)
		default:
			s.WriteDirect(qpGutter + num + qpText + text)
		}
		s.WriteDirect(qpReset)
		s.ClearEOL()
	}
	s.WriteDirect(qpReset)
}

// quoteAuthor returns the display name of the message being quoted.
func (ch *CommandHandler) quoteAuthor() string {
	if ch.quoteData == nil {
		return ""
	}
	if ch.quoteData.IsAnon {
		return "Anonymous"
	}
	return ch.quoteData.From
}

// quotePrefix builds the per-line prefix from the configured template.
// ^I expands to the author's initials and ^N to their full name; a template with
// neither token is used verbatim, so a sysop can still choose a plain "> ".
func (ch *CommandHandler) quotePrefix() string {
	tmpl := ch.quotePrefixStr
	if tmpl == "" {
		tmpl = defaultQuotePrefix
	}
	tmpl = strings.ReplaceAll(tmpl, "^I", quoteInitials(ch.quoteAuthor()))
	return ch.processQuoteCodes(tmpl)
}

func (ch *CommandHandler) quoteTopText() string {
	if ch.quoteTopStr != "" {
		return ch.quoteTopStr
	}
	return defaultQuoteTop
}

func (ch *CommandHandler) quoteBottomText() string {
	if ch.quoteBottomStr != "" {
		return ch.quoteBottomStr
	}
	return defaultQuoteBottom
}

// cleanQuoteLine strips pipe codes, raw ANSI escapes and trailing whitespace so
// quoted text carries no formatting from the original message.
func (ch *CommandHandler) cleanQuoteLine(text string) string {
	return strings.TrimRight(stripANSI(ch.filterPipeCodes(text)), " \t\r")
}

// quoteInitials derives the FTN-style initials used in the "XX> " prefix:
// the first letters of the first two words, or the first two letters of a
// single-word handle ("Shurato" -> "Sh", "John Smith" -> "JS").
func quoteInitials(name string) string {
	fields := strings.Fields(name)
	switch {
	case len(fields) == 0:
		return ""
	case len(fields) >= 2:
		a := []rune(fields[0])
		b := []rune(fields[1])
		return string(unicode.ToUpper(a[0])) + string(unicode.ToUpper(b[0]))
	default:
		r := []rune(fields[0])
		if len(r) == 1 {
			return string(unicode.ToUpper(r[0]))
		}
		return string(unicode.ToUpper(r[0])) + string(unicode.ToLower(r[1]))
	}
}

// wrapQuoted breaks text into prefixed lines no wider than max, re-applying the
// prefix to every continuation line. Long unbreakable words are split rather
// than truncated, which is what the old range-based quoter did to them.
func wrapQuoted(prefix, text string, max int) []string {
	plen := runeLen(prefix)
	avail := max - plen
	if avail < 10 {
		avail = 10
	}

	if strings.TrimSpace(text) == "" {
		return []string{strings.TrimRight(prefix, " ")}
	}

	// The source line's own leading indent is dropped: it is almost always the
	// margin left by a previous round of quoting, and keeping it would stack an
	// extra space into the nesting on every reply ("Bu>  Sh> ...").
	var out []string
	cur := ""
	empty := true // cur holds no words yet, so the next one needs no space
	flush := func() {
		out = append(out, strings.TrimRight(prefix+cur, " "))
		cur = ""
		empty = true
	}

	for _, word := range strings.Fields(text) {
		for runeLen(word) > avail {
			// A word longer than the line (a URL, usually) starts on a line of
			// its own rather than being jammed onto the words already buffered,
			// then spills a full line at a time until the remainder fits.
			if !empty {
				flush()
			}
			chunk := truncRunes(word, avail)
			out = append(out, prefix+chunk)
			word = string([]rune(word)[runeLen(chunk):])
		}
		switch {
		case empty:
			cur += word
			empty = false
		case runeLen(cur)+1+runeLen(word) <= avail:
			cur += " " + word
		default:
			flush()
			cur = word
			empty = false
		}
	}
	if !empty {
		flush()
	}
	if len(out) == 0 {
		out = append(out, strings.TrimRight(prefix, " "))
	}
	return out
}

// prepareQuoteSource trims the message down to the part worth quoting: the FTN
// trailer (tagline, tearline, origin, SEEN-BY/PATH kludges) and surrounding
// blank runs are dropped, since nobody quotes them and they crowd the pane.
func prepareQuoteSource(lines []string) []string {
	end := len(lines)

	// Walk back from the end and cut at the first trailer line found. The
	// trailer is always last, so anything after the cut is trailer too.
	cut := end
	for i := end - 1; i >= 0; i-- {
		if isQuoteTrailerLine(lines[i]) {
			cut = i
		} else if strings.TrimSpace(lines[i]) != "" {
			break
		}
	}
	lines = lines[:cut]

	// Drop leading and trailing blank runs.
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	stop := len(lines)
	for stop > start && strings.TrimSpace(lines[stop-1]) == "" {
		stop--
	}

	out := make([]string, 0, stop-start)
	out = append(out, lines[start:stop]...)
	return out
}

// isQuoteTrailerLine reports whether a line belongs to the FTN trailer block.
func isQuoteTrailerLine(line string) bool {
	trimmed := strings.TrimRight(line, " \t\r")
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "\x01") { // kludge line
		return true
	}
	if strings.HasPrefix(trimmed, "SEEN-BY:") || strings.HasPrefix(trimmed, "@PATH:") {
		return true
	}
	if trimmed == "---" || strings.HasPrefix(trimmed, "--- ") { // tearline
		return true
	}
	if strings.HasPrefix(strings.TrimLeft(trimmed, " "), "* Origin:") {
		return true
	}
	if strings.HasPrefix(trimmed, "...") && runeLen(trimmed) > 3 {
		return true // tagline
	}
	return false
}

// stripANSI removes CSI escape sequences so raw ANSI in a message body cannot
// bleed colors into the quote pane or the composed reply.
func stripANSI(s string) string {
	if !strings.ContainsRune(s, 0x1B) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1B {
			i++
			if i < len(s) && s[i] == '[' {
				i++
				for i < len(s) && (s[i] < 0x40 || s[i] > 0x7E) {
					i++
				}
				if i < len(s) {
					i++ // final byte
				}
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func runeLen(s string) int {
	return len([]rune(s))
}

// truncRunes cuts a string to n display cells without splitting a rune.
func truncRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// padRunes pads a string with spaces to n cells, truncating if it is longer.
func padRunes(s string, n int) string {
	s = truncRunes(s, n)
	if pad := n - runeLen(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}
