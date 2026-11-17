package menu

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// themeKonfigKey is the Theme item's hotkey (#1099).
const themeKonfigKey = 'V'

// themeKonfigOffered reports whether Konfig offers the Theme item: unless
// the sysop has turned caller choice off (theme.callerChoice).
func themeKonfigOffered(e *MenuExecutor) bool {
	return e.GetServerConfig().Theme.CallersChooseTheme()
}

// boardThemeName is the name of the theme the board runs.
func (e *MenuExecutor) boardThemeName() string {
	return filepath.Base(e.shared().MenuSetPath)
}

// installedThemes lists the themes a caller can choose: the menu sets
// beside the board's, under menus/, with valid names and a MAIN menu, the
// board's own among them, sorted.
func (e *MenuExecutor) installedThemes() []string {
	parent := filepath.Dir(e.shared().MenuSetPath)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	var names []string
	for _, de := range entries {
		name := de.Name()
		if !de.IsDir() || menuset.ValidThemeName(name) != nil {
			continue
		}
		if ok, err := menuset.FromPath(filepath.Join(parent, name)).Exists("mnu", "MAIN.MNU"); err != nil || !ok {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// themeValue shows the caller's theme: their choice, or the board's,
// marked as the default, when they have none.
func themeValue(st *konfigState) konfigValue {
	board := st.c.e.boardThemeName()
	if t := st.user().Theme; t != "" && t != board {
		return konfigValue{t, toneValue}
	}
	return konfigValue{fmt.Sprintf(st.c.e.Strings().KonfigDefaultFormat, board), toneDim}
}

// editTheme opens a box listing the installed themes. Choosing one saves
// it and leaves Konfig, so the menu the caller came from is drawn in the
// new theme straight away. With only the board's theme installed there is
// nothing to choose, and the status line says so.
func editTheme(st *konfigState) error {
	str := st.c.e.Strings()
	board := st.c.e.boardThemeName()
	themes := st.c.e.installedThemes()
	if len(themes) < 2 {
		st.status = fmt.Sprintf(str.KonfigThemeOnlyOne, board)
		return nil
	}
	st.redraw = true

	current := st.user().Theme
	if current == "" {
		current = board
	}
	sel := 0
	for i, t := range themes {
		if t == current {
			sel = i
		}
	}

	inner := colBoxWidth - 2
	// The box fits the form: title, the list, a blank row, the hint and
	// the bottom edge. A longer list scrolls.
	visible := min(len(themes), konfigFormRows-4)
	top := 0
	line := func(row int, s string) string {
		return moveTo(row, colBoxLeft) + pc(9) + "│" + s + pc(9) + "│"
	}
	cell := func(i int) string {
		name, tag := st.fit(themes[i], 20), ""
		if themes[i] == board {
			tag = str.KonfigThemeBoardTag
		}
		if i == sel {
			return st.barStyle() + st.fit(" "+name+" "+tag, inner) + konfigReset
		}
		return pc(15) + " " + name + " " + pc(8) + st.fit(tag, inner-22) + konfigReset
	}
	drawList := func() error {
		var b strings.Builder
		for r := 0; r < visible; r++ {
			b.WriteString(line(colBoxTop+1+r, cell(top+r)))
		}
		return st.raw(b.String())
	}
	title := " " + st.clip(str.KonfigThemeTitle, inner-3, "..") + " "
	rule := strings.Repeat("─", inner-st.cells(title)-1)
	var b strings.Builder
	b.WriteString(moveTo(colBoxTop, colBoxLeft) + pc(9) + "┌─" + pc(15) + title + pc(9) + rule + "┐")
	hintRow := colBoxTop + 1 + visible
	b.WriteString(line(hintRow, konfigReset+strings.Repeat(" ", inner)))
	b.WriteString(line(hintRow+1, pc(8)+st.fit(" "+str.KonfigThemeHint, inner)))
	b.WriteString(moveTo(hintRow+2, colBoxLeft) + pc(9) + "└" + strings.Repeat("─", inner) + "┘" + konfigReset)
	if err := st.raw(b.String()); err != nil {
		return err
	}
	scroll := func() {
		if sel < top {
			top = sel
		} else if sel >= top+visible {
			top = sel - visible + 1
		}
	}
	scroll()
	if err := drawList(); err != nil {
		return err
	}

	for {
		key, err := st.ih.ReadKey()
		if err != nil {
			return err
		}
		switch key {
		case editor.KeyEsc, 'q', 'Q':
			return nil
		case editor.KeyArrowUp:
			sel = (sel + len(themes) - 1) % len(themes)
		case editor.KeyArrowDown:
			sel = (sel + 1) % len(themes)
		case editor.KeyHome, editor.KeyPageUp:
			sel = 0
		case editor.KeyEnd, editor.KeyPageDown:
			sel = len(themes) - 1
		case editor.KeyEnter, ' ':
			return st.chooseTheme(themes[sel], board)
		}
		scroll()
		if err := drawList(); err != nil {
			return err
		}
	}
}

// chooseTheme saves theme as the caller's, stored as none for the board's
// own, and leaves Konfig when it changed.
func (st *konfigState) chooseTheme(theme, board string) error {
	if theme == board {
		theme = ""
	}
	old := st.user().Theme
	if theme == old {
		return nil
	}
	if st.commit(st.c.e.Strings().KonfigThemeLabel,
		func(u *user.User) { u.Theme = theme },
		func(u *user.User) { u.Theme = old }) {
		// Konfig closes, and the menu it was opened from is drawn in the
		// new theme.
		st.leave = true
	}
	return nil
}
