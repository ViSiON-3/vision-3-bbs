package menu

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestSplitChain(t *testing.T) {
	cases := map[string][]string{
		"m;n":         {"M", "N"},
		" m ;; n ; ":  {"M", "N"},
		";m;n":        {"M", "N"},
		"/g;q":        {"/G", "Q"},
		"t i;42":      {"T I", "42"},
		";":           nil,
		" ; ;; ":      nil,
		"single":      {"SINGLE"},
		"^p;a":        {"^P", "A"},
		"tab\t;\tnew": {"TAB", "NEW"},
	}
	for in, want := range cases {
		if got := splitChain(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitChain(%q) = %q, want %q", in, got, want)
		}
	}
}

// execcovChainEnv is the dispatch test set (see execcovDispatchEnv) with MAIN
// leading to CMDS on C, CMDS leading back on M, and an ASK command that reads
// a line from the keyboard the way an interactive RUN handler does.
func execcovChainEnv(t *testing.T) (env *menuEnv, m *execcovMenus, args *[]string, asked *[]string) {
	t.Helper()
	env, m, args = execcovDispatchEnv(t)
	asked = &[]string{}
	env.e.RunRegistry["EXECCOVASK"] = func(c *cmdCtx, _ string) (*user.User, string, error) {
		line, err := readLineFromSessionIH(c.s, c.terminal)
		if err != nil {
			return c.currentUser, "", err
		}
		*asked = append(*asked, line)
		return c.currentUser, "", nil
	}
	m.menu("MAIN", MenuRecord{}, "MAIN-SCREEN\r\n",
		CommandRecord{Keys: "A", Command: "GOTO:sub"},
		CommandRecord{Keys: "C", Command: "GOTO:CMDS"},
		CommandRecord{Keys: "Q", Command: "LOGOFF"})
	m.menu("CMDS", MenuRecord{}, "CMDS-SCREEN\r\n",
		CommandRecord{Keys: "W X", Command: "RUN:execcovargs one two"},
		CommandRecord{Keys: "##", Command: "RUN:EXECCOVARGS"},
		CommandRecord{Keys: "TI", Command: "RUN:EXECCOVARGS ti"},
		CommandRecord{Keys: "K", Command: "RUN:EXECCOVASK"},
		CommandRecord{Keys: "J", Command: "RUN:EXECCOVGOTO"},
		CommandRecord{Keys: "L", Command: "RUN:EXECCOVLOGOFF"},
		CommandRecord{Keys: "E", Command: "RUN:EXECCOVFAIL"},
		CommandRecord{Keys: "N", Command: "RUN:NOSUCHTHING"},
		CommandRecord{Keys: "B", Command: "BOGUS:THING"},
		CommandRecord{Keys: "S", Command: "RUN:EXECCOVARGS staff", ACS: "S255"},
		CommandRecord{Keys: "M", Command: "GOTO:MAIN"},
		CommandRecord{Keys: "Q", Command: "LOGOFF"})
	m.menu("SUB", MenuRecord{}, "SUB-SCREEN\r\n",
		CommandRecord{Keys: "C", Command: "GOTO:CMDS"},
		CommandRecord{Keys: "Q", Command: "LOGOFF"})
	return env, m, args, asked
}

