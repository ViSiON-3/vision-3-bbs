package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// chatcovMatrix runs the pre-login matrix with input as keystrokes. The
// selected action comes back as runResult.next and any account the matrix
// handed on as runResult.user.
func chatcovMatrix(env *menuEnv, input string) runResult {
	env.t.Helper()
	return env.run(func(c *cmdCtx, _ string) (*user.User, string, error) {
		action, u, err := c.e.RunMatrixScreen(c.s, c.terminal, c.userManager, c.nodeNumber, c.outputMode, c.termWidth, c.termHeight)
		return u, action, err
	}, nil, "", input)
}

// chatcovMatrixSet points env at a private menu set holding only the shipped
// PDMATRIX .ANS/.BAR/.CFG, so a test can remove or rewrite them, and returns
// its directory.
func chatcovMatrixSet(t *testing.T, env *menuEnv) string {
	t.Helper()
	shipped := env.e.MenuSetPath
	set := filepath.Join(t.TempDir(), "menus", "v3")
	for _, f := range [][2]string{{"ansi", "PDMATRIX.ANS"}, {"bar", "PDMATRIX.BAR"}, {"cfg", "PDMATRIX.CFG"}} {
		b, err := os.ReadFile(filepath.Join(shipped, f[0], f[1]))
		if err != nil {
			t.Fatal(err)
		}
		chatcovWriteFile(t, filepath.Join(set, f[0], f[1]), string(b))
	}
	env.e.MenuSetPath = set
	return set
}

// chatcovWriteFile writes content to path, creating its directory.
func chatcovWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// chatcovMatrixArt is text from the shipped PDMATRIX.ANS background (the
// option labels themselves are drawn over it from the .BAR file), so counting
// it counts full-screen draws.
const chatcovMatrixArt = "1. Journey onward."

// chatcovHighlight is what the matrix writes for an option when it is the
// highlighted one (colour 31 in the shipped PDMATRIX.BAR).
func chatcovHighlight(text string) string { return colorCodeToAnsi(31) + text }

func TestChatcovMatrixDrawsAndDisconnects(t *testing.T) {
	env := newMenuEnv(t)

	r := chatcovMatrix(env, "D")
	if r.err != nil || r.next != "DISCONNECT" || r.user != nil {
		t.Fatalf("matrix = (%q, %v, %v), want DISCONNECT", r.next, r.user, r.err)
	}
	if !r.has("Journey onward.Create an account.Check your access.Disconnect.", "Disconnecting...") {
		t.Errorf("matrix output missing options or the goodbye:\n%s", r.text())
	}
	// Options are drawn at their .BAR coordinates, the first one highlighted.
	if !strings.Contains(r.raw, "\x1b[10;49H"+chatcovHighlight("Journey onward.")) {
		t.Errorf("first option not drawn highlighted at 49,10")
	}
	// Typing a hotkey moves the highlight to that option before acting on it.
	if !strings.Contains(r.raw, "\x1b[13;49H"+chatcovHighlight("Disconnect.")) {
		t.Errorf("hotkey D did not highlight the Disconnect option")
	}
	// The cursor hidden while the lightbar is up is shown again on the way out.
	if !strings.HasSuffix(r.raw, "\x1b[?25h") {
		t.Errorf("matrix did not restore the cursor on exit; output ends %q", r.raw[max(0, len(r.raw)-20):])
	}
}

