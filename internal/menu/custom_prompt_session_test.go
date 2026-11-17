//go:build !windows

package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// showPrompt draws menu's prompt for the fixture's caller as the run loop
// does (displayMenuPrompt), or as the screens that read their own input do
// (displayPrompt), and returns the output.
func (f *stringHookFixture) showPrompt(t *testing.T, menu *MenuRecord, runLoop bool) string {
	t.Helper()
	terminal := newDoorcovTerminal(f.s.doorcovSession)
	RegisterStringHookSession(terminal, f.s, nil, f.bbs)
	defer ClearStringHookSession(terminal)
	var err error
	if runLoop {
		_, err = f.e.displayMenuPrompt(f.s, terminal, menu, f.bbs.User, nil, 3, "FILES", time.Now(), ansi.OutputModeUTF8, "")
	} else {
		err = f.e.displayPrompt(f.s, terminal, menu, f.bbs.User, nil, 3, "SPONSORM", time.Now(), ansi.OutputModeUTF8, "")
	}
	if err != nil {
		t.Fatal(err)
	}
	return f.term()
}

// On a USEPROMPT menu a caller's own prompt is the whole prompt, in place of
// PROMPT1 and PROMPT2; the board's customPromptACS decides whose is shown.
func TestCustomPromptIsTheWholePrompt(t *testing.T) {
	menu := &MenuRecord{UsePrompt: true, Prompt1: "[P1]", Prompt2: "[P2]"}
	for _, tc := range []struct {
		name    string
		prompt  string
		acs     string
		runLoop bool
		mine    bool
	}{
		{name: "set", prompt: "|11Mine |UH>", runLoop: true, mine: true},
		{name: "blank is the menu's", prompt: "  ", runLoop: true},
		{name: "screens with their own input keep theirs", prompt: "|11Mine |UH>"},
		{name: "allowed by the ACS", prompt: "|11Mine |UH>", acs: "S50", runLoop: true, mine: true},
		{name: "denied by the ACS", prompt: "|11Mine |UH>", acs: "S100", runLoop: true},
		{name: "an ACS that doesn't parse lets nobody", prompt: "|11Mine |UH>", acs: "S10&", runLoop: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStringHookFixture(t, "")
			f.e.SetServerConfig(config.ServerConfig{CoSysOpLevel: 250, CustomPromptACS: tc.acs})
			f.bbs.User.CustomPrompt = tc.prompt
			out := f.showPrompt(t, menu, tc.runLoop)
			mine := strings.Index(out, "Mine Neo>")
			p2 := strings.Index(out, "[P2]")
			if tc.mine {
				if mine < 0 || strings.Contains(out, "[P1]") || p2 >= 0 {
					t.Errorf("want the caller's prompt in place of both menu prompts: %q", out)
				}
				return
			}
			if mine >= 0 || !strings.Contains(out, "[P1]") || p2 < 0 {
				t.Errorf("want the menu's own prompts: %q", out)
			}
		})
	}
}

// A custom prompt without colours of its own is drawn in the caller's
// prompt colour, and what they type after it, a space on, in their input
// colour; colours in the text still win.
func TestCustomPromptTakesTheCallersColours(t *testing.T) {
	menu := &MenuRecord{UsePrompt: true, Prompt1: "[P1]", Prompt2: "[P2]"}
	f := newStringHookFixture(t, "")
	f.bbs.User.SetColorCode(1, "|13")
	f.bbs.User.SetColorCode(2, "|12")
	f.bbs.User.CustomPrompt = "CHOOSE:"
	out := f.showPrompt(t, menu, true)
	if want := slotEsc("|13") + "CHOOSE:" + slotEsc("|12") + " "; !strings.Contains(out, want) {
		t.Errorf("output %q\nwant %q", out, want)
	}
	f.bbs.User.CustomPrompt = "|11Pick"
	out = f.showPrompt(t, menu, true)
	if !strings.Contains(out, esc("|11")+"Pick"+slotEsc("|12")+" ") {
		t.Errorf("a colour in the text should win: %q", out)
	}
}

// A custom prompt is never a !script or %file value, whatever it starts with,
// and it stands in for the default prompt, so that one's hook doesn't run
// either.
func TestCustomPromptNeverRunsAHook(t *testing.T) {
	for _, prompt := range []string{"!ANSWER goto", "%BAR.ANS then text"} {
		t.Run(prompt, func(t *testing.T) {
			f := newStringHookFixture(t, "x\r")
			f.bbs.User.CustomPrompt = prompt
			st, input, act, out := f.readMenuInput(t, "!ANSWER goto")
			if !strings.Contains(out, prompt) {
				t.Errorf("want the prompt printed as written: %q", out)
			}
			if strings.Contains(out, "[kind") || strings.Contains(out, "[bar art]") {
				t.Errorf("a hook ran: %q", out)
			}
			if act != loopFallthrough || input != "X" || st.currentMenuName != "MAIN" {
				t.Errorf("act=%v input=%q current=%q", act, input, st.currentMenuName)
			}
		})
	}
}
