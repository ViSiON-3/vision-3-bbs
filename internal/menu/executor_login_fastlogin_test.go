package menu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// FASTLOGIN with the shipped FASTLOGN.BAR is a lightbar: each hotkey maps to
// its FASTLOGN.CFG command — 1 continues the full login sequence, 2-4 jump to
// a menu, G logs off normally and /G hangs up at once — and the arrows plus
// Enter pick the highlighted entry.
func TestFastLogin_LightbarChoices(t *testing.T) {
	env := newMenuEnv(t)
	cases := []struct {
		name, input, next string
		keepUser          bool
	}{
		{"full sequence", "1", "", true},
		{"main", "2", "GOTO:MAIN", true},
		{"messages after unknown key", "Z4", "GOTO:MSGMENU", true},
		{"goodbye", "G", "LOGOFF", true},
		{"immediate logoff", "/G", "LOGOFF", false},
		{"enter on first", "\r", "", true},
		{"down enter", "\x1b[B\r", "GOTO:MAIN", true},
		{"down up enter", "\x1b[B\x1b[B\x1b[A\r", "GOTO:MAIN", true},
		{"up at top, esc ignored", "\x1b[A\x1b\r", "", true},
		{"disconnect", "", "LOGOFF", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := env.sub(t).runCmd("FASTLOGIN", env.caller, "", tc.input)
			if r.err != nil {
				t.Fatalf("err = %v", r.err)
			}
			if r.next != tc.next {
				t.Errorf("next = %q, want %q", r.next, tc.next)
			}
			if (r.user == env.caller) != tc.keepUser {
				t.Errorf("user = %v, keepUser %v", r.user, tc.keepUser)
			}
			if !r.has("Normal Login", "Message Menu") {
				t.Errorf("lightbar not drawn: %q", r.text())
			}
		})
	}
}

// Menu jumps in FASTLOGN.CFG are guarded by "!FQ": a caller with the Q flag
// cannot use them and falls through to the next key.
func TestFastLogin_ACSBlocksFlaggedCaller(t *testing.T) {
	env := newMenuEnv(t)
	flagged := *env.caller
	flagged.Flags = "Q"
	r := env.runCmd("FASTLOGIN", &flagged, "", "21")
	if r.next != "" || r.user != &flagged {
		t.Errorf("Q-flagged caller: next=%q user=%v, want the full sequence", r.next, r.user)
	}
}

// fastLoginMenuSet builds a menu set holding only FASTLOGN's .CFG, .ANS and a
// .MNU with the prompt enabled — no .BAR — so FASTLOGIN uses plain hotkeys.
func fastLoginMenuSet(t *testing.T) string {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("..", "..", "menus", "v3"))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "menus", "v3")
	for _, f := range []string{"cfg/FASTLOGN.CFG", "ansi/FASTLOGN.ANS"} {
		b, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dst, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, f), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mnu := `{"TITLE":"Fast Logon","CLR":true,"USEPROMPT":true,"PROMPT1":"Which one, Ace?","PROMPT2":"Pick a number","ACS":"*"}`
	if err := os.MkdirAll(filepath.Join(dst, "mnu"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "mnu", "FASTLOGN.MNU"), []byte(mnu), 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

// Without a lightbar FASTLOGIN shows the menu prompt and reads hotkeys,
// including two-key commands like /G; an unknown key reports an unknown
// command and redraws.
func TestFastLogin_HotkeyFallback(t *testing.T) {
	env := newMenuEnv(t)
	env.e.MenuSetPath = fastLoginMenuSet(t)

	cases := []struct {
		name, input, next string
		wantUser          *user.User
	}{
		{"full sequence", "1", "", env.caller},
		{"file menu", "3", "GOTO:FILEM", env.caller},
		{"immediate logoff", "/g", "LOGOFF", nil},
		{"disconnect", "", "LOGOFF", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := env.sub(t).runCmd("FASTLOGIN", env.caller, "", tc.input)
			if r.next != tc.next || r.user != tc.wantUser {
				t.Errorf("next=%q user=%v, want %q / %v", r.next, r.user, tc.next, tc.wantUser)
			}
			if !r.has("Which one, Ace?", "Pick a number") {
				t.Errorf("prompt not shown: %q", r.text())
			}
		})
	}

	r := env.runCmd("FASTLOGIN", env.caller, "", "\x1bZ1")
	if !r.has("Unknown command!") || r.next != "" || r.user != env.caller {
		t.Errorf("unknown key: next=%q out=%q", r.next, r.text())
	}
}

// An unreadable FASTLOGN.CFG is logged and FASTLOGIN just continues the
// login rather than trapping the caller.
func TestFastLogin_BrokenConfigContinues(t *testing.T) {
	env := newMenuEnv(t)
	set := fastLoginMenuSet(t)
	if err := os.WriteFile(filepath.Join(set, "cfg", "FASTLOGN.CFG"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	env.e.MenuSetPath = set
	r := env.runCmd("FASTLOGIN", env.caller, "", "2")
	if r.next != "" || r.user != env.caller || r.err != nil {
		t.Errorf("next=%q user=%v err=%v", r.next, r.user, r.err)
	}
}

// A missing or empty FASTLOGN.CFG leaves FASTLOGIN nothing to match; it
// continues the login like a malformed CFG instead of answering every key
// with "Unknown command!".
func TestFastLogin_MissingOrEmptyConfigContinues(t *testing.T) {
	for _, tc := range []struct {
		name string
		prep func(cfg string) error
	}{
		{"missing", os.Remove},
		{"empty", func(cfg string) error { return os.WriteFile(cfg, nil, 0o644) }},
		{"empty array", func(cfg string) error { return os.WriteFile(cfg, []byte("[]"), 0o644) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newMenuEnv(t)
			set := fastLoginMenuSet(t)
			if err := tc.prep(filepath.Join(set, "cfg", "FASTLOGN.CFG")); err != nil {
				t.Fatal(err)
			}
			env.e.MenuSetPath = set
			r := env.runCmd("FASTLOGIN", env.caller, "", "1")
			if r.next != "" || r.user != env.caller || r.err != nil {
				t.Errorf("next=%q user=%v err=%v", r.next, r.user, r.err)
			}
			if r.has("Unknown command!") {
				t.Errorf("caller trapped at the FASTLOGIN prompt: %q", r.text())
			}
		})
	}
}
