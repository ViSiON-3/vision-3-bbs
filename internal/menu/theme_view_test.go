package menu

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/types"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// themeViewBoard is a board running menus/v3 beside a second theme,
// menus/amber, whose own colours differ: lightbarSelected 30 and
// callerPrompt |12. The board overrides lightbarSelected with 78.
func themeViewBoard(t *testing.T) (board *MenuExecutor, amber string) {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("menus/v3/theme.json", `{"lightbarSelected": 31, "callerPrompt": 7}`)
	write("menus/v3/mnu/MAIN.MNU", "{}")
	write("menus/v3/mnu/MSGMENU.MNU", "{}")
	write("menus/amber/theme.json", `{"lightbarSelected": 30, "callerPrompt": 12}`)
	write("menus/amber/mnu/MAIN.MNU", "{}")
	write("menus/nomain/theme.json", `{}`)
	write("menus/badjson/theme.json", `{`)
	write("menus/badjson/mnu/MAIN.MNU", "{}")

	v3 := filepath.Join(root, "menus", "v3")
	theme, err := config.LoadThemeConfig(v3)
	if err != nil {
		t.Fatal(err)
	}
	board = &MenuExecutor{MenuSetPath: v3}
	board.SetStrings(config.StringsConfig{})
	board.SetServerConfig(config.ServerConfig{BoardName: "Board", Theme: config.ThemeSelection{
		Colors: config.ThemeColors{LightbarSelected: config.SetColor(78)},
	}})
	board.SetTheme(theme)
	t.Cleanup(func() { ansi.SetColorSlotDefaults([7]string{}) })
	return board, filepath.Join(root, "menus", "amber")
}

