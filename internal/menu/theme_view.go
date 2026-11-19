package menu

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// A caller may choose a theme other than the board's in User Konfig
// (#1099). Their session then runs on a view of the board's executor: the
// same managers, config and registries, read through the board so a
// reload reaches it, with the chosen theme's menus, art, templates, BAR
// files, colours and strings.json. Screens before login stay on the
// board's theme.

// ForTheme is the theme choice this executor was made for by ForUser: ""
// for the board's own.
func (e *MenuExecutor) ForTheme() string { return e.forTheme }

// CallerTheme is the theme u's session should run on: u's choice, or ""
// for the board's own theme when u has none, chooses the board's, or the
// board doesn't let callers choose. It reads no files, so the menu loop
// can call it on every screen.
func (e *MenuExecutor) CallerTheme(u *user.User) string {
	if u == nil || u.Theme == "" {
		return ""
	}
	b := e.shared()
	if !b.GetServerConfig().Theme.CallersChooseTheme() || u.Theme == filepath.Base(b.MenuSetPath) {
		return ""
	}
	return u.Theme
}

// ForUser returns the executor u's session runs on: the board's when
// CallerTheme is "", and otherwise a view of it on u's theme. A theme that
// can't be used (removed, or without a MAIN menu or a readable theme.json)
// is logged, and the view draws the board's theme; u's choice is left
// alone, so it applies again once the theme is back.
func (e *MenuExecutor) ForUser(u *user.User) *MenuExecutor {
	b := e.shared()
	want := b.CallerTheme(u)
	if want == "" {
		return b
	}
	dir, theme, err := b.loadCallerTheme(want)
	if err != nil {
		slog.Warn("caller's theme can't be used; showing the board's", "user", u.Handle, "theme", want, "error", err)
		return b.newView(want, "", nil)
	}
	return b.newView(want, dir, &theme)
}

// loadCallerTheme finds theme name beside the board's menu set and loads
// its theme.json.
func (e *MenuExecutor) loadCallerTheme(name string) (string, config.ThemeConfig, error) {
	if err := menuset.ValidThemeName(name); err != nil {
		return "", config.ThemeConfig{}, err
	}
	dir := filepath.Join(filepath.Dir(e.MenuSetPath), name)
	if err := checkCallerTheme(dir); err != nil {
		return "", config.ThemeConfig{}, err
	}
	theme, err := config.LoadThemeConfig(dir)
	if err != nil {
		return "", config.ThemeConfig{}, err
	}
	return dir, theme, nil
}

// checkCallerTheme reports why the menu set at dir can't be a caller's
// theme, if it can't: it isn't a directory, has no MAIN menu, or its
// theme.json doesn't load. Konfig offers only themes that pass, so it
// shows what the session would draw.
func checkCallerTheme(dir string) error {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not a theme directory", dir)
	}
	if ok, err := menuset.FromPath(dir).Exists("mnu", "MAIN.MNU"); err != nil || !ok {
		return fmt.Errorf("%s has no MAIN menu", dir)
	}
	if err := config.CheckThemeConfig(dir); err != nil {
		return fmt.Errorf("%s: %w", dir, err)
	}
	return nil
}

// newView makes a caller's view of the board e for the theme choice
// forTheme: on the theme at dir, or on the board's own theme when theme is
// nil. Every field but the theme's is the board's. A view on its own
// theme has that theme's strings.json over the board's strings.
func (e *MenuExecutor) newView(forTheme, dir string, theme *config.ThemeConfig) *MenuExecutor {
	v := &MenuExecutor{
		ConfigPath:      e.ConfigPath,
		AssetsPath:      e.AssetsPath,
		MenuSetPath:     e.MenuSetPath,
		RootConfigPath:  e.RootConfigPath,
		RootAssetsPath:  e.RootAssetsPath,
		RunRegistry:     e.RunRegistry,
		OneLiners:       e.OneLiners,
		MessageMgr:      e.MessageMgr,
		FileMgr:         e.FileMgr,
		ConferenceMgr:   e.ConferenceMgr,
		IPLockoutCheck:  e.IPLockoutCheck,
		SessionRegistry: e.SessionRegistry,
		ChatLeaves:      e.ChatLeaves,
		V3NetReload:     e.V3NetReload,
		V3NetStatus:     e.V3NetStatus,
		V3NetCentral:    e.V3NetCentral,
		Pager:           e.Pager,
		Nodelists:       e.Nodelists,
		parent:          e,
		forTheme:        forTheme,
	}
	if theme == nil {
		v.sharesTheme = true
		return v
	}
	v.MenuSetPath = dir
	v.SetTheme(*theme)
	v.ReloadThemeStrings()
	return v
}

// hasMenu reports whether the executor's menu set has menu name.
func (e *MenuExecutor) hasMenu(name string) bool {
	ok, err := e.Menus().Exists("mnu", strings.ToUpper(name)+".MNU")
	return err == nil && ok
}

// SwitchTheme returns the executor u's session should run on now, and the
// menu to show on it, when u's theme choice is no longer the one e runs
// on: after u chose another in Konfig, or the sysop turned the choice off.
// The menu is menuName, or MAIN where the new theme has no such menu. ok is
// false when e is still the right executor.
func (e *MenuExecutor) SwitchTheme(u *user.User, menuName string) (next *MenuExecutor, menu string, ok bool) {
	if u == nil || e.CallerTheme(u) == e.forTheme {
		return e, menuName, false
	}
	next = e.ForUser(u)
	if menuName != "" && menuName != "LOGOFF" && !next.hasMenu(menuName) {
		slog.Info("menu not in the caller's new theme; going to MAIN", "user", u.Handle, "menu", menuName, "theme", next.Menus().Base)
		menuName = "MAIN"
	}
	return next, menuName, true
}

// withSessionTheme gives the message editor the session's strings, and
// the menu set and theme of a session on a caller's own theme. On the
// board's theme it leaves those two alone, and the editor reads the theme
// the server runs.
func withSessionTheme(e *MenuExecutor, ctx editor.EditorContext) editor.EditorContext {
	if e == nil {
		return ctx
	}
	ctx.Strings = e.Strings()
	if e.parent == nil || e.sharesTheme {
		return ctx
	}
	ctx.MenuSetPath = e.MenuSetPath
	ctx.Theme = e.Theme()
	return ctx
}