func TestChatcovMatrixNavigation(t *testing.T) {
	const up, down = "\x1b[A", "\x1b[B"
	env := newMenuEnv(t)

	for _, tc := range []struct {
		name, input string
		highlighted string // option the keys should leave highlighted
	}{
		{"down three times", down + down + down + "\r", "Disconnect."},
		{"up wraps to the bottom", up + "\r", "Disconnect."},
		{"down wraps to the top", down + down + down + down + up + "\r", "Disconnect."},
		{"position digit", "4", "Disconnect."},
		{"lower-case hotkey", "d", "Disconnect."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := chatcovMatrix(env.sub(t), tc.input)
			if r.next != "DISCONNECT" || !r.has("Disconnecting...") {
				t.Fatalf("matrix = %q, want DISCONNECT with the goodbye:\n%s", r.next, r.text())
			}
			if !strings.Contains(r.raw, chatcovHighlight(tc.highlighted)) {
				t.Errorf("%q was never highlighted", tc.highlighted)
			}
		})
	}

	// Down once lands on the second option, which is not the login one.
	r := chatcovMatrix(env, down)
	if !strings.Contains(r.raw, chatcovHighlight("Create an account.")) {
		t.Errorf("down arrow did not highlight the second option")
	}
}

func TestChatcovMatrixIgnoredAndUnknownKeys(t *testing.T) {
	env := newMenuEnv(t)

	// Space redraws; a control key and a bare ESC do nothing. None of them
	// select anything, so the session ends (input runs out) without an action
	// having been processed.
	r := chatcovMatrix(env, " \x01\x1b")
	if r.next != "DISCONNECT" || r.has("Disconnecting...") || r.has("Unknown command!") {
		t.Errorf("ignored keys = %q, want a plain disconnect with nothing reported:\n%s", r.next, r.text())
	}
	if n := strings.Count(r.text(), chatcovMatrixArt); n != 2 {
		t.Errorf("screen drawn %d times, want 2 (initial + space redraw)", n)
	}

	// A printable key that matches no option is reported and the screen redrawn.
	r = chatcovMatrix(env, "Z9D")
	if n := strings.Count(r.text(), "Unknown command!"); n != 2 {
		t.Errorf("unknown-command notices = %d, want 2 (Z and 9):\n%s", n, r.text())
	}
	if r.next != "DISCONNECT" || !r.has("Disconnecting...") {
		t.Errorf("matrix = %q after unknown keys, want DISCONNECT", r.next)
	}
}

func TestChatcovMatrixLoginShowsPrelogon(t *testing.T) {
	env := newMenuEnv(t)

	// The shipped set has a PRELOGON.ANS: it is shown and held until Enter.
	r := chatcovMatrix(env, "J\r")
	if r.err != nil || r.next != "LOGIN" || r.user != nil {
		t.Fatalf("matrix = (%q, %v, %v), want LOGIN", r.next, r.user, r.err)
	}
	if !r.has("SlAm eNtEr!") {
		t.Errorf("prelogon screen was not held with the pause prompt:\n%s", r.text())
	}

	// With no PRELOGON file the matrix goes straight on, no pause.
	chatcovMatrixSet(t, env)
	r = chatcovMatrix(env, "J")
	if r.next != "LOGIN" || r.has("SlAm eNtEr!") {
		t.Errorf("matrix without prelogon = %q, want LOGIN and no pause:\n%s", r.next, r.text())
	}
}

// Numbered PRELOGON.n files take precedence over PRELOGON.ANS, stop at the
// first gap, and one of them is shown per login.
func TestChatcovMatrixNumberedPrelogon(t *testing.T) {
	env := newMenuEnv(t)
	set := chatcovMatrixSet(t, env)
	chatcovWriteFile(t, filepath.Join(set, "ansi", "PRELOGON.ANS"), "PRELOGON-FALLBACK\r\n")
	chatcovWriteFile(t, filepath.Join(set, "ansi", "PRELOGON.1"), "PRELOGON-ONE\r\n")
	chatcovWriteFile(t, filepath.Join(set, "ansi", "PRELOGON.2"), "PRELOGON-TWO\r\n")
	chatcovWriteFile(t, filepath.Join(set, "ansi", "PRELOGON.4"), "PRELOGON-FOUR\r\n") // past the gap at .3

	r := chatcovMatrix(env, "J\r")
	if r.next != "LOGIN" {
		t.Fatalf("matrix = %q, want LOGIN", r.next)
	}
	shown := 0
	for _, s := range []string{"PRELOGON-ONE", "PRELOGON-TWO"} {
		if r.has(s) {
			shown++
		}
	}
	if shown != 1 {
		t.Errorf("%d numbered prelogon screens shown, want exactly 1:\n%s", shown, r.text())
	}
	if r.has("PRELOGON-FALLBACK") || r.has("PRELOGON-FOUR") {
		t.Errorf("showed a prelogon file outside the numbered run:\n%s", r.text())
	}

	// Only when there are no numbered files is PRELOGON.ANS used.
	for _, n := range []string{"PRELOGON.1", "PRELOGON.2"} {
		if err := os.Remove(filepath.Join(set, "ansi", n)); err != nil {
			t.Fatal(err)
		}
	}
	if r := chatcovMatrix(env, "J\r"); !r.has("PRELOGON-FALLBACK") || r.has("PRELOGON-FOUR") {
		t.Errorf("want PRELOGON.ANS once the numbered files are gone:\n%s", r.text())
	}
}

