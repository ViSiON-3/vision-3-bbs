//go:build !windows

package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// readMenuInput runs readStandardInput on a MAIN menu whose default prompt is
// value, with the fixture's session registered, and returns what the loop
// got back and the output. It fails if the read is still waiting after 10s.
func (f *stringHookFixture) readMenuInput(t *testing.T, value string) (st *runLoopState, input string, act loopAction, out string) {
	t.Helper()
	f.e.SetStrings(config.StringsConfig{DefPrompt: value, ExecAccessDenied: "[denied]"})
	terminal := newDoorcovTerminal(f.s.doorcovSession)
	RegisterStringHookSession(terminal, f.s, nil, f.bbs)
	defer ClearStringHookSession(terminal)
	defer resetSessionIH(f.s)
	st = &runLoopState{e: f.e, s: f.s, terminal: terminal, nodeNumber: 3, sessionStartTime: time.Now(),
		outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
		currentMenuName: "MAIN", currentUser: f.bbs.User}
	type result struct {
		input string
		act   loopAction
		err   error
	}
	done := make(chan result, 1)
	go func() {
		in, a, err := st.readStandardInput(&MenuRecord{UsePrompt: true})
		done <- result{in, a, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("readStandardInput: %v; output:\n%s", r.err, f.term())
		}
		return st, r.input, r.act, f.term()
	case <-time.After(10 * time.Second):
		t.Fatalf("readStandardInput still waiting after 10s; output:\n%s", f.term())
		return nil, "", 0, ""
	}
}

// A script in place of the menu prompt can answer with a menu to go to, a
// logoff, or the command line the caller would have typed (#645 via #646).
func TestMenuPromptScriptAnswer(t *testing.T) {
	t.Run("GOTO changes menu without reading input", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		st, _, act, out := f.readMenuInput(t, "!ANSWER goto")
		if act != loopContinue || st.currentMenuName != "WHOSON" || st.previousMenuName != "MAIN" {
			t.Errorf("act=%v current=%q previous=%q", act, st.currentMenuName, st.previousMenuName)
		}
		if !strings.Contains(out, "[kind menu]") {
			t.Errorf("output=%q", out)
		}
	})

	t.Run("LOGOFF ends the call", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		_, input, act, _ := f.readMenuInput(t, "!ANSWER logoff")
		if act != loopReturn || input != "LOGOFF" {
			t.Errorf("act=%v input=%q", act, input)
		}
	})

	t.Run("other text is the command line", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		st, input, act, _ := f.readMenuInput(t, "!ANSWER cmd")
		if act != loopFallthrough || input != "W" || st.userInput != "W" {
			t.Errorf("act=%v input=%q userInput=%q", act, input, st.userInput)
		}
	})

	t.Run("menu the caller may not enter is refused", func(t *testing.T) {
		f := newStringHookFixture(t, "x\r")
		st, input, act, out := f.readMenuInput(t, "!ANSWER locked")
		if act != loopFallthrough || input != "X" || st.currentMenuName != "MAIN" {
			t.Errorf("act=%v input=%q current=%q", act, input, st.currentMenuName)
		}
		if !strings.Contains(out, "[denied]") {
			t.Errorf("caller not told: %q", out)
		}
	})

	t.Run("unknown menu falls back to reading input", func(t *testing.T) {
		f := newStringHookFixture(t, "x\r")
		st, input, act, _ := f.readMenuInput(t, "!ANSWER nomenu")
		if act != loopFallthrough || input != "X" || st.currentMenuName != "MAIN" {
			t.Errorf("act=%v input=%q current=%q", act, input, st.currentMenuName)
		}
	})

	t.Run("non-text answer falls back to reading input", func(t *testing.T) {
		f := newStringHookFixture(t, "x\r")
		_, input, act, _ := f.readMenuInput(t, "!ANSWER yes")
		if act != loopFallthrough || input != "X" {
			t.Errorf("act=%v input=%q", act, input)
		}
	})

	t.Run("script that does not answer reads input", func(t *testing.T) {
		f := newStringHookFixture(t, "x\r")
		_, input, act, out := f.readMenuInput(t, "!ANSWER silent")
		if act != loopFallthrough || input != "X" || !strings.Contains(out, "[kind menu]") {
			t.Errorf("act=%v input=%q output=%q", act, input, out)
		}
	})
}