// The theme a caller's session runs on: their choice, unless they have
// none, chose the board's own, or the board doesn't let callers choose.
func TestCallerTheme(t *testing.T) {
	board, _ := themeViewBoard(t)
	for _, c := range []struct {
		name string
		u    *user.User
		want string
	}{
		{"no caller", nil, ""},
		{"no choice", &user.User{}, ""},
		{"the board's", &user.User{Theme: "v3"}, ""},
		{"another", &user.User{Theme: "amber"}, "amber"},
		{"one not installed", &user.User{Theme: "gone"}, "gone"},
	} {
		if got := board.CallerTheme(c.u); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	cfg := board.GetServerConfig()
	cfg.Theme.CallerChoice = boolPtr(false)
	board.SetServerConfig(cfg)
	if got := board.CallerTheme(&user.User{Theme: "amber"}); got != "" {
		t.Errorf("choice off: %q, want the board's", got)
	}
}

// A caller on another theme gets a view on it: that theme's menus and
// colours, without the board's overrides, and the board's strings and
// config, which reach it when they reload. The board's colour slot
// defaults, which art and tools draw, are left alone.
func TestForUserViewOnTheCallersTheme(t *testing.T) {
	board, amber := themeViewBoard(t)
	slotBefore := string(ansi.ReplacePipeCodes([]byte("|C1")))
	u := &user.User{Handle: "Amb", Theme: "amber"}
	view := board.ForUser(u)

	if view == board || view.ForTheme() != "amber" || view.Menus().Base != amber {
		t.Fatalf("view for %q on %q", view.ForTheme(), view.Menus().Base)
	}
	if got := board.Theme().Color(config.RoleLightbarSelected); got != 78 {
		t.Errorf("board's selected = %d, want its override 78", got)
	}
	if got := view.Theme().Color(config.RoleLightbarSelected); got != 30 {
		t.Errorf("view's selected = %d, want amber's own 30", got)
	}
	if got := view.colorSlot(nil, 1); got != "|12" {
		t.Errorf("view's |C1 default = %q, want amber's |12", got)
	}
	if got := board.colorSlot(nil, 1); got != "|07" {
		t.Errorf("board's |C1 default = %q, want v3's |07", got)
	}
	if got := string(ansi.ReplacePipeCodes([]byte("|C1"))); got != slotBefore {
		t.Errorf("the view changed the board's art colours: %q, was %q", got, slotBefore)
	}

	if view.Strings() != board.Strings() {
		t.Error("the view has strings of its own")
	}
	board.SetStrings(config.StringsConfig{KonfigTitle: "reloaded"})
	cfg := board.GetServerConfig()
	cfg.BoardName = "Renamed"
	board.SetServerConfig(cfg)
	if view.Strings().KonfigTitle != "reloaded" || view.GetServerConfig().BoardName != "Renamed" {
		t.Errorf("reloads don't reach the view: %q, %q", view.Strings().KonfigTitle, view.GetServerConfig().BoardName)
	}
	if got := view.Theme().Color(config.RoleLightbarSelected); got != 30 {
		t.Errorf("after the board's reload the view's selected = %d, want 30", got)
	}
	// A slot amber leaves unset takes an older strings.json's defColorN,
	// and a strings reload reaches it.
	board.SetStrings(config.StringsConfig{LegacyColorSlots: [7]string{1: "|02"}})
	if got := view.colorSlot(nil, 2); got != "|02" {
		t.Errorf("view's |C2 = %q, want the legacy |02", got)
	}
	board.SetStrings(config.StringsConfig{LegacyColorSlots: [7]string{1: "|04"}})
	if got := view.colorSlot(nil, 2); got != "|04" {
		t.Errorf("after the strings reload the view's |C2 = %q, want |04", got)
	}
	if got := view.colorSlot(nil, 1); got != "|12" {
		t.Errorf("view's |C1 = %q, want amber's own |12 still", got)
	}

	ctx := withSessionTheme(view, editor.EditorContext{})
	if ctx.MenuSetPath != amber || ctx.Theme == nil || ctx.Theme.Color(config.RoleLightbarSelected) != 30 {
		t.Errorf("editor gets %q, %v", ctx.MenuSetPath, ctx.Theme)
	}
	if ctx := withSessionTheme(board, editor.EditorContext{}); ctx.MenuSetPath != "" || ctx.Theme != nil {
		t.Errorf("on the board's theme the editor is given %q, %v; want its own lookup", ctx.MenuSetPath, ctx.Theme)
	}
}

// With no choice to honour, a caller runs on the board's executor itself.
func TestForUserBoardsOwn(t *testing.T) {
	board, _ := themeViewBoard(t)
	for _, u := range []*user.User{nil, {}, {Theme: "v3"}} {
		if got := board.ForUser(u); got != board {
			t.Errorf("ForUser(%+v) is a view; want the board", u)
		}
	}
	view := board.ForUser(&user.User{Theme: "amber"})
	if got := view.ForUser(&user.User{}); got != board {
		t.Error("a view's ForUser should give the board for no choice")
	}
}

// A theme that can't be used gives a view on the board's own theme, which
// keeps following the board's reloads, and is logged; the caller's
// choice is left for when the theme is back.
func TestForUserFallsBackToTheBoardsTheme(t *testing.T) {
	board, _ := themeViewBoard(t)
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	for _, name := range []string{"gone", "nomain", "badjson", "../v3"} {
		logged.Reset()
		u := &user.User{Handle: "Fal", Theme: name}
		view := board.ForUser(u)
		if view == board || view.ForTheme() != name || view.Menus().Base != board.Menus().Base || view.Theme() != board.Theme() {
			t.Errorf("%s: view for %q on %q", name, view.ForTheme(), view.Menus().Base)
		}
		if u.Theme != name {
			t.Errorf("%s: the choice was changed to %q", name, u.Theme)
		}
		if !bytes.Contains(logged.Bytes(), []byte("can't be used")) {
			t.Errorf("%s: not logged: %s", name, logged.String())
		}
		if _, _, again := view.SwitchTheme(u, "MAIN"); again {
			t.Errorf("%s: the fallback view asks to switch again, which would loop", name)
		}
	}
	view := board.ForUser(&user.User{Theme: "gone"})
	board.SetTheme(config.ThemeConfig{LightbarSelected: config.SetColor(14)})
	if got := view.Theme().Color(config.RoleLightbarSelected); got != 78 {
		t.Errorf("fallback view's selected = %d; want the board's, override 78 included", got)
	}
	if view.colorSlot(nil, 7) != board.colorSlot(nil, 7) {
		t.Errorf("fallback view's |C7 %q, board's %q", view.colorSlot(nil, 7), board.colorSlot(nil, 7))
	}
}

// SwitchTheme moves a session to the executor for the caller's choice,
// keeping the menu where the new theme has it and going to MAIN where it
// doesn't.
func TestSwitchTheme(t *testing.T) {
	board, amber := themeViewBoard(t)
	u := &user.User{Handle: "Sw"}
	if _, _, ok := board.SwitchTheme(u, "MSGMENU"); ok {
		t.Error("no change asked to switch")
	}
	if _, _, ok := board.SwitchTheme(nil, "MAIN"); ok {
		t.Error("no caller asked to switch")
	}

	u.Theme = "amber"
	next, menuName, ok := board.SwitchTheme(u, "MSGMENU")
	if !ok || next.Menus().Base != amber || menuName != "MAIN" {
		t.Errorf("to amber from MSGMENU: %v, %q on %q; want MAIN, which amber has", ok, menuName, next.Menus().Base)
	}
	if _, menuName, _ := board.SwitchTheme(u, "LOGOFF"); menuName != "LOGOFF" {
		t.Errorf("LOGOFF became %q", menuName)
	}
	if again, _, ok := next.SwitchTheme(u, "MAIN"); ok || again != next {
		t.Error("the new view asks to switch again")
	}

	u.Theme = ""
	back, menuName, ok := next.SwitchTheme(u, "MAIN")
	if !ok || back != board || menuName != "MAIN" {
		t.Errorf("back to the board: %v, %q", ok, menuName)
	}
}

// newView copies every field of the board but its theme. A field added to
// MenuExecutor has to be added to newView too, or a session on a caller's
// theme runs without it; this list is the reminder.
func TestNewViewCopiesEveryField(t *testing.T) {
	copied := []string{
		"AssetsPath", "ChatLeaves", "ConferenceMgr", "ConfigPath", "FileMgr", "IPLockoutCheck",
		"MenuSetPath", "MessageMgr", "Nodelists", "OneLiners", "Pager", "RootAssetsPath",
		"RootConfigPath", "RunRegistry", "SessionRegistry", "V3NetCentral", "V3NetReload", "V3NetStatus",
	}
	sort.Strings(copied)
	var exported []string
	typ := reflect.TypeOf(MenuExecutor{})
	for i := 0; i < typ.NumField(); i++ {
		if f := typ.Field(i); f.IsExported() {
			exported = append(exported, f.Name)
		}
	}
	sort.Strings(exported)
	if !reflect.DeepEqual(exported, copied) {
		t.Errorf("MenuExecutor's exported fields\n %v\nnewView copies\n %v", exported, copied)
	}

	board, _ := themeViewBoard(t)
	board.RunRegistry = map[string]RunnableFunc{"X": nil}
	board.OneLiners = []string{"hi"}
	board.RootConfigPath = "configs"
	view := board.ForUser(&user.User{Theme: "amber"})
	if len(view.RunRegistry) != 1 || len(view.OneLiners) != 1 || view.RootConfigPath != "configs" {
		t.Errorf("view fields: %v %v %q", view.RunRegistry, view.OneLiners, view.RootConfigPath)
	}
}

// Views read the board while it reloads; run with -race.
func TestThemeViewsUnderReload(t *testing.T) {
	board, _ := themeViewBoard(t)
	theme := *board.Theme()
	cfg := board.GetServerConfig()
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			board.SetTheme(theme)
			board.SetServerConfig(cfg)
			board.SetStrings(config.StringsConfig{})
		}
	}()
	for _, name := range []string{"amber", "gone"} {
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				v := board.ForUser(&user.User{Handle: "R", Theme: name})
				_ = v.Theme().Color(config.RoleLightbarSelected)
				_ = v.Strings().KonfigTitle
				_ = v.colorSlot(nil, 3)
				_ = v.GetServerConfig().BoardName
			}
		}()
	}
	wg.Wait()
}