// Each command of a chain runs at the menu the one before it reached, and
// the screens on the way are still drawn.
func TestExeccovChain_RunsAcrossMenus(t *testing.T) {
	env, _, args, _ := execcovChainEnv(t)

	r := execcovRun(env.sub(t), execcovCall{user: env.caller, start: "MAIN", input: "c;w\rQ\r"})
	if r.err != nil || r.next != "LOGOFF" || strings.Join(*args, "|") != "one two" {
		t.Fatalf("next=%q err=%v args=%q output:\n%s", r.next, r.err, *args, r.text())
	}
	if execcovCount(r, "MAIN-SCREEN") != 1 || execcovCount(r, "CMDS-SCREEN") != 2 {
		t.Errorf("want MAIN once, CMDS before and after W:\n%s", r.text())
	}

	// Through three menus and off, without another keystroke.
	*args = nil
	r = execcovRun(env.sub(t), execcovCall{user: env.caller, start: "MAIN", input: "a;c;m;q\r"})
	if r.err != nil || r.next != "LOGOFF" || execcovHandle(r.user) != "Caller" {
		t.Fatalf("next=%q user=%s err=%v", r.next, execcovHandle(r.user), r.err)
	}
	if !r.has("SUB-SCREEN", "CMDS-SCREEN") || execcovCount(r, "MAIN-SCREEN") != 2 {
		t.Errorf("a;c;m;q:\n%s", r.text())
	}

	// A RUN handler that sends the caller elsewhere: the next command is
	// matched where it sent them.
	r = execcovRun(env.sub(t), execcovCall{user: env.caller, start: "CMDS", input: "j;c;q\r"})
	if r.next != "LOGOFF" || !r.has("SUB-SCREEN") || execcovCount(r, "CMDS-SCREEN") != 2 || r.has("UNKNOWN-CMD") {
		t.Errorf("j;c;q: next=%q output:\n%s", r.next, r.text())
	}

	// Commands that stay on the same menu.
	*args = nil
	execcovRun(env.sub(t), execcovCall{user: env.caller, start: "CMDS", input: "w;x;q\r"})
	if got := strings.Join(*args, "|"); got != "one two|one two" {
		t.Errorf("w;x;q args = %q", got)
	}
}

// Segments are trimmed and upper-cased, empty ones are skipped, and
// aliases, multi-key commands and numbers match as they would typed alone.
func TestExeccovChain_Normalising(t *testing.T) {
	env, _, args, _ := execcovChainEnv(t)
	r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: " c ;; x;ti ; 42 ;q;\r"})
	if r.next != "LOGOFF" || r.has("UNKNOWN-CMD") {
		t.Fatalf("next=%q output:\n%s", r.next, r.text())
	}
	if got := strings.Join(*args, "|"); got != "one two|ti|42" {
		t.Errorf("args = %q, want one two|ti|42", got)
	}
}

// The commands waiting in a chain are menu selections, not keystrokes: an
// interactive command still reads its answers from the keyboard, and the
// next command runs only once it returns.
func TestExeccovChain_NotKeyboardInput(t *testing.T) {
	env, _, args, asked := execcovChainEnv(t)
	r := execcovRun(env, execcovCall{user: env.caller, start: "CMDS", input: "k;w;q\ranswer\r"})
	if r.err != nil || r.next != "LOGOFF" {
		t.Fatalf("next=%q err=%v", r.next, r.err)
	}
	if got := strings.Join(*asked, "|"); got != "answer" {
		t.Errorf("handler read %q, want the typed answer", got)
	}
	if got := strings.Join(*args, "|"); got != "one two" {
		t.Errorf("W after K: args = %q", got)
	}
}

// A command that cannot run drops the rest of the chain, after the usual
// treatment, and the next selection comes from the keyboard.
func TestExeccovChain_StopsOnTrouble(t *testing.T) {
	env, m, args, _ := execcovChainEnv(t)
	cases := []struct {
		name, input, want string
	}{
		{"unknown command", "z;w\rQ\r", "UNKNOWN-CMD"},
		{"command the caller may not use", "s;w\rQ\r", "UNKNOWN-CMD"},
		{"handler error", "e;w\rQ\r", "RUNERR EXECCOVFAIL: boom"},
		{"no such handler", "n;w\rQ\r", "NOTFOUND NOSUCHTHING"},
		{"unrecognised command type", "b;w\rQ\r", "CMDS-SCREEN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			*args = nil
			r := execcovRun(env.sub(t), execcovCall{user: env.caller, start: "CMDS", input: tc.input})
			if r.err != nil || r.next != "LOGOFF" || !r.has(tc.want) {
				t.Errorf("next=%q err=%v output:\n%s", r.next, r.err, r.text())
			}
			if len(*args) != 0 {
				t.Errorf("W ran after the chain should have stopped: %q", *args)
			}
			if execcovCount(r, "CMDS-SCREEN") != 2 {
				t.Errorf("want the menu back once for Q:\n%s", r.text())
			}
		})
	}

	t.Run("fallback menu", func(t *testing.T) {
		m.menu("MAIN", MenuRecord{Fallback: "sub"}, "MAIN-SCREEN\r\n",
			CommandRecord{Keys: "Q", Command: "LOGOFF"})
		r := execcovRun(env.sub(t), execcovCall{user: env.caller, start: "MAIN", input: "z;c\rQ\r"})
		// The fallback is taken, and the next selection there comes from the
		// keyboard, not from the chain's C.
		if r.next != "LOGOFF" || !r.has("SUB-SCREEN") || r.has("CMDS-SCREEN") {
			t.Errorf("next=%q output:\n%s", r.next, r.text())
		}
	})

	t.Run("logoff", func(t *testing.T) {
		*args = nil
		r := execcovRun(env.sub(t), execcovCall{user: env.caller, start: "CMDS", input: "l;w\r"})
		if r.next != "LOGOFF" || execcovHandle(r.user) != "Caller" || len(*args) != 0 {
			t.Errorf("next=%q user=%s args=%q", r.next, execcovHandle(r.user), *args)
		}
	})
}

