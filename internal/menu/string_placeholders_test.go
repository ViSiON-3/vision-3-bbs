//go:build !windows

package menu

import (
	"sort"
	"strings"
	"testing"
	"time"

	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// TestSessionPlaceholderKeys keeps the gate list in step with the map: a
// placeholder added to one without the other is either never expanded in a
// string or builds the map for nothing.
func TestSessionPlaceholderKeys(t *testing.T) {
	e := &MenuExecutor{}
	m := e.sessionPlaceholders(nil, nil, nil, 1, "MAIN", time.Now(), "", false)
	var fromMap []string
	for k := range m {
		fromMap = append(fromMap, k)
	}
	listed := append([]string(nil), sessionPlaceholderKeys...)
	sort.Strings(fromMap)
	sort.Strings(listed)
	if strings.Join(fromMap, " ") != strings.Join(listed, " ") {
		t.Fatalf("sessionPlaceholderKeys and sessionPlaceholders disagree:\nmap:  %v\nlist: %v", fromMap, listed)
	}
}

// writeVia writes through fn with the fixture's session registered behind
// the terminal, as write does for writeString and writeStringf.
func (f *stringHookFixture) writeVia(t *testing.T, fn func(terminal *term.Terminal) error) string {
	t.Helper()
	terminal := newDoorcovTerminal(f.s.doorcovSession)
	RegisterStringHookSession(terminal, f.s, nil, f.bbs)
	defer ClearStringHookSession(terminal)
	if err := fn(terminal); err != nil {
		t.Fatal(err)
	}
	return f.term()
}

func TestWriteStringPlaceholders(t *testing.T) {
	t.Run("session values", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		f.bbs.CurrentMenu = "MAIN"
		out := f.write(t, "|UH on node |NODE in |MN, |TL left")
		if !strings.HasPrefix(out, "Neo on node 3 in MAIN, ") || strings.Contains(out, "N/A") {
			t.Fatalf("placeholders not expanded: %q", out)
		}
	})

	t.Run("optional group drops with empty value", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		if out := f.write(t, "|UH|{ (|UN)|}!"); out != "Neo!" {
			t.Fatalf("got %q, want %q", out, "Neo!")
		}
	})

	t.Run("optional group kept with value", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		f.bbs.User.PrivateNote = "SysOp"
		if out := f.write(t, "|UH|{ (|UN)|}!"); out != "Neo (SysOp)!" {
			t.Fatalf("got %q, want %q", out, "Neo (SysOp)!")
		}
	})

	t.Run("formatted argument is not expanded", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		if out := f.write(t, "%s greets |UH", "Trinity|UH"); out != "Trinity|UH greets Neo" {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("doubled pipe is a literal, not a placeholder", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		if out := f.write(t, "||UH is |UH; ||{|UN|} stays"); out != "|UH is Neo; |{|} stays" {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("verb inside a dropped group keeps its argument", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		if out := f.write(t, "|{Subject: %s |GL|}Count: %d", "hello", 7); out != "Count: 7" {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("published user wins over the session's", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		out := f.writeVia(t, func(terminal *term.Terminal) error {
			SetStringHookUser(terminal, &user.User{ID: 8, Handle: "Trinity", AccessLevel: 60, TimeLimit: 60})
			return f.e.writeString(terminal, ansi.OutputModeUTF8, "|UH/|LEVEL")
		})
		if out != "Trinity/60" {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("centred pause prompt expands placeholders", func(t *testing.T) {
		f := newStringHookFixture(t, "\r")
		out := f.writeVia(t, func(terminal *term.Terminal) error {
			defer resetSessionIH(f.s)
			return f.e.writeCenteredPausePrompt(f.s, terminal, "|UH, press Enter", ansi.OutputModeUTF8, 80, 24)
		})
		if !strings.Contains(out, "Neo, press Enter") || strings.Contains(out, "|UH") {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("percent in a value survives formatting", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		f.bbs.User.Handle = "100%"
		if out := f.write(t, "%s: |UH", "x"); out != "x: 100%" {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("call site keeps its own code", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		f.bbs.User.PrivateNote = "note"
		out := f.writeVia(t, func(terminal *term.Terminal) error {
			return f.e.writeStringSubst(terminal, ansi.OutputModeUTF8, "#|UN for |UH", "|UN", "42")
		})
		if out != "#42 for Neo" {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("substituted value is not expanded", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		out := f.writeVia(t, func(terminal *term.Terminal) error {
			return f.e.writeStringSubst(terminal, ansi.OutputModeUTF8, "|NA", "|NA", "|UH")
		})
		if out != "|UH" {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("text after a file is expanded", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		if out := f.write(t, "%BAR.ANS hi |UH"); out != "[bar art]hi Neo" {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("before login shows the guest defaults", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		f.bbs.User = nil
		if out := f.write(t, "|UH/|LEVEL/|TL"); out != "Guest/0/N/A" {
			t.Fatalf("got %q", out)
		}
	})

	t.Run("no session leaves the text alone", func(t *testing.T) {
		f := newStringHookFixture(t, "")
		terminal := newDoorcovTerminal(f.s.doorcovSession)
		if err := f.e.writeString(terminal, ansi.OutputModeUTF8, "|UH"); err != nil {
			t.Fatal(err)
		}
		if out := f.term(); out != "|UH" {
			t.Fatalf("got %q", out)
		}
	})
}
