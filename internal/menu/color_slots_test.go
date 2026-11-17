package menu

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// esc is what a pipe code draws.
func esc(code string) string { return string(ansi.ReplacePipeCodes([]byte(code))) }

// A slot is the caller's colour, else the board's from an older strings.json,
// else the built-in one; a caller who chose black gets black.
func TestColorSlotResolution(t *testing.T) {
	e := &MenuExecutor{}
	e.SetStrings(config.StringsConfig{LegacyColorSlots: [7]string{1: "|12"}})
	u := &user.User{}
	u.SetColorCode(1, "|14|B1")
	u.SetColorCode(3, "|00")
	for n, want := range map[int]string{1: "|14|B1", 2: "|12", 3: "|00", 4: ansi.ColorSlotDefaults[3], 7: ansi.ColorSlotDefaults[6]} {
		if got := e.colorSlot(u, n); got != want {
			t.Errorf("slot %d = %q, want %q", n, got, want)
		}
	}
	if got := e.colorSlot(nil, 1); got != ansi.ColorSlotDefaults[0] {
		t.Errorf("no caller: slot 1 = %q", got)
	}
}

// Prompts fill in the caller's colours; templates draw the board's.
func TestColorSlotsInPromptsAndTemplates(t *testing.T) {
	e := promptExecutor(t, nil)
	e.SetStrings(config.StringsConfig{})
	u := &user.User{Handle: "Neo"}
	u.SetColorCode(2, "|13")

	ph := e.sessionPlaceholders(nil, u, nil, 1, "MAIN", time.Now(), "", false)
	if got := string(e.renderPromptText("|C2>|C3x", ph, 0, 0, 1)); got != esc("|13")+">"+esc("|07")+"x" {
		t.Errorf("prompt = %q", got)
	}
	// Templates are menu-set art: the board's defaults, not the caller's.
	if got := string(ansi.ReplacePipeCodes(e.applyCommonTemplateTokens([]byte("|C2>|C3x"), u, 1))); got != esc("|15")+">"+esc("|07")+"x" {
		t.Errorf("template = %q, want the board's defaults", got)
	}
}

// A slot's colour never runs into the text after it: a red background
// before "0" is not |B10.
func TestColorSlotKeepsTokensApart(t *testing.T) {
	e := promptExecutor(t, nil)
	e.SetStrings(config.StringsConfig{})
	u := &user.User{Handle: "Neo"}
	u.SetColorCode(7, "|15|B1")
	ph := e.sessionPlaceholders(nil, u, nil, 1, "MAIN", time.Now(), "", false)
	if got, want := string(e.renderPromptText("|C70 files", ph, 0, 0, 1)), esc("|15|B1")+"0 files"; got != want {
		t.Errorf("prompt = %q, want %q", got, want)
	}
}

// The Colours box saves a slot, a background, and a reset.
func TestKonfigColoursBox(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	// slot 1 (|07): right to |08, Enter; slot 2 (|15): Tab, right to |B0,
	// Enter; slot 2 again: R back to the default.
	runKonfig(t, um, u, "u"+keyRight+"\r"+keyDown+"\t"+keyRight+"\r"+keyEsc+"q", "")
	got := reloadUser(t, um)
	if got.ColorCode(1) != "|08" || got.ColorCode(2) != "|15|B0" {
		t.Fatalf("ColorCodes = %q", got.ColorCodes)
	}
	screen, _, _ := runKonfig(t, um, got, "u2r"+keyEsc+"q", "")
	if got := reloadUser(t, um); got.ColorCode(2) != "" || got.ColorCode(1) != "|08" {
		t.Fatalf("after R: ColorCodes = %q", got.ColorCodes)
	}
	if v := konfigFieldText(screen, konfigTestStrings(t).KonfigColorsLabel); !strings.HasPrefix(v, konfigTestStrings(t).KonfigColorsCustom) {
		t.Errorf("Colours value = %q", v)
	}
}

func TestKonfigColoursBoxFits(t *testing.T) {
	bottom := colorBoxTop + 1 + user.ColorSlots + 1 + 2 + 1
	if bottom >= konfigRuleRow {
		t.Errorf("box ends on row %d, over the rule on row %d", bottom, konfigRuleRow)
	}
	if colorBoxLeft < 1 || colorBoxLeft+colorBoxWidth-1 > 80 {
		t.Errorf("box spans columns %d-%d", colorBoxLeft, colorBoxLeft+colorBoxWidth-1)
	}
}

func TestColorChoiceWraps(t *testing.T) {
	ch := colorChoiceOf("|15")
	ch.move(1)
	if ch.code() != "|00" {
		t.Errorf("fg past 15 = %s", ch.code())
	}
	ch.bgRow = true
	ch.move(-1)
	if ch.code() != "|00|B7" {
		t.Errorf("bg before none = %s", ch.code())
	}
	ch.move(1)
	if ch.code() != "|00" {
		t.Errorf("bg after 7 = %s", ch.code())
	}
	for _, c := range []string{"|07", "|14|B1", "|00|B6"} {
		if got := colorChoiceOf(c).code(); got != c {
			t.Errorf("round trip %s = %s", c, got)
		}
	}
}