// A chain that is only separators runs nothing, not even the menu's Enter
// default, and plain Enter keeps that default.
func TestExeccovChain_OnlySeparators(t *testing.T) {
	env, m, args, _ := execcovChainEnv(t)
	m.menu("DEF", MenuRecord{}, "DEF-SCREEN\r\n",
		CommandRecord{Keys: "^M", Command: "RUN:EXECCOVARGS enter"},
		CommandRecord{Keys: "Q", Command: "LOGOFF"})
	r := execcovRun(env, execcovCall{user: env.caller, start: "DEF", input: ";\r ; ;\r\rQ\r"})
	if r.next != "LOGOFF" || r.has("UNKNOWN-CMD") || execcovCount(r, "DEF-SCREEN") != 4 {
		t.Errorf("next=%q output:\n%s", r.next, r.text())
	}
	if got := strings.Join(*args, "|"); got != "enter" {
		t.Errorf("args = %q, want just Enter's default", got)
	}
}

// A menu that binds a key containing the separator keeps it.
func TestExeccovChain_LiteralSeparatorKey(t *testing.T) {
	env, m, args, _ := execcovChainEnv(t)
	m.menu("SEMI", MenuRecord{}, "SEMI-SCREEN\r\n",
		CommandRecord{Keys: "A;B", Command: "RUN:EXECCOVARGS literal"},
		CommandRecord{Keys: "A", Command: "RUN:EXECCOVARGS a"},
		CommandRecord{Keys: "B", Command: "RUN:EXECCOVARGS b"},
		CommandRecord{Keys: "Q", Command: "LOGOFF"})
	execcovRun(env, execcovCall{user: env.caller, start: "SEMI", input: "a;b\rb;a\rQ\r"})
	if got := strings.Join(*args, "|"); got != "literal|b|a" {
		t.Errorf("args = %q, want literal|b|a", got)
	}
}

// Each menu a chain passes through still asks its password and runs its
// autorun commands before the next command is taken.
func TestExeccovChain_MenuGatesStillApply(t *testing.T) {
	env, m, _, _ := execcovChainEnv(t)
	var order []string
	env.e.RunRegistry["EXECCOVNOTE"] = func(c *cmdCtx, a string) (*user.User, string, error) {
		order = append(order, a)
		return c.currentUser, "", nil
	}
	m.menu("MAIN", MenuRecord{}, "MAIN-SCREEN\r\n",
		CommandRecord{Keys: "V", Command: "GOTO:VAULT"},
		CommandRecord{Keys: "Q", Command: "LOGOFF"})
	m.menu("VAULT", MenuRecord{Password: "sesame"}, "VAULT-SCREEN\r\n",
		CommandRecord{Keys: "~~", Command: "RUN:EXECCOVNOTE autorun"},
		CommandRecord{Keys: "N", Command: "RUN:EXECCOVNOTE n"},
		CommandRecord{Keys: "Q", Command: "LOGOFF"})
	execcovStrings(env, func(s *config.StringsConfig) {
		s.ExecMenuPasswordPrompt = "\r\nPW %s try %d: "
		s.ExecPasswordAccepted = "PW-OK\r\n"
	})

	// The password comes from the keyboard, not the chain, and is asked on
	// each pass through the menu as it is for typed commands.
	r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "v;n;q\rsesame\rsesame\r"})
	if r.err != nil || r.next != "LOGOFF" || execcovCount(r, "PW-OK") != 2 || r.has("try 2") {
		t.Fatalf("next=%q err=%v output:\n%s", r.next, r.err, r.text())
	}
	if got := strings.Join(order, ","); got != "autorun,n,autorun" {
		t.Errorf("order = %q, want autorun,n,autorun", got)
	}
}

