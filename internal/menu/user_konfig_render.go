package menu

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/scripting"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Colours for the form. Values the caller typed are written raw, never
// through pipe-code expansion, so a "|" in a real name or note is shown as
// typed instead of being read as a colour code.
const (
	konfigReset      = "\x1b[0m"
	konfigFieldStyle = "\x1b[0;1;37;44m" // text being edited
)

// barStyle is the highlighted row's colour: the board's Bar colour slot,
// |C7, so the form follows the board's default (or an older strings.json's
// defColor7) like any other highlighted choice.
func (st *konfigState) barStyle() string {
	return konfigReset + string(ansi.ReplacePipeCodes([]byte(st.c.e.colorSlot(nil, user.ColorSlots))))
}

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

// cells is the number of terminal cells s takes: by grapheme cluster on a
// terminal with wide cells, as uniseg measures them, so an emoji with VS16
// or a joined emoji counts as the terminal draws it.
func (st *konfigState) cells(s string) int {
	if st.wideCells() {
		return uniseg.StringWidth(s)
	}
	return utf8.RuneCountInString(s)
}

// clip cuts s to at most n cells, ending it with ellipsis when it had to
// cut. It cuts between whole characters (grapheme clusters).
func (st *konfigState) clip(s string, n int, ellipsis string) string {
	if !st.wideCells() {
		return ansi.TruncateRunes(s, n, ellipsis)
	}
	if uniseg.StringWidth(s) <= n {
		return s
	}
	room := n - uniseg.StringWidth(ellipsis)
	if room < 0 {
		return ""
	}
	var b strings.Builder
	w := 0
	for rest := s; rest != ""; {
		var cluster string
		var cw int
		cluster, rest, cw, _ = uniseg.FirstGraphemeClusterInString(rest, -1)
		if w+cw > room {
			break
		}
		b.WriteString(cluster)
		w += cw
	}
	return b.String() + ellipsis
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

func moveTo(row, col int) string { return fmt.Sprintf("\x1b[%d;%dH", row, col) }

func clearRow(row int) string { return fmt.Sprintf("\x1b[%d;1H\x1b[2K", row) }

// renderAll repaints the whole screen.
func (st *konfigState) renderAll() error {
	if err := st.renderHeader(); err != nil {
		return err
	}
	var b strings.Builder
	for row := konfigHeaderRows + 1; row <= st.legendRow(); row++ {
		b.WriteString(clearRow(row))
	}
	for _, h := range st.headings {
		b.WriteString(moveTo(h.row, h.col) + st.heading(h.title))
	}
	for i, it := range st.items {
		b.WriteString(moveTo(it.row, it.col))
		b.WriteString(st.itemCell(it, i == st.sel))
	}
	b.WriteString(moveTo(st.ruleRow(), 1) + pc(8) + strings.Repeat("─", 79))
	b.WriteString(moveTo(st.legendRow(), 2))
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
	// Without art there is nothing else saying how to leave, the key
	// legend being blank by default: the title row says, at its right, as
	// the stock art does. The title is cut short of it.
	quit := ""
	if s := st.c.e.Strings().KonfigTitleQuit; s != "" {
		quit = st.line(s)
	}
	titleWidth, quitCol := konfigLineWidth, 0
	if quit != "" {
		// In screen cells, as clipRow measures, so a wide character counts
		// twice in UTF-8.
		quitCol = max(konfigScreenCols-st.cells(ansi.StripAnsi(quit)), 2)
		titleWidth = max(min(titleWidth, quitCol-3), 0) // a space before the hint
	}
	title := st.clip(st.c.e.Strings().KonfigTitle, titleWidth, "..")
	out := ansi.ClearScreen() + moveTo(2, 2) + pc(15) + title +
		moveTo(3, 2) + pc(8) + strings.Repeat("─", st.cells(title)) + konfigReset
	if quit != "" {
		out += moveTo(2, quitCol) + quit + konfigReset
	}
	return st.raw(out)
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
		return st.barStyle() + fmt.Sprintf("[%c] ", it.key) + label + value + konfigReset
	}
	return pc(8) + "[" + pc(15) + string(it.key) + pc(8) + "] " + pc(7) + label +
		toneColor(v.tone) + value + konfigReset
}