func TestChatcovMatrixCheckAccess(t *testing.T) {
	env := newMenuEnv(t)
	env.seedUsers(&user.User{ID: 3, Handle: "Newbie", AccessLevel: 5, TimeLimit: 60})

	for _, tc := range []struct {
		name, handle string
		want         string
		wantPause    bool
	}{
		{"can log on", "Caller", "Account 'Caller' can log on. Access level: 10", true},
		{"below logon level", "Newbie", "Account 'Newbie' cannot log on yet (level 5, minimum 10).", true},
		{"unknown handle", "Nobody", "Username not found.", true},
		{"blank handle", "  ", "Enter your username to check access", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := "A" + tc.handle + "\r"
			if tc.wantPause {
				input += "\r"
			}
			r := chatcovMatrix(env.sub(t), input+"D")
			if !r.has(tc.want) {
				t.Errorf("output missing %q:\n%s", tc.want, r.text())
			}
			if got := r.has("SlAm eNtEr!"); got != tc.wantPause {
				t.Errorf("pause shown = %v, want %v", got, tc.wantPause)
			}
			// Checking access never logs anyone in: it returns to the matrix,
			// where the trailing D is then acted on.
			if r.next != "DISCONNECT" || r.user != nil || !r.has("Disconnecting...") {
				t.Errorf("after check access: (%q, %v), want back at the matrix then DISCONNECT", r.next, r.user)
			}
			if tc.name != "below logon level" && r.has("voting queue") {
				t.Errorf("NUV progress shown with NUV off:\n%s", r.text())
			}
		})
	}
}

func TestChatcovMatrixCheckAccessNUVProgress(t *testing.T) {
	env := newMenuEnv(t)
	env.seedUsers(
		&user.User{ID: 3, Handle: "Newbie", AccessLevel: 5, TimeLimit: 60},
		&user.User{ID: 4, Handle: "Other", AccessLevel: 5, TimeLimit: 60},
	)
	chatcovWriteFile(t, filepath.Join(env.dataDir(), "nuv.json"), `{"candidates":[
		{"handle":"someoneelse","votes":[{"voter":"a","yes":true}]},
		{"handle":"NEWBIE","votes":[{"voter":"a","yes":true},{"voter":"b","yes":true},{"voter":"c","yes":false}]}
	]}`)

	// NUV off: the queue is not mentioned even though the caller is in it.
	if r := chatcovMatrix(env, "ANewbie\r\rD"); r.has("voting queue") {
		t.Errorf("NUV progress shown with NUV disabled:\n%s", r.text())
	}

	cfg := env.e.GetServerConfig()
	cfg.UseNUV = true
	cfg.NUVValidate = false
	cfg.NUVYesVotes = 4
	env.e.SetServerConfig(cfg)
	r := chatcovMatrix(env, "ANewbie\r\rD")
	if !r.has("cannot log on yet", "Your application is in the voting queue.",
		"Votes so far: 2 Yes, 1 No  (4 yes needed to reach threshold)") {
		t.Errorf("NUV progress missing or wrong:\n%s", r.text())
	}

	cfg.NUVValidate = true
	env.e.SetServerConfig(cfg)
	if r := chatcovMatrix(env, "ANewbie\r\rD"); !r.has("(4 yes needed to validate)") {
		t.Errorf("want the auto-validate wording when nuvValidate is on:\n%s", r.text())
	}

	// A caller who cannot log on but is not a candidate gets no vote line.
	if r := chatcovMatrix(env, "AOther\r\rD"); !r.has("Account 'Other' cannot log on yet") || r.has("voting queue") {
		t.Errorf("non-candidate shown NUV progress:\n%s", r.text())
	}
}