// ^P in a chain goes back as it does typed.
func TestExeccovChain_BackNavigation(t *testing.T) {
	env, _, args, _ := execcovChainEnv(t)
	r := execcovRun(env, execcovCall{user: env.caller, start: "MAIN", input: "a;^p;c;w;q\r"})
	if r.next != "LOGOFF" || r.has("UNKNOWN-CMD") || execcovCount(r, "MAIN-SCREEN") != 2 || strings.Join(*args, "|") != "one two" {
		t.Errorf("next=%q args=%q output:\n%s", r.next, *args, r.text())
	}
}

// With hot keys a single key still acts at once; the separator typed first
// opens a line for a chain.
func TestExeccovChain_HotKeys(t *testing.T) {
	env, m, args, _ := execcovChainEnv(t)
	m.menu("HOT", MenuRecord{ForceHotKey: true}, "HOT-SCREEN\r\n",
		CommandRecord{Keys: "C", Command: "GOTO:CMDS"},
		CommandRecord{Keys: "Q", Command: "LOGOFF"})

	r := execcovRun(env.sub(t), execcovCall{user: env.caller, start: "HOT", input: ";c;w;q\r"})
	if r.next != "LOGOFF" || r.has("UNKNOWN-CMD") || strings.Join(*args, "|") != "one two" {
		t.Errorf("next=%q args=%q output:\n%s", r.next, *args, r.text())
	}
	if !strings.Contains(strings.ToUpper(r.text()), ";C;W;Q") {
		t.Errorf("the chain should be echoed as typed:\n%s", r.text())
	}

	r = execcovRun(env.sub(t), execcovCall{user: env.caller, start: "HOT", input: "c"})
	if !r.has("CMDS-SCREEN") {
		t.Errorf("hot key C should still act without Enter:\n%s", r.text())
	}
}

// At a lightbar menu the separator opens a command line, unless one of the
// bar's options has it as its hotkey.
func TestExeccovChain_Lightbar(t *testing.T) {
	env, m, _, _ := execcovChainEnv(t)
	var picks []string
	env.e.RunRegistry["EXECCOVPICK"] = func(c *cmdCtx, a string) (*user.User, string, error) {
		picks = append(picks, a)
		return c.currentUser, "", nil
	}
	m.menu("BARM", MenuRecord{}, "BARM-SCREEN\r\n",
		CommandRecord{Keys: "A", Command: "RUN:EXECCOVPICK A"},
		CommandRecord{Keys: "B", Command: "RUN:EXECCOVPICK B"},
		CommandRecord{Keys: ";", Command: "RUN:EXECCOVPICK semi"},
		CommandRecord{Keys: "Q", Command: "LOGOFF"})
	m.write("bar", "BARM.BAR", execcovBar)

	r := execcovRun(env.sub(t), execcovCall{user: env.caller, start: "BARM", input: ";b;a;q\r"})
	if r.err != nil || r.next != "LOGOFF" || strings.Join(picks, ",") != "B,A" {
		t.Errorf("next=%q err=%v picks=%q output:\n%s", r.next, r.err, picks, r.text())
	}

	// Erasing the line and pressing Enter just redraws the bar.
	picks = nil
	r = execcovRun(env.sub(t), execcovCall{user: env.caller, start: "BARM", input: ";\x7f\rb"})
	if strings.Join(picks, ",") != "B" || execcovCount(r, "BARM-SCREEN") < 2 {
		t.Errorf("picks=%q output:\n%s", picks, r.text())
	}

	// A bar option bound to the separator is picked instead.
	m.write("bar", "BARM.BAR", execcovBar+"1,4,31,5,;,UNUSED,Semi\n")
	picks = nil
	execcovRun(env.sub(t), execcovCall{user: env.caller, start: "BARM", input: ";"})
	if strings.Join(picks, ",") != "semi" {
		t.Errorf("picks = %q, want the ; option", picks)
	}
}
