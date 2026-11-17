package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// The stock strings converted to the caller colour codes (#426), and what
// each one was before. With the default colours each must draw exactly
// what it did.
var convertedStockStrings = map[string]string{
	"execUsernamePrompt":    "|07Username/Handle: |15",
	"execPasswordPrompt":    "|07Password: |15",
	"doorPrompt":            "|07Door Name (?=List, Q=Quit): |15",
	"cfgCurrentPwPrompt":    "\r\n|07Current Password: ",
	"msgAutoSigPrompt":      "|07Add your Auto-Signature? @",
	"execAbortLoginPrompt":  "|07Abort Login? @",
	"filePausePrompt":       "\r\n|07Press |15[ENTER]|07 to continue... ",
	"filePromptFormat":      "\r\n|07Enter filename to %s (or |15ENTER|07 to cancel): |15",
	"fileScanDatePrompt":    "\r\n|07File newscan since |08(|15MM/DD/YY|08, |15A|08=all files, |15R|08=reset to last logon|08)|07: |15",
	"msgSearchPagePrompt":   "\r\n|07Page |15%d|07/|15%d|07  |15#|07 read  |15Enter|07 next  |15P|07rev  |15Q|07uit: |15",
	"scanDatePrompt":        "\r\n|07Scan From; |15A|07ll, |15N|07ew Messages, or Enter |15Date|07 (e.g. |15MM/DD/YY|07): |15",
	"scanFromPrompt":        "\r\n|07\"From\" string to Search for (Cr/none): |15",
	"scanRangeStartPrompt":  "\r\n|07Range Start (|151-%d|07, Cr/none) : |15",
	"scanRangeEndPrompt":    "\r\n|07Range End (|15%d-%d|07, Cr/none) : |15",
	"scanToPrompt":          "\r\n|07\"To\" string to Search for (Cr/none): |15",
	"scanWhichPrompt":       "\r\n|15M|07arked Areas, |15A|07ll Areas, |15C|07urrent Area : |15",
	"acsTestExprPrompt":     "|07ACS expression: ",
	"acsTestSessionPrompt":  "|07Test with a session (a call in progress)?",
	"acsTestLocalPrompt":    "|07Local call (for L)?",
	"acsTestTerminalPrompt": "|07Connection has a terminal (for A)?",
	"acsTestElapsedPrompt":  "|07Minutes into the call (for T): ",
	"acsTestCreditPrompt":   "|07Minutes of chat credit (for T): ",
	"acsTestTimePrompt":     "|07Time to test at (|15hh:mm|07 or |15yyyy-mm-dd hh:mm|07, Enter for now): ",
}

// The stock menu prompts converted to the caller colour codes, and what
// each was before: menu file, prompt line, old value.
var convertedStockPrompts = []struct{ menu, key, old string }{
	{"BBSLISTM", "PROMPT2", "|08[|15BBS List|08] |07Command |08->|15 "},
	{"FILEM", "PROMPT2", "|08[|15Transfer Menu|08] |07Command |08->|15 "},
	{"INFORMM", "PROMPT2", "|08[|15InfoForms|08] |07Command |08->|15 "},
	{"MSGMENU", "PROMPT2", "|08[|15Message Menu|08] |07Command |08->|15 "},
	{"QWKM", "PROMPT2", "|08[|15QWK Mail|08] |07Command |08->|15 "},
	{"RUMORM", "PROMPT2", "|08[|15Rumors|08] |07Command |08->|15 "},
	{"SPONSORM", "PROMPT2", "|08[|15Sponsor|08] |07Command |08->|15 "},
	{"V3NETM", "PROMPT2", "|08[|15V3Net|08] |07Command |08->|15 "},
	{"EMAILM", "PROMPT1", "|08[|07|TIME|08] |08[|15Email Menu|08] |07Command |08->|15 "},
}

// stockSlotExecutor is an executor with the stock theme's colours and no
// colours of the board's own.
func stockSlotExecutor(t *testing.T) *MenuExecutor {
	t.Helper()
	t.Cleanup(func() { ansi.SetColorSlotDefaults([7]string{}) })
	e := promptExecutor(t, nil)
	e.SetStrings(config.StringsConfig{})
	// The stock theme's Yes/No colours, which the Bar slot falls back to.
	e.SetTheme(config.ThemeConfig{YesNoHighlightColor: 31, YesNoRegularColor: 13})
	return e
}

// slotCodeRe finds the caller colour codes.
var slotCodeRe = regexp.MustCompile(`\|C[1-7]`)