func (st *konfigState) renderItem(i int) error {
	it := st.items[i]
	return st.raw(moveTo(it.row, it.col) + st.itemCell(it, i == st.sel))
}

func (st *konfigState) renderHelp() error {
	if st.helpRow() == 0 {
		return nil
	}
	return st.raw(clearRow(st.helpRow()) + moveTo(st.helpRow(), 2) +
		st.line("|07"+st.items[st.sel].help) + konfigReset)
}

// boxStatus is st.status as a box's blank row shows it, inner cells wide.
func (st *konfigState) boxStatus(inner int) string {
	text := ansi.StripAnsi(string(ansi.ReplacePipeCodes([]byte(st.status))))
	return konfigReset + pc(15) + st.fit(" "+text, inner)
}

// renderStatus shows st.status on the edit row, or clears it. On a screen
// a script draws it does nothing: the box editors that call it may cover
// the script's prompt row, and the status reaches the script as its
// notice when the editor returns.
func (st *konfigState) renderStatus() error {
	if st.place != nil || st.editRow() == 0 {
		return nil
	}
	if st.status == "" {
		return st.raw(clearRow(st.editRow()))
	}
	return st.raw(clearRow(st.editRow()) + moveTo(st.editRow(), 2) + st.line(st.status+"|07"))
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
func (st *konfigState) clipRow(out string) string { return st.clipCells(out, konfigLineWidth) }

// clipCells cuts text whose codes are already expanded to n screen cells,
// keeping its escape sequences.
func (st *konfigState) clipCells(out string, n int) string {
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
		// A whole character (grapheme cluster) at a time; a control
		// character such as ESC always starts a new one.
		cluster, _, cw, _ := uniseg.FirstGraphemeClusterInString(out[i:], -1)
		if !st.wideCells() {
			cw = utf8.RuneCountInString(cluster)
		}
		if w+cw > n {
			break
		}
		b.WriteString(cluster)
		w += cw
		i += len(cluster)
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

// minScriptField is the narrowest script field readField edits in place:
// the widest character the terminal draws and the cursor after it. On a
// terminal with wide cells the widest is four (U+2E3B, as uniseg measures
// it); on CP437 every character is one.
func (st *konfigState) minScriptField() int {
	if st.wideCells() {
		return 5
	}
	return 2
}

// konfigMinFieldWidth is the least room readField leaves for typing.
const konfigMinFieldWidth = 10

// readField edits a single line of text on the edit row, or in the field a
// script gave for the setting (st.place) with the label on the edit row,
// starting from initial. ok is false when the caller pressed Esc. mask
// shows the text as asterisks; hint is drawn after the box, or after the
// label when the box is in the script's field. A value longer than the room left
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
	if err := st.ensurePromptRow(); err != nil {
		return "", false, err
	}
	f := st.scriptField()
	if f != nil && st.fieldWidth(f, maxLen) < st.minScriptField() {
		// Too narrow to show any character and the cursor: edit on the
		// prompt row instead.
		f = nil
		if st.editRow() == 0 {
			if err := st.promptOnClearScreen(); err != nil {
				return "", false, err
			}
		}
	}
	if f != nil && st.editRow() == 0 && !st.fitsBesideField(f, label, hint, maxLen) {
		// No prompt row, and no room for the label left of the field or
		// for the hint right of it.
		if err := st.promptOnClearScreen(); err != nil {
			return "", false, err
		}
		f = nil
	}
	boxRow, boxCol := st.editRow(), 2+st.cells(label)+2
	width := maxLen + 1
	if room := 80 - boxCol; width > room {
		width = room
	}
	hintRow, hintCol := boxRow, boxCol+width+1

	if f != nil {
		// The script shows the field; the label and hint go on the prompt
		// row, or without one just left of the field.
		boxRow, boxCol = f.Y, f.X
		width = st.fieldWidth(f, maxLen)
		hintRow, hintCol = st.editRow(), 2+st.textCells(label)+3
		if hintRow == 0 && hint != "" {
			hintCol = boxCol + width + 1
			text := string(ansi.ReplacePipeCodes([]byte(hint)))
			if err := st.raw(moveTo(boxRow, hintCol) + text + konfigReset); err != nil {
				return "", false, err
			}
		}
		if hintRow == 0 {
			// Beside the field. A longer label drawn there by an earlier
			// prompt of the same editor is blanked where this one is shorter.
			from := boxCol - st.textCells(label) - 2
			blank := ""
			if prev := st.place.labelFrom; prev > 0 && prev < from {
				blank = moveTo(boxRow, prev) + konfigReset + strings.Repeat(" ", from-prev)
			}
			if st.place.labelFrom == 0 || from < st.place.labelFrom {
				st.place.labelFrom = from
			}
			if err := st.raw(blank + moveTo(boxRow, from) +
				pc(15) + label + pc(8) + ": "); err != nil {
				return "", false, err
			}
		}
	}
	if hintRow > 0 {
		if err := st.raw(clearRow(hintRow) + moveTo(hintRow, 2) +
			pc(15) + label + pc(8) + ": "); err != nil {
			return "", false, err
		}
		if room := st.width() - hintCol + 1; hint != "" && room > 0 {
			// Clipped to the row, so a long translation cannot wrap onto
			// the next one.
			text := st.clipCells(string(ansi.ReplacePipeCodes([]byte(hint))), room)
			if err := st.raw(moveTo(hintRow, hintCol) + text + konfigReset); err != nil {
				return "", false, err
			}
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
		var shown string
		var used, at int
		off, shown, used, at = st.fieldView(buf, cur, off, width, mask)
		return st.raw(moveTo(boxRow, boxCol) + konfigFieldStyle + shown +
			strings.Repeat(" ", width-used) + konfigReset + moveTo(boxRow, boxCol+at))
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
		// Moves and deletions take a whole character as fieldView draws
		// it, so the cursor is never inside one.
		case editor.KeyArrowLeft:
			if cur > 0 {
				cur, _ = st.fieldUnitAt(buf, cur-1, mask)
			}
		case editor.KeyArrowRight:
			if cur < len(buf) {
				_, cur = st.fieldUnitAt(buf, cur, mask)
			}
		case editor.KeyHome:
			cur = 0
		case editor.KeyEnd:
			cur = len(buf)
		case editor.KeyBackspace, editor.KeyDelete:
			if cur > 0 {
				from, _ := st.fieldUnitAt(buf, cur-1, mask)
				buf = append(buf[:from], buf[cur:]...)
				cur = st.unitStart(buf, from, mask)
			}
		case editor.KeyDeleteKey:
			if cur < len(buf) {
				_, to := st.fieldUnitAt(buf, cur, mask)
				buf = append(buf[:cur], buf[to:]...)
				cur = st.unitStart(buf, cur, mask)
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
			// A rune that joins the character after it (a regional
			// indicator before another) leaves cur inside one; put the
			// cursor after the whole character.
			if from, to := st.fieldUnitAt(buf, cur, mask); from < cur {
				cur = to
			}
		}
		if err := draw(); err != nil {
			return "", false, err
		}
	}
}

// fieldWidth is the width of the box readField draws in field f for a
// value of up to maxLen characters.
func (st *konfigState) fieldWidth(f *scripting.FieldPos, maxLen int) int {
	width := maxLen + 1
	if f.Width > 0 {
		width = f.Width
	}
	return max(min(width, st.width()-f.X+1), 1)
}

// fitsBesideField reports whether label fits left of field f and hint, if
// any, right of its box, for an editor with no prompt row.
func (st *konfigState) fitsBesideField(f *scripting.FieldPos, label, hint string, maxLen int) bool {
	if f.X-1 < st.textCells(label)+2 {
		return false
	}
	if hint == "" {
		return true
	}
	room := st.width() - (f.X + st.fieldWidth(f, maxLen) + 1) + 1
	return st.cells(ansi.StripAnsi(string(ansi.ReplacePipeCodes([]byte(hint))))) <= room
}

// fieldUnit is one character as a field draws it: the runes from..to of
// the text, taking cells cells.
type fieldUnit struct{ from, to, cells int }

// fieldUnits splits text into what the terminal draws as one character: a
// grapheme cluster on a terminal with wide cells, otherwise (CP437, or a
// masked field) each rune in one cell.
func (st *konfigState) fieldUnits(text []rune, mask bool) []fieldUnit {
	units := make([]fieldUnit, 0, len(text))
	if mask || !st.wideCells() {
		for i := range text {
			units = append(units, fieldUnit{i, i + 1, 1})
		}
		return units
	}
	rest, at := string(text), 0
	for rest != "" {
		var cluster string
		var w int
		cluster, rest, w, _ = uniseg.FirstGraphemeClusterInString(rest, -1)
		n := utf8.RuneCountInString(cluster)
		units = append(units, fieldUnit{at, at + n, w})
		at += n
	}
	return units
}

// fieldUnitAt is the runes from..to of the character holding rune i of
// text, as fieldUnits splits it.
func (st *konfigState) fieldUnitAt(text []rune, i int, mask bool) (from, to int) {
	for _, u := range st.fieldUnits(text, mask) {
		if i < u.to {
			return u.from, u.to
		}
	}
	return len(text), len(text)
}

// unitStart is i, or the start of the character holding it when a
// deletion has joined the characters either side of i into one (two
// regional indicators that were apart make a flag).
func (st *konfigState) unitStart(text []rune, i int, mask bool) int {
	if from, _ := st.fieldUnitAt(text, i, mask); from < i {
		return from
	}
	return i
}

// textCells is the cells s takes as a field measures it (fieldUnits), so a
// label drawn beside a field is measured as the field's own text is.
func (st *konfigState) textCells(s string) int {
	n := 0
	for _, u := range st.fieldUnits([]rune(s), false) {
		n += u.cells
	}
	return n
}

// fieldView is what a field width cells wide shows of text with the cursor
// at rune cur and the view starting at rune off: the new off, scrolled so
// the text before the cursor and the cursor's own cell fit; the text shown
// (asterisks when masked) and the cells it takes; and the cursor's cell
// offset in the field. Whole characters are shown, so the text shown and
// the cursor agree on where each one is.
func (st *konfigState) fieldView(text []rune, cur, off, width int, mask bool) (newOff int, shown string, used, at int) {
	units := st.fieldUnits(text, mask)
	// unitAt is the unit holding rune i, or len(units) at the end.
	unitAt := func(i int) int {
		for u, un := range units {
			if i < un.to {
				return u
			}
		}
		return len(units)
	}
	span := func(a, b int) int {
		n := 0
		for _, un := range units[a:b] {
			n += un.cells
		}
		return n
	}
	ci, oi := unitAt(cur), unitAt(off)
	if ci < oi {
		oi = ci
	}
	// The cursor's cell: the whole character under it, or one at the end.
	cursorCells := 1
	if ci < len(units) {
		cursorCells = max(units[ci].cells, 1)
	}
	for oi < ci && span(oi, ci)+cursorCells > width {
		oi++
	}
	ei := oi
	for ei < len(units) && used+units[ei].cells <= width {
		used += units[ei].cells
		ei++
	}
	newOff = len(text)
	if oi < len(units) {
		newOff = units[oi].from
	}
	if ei > oi {
		shown = string(text[units[oi].from:units[ei-1].to])
		if mask {
			shown = strings.Repeat("*", ei-oi)
		}
	}
	return newOff, shown, used, span(oi, ci)
}

// readChoice shows prompt on the edit row, or at the script's field for
// the setting when there is no edit row and the whole prompt fits there,
// and waits for one of keys (upper case). It returns 0 for Esc, Enter or Q.
func (st *konfigState) readChoice(prompt, keys string) (byte, error) {
	if err := st.ensurePromptRow(); err != nil {
		return 0, err
	}
	if f := st.scriptField(); f != nil && st.editRow() == 0 {
		room := st.width() - f.X + 1
		if f.Width > 0 {
			room = min(room, f.Width)
		}
		text := string(ansi.ReplacePipeCodes([]byte(prompt + "|07")))
		if n := st.cells(ansi.StripAnsi(text)); n <= room {
			// Blank the rest of the field, so none of the value the
			// script drew there shows after the choice.
			if err := st.raw(moveTo(f.Y, f.X) + text + konfigReset + strings.Repeat(" ", room-n)); err != nil {
				return 0, err
			}
			return st.readChoiceKey(keys)
		}
		if err := st.promptOnClearScreen(); err != nil {
			return 0, err
		}
	}
	if err := st.raw(clearRow(st.editRow()) + moveTo(st.editRow(), 2) + st.line(prompt+"|07")); err != nil {
		return 0, err
	}
	return st.readChoiceKey(keys)
}

// readChoiceKey waits for one of keys (upper case), 0 for Esc, Enter or Q.
func (st *konfigState) readChoiceKey(keys string) (byte, error) {
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

// File-column box width, borders included. The box is centred on the
// screen (boxAt).
const colBoxWidth = 36

// editFileColumns opens a box of switches, one per file-listing column.
// Each switch is saved as it is flipped.
func editFileColumns(st *konfigState) error {
	st.redraw = true
	sel := 0
	inner := colBoxWidth - 2
	colBoxTop, colBoxLeft := st.boxAt(colBoxWidth, len(fileColumns)+4)

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
			return st.barStyle() + st.fit(fmt.Sprintf(" [%c] %s%s", fc.key, label, v.text), inner) + konfigReset
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
		if st.place != nil {
			// A script draws the screen: the status goes in the box.
			if err := st.raw(line(hintRow, st.boxStatus(inner))); err != nil {
				return err
			}
		} else if err := st.renderStatus(); err != nil {
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
	colorNoBG      = -1
	colorNoBGWidth = 4 // the "none" background label, between its brackets
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
	if len(code) > 4 {
		if n, err := strconv.Atoi(code[5:]); err == nil {
			ch.bg = n
		}
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
	// Title, the slots, a blank row, the two picker rows, the hint and the
	// bottom edge.
	colorBoxTop, colorBoxLeft := st.boxAt(colorBoxWidth, user.ColorSlots+6)
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
			head = st.barStyle() + fmt.Sprintf(" [%d] ", i+1) + label + konfigReset
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
				// Black as a shade over grey: a solid block of it would be
				// a gap on a black screen.
				sw = expand("|00|B7") + "▒▒" + konfigReset
			}
			b.WriteString(bracket(n == ch.fg, !ch.bgRow, sw))
		}
		return b.String() + konfigReset + " "
	}
	bgRow := func() string {
		var b strings.Builder
		b.WriteString(" ")
		b.WriteString(bracket(ch.bg == colorNoBG, ch.bgRow, pc(7)+st.fit(str.KonfigColorsNoBG, colorNoBGWidth)))
		for n := 0; n < 8; n++ {
			b.WriteString(bracket(n == ch.bg, ch.bgRow, expand(fmt.Sprintf("|15|B%d", n))+"  "+konfigReset))
		}
		return b.String() + konfigReset + strings.Repeat(" ", inner-1-(colorNoBGWidth+2)-8*colorBoxSwatch)
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
			// The board's own default is saved as "not set", so the caller
			// follows it if the sysop changes it.
			code := ch.code()
			if code == st.c.e.colorSlot(nil, sel+1) {
				code = ""
			}
			save(code)
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
		if st.place != nil {
			// A script draws the screen: the status goes in the box.
			if err := st.raw(line(top+1+user.ColorSlots, st.boxStatus(inner))); err != nil {
				return err
			}
		} else if err := st.renderStatus(); err != nil {
			return err
		}
	}
}
