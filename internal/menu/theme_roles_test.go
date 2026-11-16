package menu

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// legacyTheme is a theme.json from before the lightbar roles: the Yes/No
// colours the stock theme has, and no role set.
func legacyTheme() config.ThemeConfig {
	return config.ThemeConfig{YesNoHighlightColor: 31, YesNoRegularColor: 13, ChatSysopColor: 11, ChatUserColor: 10, AutocorrectHighlightColor: 15}
}

// stockMenuSet is the menu set ViSiON/3 ships.
func stockMenuSet(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "menus", "v3"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// roleScreens are lightbar screens that take their colours from the theme
// with the stock menu set: no BAR file overrides them. Each is run to its
// first screen and quit.
var roleScreens = []struct {
	name, cmd, args, input string
	setup                  func(env *menuEnv)
}{
	{name: "message reader", cmd: "READMSGS", input: "Q", setup: func(env *menuEnv) { env.generalMsgs(2) }},
	{name: "file area selector", cmd: "SELECTFILEAREA", input: "q", setup: func(env *menuEnv) { env.caller.CurrentFileConferenceID = 1 }},
	// These two had a stock *HI.BAR until #1079 retired it.
	{name: "message area selector", cmd: "SELECTMSGAREA", input: "q"},
	{name: "message conference selector", cmd: "CHANGEMSGCONF", input: "q"},
	{name: "BBS list", cmd: "BBSLIST", input: "q", setup: func(env *menuEnv) {
		if err := saveBBSListData(env.dataDir(), &bbsListData{NextID: 2, Listings: []BBSListing{
			{ID: 1, Name: "A Board", Address: "a.example", AddedBy: "Sysop"},
		}}); err != nil {
			env.t.Fatal(err)
		}
	}},
}

func runRoleScreen(t *testing.T, i int, theme config.ThemeConfig) string {
	t.Helper()
	sc := roleScreens[i]
	env := newMsgEnv(t)
	if sc.setup != nil {
		sc.setup(env)
	}
	env.e.SetTheme(theme)
	r := env.runCmd(sc.cmd, env.caller, sc.args, sc.input)
	if r.err != nil {
		t.Fatalf("%s: %v", sc.name, r.err)
	}
	// Two runs are compared, and the clock may tick between them: a
	// message header shows when its message was posted.
	return clockRe.ReplaceAllString(r.raw, "TIME")
}

// clockRe matches a date or a time of day as the screens print them.
var clockRe = regexp.MustCompile(`\d{2}/\d{2}/\d{2}|\d{1,2}:\d{2} [ap]m`)

// A theme.json from before the roles draws every lightbar screen exactly
// as the stock theme, which sets each role to the colour it used to be.
func TestLegacyThemeLooksAsBefore(t *testing.T) {
	stock, err := config.LoadThemeConfig(stockMenuSet(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range config.LightbarRoles {
		if _, src := stock.ColorWithSource(role); src != config.SourceTheme {
			t.Errorf("the stock theme.json doesn't set %s", role)
		}
		if got, want := stock.Color(role), legacyTheme().Color(role); got != want {
			t.Errorf("stock %s = %d, but a theme without it draws %d", role, got, want)
		}
	}
	// The stock theme's other settings, with the roles taken out.
	legacy := stock
	legacy.LightbarSelected, legacy.LightbarNormal, legacy.LightbarCommand = config.OptColor{}, config.OptColor{}, config.OptColor{}
	legacy.LightbarHotkey, legacy.LightbarHint, legacy.LightbarBorder, legacy.LightbarDestructive = config.OptColor{}, config.OptColor{}, config.OptColor{}, config.OptColor{}
	for i, sc := range roleScreens {
		t.Run(sc.name, func(t *testing.T) {
			with, without := runRoleScreen(t, i, stock), runRoleScreen(t, i, legacy)
			if with == "" {
				t.Fatal("the screen drew nothing")
			}
			if with != without {
				t.Errorf("output differs between the stock theme and one without roles:\n%q\n%q", with, without)
			}
		})
	}
}

// Changing lightbarSelected in the theme changes the highlight of every
// lightbar that inherits it, with no BAR file edited, and leaves the
// Yes/No prompt's own colours alone.
func TestLightbarSelectedReachesEveryLightbar(t *testing.T) {
	theme := legacyTheme()
	theme.LightbarSelected = config.SetColor(78) // yellow on red
	oldSeq, newSeq := colorCodeToAnsi(31), colorCodeToAnsi(78)
	for i, sc := range roleScreens {
		t.Run(sc.name, func(t *testing.T) {
			before, after := runRoleScreen(t, i, legacyTheme()), runRoleScreen(t, i, theme)
			if !strings.Contains(before, oldSeq) {
				t.Fatalf("the screen has no highlight in the Yes/No colour to begin with:\n%q", before)
			}
			if !strings.Contains(after, newSeq) || strings.Contains(after, oldSeq) {
				t.Errorf("highlight not moved to lightbarSelected:\n%q", after)
			}
			if got := strings.ReplaceAll(after, newSeq, oldSeq); got != before {
				t.Errorf("more than the highlight changed:\n%q\n%q", before, got)
			}
		})
	}

	env := newMenuEnv(t)
	env.e.SetTheme(theme)
	if got, want := env.e.roleColor(roleYesNoHi), 31; got != want {
		t.Errorf("Yes/No highlight = %d with lightbarSelected changed, want %d", got, want)
	}
}

// Del on the reader's bar is drawn in the destructive role, and a role of
// 0 is black, not "no override": the bar's option keeps 0 apart from unset.
func TestReaderDelUsesTheDestructiveRole(t *testing.T) {
	del := func(theme config.ThemeConfig) string {
		t.Helper()
		env := newMsgEnv(t)
		env.generalMsgs(1)
		env.e.SetTheme(theme)
		r := env.runCmd("READMSGS", env.sysop, "", "Q")
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r.raw
	}
	if out := del(legacyTheme()); !strings.Contains(out, colorCodeToAnsi(4)+" Del ") {
		t.Errorf("default Del is not dark red:\n%q", out)
	}
	black := legacyTheme()
	black.LightbarDestructive = config.SetColor(0)
	out := del(black)
	if !strings.Contains(out, colorCodeToAnsi(0)+" Del ") || strings.Contains(out, colorCodeToAnsi(9)+" Del ") {
		t.Errorf("lightbarDestructive 0 did not draw Del in colour 0:\n%q", out)
	}
}

// The helpers that colour generated text: a plain foreground is the pipe
// code the screens always wrote, a colour with a background the escape
// sequence, and colour 0 is used, not taken for "unset".
func TestRoleHelpers(t *testing.T) {
	env := newMenuEnv(t)
	env.e.SetTheme(legacyTheme())
	hint := "|08[ |15Up|08/|15Dn|08 ] Nav"
	if got := env.e.lightbarHint(hint); got != hint {
		t.Errorf("default hint = %q, want it unchanged", got)
	}
	if got, want := env.e.lightbarRule("--"), "|08--|07"; got != want {
		t.Errorf("default rule = %q, want %q", got, want)
	}

	theme := legacyTheme()
	theme.LightbarHint = config.SetColor(3)
	theme.LightbarHotkey = config.SetColor(0x1e) // yellow on blue
	theme.LightbarBorder = config.SetColor(0)
	theme.LightbarSelected = config.SetColor(0)
	env.e.SetTheme(theme)
	// A role with a background: every colour change in the row is then an
	// escape sequence that resets first, so the key names' blue doesn't
	// stay behind the words after them, and the row ends with a reset.
	key, text := colorCodeToAnsi(0x1e), colorCodeToAnsi(3)
	if got, want := env.e.lightbarHint(hint), text+"[ "+key+"Up"+text+"/"+key+"Dn"+text+" ] Nav\x1b[0m"; got != want {
		t.Errorf("hint = %q, want %q", got, want)
	}
	if strings.Contains(env.e.lightbarHint(hint), "|") {
		t.Error("a pipe code follows a background colour in the hint; it would keep the background")
	}
	if got, want := env.e.lightbarRule("--"), "|00--|07"; got != want {
		t.Errorf("rule = %q, want %q", got, want)
	}
	// A border with a background is closed before the colour goes back.
	theme.LightbarBorder = config.SetColor(0x4f)
	env.e.SetTheme(theme)
	if got, want := env.e.lightbarRule("--"), colorCodeToAnsi(0x4f)+"--\x1b[0m|07"; got != want {
		t.Errorf("rule with a background = %q, want %q", got, want)
	}
	// roleText reads the role once, for the colour and the reset alike.
	if got, want := env.e.roleText(roleBorder, "x"), colorCodeToAnsi(0x4f)+"x\x1b[0m"; got != want {
		t.Errorf("roleText with a background = %q, want %q", got, want)
	}
	if got := env.e.roleText(config.RoleLightbarSelected, "x"); got != "|00x" {
		t.Errorf("roleText with a plain foreground = %q, want |00x", got)
	}
	if got := env.e.roleColor(config.RoleLightbarSelected); got != 0 {
		t.Errorf("lightbarSelected 0 read as %d", got)
	}

	// With no executor, the built-in colours.
	var none *MenuExecutor
	if got := none.lightbarRule("--"); got != "|08--|07" {
		t.Errorf("nil executor rule = %q", got)
	}
}
