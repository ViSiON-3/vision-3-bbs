package menu

import (
	"fmt"
	"strings"
	"unicode/utf8"

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

// padRunes pads or truncates s to exactly n columns.
func padRunes(s string, n int) string {
	s = ansi.TruncateRunes(s, n, "..")
	if pad := n - utf8.RuneCountInString(s); pad > 0 {
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
	for row := konfigTopRow; row <= konfigLastRow; row++ {
		b.WriteString(clearRow(row))
	}
	for _, h := range st.headings {
		b.WriteString(moveTo(h.row, h.col))
		b.WriteString(pc(11) + h.title + " " + pc(1))
		b.WriteString(strings.Repeat("─", konfigColWidth-utf8.RuneCountInString(h.title)-1))
	}
	for i, it := range st.items {
		b.WriteString(moveTo(it.row, it.col))
		b.WriteString(st.itemCell(it, i == st.sel))
	}
	b.WriteString(moveTo(konfigRuleRow, 1) + pc(8) + strings.Repeat("─", 79))
	b.WriteString(moveTo(konfigLegendRow, 2))
	b.WriteString(pc(15) + "Arrows" + pc(7) + " Move   " +
		pc(15) + "Enter" + pc(7) + " Change   " +
		pc(15) + "A" + pc(8) + "-" + pc(15) + "L" + pc(7) + " Go straight to a setting   " +
		pc(15) + "Q" + pc(7) + " Done" + konfigReset)
	if err := st.raw(b.String()); err != nil {
		return err
	}
	if err := st.renderHelp(); err != nil {
		return err
	}
	return st.renderStatus()
}

// renderHeader clears the screen and draws USERCFG.ANS from the menu set,
// or a plain title when the set has none. The art should be at most
// konfigHeaderRows rows; the form is drawn below that.
func (st *konfigState) renderHeader() error {
	e := st.c.e
	if ok, _ := e.Menus().Exists("ansi", "USERCFG.ANS"); ok {
		if err := e.displayFile(st.c.terminal, "USERCFG.ANS", st.c.outputMode, st.c.termWidth, st.c.termHeight, true); err == nil {
			return nil
		}
	}
	return st.raw(ansi.ClearScreen() + moveTo(2, 2) + pc(15) + "User Konfig" +
		moveTo(3, 2) + pc(8) + strings.Repeat("─", 11) + konfigReset)
}

// itemCell is the fixed-width text for one item, so repainting it always
// overwrites whatever was there.
func (st *konfigState) itemCell(it *konfigItem, selected bool) string {
	v := it.value(st)
	label := padRunes(it.label, konfigLabelWidth)
	value := padRunes(v.text, konfigValueWidth)
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
	return st.raw(clearRow(konfigHelpRow) + moveTo(konfigHelpRow, 2) + pc(7) +
		st.items[st.sel].help + konfigReset)
}

// renderStatus shows st.status on the edit row, or clears it.
func (st *konfigState) renderStatus() error {
	if st.status == "" {
		return st.raw(clearRow(konfigEditRow))
	}
	return st.pipe(clearRow(konfigEditRow) + moveTo(konfigEditRow, 2) + st.status + "|07")
}

func (st *konfigState) showCursor(on bool) {
	if on {
		_ = st.raw("\x1b[?25h")
	} else {
		_ = st.raw("\x1b[?25l")
	}
}

// readField edits a single line of text on the edit row, starting from
// initial. ok is false when the caller pressed Esc. mask shows the text as
// asterisks; hint is drawn after the box.
//
// Keys: printable and extended characters insert, Left/Right/Home/End move,
// Backspace and Delete remove, Ctrl-U or Ctrl-Y empty the field.
func (st *konfigState) readField(label, initial string, maxLen int, mask bool, hint string) (string, bool, error) {
	buf := []rune(ansi.TruncateRunes(initial, maxLen, ""))
	cur := len(buf)
	boxCol := 2 + utf8.RuneCountInString(label) + 2
	width := maxLen + 1

	if err := st.raw(clearRow(konfigEditRow) + moveTo(konfigEditRow, 2) +
		pc(15) + label + pc(8) + ": "); err != nil {
		return "", false, err
	}
	if hint != "" {
		if err := st.pipe(moveTo(konfigEditRow, boxCol+width+1) + hint + "|07"); err != nil {
			return "", false, err
		}
	}

	draw := func() error {
		shown := string(buf)
		if mask {
			shown = strings.Repeat("*", len(buf))
		}
		return st.raw(moveTo(konfigEditRow, boxCol) + konfigFieldStyle + shown +
			strings.Repeat(" ", width-len(buf)) + konfigReset + moveTo(konfigEditRow, boxCol+cur))
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
				line, _, pending = decodeExtendedKey(nil, mode, byte(key), pending)
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
	if err := st.pipe(clearRow(konfigEditRow) + moveTo(konfigEditRow, 2) + prompt + "|07"); err != nil {
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
	cell := func(i int) string {
		fc := fileColumns[i]
		u := st.user()
		on := fileColumnsAllDefault(u) || *fc.get(u)
		v := onOffValue(on)
		if i == sel {
			return konfigBarStyle + padRunes(fmt.Sprintf(" [%c] %-14s%s", fc.key, fc.label, v.text), inner) + konfigReset
		}
		return pc(8) + " [" + pc(15) + string(fc.key) + pc(8) + "] " + pc(7) +
			fmt.Sprintf("%-14s", fc.label) + toneColor(v.tone) + padRunes(v.text, inner-20) + konfigReset
	}
	drawItem := func(i int) error {
		return st.raw(line(colBoxTop+1+i, cell(i)))
	}
	title := " File Columns "
	rule := strings.Repeat("─", inner-utf8.RuneCountInString(title)-1)
	var b strings.Builder
	b.WriteString(moveTo(colBoxTop, colBoxLeft) + pc(9) + "┌─" + pc(15) + title + pc(9) + rule + "┐")
	for i := range fileColumns {
		b.WriteString(line(colBoxTop+1+i, cell(i)))
	}
	hintRow := colBoxTop + 1 + len(fileColumns)
	b.WriteString(line(hintRow, konfigReset+strings.Repeat(" ", inner)))
	b.WriteString(line(hintRow+1, pc(8)+padRunes(" Enter switches   Esc when done", inner)))
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
			st.status = "|12At least one column has to stay on.|07"
			return
		}
		if st.commit("File Columns",
			func(u *user.User) {
				for j, fc := range fileColumns {
					*fc.get(u) = shown[j]
				}
			},
			func(u *user.User) { u.FileListColumns = old }) {
			st.saved(fileColumns[i].label+" column", onOffValue(shown[i]).text)
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
