package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
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

// Prompts and templates fill in the caller's colours.
func TestColorSlotsInPromptsAndTemplates(t *testing.T) {
	e := promptExecutor(t, nil)
	e.SetStrings(config.StringsConfig{})
	u := &user.User{Handle: "Neo"}
	u.SetColorCode(2, "|13")

	ph := e.sessionPlaceholders(nil, u, nil, 1, "MAIN", time.Now(), "", false)
	if got := string(e.renderPromptText("|C2>|C3x", ph, 0, 0, 1)); got != esc("|13")+">"+esc("|07")+"x" {
		t.Errorf("prompt = %q", got)
	}
	if got := string(ansi.ReplacePipeCodes(e.applyCommonTemplateTokens([]byte("|C2>|C3x"), u, 1))); got != esc("|13")+">"+esc("|07")+"x" {
		t.Errorf("template = %q", got)
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
	if got, want := string(ansi.ReplacePipeCodes(e.applyCommonTemplateTokens([]byte("|C70"), u, 1))), esc("|15|B1")+"0"; got != want {
		t.Errorf("template = %q, want %q", got, want)
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
	for _, c := range []string{"|07", "|14|B1", "|00|B3"} {
		if got := colorChoiceOf(c).code(); got != c {
			t.Errorf("round trip %s = %s", c, got)
		}
	}
}
