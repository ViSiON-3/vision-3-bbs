//go:build !windows

package menu

import (
	"strings"
	"testing"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// A script run in place of a Yes/No prompt can answer it (#646). The
// fixture's ANSWER door answers as its arguments say; see answer.js there.
func TestPromptYesNoScriptAnswer(t *testing.T) {
	yesNo := func(value string, termHeight int) func(*MenuExecutor, ssh.Session, *term.Terminal) (bool, error) {
		return func(e *MenuExecutor, s ssh.Session, tm *term.Terminal) (bool, error) {
			return e.PromptYesNoString(s, tm, value, ansi.OutputModeUTF8, 3, 80, termHeight, false)
		}
	}

	t.Run("boolean answer skips the lightbar", func(t *testing.T) {
		// No input: a prompt that read the caller would hang.
		f := newStringHookFixture(t, "")
		yes, out := f.prompt(t, yesNo("!ANSWER yes", 24))
		if !yes || !strings.Contains(out, "[kind yesno]") || strings.Contains(out, " Yes ") {
			t.Errorf("yes=%v output=%q", yes, out)
		}
	})

	t.Run("false answer", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		if yes, _ := f.prompt(t, yesNo("!ANSWER no", 24)); yes {
			t.Error("answered false, got yes")
		}
	})

	t.Run("string answer", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		if yes, _ := f.prompt(t, yesNo("!ANSWER str", 0)); !yes {
			t.Error(`answered "Yes", got no`)
		}
	})

	t.Run("formatted value passes its arguments", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		yes, _ := f.prompt(t, func(e *MenuExecutor, s ssh.Session, tm *term.Terminal) (bool, error) {
			return e.PromptYesNof(s, tm, ansi.OutputModeUTF8, 3, 80, 24, false, "!ANSWER", "yes")
		})
		if !yes {
			t.Error("script given yes as a formatted argument did not answer yes")
		}
	})

	t.Run("answer of another type asks the caller", func(t *testing.T) {
		f := newStringHookFixture(t, "n\r")
		yes, out := f.prompt(t, yesNo("!ANSWER text", 0))
		if yes || !strings.Contains(out, "[y/N]? ") {
			t.Errorf("yes=%v output=%q", yes, out)
		}
	})

	t.Run("answer with no value asks the caller", func(t *testing.T) {
		f := newStringHookFixture(t, "y\r")
		yes, out := f.prompt(t, yesNo("!ANSWER", 0))
		if !yes || !strings.Contains(out, "[y/N]? ") {
			t.Errorf("yes=%v output=%q", yes, out)
		}
	})

	t.Run("script that fails after answering asks the caller", func(t *testing.T) {
		f := newStringHookFixture(t, "n\r")
		yes, out := f.prompt(t, yesNo("!ANSWER throw", 0))
		if yes || !strings.Contains(out, "[y/N]? ") {
			t.Errorf("yes=%v output=%q", yes, out)
		}
	})

	t.Run("script that does not answer asks the caller", func(t *testing.T) {
		f := newStringHookFixture(t, "y\r")
		yes, out := f.prompt(t, yesNo("!ANSWER silent", 0))
		if !yes || !strings.Contains(out, "[y/N]? ") {
			t.Errorf("yes=%v output=%q", yes, out)
		}
	})
}

// pause runs the centred pause prompt with value, with the session registered,
// and returns its output. It fails if the prompt is still waiting after 10s.
func (f *stringHookFixture) pause(t *testing.T, value string) string {
	t.Helper()
	terminal := newDoorcovTerminal(f.s.doorcovSession)
	RegisterStringHookSession(terminal, f.s, nil, f.bbs)
	defer ClearStringHookSession(terminal)
	defer resetSessionIH(f.s)
	done := make(chan error, 1)
	go func() {
		done <- f.e.writeCenteredPausePrompt(f.s, terminal, value, ansi.OutputModeUTF8, 80, 24)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("pause: %v; output:\n%s", err, f.term())
		}
	case <-time.After(handlerWait):
		t.Fatalf("pause still waiting after %v; output:\n%s", handlerWait, f.term())
	}
	return f.term()
}

func TestPauseScriptAnswer(t *testing.T) {
	t.Run("answered pause does not wait for Enter", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		if out := f.pause(t, "!ANSWER"); !strings.Contains(out, "[kind pause]") {
			t.Errorf("output=%q", out)
		}
	})

	t.Run("unanswered pause waits for Enter", func(t *testing.T) {
		f := newStringHookFixture(t, "x\r")
		out := f.pause(t, "!ANSWER silent")
		if !strings.Contains(out, "[kind pause]") {
			t.Errorf("output=%q", out)
		}
	})
}

// pauseAt runs fn, which ends at a pause prompt, with the session registered,
// and returns the output. It fails if fn is still waiting after handlerWait.
func (f *stringHookFixture) pauseAt(t *testing.T, fn func(*term.Terminal)) string {
	t.Helper()
	terminal := newDoorcovTerminal(f.s.doorcovSession)
	RegisterStringHookSession(terminal, f.s, nil, f.bbs)
	defer ClearStringHookSession(terminal)
	defer resetSessionIH(f.s)
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(terminal)
	}()
	select {
	case <-done:
	case <-time.After(handlerWait):
		t.Fatalf("pause still waiting after %v; output:\n%s", handlerWait, f.term())
	}
	return f.term()
}

// The file viewer's pause is printed where the cursor is rather than centred.
func TestFilePauseScriptAnswer(t *testing.T) {
	pause := func(f *stringHookFixture, value string) string {
		return f.pauseAt(t, func(tm *term.Terminal) {
			f.e.pauseEnter(f.s, tm, ansi.OutputModeUTF8, value)
		})
	}
	t.Run("answered pause does not wait for Enter", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		if out := pause(f, "!ANSWER"); !strings.Contains(out, "[kind pause]") {
			t.Errorf("output=%q", out)
		}
	})
	t.Run("unanswered pause waits for Enter", func(t *testing.T) {
		f := newStringHookFixture(t, "x\r")
		if out := pause(f, "!ANSWER silent"); !strings.Contains(out, "[kind pause]") {
			t.Errorf("output=%q", out)
		}
	})
}

// RUN:NEWUSER runs the signup form for a caller who is logged in, so its
// pauses can be scripts.
func TestSignupPauseScriptAnswer(t *testing.T) {
	signup := func(t *testing.T, f *stringHookFixture, um *user.UserMgr) (*user.User, string) {
		var created *user.User
		out := f.pauseAt(t, func(tm *term.Terminal) {
			created, _ = f.e.handleNewUserApplication(f.s, tm, um, 3, ansi.OutputModeUTF8, 80, 0)
		})
		return created, out
	}
	t.Run("signups closed", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		f.e.SetStrings(config.StringsConfig{PauseString: "!ANSWER"})
		if _, out := signup(t, f, nil); !strings.Contains(out, "[kind pause]") {
			t.Errorf("output=%q", out)
		}
	})
	t.Run("account created", func(t *testing.T) {
		f := newStringHookFixture(t, "Y\rFresh\rpw123\rpw123\rFresh Face\r\rHere\r")
		f.e.SetStrings(config.StringsConfig{PauseString: "!ANSWER"})
		cfg := f.e.GetServerConfig()
		cfg.AllowNewUsers = true
		f.e.SetServerConfig(cfg)
		um, err := user.NewUserManager(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		created, out := signup(t, f, um)
		if created == nil || !strings.Contains(out, "[kind pause]") {
			t.Errorf("created=%v output=%q", created, out)
		}
	})
}
