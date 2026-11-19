package menu

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// themeKonfigKey is the Theme item's hotkey (#1099).
const themeKonfigKey = 'V'

// konfigScreenCols is the width the form is laid out for; themeBoxWidth
// is the themes box's, borders included. Boxes are centred on the
// caller's screen (boxAt).
const (
	konfigScreenCols = 80
	themeBoxWidth    = 36
)

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
// beside the board's, under menus/, with valid names that a session can
// run on (checkCallerTheme), the board's own among them, sorted. A theme installed as a symlink to a
// directory counts, as it does for the board (menuset.ListThemes).
func (e *MenuExecutor) installedThemes() []string {
	parent := filepath.Dir(e.shared().MenuSetPath)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	var names []string
	for _, de := range entries {
		name := de.Name()
		if menuset.ValidThemeName(name) != nil || checkCallerTheme(filepath.Join(parent, name)) != nil {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// themeLabel is how the installed theme in directory dir, beside the
// board's, is shown: its name and description (config.ThemeLabelFor),
// with the board's theme.labels over the theme's own theme.json.
func (e *MenuExecutor) themeLabel(dir string) config.ThemeLabel {
	parent := filepath.Dir(e.shared().MenuSetPath)
	return config.ThemeLabelFor(filepath.Join(parent, dir), e.GetServerConfig().Theme)
}

// themeValue shows the theme the caller sees, by its name: their choice,
// or the board's, marked as the default, when they have none or their
// choice is no longer installed (the session shows the board's then; see
// ForUser).
func themeValue(st *konfigState) konfigValue {
	board := st.c.e.boardThemeName()
	if t := st.shownTheme(); t != board {
		return konfigValue{st.c.e.themeLabel(t).Name, toneValue}
	}
	return konfigValue{fmt.Sprintf(st.c.e.Strings().KonfigDefaultFormat, st.c.e.themeLabel(board).Name), toneDim}
}

// shownTheme is the theme the caller sees: their choice while it is
// installed, else the board's. The choice itself is kept, so it applies
// again if the theme comes back.
func (st *konfigState) shownTheme() string {
	board := st.c.e.boardThemeName()
	t := st.user().Theme
	if t == "" || t == board || !slices.Contains(st.c.e.installedThemes(), t) {
		return board
	}
	return t
}

// editTheme opens a box listing the installed themes. Choosing one saves
// it and redraws Konfig in the new theme straight away; every screen
// after Konfig is drawn in it too. With only the board's theme installed there is
// nothing to choose, and the status line says so.
func editTheme(st *konfigState) error {
	str := st.c.e.Strings()
	board := st.c.e.boardThemeName()
	themes := st.c.e.installedThemes()
	if len(themes) < 2 {
		// The status line expands pipe codes; a theme's name prints as typed.
		st.status = fmt.Sprintf(str.KonfigThemeOnlyOne, ansi.EscapePipes(st.c.e.themeLabel(board).Name))
		return nil
	}
	st.redraw = true

	// Listed by name, in the order callers read them; the choice is still
	// the directory.
	labels := make(map[string]config.ThemeLabel, len(themes))
	for _, t := range themes {
		labels[t] = st.c.e.themeLabel(t)
	}
	sort.SliceStable(themes, func(i, j int) bool {
		return strings.ToLower(labels[themes[i]].Name) < strings.ToLower(labels[themes[j]].Name)
	})

	current := st.shownTheme()
	sel := 0
	for i, t := range themes {
		if t == current {
			sel = i
		}
	}

	// The box is wide enough for the longest description, up to
	// config.MaxThemeDescription columns, with a column of space each side.
	width := themeBoxWidth
	for _, l := range labels {
		width = max(width, min(st.cells(l.Description), config.MaxThemeDescription)+4)
	}
	inner := width - 2
	// The box fits the form: title, the list, the highlighted theme's
	// description, the hint and the bottom edge. A longer list scrolls. It
	// is centred on the caller's screen.
	visible := min(len(themes), konfigFormRows-4)
	boxTop, boxLeft := st.boxAt(width, visible+4)
	top := 0
	line := func(row int, s string) string {
		return moveTo(row, boxLeft) + pc(9) + "│" + s + pc(9) + "│"
	}
	cell := func(i int) string {
		name, tag := st.fit(labels[themes[i]].Name, config.MaxThemeName), ""
		if themes[i] == board {
			tag = str.KonfigThemeBoardTag
		}
		if i == sel {
			return st.barStyle() + st.fit(" "+name+" "+tag, inner) + konfigReset
		}
		return pc(15) + " " + name + " " + pc(8) + st.fit(tag, inner-config.MaxThemeName-2) + konfigReset
	}
	hintRow := boxTop + 1 + visible
	drawList := func() error {
		var b strings.Builder
		for r := 0; r < visible; r++ {
			b.WriteString(line(boxTop+1+r, cell(top+r)))
		}
		desc := st.fit(labels[themes[sel]].Description, min(inner-2, config.MaxThemeDescription))
		b.WriteString(line(hintRow, konfigReset+pc(7)+st.fit(" "+desc, inner)))
		return st.raw(b.String())
	}
	title := " " + st.clip(str.KonfigThemeTitle, inner-3, "..") + " "
	rule := strings.Repeat("─", inner-st.cells(title)-1)
	var b strings.Builder
	b.WriteString(moveTo(boxTop, boxLeft) + pc(9) + "┌─" + pc(15) + title + pc(9) + rule + "┐")
	b.WriteString(line(hintRow+1, pc(8)+st.fit(" "+str.KonfigThemeHint, inner)))
	b.WriteString(moveTo(hintRow+2, boxLeft) + pc(9) + "└" + strings.Repeat("─", inner) + "┘" + konfigReset)
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
// own. When it changed, Konfig carries on on the executor for the new
// theme, so its art, bars and colours change at once; the menu loop
// switches to it when Konfig closes (Run, SwitchTheme).
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
		st.c.e = st.c.e.ForUser(st.user())
		st.layout()       // labels and headings in the new theme's strings
		st.hdrNames = nil // header style names are the new theme's
		st.redraw = true
		st.saved(st.c.e.Strings().KonfigThemeLabel, ansi.EscapePipes(themeValue(st).text))
	}
	return nil
}