// The converted stock strings use the codes, draw what they drew before for
// a caller with the default colours, and take a caller's own.
func TestStockStringsUseColorSlots(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "templates", "configs", "strings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var shipped map[string]string
	if err := json.Unmarshal(data, &shipped); err != nil {
		t.Fatal(err)
	}
	e := stockSlotExecutor(t)
	plain := &user.User{Handle: "Neo"}
	ph := e.sessionPlaceholders(nil, plain, nil, 1, "MAIN", time.Now(), "", false)
	mine := &user.User{Handle: "Neo"}
	mine.SetColorCode(1, "|13")
	mph := e.sessionPlaceholders(nil, mine, nil, 1, "MAIN", time.Now(), "", false)
	for key, old := range convertedStockStrings {
		v, ok := shipped[key]
		if !ok || !slotCodeRe.MatchString(v) {
			t.Errorf("%s: %q uses no colour slot", key, v)
			continue
		}
		// Each slot also sets a black background where its colour has none,
		// so a slot's background can't run on: the same on a black screen.
		got := strings.ReplaceAll(string(e.renderPromptText(v, ph, 0, 0, 1)), "\x1b[40m", "")
		if want := string(ansi.ReplacePipeCodes([]byte(old))); got != want {
			t.Errorf("%s draws %q with the default colours, want %q as before", key, got, want)
		}
		if strings.Contains(v, "|C1") && !strings.Contains(string(e.renderPromptText(v, mph, 0, 0, 1)), esc("|13")) {
			t.Errorf("%s doesn't take the caller's prompt colour", key)
		}
	}
}

// The same for the stock menu prompts.
func TestStockMenuPromptsUseColorSlots(t *testing.T) {
	e := stockSlotExecutor(t)
	plain := &user.User{Handle: "Neo"}
	mine := &user.User{Handle: "Neo"}
	mine.SetColorCode(2, "|13")
	for _, p := range convertedStockPrompts {
		data, err := os.ReadFile(filepath.Join("..", "..", "menus", "v3", "mnu", p.menu+".MNU"))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		v, _ := m[p.key].(string)
		if !slotCodeRe.MatchString(v) {
			t.Errorf("%s %s: %q uses no colour slot", p.menu, p.key, v)
			continue
		}
		at := time.Date(2026, 11, 16, 9, 30, 0, 0, time.UTC)
		ph := e.sessionPlaceholders(nil, plain, nil, 1, p.menu, at, "", false)
		// The same on a black screen; each slot also sets black where its
		// colour has no background, so a background can't run on.
		got := strings.ReplaceAll(string(e.renderPromptText(v, ph, 0, 0, 1)), "\x1b[40m", "")
		if want := string(e.renderPromptText(p.old, ph, 0, 0, 1)); got != want {
			t.Errorf("%s %s draws %q, want %q as before", p.menu, p.key, got, want)
		}
		mph := e.sessionPlaceholders(nil, mine, nil, 1, p.menu, at, "", false)
		if got := string(e.renderPromptText(v, mph, 0, 0, 1)); !strings.HasSuffix(got, slotEsc("|13")+" ") {
			t.Errorf("%s %s: the caller's input colour isn't last: %q", p.menu, p.key, got)
		}
	}
}

// A converted key's fallback in code, used where strings.json leaves it out
// or blank, is the shipped value, so it follows the caller's colours too.
func TestConvertedStringFallbacksMatchTemplate(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "templates", "configs", "strings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var shipped map[string]string
	if err := json.Unmarshal(data, &shipped); err != nil {
		t.Fatal(err)
	}
	for key := range convertedStockStrings {
		for name, m := range map[string]map[string]string{"StringFallbacks": config.StringFallbacks, "StringMissingDefaults": config.StringMissingDefaults} {
			if v, ok := m[key]; ok && v != shipped[key] {
				t.Errorf("%s[%s] = %q, want the shipped %q", name, key, v, shipped[key])
			}
		}
	}
}

// MAIN's prompts are written with the caller colour codes, like the other
// stock menus', so a caller's colours apply there too; what they type
// after PROMPT2 is in their input colour.
func TestStockMainPromptUsesColorSlots(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "menus", "v3", "mnu", "MAIN.MNU"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	fixed := regexp.MustCompile(`\|[0-9]{2}`)
	for _, key := range []string{"PROMPT1", "PROMPT2"} {
		v, _ := m[key].(string)
		if !slotCodeRe.MatchString(v) || fixed.MatchString(v) {
			t.Errorf("MAIN %s = %q, want caller colour codes only", key, v)
		}
	}
	if v, _ := m["PROMPT2"].(string); !strings.HasSuffix(v, "|C2 ") {
		t.Errorf("MAIN PROMPT2 = %q, want it to end in the input colour", v)
	}
}
