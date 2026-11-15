package menu

import (
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Colours for the form. Values the caller typed are written raw, never
// through pipe-code expansion, so a "|" in a real name or note is shown as
// typed instead of being read as a colour code.
const (
	konfigReset      = "\x1b[0m"
	konfigBarStyle   = "\x1b[0;30;46m"   // highlighted row
	konfigFieldStyle = "\x1b[0;1;37;44m" // text being edited
)

// pc returns the SGR sequence for pipe colour n.
func pc(n int) string {
	return string(ansi.ReplacePipeCodes([]byte(fmt.Sprintf("|%02d", n))))
}

func toneColor(t konfigTone) string {
	switch t {
	case toneDim:
		return pc(8)
	case toneOn:
		return pc(10)
	case toneOff:
		return pc(12)
	}
	return pc(15)
}

// wideCells reports whether the caller's terminal draws a double-width glyph
// in two cells. A CP437 session has none: every character is one cell.
func (st *konfigState) wideCells() bool { return st.c.outputMode != ansi.OutputModeCP437 }

// cells is the number of terminal cells s takes.
func (st *konfigState) cells(s string) int {
	if st.wideCells() {
		return runewidth.StringWidth(s)
	}
	return utf8.RuneCountInString(s)
}

// clip cuts s to at most n cells, ending it with ellipsis when it had to cut.
func (st *konfigState) clip(s string, n int, ellipsis string) string {
	if st.wideCells() {
		return runewidth.Truncate(s, n, ellipsis)
	}
	return ansi.TruncateRunes(s, n, ellipsis)
}

// fit pads or cuts s to exactly n cells.
func (st *konfigState) fit(s string, n int) string {
	s = st.clip(s, n, "..")
	if pad := n - st.cells(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

func (st *konfigState) raw(s string) error {
	return terminalio.WriteProcessedBytes(st.c.terminal, []byte(s), st.c.outputMode)
}

func (st *konfigState) pipe(s string) error {
	return st.raw(string(ansi.ReplacePipeCodes([]byte(s))))
}

func moveTo(row, col int) string { return fmt.Sprintf("\x1b[%d;%dH", row, col) }

func clearRow(row int) string { return fmt.Sprintf("\x1b[%d;1H\x1b[2K", row) }

// renderAll repaints the whole screen.
func (st *konfigState) renderAll() error {
	if err := st.renderHeader(); err != nil {
		return err
	}
	var b strings.Builder
	for row := konfigHeaderRows + 1; row <= konfigLastRow; row++ {
		b.WriteString(clearRow(row))
	}
	for _, h := range st.headings {
		b.WriteString(moveTo(h.row, h.col) + st.heading(h.title))
	}
	for i, it := range st.items {
		b.WriteString(moveTo(it.row, it.col))
		b.WriteString(st.itemCell(it, i == st.sel))
	}
	b.WriteString(moveTo(konfigRuleRow, 1) + pc(8) + strings.Repeat("─", 79))
	b.WriteString(moveTo(konfigLegendRow, 2))
	b.WriteString(st.line(st.c.e.Strings().KonfigLegend) + konfigReset)
	if err := st.raw(b.String()); err != nil {
		return err
	}
	if err := st.renderHelp(); err != nil {
		return err
	}
	if err := st.renderStatus(); err != nil {
		return err
	}
	// The screens a field hands off to (the header picker, the message
	// editor) show the cursor again when they finish, so hide it on every
	// full repaint rather than only on the way in.
	return st.raw("\x1b[?25l")
}

// renderHeader clears the screen and draws KONFIG.ANS from the menu set,
// or a plain title when the set has none. The art should be at most
// konfigHeaderRows rows; the form is drawn below that. The art may use the
// common template tokens (|UH, |LEVEL, |NODE, |DATE and the rest).
func (st *konfigState) renderHeader() error {
	e := st.c.e
	if ok, _ := e.Menus().Exists("ansi", "KONFIG.ANS"); ok {
		data, err := ansi.GetAnsiFileContent(e.menuFile("ansi", "KONFIG.ANS"))
		if err == nil {
			// Settle the art's encoding before substituting, as displayFile
			// does, so a UTF-8 handle is not mistaken for CP437.
			data = ansi.ArtForOutput(data, st.c.outputMode)
			data = e.applyCommonTemplateTokens(data, st.user(), st.c.nodeNumber)
			data = ansi.ReplacePipeCodes(data)
			if err := st.raw(ansi.ClearScreen()); err != nil {
				return err
			}
			return writeArt(st.c.terminal, data, st.c.outputMode, st.c.termWidth)
		}
		slog.Warn("failed to read KONFIG.ANS", "node", st.c.nodeNumber, "error", err)
	}
	title := st.clip(st.c.e.Strings().KonfigTitle, konfigLineWidth, "..")
	return st.raw(ansi.ClearScreen() + moveTo(2, 2) + pc(15) + title +
		moveTo(3, 2) + pc(8) + strings.Repeat("─", st.cells(title)) + konfigReset)
}

// heading is a section title followed by a rule, konfigColWidth cells wide.
func (st *konfigState) heading(title string) string {
	title = st.clip(title, konfigColWidth-2, "..")
	return pc(11) + title + " " + pc(1) + strings.Repeat("─", konfigColWidth-st.cells(title)-1)
}

// itemCell is the fixed-width text for one item, so repainting it always
// overwrites whatever was there.
func (st *konfigState) itemCell(it *konfigItem, selected bool) string {
	v := it.value(st)
	label := st.fit(it.label, konfigLabelWidth)
	value := st.fit(v.text, konfigValueWidth)
	if selected {
		return konfigBarStyle + fmt.Sprintf("[%c] ", it.key) + label + value + konfigReset
	}
	return pc(8) + "[" + pc(15) + string(it.key) + pc(8) + "] " + pc(7) + label +
		toneColor(v.tone) + value + konfigReset
}

func (st *konfigState) renderItem(i int) error {
	it := st.items[i]
	return st.raw(moveTo(it.row, it.col) + st.itemCell(it, i == st.sel))
}

func (st *konfigState) renderHelp() error {
	return st.raw(clearRow(konfigHelpRow) + moveTo(konfigHelpRow, 2) +
		st.line("|07"+st.items[st.sel].help) + konfigReset)
}

// renderStatus shows st.status on the edit row, or clears it.
func (st *konfigState) renderStatus() error {
	if st.status == "" {
		return st.raw(clearRow(konfigEditRow))
	}
	return st.raw(clearRow(konfigEditRow) + moveTo(konfigEditRow, 2) + st.line(st.status+"|07"))
}

// konfigLineWidth is the room on a row that starts at column 2.
const konfigLineWidth = 78

// line expands the pipe codes in a configured string and cuts it to the
// width of a row, so a long translation cannot wrap onto the next one.
func (st *konfigState) line(s string) string {
	return st.clipRow(string(ansi.ReplacePipeCodes([]byte(s))))
}

// clipRow cuts text whose codes are already expanded to the width of a row,
// keeping its escape sequences.
func (st *konfigState) clipRow(out string) string {
	var b strings.Builder
	w := 0
	for i := 0; i < len(out); {
		if out[i] == 0x1b {
			j := i + 1
			if j < len(out) && out[j] == '[' {
				for j++; j < len(out) && (out[j] < 0x40 || out[j] > 0x7e); j++ {
				}
			}
			if j < len(out) {
				j++
			}
			b.WriteString(out[i:j])
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(out[i:])
		rw := 1
		if st.wideCells() {
			rw = runewidth.RuneWidth(r)
		}
		if w+rw > konfigLineWidth {
			break
		}
		b.WriteString(out[i : i+size])
		w += rw
		i += size
	}
	return b.String()
}

func (st *konfigState) showCursor(on bool) {
	if on {
		_ = st.raw("\x1b[?25h")
	} else {
		_ = st.raw("\x1b[?25l")
	}
}

// konfigMinFieldWidth is the least room readField leaves for typing.
const konfigMinFieldWidth = 10

// readField edits a single line of text on the edit row, starting from
// initial. ok is false when the caller pressed Esc. mask shows the text as
// asterisks; hint is drawn after the box. A value longer than the room left
// on the row scrolls sideways to keep the cursor in view, so maxLen is never
// limited by the screen.
//
// Keys: printable and extended characters insert, Left/Right/Home/End move,
// Backspace and Delete remove, Ctrl-U or Ctrl-Y empty the field.
func (st *konfigState) readField(label, initial string, maxLen int, mask bool, hint string) (string, bool, error) {
	buf := []rune(ansi.TruncateRunes(initial, maxLen, ""))
	cur, off := len(buf), 0
	// A configured label can be any length; keep room on the row to type.
	label = st.clip(label, konfigLineWidth-2-konfigMinFieldWidth, "..")
	boxCol := 2 + st.cells(label) + 2
	width := maxLen + 1
	if room := 80 - boxCol; width > room {
		width = room
	}

	if err := st.raw(clearRow(konfigEditRow) + moveTo(konfigEditRow, 2) +
		pc(15) + label + pc(8) + ": "); err != nil {
		return "", false, err
	}
	if hint != "" {
		if err := st.pipe(moveTo(konfigEditRow, boxCol+width+1) + hint + "|07"); err != nil {
			return "", false, err
		}
	}

	// onEdit hears the text only when it changes, not on a cursor move.
	edited, lastText := false, ""
	draw := func() error {
		if text := string(buf); st.onEdit != nil && (!edited || text != lastText) {
			edited, lastText = true, text
			if err := st.onEdit(text); err != nil {
				return err
			}
		}
		if cur < off {
			off = cur
		}
		if cur-off >= width {
			off = cur - width + 1
		}
		end := off + width
		if end > len(buf) {
			end = len(buf)
		}
		shown := string(buf[off:end])
		if mask {
			shown = strings.Repeat("*", end-off)
		}
		return st.raw(moveTo(konfigEditRow, boxCol) + konfigFieldStyle + shown +
			strings.Repeat(" ", width-(end-off)) + konfigReset + moveTo(konfigEditRow, boxCol+cur-off))
	}

	st.showCursor(true)
	defer st.showCursor(false)
	if err := draw(); err != nil {
		return "", false, err
	}

	mode := sessionOutputMode(st.c.s)
	var pending []byte
	for {
		key, err := st.ih.ReadKey()
		if err != nil {
			return "", false, err
		}
		switch key {
		case editor.KeyEnter:
			return string(buf), true, nil
		case editor.KeyEsc:
			return "", false, nil
		case editor.KeyArrowLeft:
			if cur > 0 {
				cur--
			}
		case editor.KeyArrowRight:
			if cur < len(buf) {
				cur++
			}
		case editor.KeyHome:
			cur = 0
		case editor.KeyEnd:
			cur = len(buf)
		case editor.KeyBackspace, editor.KeyDelete:
			if cur > 0 {
				buf = append(buf[:cur-1], buf[cur:]...)
				cur--
			}
		case editor.KeyDeleteKey:
			if cur < len(buf) {
				buf = append(buf[:cur], buf[cur+1:]...)
			}
		case 0x15, editor.KeyCtrlY: // Ctrl-U, Ctrl-Y
			buf, cur = buf[:0], 0
		default:
			var r rune
			switch {
			case key >= 32 && key < 127:
				r, pending = rune(key), nil
			case key >= 128 && key <= 255:
				var line []byte
				line, _, pending = ansi.DecodeExtendedKey(nil, mode, byte(key), pending)
				if len(line) == 0 {
					continue
				}
				r, _ = utf8.DecodeRune(line)
			default:
				pending = nil
				continue
			}
			if len(buf) >= maxLen {
				continue
			}
			buf = append(buf[:cur], append([]rune{r}, buf[cur:]...)...)
			cur++
		}
		if err := draw(); err != nil {
			return "", false, err
		}
	}
}

// readChoice shows prompt on the edit row and waits for one of keys (upper
// case). It returns 0 for Esc, Enter or Q.
func (st *konfigState) readChoice(prompt, keys string) (byte, error) {
	if err := st.raw(clearRow(konfigEditRow) + moveTo(konfigEditRow, 2) + st.line(prompt+"|07")); err != nil {
		return 0, err
	}
	for {
		key, err := st.ih.ReadKey()
		if err != nil {
			return 0, err
		}
		if key == editor.KeyEsc || key == editor.KeyEnter || key == 'q' || key == 'Q' {
			return 0, nil
		}
		if key > 0 && key < 128 {
			k := strings.ToUpper(string(rune(key)))
			if strings.Contains(keys, k) {
				return k[0], nil
			}
		}
	}
}

// File-column box geometry: drawn over the middle of the form.
const (
	colBoxTop   = konfigTopRow
	colBoxLeft  = 22
	colBoxWidth = 36 // including borders
)

// editFileColumns opens a box of switches, one per file-listing column.
// Each switch is saved as it is flipped.
func editFileColumns(st *konfigState) error {
	st.redraw = true
	sel := 0
	inner := colBoxWidth - 2

	line := func(row int, s string) string {
		return moveTo(row, colBoxLeft) + pc(9) + "│" + s + pc(9) + "│"
	}
	str := st.c.e.Strings()
	cell := func(i int) string {
		fc := fileColumns[i]
		u := st.user()
		on := fileColumnsAllDefault(u) || *fc.get(u)
		v := st.onOffValue(on)
		label := st.fit(fc.label(str), 14)
		if i == sel {
			return konfigBarStyle + st.fit(fmt.Sprintf(" [%c] %s%s", fc.key, label, v.text), inner) + konfigReset
		}
		return pc(8) + " [" + pc(15) + string(fc.key) + pc(8) + "] " + pc(7) +
			label + toneColor(v.tone) + st.fit(v.text, inner-20) + konfigReset
	}
	drawItem := func(i int) error {
		return st.raw(line(colBoxTop+1+i, cell(i)))
	}
	title := " " + st.clip(str.KonfigColumnsTitle, inner-3, "..") + " "
	rule := strings.Repeat("─", inner-st.cells(title)-1)
	var b strings.Builder
	b.WriteString(moveTo(colBoxTop, colBoxLeft) + pc(9) + "┌─" + pc(15) + title + pc(9) + rule + "┐")
	for i := range fileColumns {
		b.WriteString(line(colBoxTop+1+i, cell(i)))
	}
	hintRow := colBoxTop + 1 + len(fileColumns)
	b.WriteString(line(hintRow, konfigReset+strings.Repeat(" ", inner)))
	b.WriteString(line(hintRow+1, pc(8)+st.fit(" "+str.KonfigColumnsHint, inner)))
	b.WriteString(moveTo(hintRow+2, colBoxLeft) + pc(9) + "└" + strings.Repeat("─", inner) + "┘" + konfigReset)
	if err := st.raw(b.String()); err != nil {
		return err
	}

	flip := func(i int) {
		u := st.user()
		old := u.FileListColumns
		all := fileColumnsAllDefault(u)
		shown := make([]bool, len(fileColumns))
		count := 0
		for j, fc := range fileColumns {
			shown[j] = all || *fc.get(u)
			if j == i {
				shown[j] = !shown[j]
			}
			if shown[j] {
				count++
			}
		}
		if count == 0 {
			st.status = str.KonfigColumnsKeepOne
			return
		}
		if st.commit(str.KonfigFileColumnsLabel,
			func(u *user.User) {
				for j, fc := range fileColumns {
					*fc.get(u) = shown[j]
				}
			},
			func(u *user.User) { u.FileListColumns = old }) {
			st.status = fmt.Sprintf(st.c.e.Strings().KonfigColumnSaved, fileColumns[i].label(str), st.onOffValue(shown[i]).text)
		}
	}

	for {
		key, err := st.ih.ReadKey()
		if err != nil {
			return err
		}
		st.status = ""
		switch key {
		case editor.KeyEsc, 'q', 'Q':
			return nil
		case editor.KeyArrowUp:
			sel = (sel + len(fileColumns) - 1) % len(fileColumns)
		case editor.KeyArrowDown:
			sel = (sel + 1) % len(fileColumns)
		case editor.KeyEnter, ' ':
			flip(sel)
		default:
			if key > 0 && key < 128 {
				k := strings.ToUpper(string(rune(key)))
				for i, fc := range fileColumns {
					if string(fc.key) == k {
						sel = i
						flip(i)
					}
				}
			}
		}
		// A flip can change every row (leaving the all-default state turns
		// each column on explicitly), so repaint them all.
		for i := range fileColumns {
			if err := drawItem(i); err != nil {
				return err
			}
		}
		if err := st.renderStatus(); err != nil {
			return err
		}
	}
}

// colorsKonfigKey is the Colours item's hotkey.
const colorsKonfigKey = 'U'

// colorsValue is "default" until the caller sets a colour slot, then "custom".
func colorsValue(st *konfigState) konfigValue {
	str := st.c.e.Strings()
	for n := 1; n <= user.ColorSlots; n++ {
		if st.user().ColorCode(n) != "" {
			return konfigValue{str.KonfigColorsCustom, toneValue}
		}
	}
	return konfigValue{str.KonfigColorsDefault, toneDim}
}

// Colours box geometry: drawn over the form, wide enough for a row of the
// sixteen foreground swatches.
const (
	colorBoxSwatch = 4
	colorBoxWidth  = 16*colorBoxSwatch + 4 // including borders
	colorBoxLeft   = (80-colorBoxWidth)/2 + 1
	colorBoxTop    = konfigTopRow
	colorNoBG      = -1
)

// colorChoice is a colour being picked in the box: a foreground 0-15 and a
// background 0-7 or colorNoBG, with row saying which of the two the arrows
// move. It works as the TUI editors' colorpick.Picker does, drawn with pipe
// codes for a caller's terminal.
type colorChoice struct {
	fg, bg int
	bgRow  bool
}

// colorChoiceOf reads a slot's pipe code (see user.ValidColorCode).
func colorChoiceOf(code string) colorChoice {
	ch := colorChoice{bg: colorNoBG}
	if len(code) >= 3 {
		ch.fg = int(code[1]-'0')*10 + int(code[2]-'0')
	}
	if len(code) == 6 {
		ch.bg = int(code[5] - '0')
	}
	return ch
}

// code is the choice as a slot's pipe code.
func (ch colorChoice) code() string {
	s := fmt.Sprintf("|%02d", ch.fg)
	if ch.bg != colorNoBG {
		s += fmt.Sprintf("|B%d", ch.bg)
	}
	return s
}

// move steps the choice along its row, wrapping at the ends.
func (ch *colorChoice) move(d int) {
	if ch.bgRow {
		ch.bg = (ch.bg+1+d+9)%9 - 1 // colorNoBG, then 0-7
		return
	}
	ch.fg = (ch.fg + d + 16) % 16
}

// editColors opens a box of the caller's colour slots, |C1 to |C7, each drawn
// in its colour. Arrows pick a slot and its colour, Tab moves between the
// foreground and background rows, Enter saves the slot, R puts it back to the
// board's default.
func editColors(st *konfigState) error {
	st.redraw = true
	str := st.c.e.Strings()
	names := strings.Split(str.KonfigColorSlotNames, ",")
	for len(names) < user.ColorSlots {
		names = append(names, fmt.Sprintf("C%d", len(names)+1))
	}
	inner := colorBoxWidth - 2
	sel := 0
	ch := colorChoiceOf(st.c.e.colorSlot(st.user(), 1))
	pick := func(i int) {
		sel = i
		ch = colorChoiceOf(st.c.e.colorSlot(st.user(), i+1))
	}

	line := func(row int, s string) string {
		return moveTo(row, colorBoxLeft) + pc(9) + "│" + s + pc(9) + "│"
	}
	expand := func(s string) string { return string(ansi.ReplacePipeCodes([]byte(s))) }
	slotRow := func(i int) string {
		code := st.c.e.colorSlot(st.user(), i+1)
		if i == sel {
			code = ch.code()
		}
		set := pc(8) + st.fit(str.KonfigColorsDefault, 9)
		if st.user().ColorCode(i+1) != "" {
			set = pc(7) + st.fit(str.KonfigColorsCustom, 9)
		}
		label := st.fit(strings.TrimSpace(names[i]), 10)
		sample := expand(code) + " " + st.fit(str.KonfigColorsSample, 16) + " " + konfigReset
		head := pc(8) + fmt.Sprintf(" [%s%d%s] ", pc(15), i+1, pc(8)) + pc(7) + label
		if i == sel {
			head = konfigBarStyle + fmt.Sprintf(" [%d] ", i+1) + label + konfigReset
		}
		used := 5 + 10 + 2 + 18 + 9
		return head + "  " + sample + set + konfigReset + strings.Repeat(" ", inner-used)
	}
	bracket := func(on, active bool, body string) string {
		l, r := " ", " "
		if on {
			l, r = "[", "]"
		}
		col := pc(8)
		if active {
			col = pc(15)
		}
		return col + l + body + col + r
	}
	fgRow := func() string {
		var b strings.Builder
		b.WriteString(" ")
		for n := 0; n < 16; n++ {
			sw := expand(fmt.Sprintf("|%02d", n)) + "██"
			if n == 0 {
				sw = expand("|00|B7") + "██" + konfigReset
			}
			b.WriteString(bracket(n == ch.fg, !ch.bgRow, sw))
		}
		return b.String() + konfigReset + " "
	}
	bgRow := func() string {
		var b strings.Builder
		b.WriteString(" ")
		b.WriteString(bracket(ch.bg == colorNoBG, ch.bgRow, pc(7)+st.fit(str.KonfigColorsNoBG, 2)))
		for n := 0; n < 8; n++ {
			b.WriteString(bracket(n == ch.bg, ch.bgRow, expand(fmt.Sprintf("|15|B%d", n))+"  "+konfigReset))
		}
		return b.String() + konfigReset + strings.Repeat(" ", inner-1-9*colorBoxSwatch)
	}

	top := colorBoxTop
	pickRow := top + 1 + user.ColorSlots + 1
	hintRow := pickRow + 2
	drawSlots := func() string {
		var b strings.Builder
		for i := 0; i < user.ColorSlots; i++ {
			b.WriteString(line(top+1+i, slotRow(i)))
		}
		return b.String()
	}
	drawPicker := func() string {
		return line(pickRow, fgRow()) + line(pickRow+1, bgRow())
	}
	title := " " + st.clip(str.KonfigColorsTitle, inner-3, "..") + " "
	var b strings.Builder
	b.WriteString(moveTo(top, colorBoxLeft) + pc(9) + "┌─" + pc(15) + title + pc(9) + strings.Repeat("─", inner-st.cells(title)-1) + "┐")
	b.WriteString(drawSlots())
	b.WriteString(line(top+1+user.ColorSlots, konfigReset+strings.Repeat(" ", inner)))
	b.WriteString(drawPicker())
	b.WriteString(line(hintRow, pc(8)+st.fit(" "+str.KonfigColorsHint, inner)))
	b.WriteString(moveTo(hintRow+1, colorBoxLeft) + pc(9) + "└" + strings.Repeat("─", inner) + "┘" + konfigReset)
	if err := st.raw(b.String()); err != nil {
		return err
	}

	save := func(code string) {
		name := strings.TrimSpace(names[sel])
		old := append([]string(nil), st.user().ColorCodes...)
		if !st.commit(str.KonfigColorsLabel,
			func(u *user.User) { u.SetColorCode(sel+1, code) },
			func(u *user.User) { u.ColorCodes = old }) {
			return
		}
		if code == "" {
			st.status = fmt.Sprintf(str.KonfigColorReset, name)
			pick(sel)
		} else {
			st.status = fmt.Sprintf(str.KonfigColorSaved, name)
		}
	}

	for {
		key, err := st.ih.ReadKey()
		if err != nil {
			return err
		}
		st.status = ""
		switch key {
		case editor.KeyEsc, 'q', 'Q':
			return nil
		case editor.KeyArrowUp:
			pick((sel + user.ColorSlots - 1) % user.ColorSlots)
		case editor.KeyArrowDown:
			pick((sel + 1) % user.ColorSlots)
		case editor.KeyArrowLeft:
			ch.move(-1)
		case editor.KeyArrowRight:
			ch.move(1)
		case editor.KeyTab:
			ch.bgRow = !ch.bgRow
		case editor.KeyEnter, ' ':
			save(ch.code())
		case 'r', 'R':
			save("")
		default:
			if key >= '1' && key < '1'+user.ColorSlots {
				pick(key - '1')
			}
		}
		if err := st.raw(drawSlots() + drawPicker()); err != nil {
			return err
		}
		if err := st.renderStatus(); err != nil {
			return err
		}
	}
}