func TestChatcovMatrixCheckAccessDefaultPause(t *testing.T) {
	env := newMenuEnv(t)
	strs := *env.e.Strings()
	strs.PauseString = ""
	env.e.SetStrings(strs)

	r := chatcovMatrix(env, "ACaller\r\rD")
	if !r.has("Account 'Caller' can log on.", "Press [ENTER] to continue...") {
		t.Errorf("built-in pause prompt not used when pauseString is empty:\n%s", r.text())
	}
}

func TestChatcovMatrixNewUser(t *testing.T) {
	env := newMenuEnv(t)
	cfg := env.e.GetServerConfig()
	cfg.AllowNewUsers = false
	env.e.SetServerConfig(cfg)

	// Signups closed: the notice is shown and the caller is back at the matrix.
	r := chatcovMatrix(env, "C\rD")
	if !r.has("This BBS is not accepting new users at this time.") {
		t.Errorf("closed-signup notice missing:\n%s", r.text())
	}
	if r.next != "DISCONNECT" || r.user != nil || !r.has("Disconnecting...") {
		t.Errorf("after closed signup: (%q, %v), want back at the matrix then DISCONNECT", r.next, r.user)
	}
	if n := strings.Count(r.text(), chatcovMatrixArt); n < 2 {
		t.Errorf("matrix not redrawn after returning from signup (drawn %d times)", n)
	}

	// Dropping the connection inside the application ends the session.
	cfg.AllowNewUsers = true
	env.e.SetServerConfig(cfg)
	r = chatcovMatrix(env, "C")
	if r.next != "DISCONNECT" || r.user != nil {
		t.Errorf("disconnect during signup = (%q, %v), want DISCONNECT", r.next, r.user)
	}
	if r.has("Disconnecting...") {
		t.Errorf("a dropped connection should not get the goodbye:\n%s", r.text())
	}
	if len(env.um.GetAllUsers()) != 2 {
		t.Errorf("abandoned signup changed the user list")
	}
}

// Actions that return to the matrix are capped, so a caller cannot sit on a
// node cycling through Check Access forever.
func TestChatcovMatrixMaxTries(t *testing.T) {
	env := newMenuEnv(t)

	// A blank handle returns to the matrix immediately. Ten of those use up
	// the tries; the D that follows must never be read.
	r := chatcovMatrix(env, strings.Repeat("A\r", 10)+"D")
	if r.err != nil || r.next != "DISCONNECT" {
		t.Fatalf("matrix = (%q, %v), want DISCONNECT", r.next, r.err)
	}
	if n := strings.Count(r.text(), "Enter your username to check access"); n != 10 {
		t.Errorf("check access ran %d times, want 10", n)
	}
	if r.has("Disconnecting...") {
		t.Errorf("matrix kept reading keys after the tries ran out:\n%s", r.text())
	}
}

func TestChatcovMatrixDisconnectOnEOF(t *testing.T) {
	env := newMenuEnv(t)
	r := chatcovMatrix(env, "")
	if r.next != "DISCONNECT" || r.user != nil {
		t.Errorf("matrix with no input = (%q, %v), want DISCONNECT", r.next, r.user)
	}
	if !r.has("Journey onward.") {
		t.Errorf("matrix was not drawn before the read:\n%s", r.text())
	}
}