// A colour slot doesn't keep an optional group alive: |{|C4Conf: |CCN|}
// vanishes with no conference, as |{|15Conf: |CCN|} does.
func TestColorSlotInOptionalGroup(t *testing.T) {
	ph := map[string]string{"|C4": esc("|15"), "|CCN": ""}
	if got := expandOptionalGroups("a|{|C4Conf: |CCN|}b", ph); got != "ab" {
		t.Errorf("empty group = %q, want ab", got)
	}
	ph["|CCN"] = "Local"
	if got := expandOptionalGroups("a|{|C4Conf: |CCN|}b", ph); got != "a|C4Conf: |CCNb" {
		t.Errorf("full group = %q", got)
	}
	if !onlyColorSlots([]string{"|C1", "|C7"}) || onlyColorSlots([]string{"|C1", "|UH"}) || onlyColorSlots(nil) {
		t.Error("onlyColorSlots")
	}
}

// A slot's board default is the board's theme.colors, else the theme's
// caller colour role, else an older strings.json's defColorN, else the
// built-in; the Bar slot's built-in is the lightbar highlight. It is what
// |Cn draws with no caller too.
func TestColorSlotDefaultsFollowTheme(t *testing.T) {
	t.Cleanup(func() { ansi.SetColorSlotDefaults([7]string{}) })
	e := &MenuExecutor{}
	e.SetStrings(config.StringsConfig{LegacyColorSlots: [7]string{0: "|14", 1: "|12", 2: "|12"}})
	e.SetTheme(config.ThemeConfig{YesNoHighlightColor: 31, CallerInput: config.SetColor(13), CallerText: config.SetColor(9)})
	var cfg config.ServerConfig
	cfg.Theme.Colors.CallerText = config.SetColor(10)
	e.SetServerConfig(cfg)
	for n, want := range map[int]string{1: "|14", 2: "|13", 3: "|10", 4: "|15", 7: "|15|B4"} {
		if got := e.colorSlot(nil, n); got != want {
			t.Errorf("slot %d = %q, want %q", n, got, want)
		}
	}
	if got := string(ansi.ReplacePipeCodes([]byte("|C1x|C2"))); got != esc("|14")+"x"+esc("|13") {
		t.Errorf("no caller: got %q", got)
	}
	// A reload of strings.json keeps the theme's colours.
	e.SetStrings(config.StringsConfig{})
	if got := e.colorSlot(nil, 1); got != "|07" {
		t.Errorf("slot 1 after the legacy default went = %q, want the built-in |07", got)
	}
	if got := e.colorSlot(nil, 2); got != "|13" {
		t.Errorf("slot 2 after a strings reload = %q, want the theme's |13", got)
	}
}

// Enter on the board's default leaves the slot unset, so the caller follows
// the board if the sysop changes it.
func TestKonfigColoursDefaultIsUnset(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	u.SetColorCode(1, "|12")
	runKonfig(t, um, u, "u"+keyLeft5()+"\r"+keyEsc+"q", "")
	if got := reloadUser(t, um); got.ColorCode(1) != "" {
		t.Fatalf("slot 1 = %q, want unset after choosing the default |07", got.ColorCode(1))
	}
}

// keyLeft5 moves |12 back to |07.
func keyLeft5() string { return strings.Repeat("\x1b[D", 5) }

// The box shows the "none" background whole and black as a visible shade.
func TestKonfigColoursBoxSwatches(t *testing.T) {
	um, u := newUserConfigTestUser(t)
	screen := testterm.New(80, 24)
	sess := testterm.NewSession(screen, keyEsc)
	t.Cleanup(func() { resetSessionIH(sess); sessionTermSizes.Delete(sess) })
	c := &cmdCtx{e: konfigTestExecutor(t), s: sess, terminal: term.NewTerminal(sess, ""), userManager: um,
		currentUser: u, nodeNumber: 1, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
	if err := editColors(&konfigState{c: c, ih: getSessionIH(sess)}); err != nil {
		t.Fatal(err)
	}
	var none, shade bool
	for row := 1; row <= 24; row++ {
		r := screen.Row(row)
		none = none || strings.Contains(r, konfigTestStrings(t).KonfigColorsNoBG)
		shade = shade || strings.Contains(r, "▒▒")
	}
	if !none || !shade {
		t.Errorf("none label shown %v, black shade shown %v", none, shade)
	}
}

// User Konfig's highlighted row is the board's Bar colour, |C7, so an older
// strings.json's defColor7 recolours it too.
func TestKonfigBarFollowsBarSlot(t *testing.T) {
	e := konfigTestExecutor(t)
	st := &konfigState{c: &cmdCtx{e: e}}
	if got, want := st.barStyle(), konfigReset+esc(ansi.ColorSlotDefaults[6]); got != want {
		t.Errorf("default bar = %q, want %q", got, want)
	}
	e.SetStrings(config.StringsConfig{LegacyColorSlots: [7]string{6: "|14|B1"}})
	if got, want := st.barStyle(), konfigReset+esc("|14|B1"); got != want {
		t.Errorf("legacy bar = %q, want %q", got, want)
	}
}