// The menu loop hands a caller back to the session loop as soon as their
// theme choice is no longer the one it runs on, without drawing the menu,
// and the session loop shows the menu again on the new theme.
func TestRunLeavesForANewTheme(t *testing.T) {
	env, m := execcovRunEnv(t)
	amber := &execcovMenus{t: t, dir: filepath.Join(filepath.Dir(m.dir), "amber")}
	for _, sub := range []string{"mnu", "cfg", "ansi", "bar"} {
		if err := os.MkdirAll(filepath.Join(amber.dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	amber.write("", "theme.json", "{}")
	amber.menu("MAIN", MenuRecord{}, "AMBER-MAIN\r\n", CommandRecord{Keys: "Q", Command: "LOGOFF"})

	caller := env.caller
	caller.Theme = "amber"
	if err := env.um.UpdateUser(caller); err != nil {
		t.Fatal(err)
	}
	r := execcovRun(env, execcovCall{user: caller, start: "MAIN", input: "q\r"})
	if r.err != nil || r.next != "MAIN" || r.has("MAIN-SCREEN") {
		t.Fatalf("board run: next %q, err %v, drew MAIN: %v; want MAIN handed back undrawn", r.next, r.err, r.has("MAIN-SCREEN"))
	}

	next, menuName, ok := env.e.SwitchTheme(caller, r.next)
	if !ok || menuName != "MAIN" {
		t.Fatalf("switch: %v, %q", ok, menuName)
	}
	env.e = next
	r = execcovRun(env, execcovCall{user: caller, start: menuName, input: "q\r"})
	if r.err != nil || r.next != "LOGOFF" || !r.has("AMBER-MAIN") {
		t.Errorf("amber run: next %q, err %v:\n%s", r.next, r.err, r.text())
	}

	// What the session chose stays for the Run after a switch: a screen
	// size changed in Konfig, and the message list mode.
	ts := newTestSession("")
	SetSessionOutputMode(ts, env.outputMode)
	t.Cleanup(func() { ClearSessionOutputMode(ts); sessionTermSizes.Delete(ts); sessionListModes.Delete(ts) })
	setSessionTermSize(ts, 100, 30)
	sessionListModes.Store(ts, true)
	caller.Theme = ""
	if err := env.um.UpdateUser(caller); err != nil {
		t.Fatal(err)
	}
	next, menuName, _ = env.e.SwitchTheme(caller, "MAIN")
	caller.Theme = "amber"
	if err := env.um.UpdateUser(caller); err != nil {
		t.Fatal(err)
	}
	action, _, err := next.Run(ts, newTestTerminal(ts), env.um, caller, menuName, 1, time.Now(), types.AutoRunTracker{}, env.outputMode, "", 80, 24)
	if err != nil || action != "MAIN" {
		t.Fatalf("handoff run: %q, %v", action, err)
	}
	if w, h, ok := takeSessionTermSize(ts); !ok || w != 100 || h != 30 {
		t.Errorf("screen size after the handoff: %d x %d, %v; want 100 x 30 still pending", w, h, ok)
	}
	if _, ok := sessionListModes.Load(ts); !ok {
		t.Error("the message list mode was dropped at the handoff")
	}

	// Before login the theme is always the board's.
	board := next.shared()
	if board.CallerTheme(nil) != "" {
		t.Error("no caller has a theme")
	}
}