func TestChatcovMatrixCP437(t *testing.T) {
	env := newMenuEnv(t)
	env.outputMode = ansi.OutputModeCP437

	r := chatcovMatrix(env, "D")
	if r.next != "DISCONNECT" || !r.has("Journey onward.", "Disconnecting...") {
		t.Errorf("CP437 matrix = %q, want DISCONNECT with the menu drawn:\n%s", r.next, r.text())
	}
	// CP437 callers get the art's bytes as they are on disk, not transcoded.
	art, err := ansi.GetAnsiFileContent(filepath.Join(env.e.MenuSetPath, "ansi", "PDMATRIX.ANS"))
	if err != nil {
		t.Fatal(err)
	}
	if high := chatcovFirstHighByte(art); high >= 0 && !strings.Contains(r.raw, string(art[high:high+1])) {
		t.Errorf("CP437 output does not carry the art's raw byte 0x%02x", art[high])
	}
}

// chatcovFirstHighByte returns the index of the first byte >= 0x80 in b, or -1.
func chatcovFirstHighByte(b []byte) int {
	for i, c := range b {
		if c >= 0x80 {
			return i
		}
	}
	return -1
}

// A matrix that cannot be shown must not strand the caller: every missing or
// empty piece falls through to the login prompt without drawing anything.
func TestChatcovMatrixSkippedWhenFilesUnusable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spoil func(t *testing.T, set string)
	}{
		{"no BAR file", func(t *testing.T, set string) {
			if err := os.Remove(filepath.Join(set, "bar", "PDMATRIX.BAR")); err != nil {
				t.Fatal(err)
			}
		}},
		{"BAR with no options", func(t *testing.T, set string) {
			chatcovWriteFile(t, filepath.Join(set, "bar", "PDMATRIX.BAR"), "; nothing here\n")
		}},
		{"CFG is not JSON", func(t *testing.T, set string) {
			chatcovWriteFile(t, filepath.Join(set, "cfg", "PDMATRIX.CFG"), "{not json")
		}},
		{"no ANS file", func(t *testing.T, set string) {
			if err := os.Remove(filepath.Join(set, "ansi", "PDMATRIX.ANS")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newMenuEnv(t)
			tc.spoil(t, chatcovMatrixSet(t, env))

			r := chatcovMatrix(env, "D")
			if r.err != nil || r.next != "LOGIN" || r.user != nil {
				t.Fatalf("matrix = (%q, %v, %v), want LOGIN", r.next, r.user, r.err)
			}
			if r.raw != "" {
				t.Errorf("skipped matrix still wrote to the session: %q", r.raw)
			}
		})
	}
}

func TestChatcovMatrixUnmappedAndUnknownCommands(t *testing.T) {
	env := newMenuEnv(t)
	set := chatcovMatrixSet(t, env)
	// J has no command at all; C maps to something the matrix does not know.
	chatcovWriteFile(t, filepath.Join(set, "cfg", "PDMATRIX.CFG"), `[
		{"KEYS": "C", "CMD": "BOGUS", "ACS": "*", "HIDDEN": false},
		{"KEYS": "D", "CMD": "disconnect", "ACS": "*", "HIDDEN": false}
	]`)

	// An option with no command is a no-op, not a login.
	r := chatcovMatrix(env, "J")
	if r.next != "DISCONNECT" || r.has("Unknown command!") || r.has("SlAm eNtEr!") {
		t.Errorf("unmapped hotkey = %q, want it ignored until input ran out:\n%s", r.next, r.text())
	}

	// An unknown command is reported and the caller stays on the matrix;
	// command names in the .CFG are matched case-insensitively.
	r = chatcovMatrix(env, "CD")
	if !r.has("Unknown command!") {
		t.Errorf("unknown matrix command not reported:\n%s", r.text())
	}
	if r.next != "DISCONNECT" || !r.has("Disconnecting...") {
		t.Errorf("matrix = %q, want the lower-case disconnect command honoured", r.next)
	}
}
