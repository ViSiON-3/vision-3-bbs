//go:build !windows

package menu

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// preLogin makes f's session one with no caller logged in, as on the
// connection matrix, the signup form and the required-intro gate.
func preLogin(f *stringHookFixture) *stringHookFixture {
	f.bbs.User = nil
	return f
}

// A !door pauseString runs its script door before login too: the pause
// prompt is a prompt, not the door's user-facing launch.
func TestPreLoginPauseRunsScript(t *testing.T) {
	t.Run("shared pause path", func(t *testing.T) {
		// The Enter is there so the unfixed code, which prints the value and
		// waits, finishes and fails on its output rather than timing out.
		f := preLogin(newStringHookFixture(t, "\r"))
		out := f.pause(t, "!ANSWER")
		if !strings.Contains(out, "[kind pause]") || strings.Contains(out, "!ANSWER") {
			t.Errorf("output=%q", out)
		}
	})

	t.Run("unanswered script still waits for Enter", func(t *testing.T) {
		f := preLogin(newStringHookFixture(t, "x\r"))
		if out := f.pause(t, "!ANSWER silent"); !strings.Contains(out, "[kind pause]") {
			t.Errorf("output=%q", out)
		}
	})

	t.Run("door with an access level is printed as text", func(t *testing.T) {
		f := preLogin(newStringHookFixture(t, "\r"))
		out := f.pause(t, "!locked")
		if !strings.Contains(out, "!locked") || strings.Contains(out, "[script") {
			t.Errorf("output=%q", out)
		}
	})

	t.Run("door with an acs is printed as text", func(t *testing.T) {
		f := preLogin(newStringHookFixture(t, "\r"))
		out := f.pause(t, "!flagged")
		if !strings.Contains(out, "!flagged") || strings.Contains(out, "[script") {
			t.Errorf("output=%q", out)
		}
	})

	// A pre-login script runs as nobody: no user record, and none of the APIs
	// that read or write the user, message, file or oneliner stores.
	t.Run("script runs as a guest", func(t *testing.T) {
		f := preLogin(newStringHookFixture(t, "\r"))
		scripts := filepath.Join(f.e.MenuSetPath, "scripts")
		guest := `v3.console.print("[guest id=" + v3.session.user.id + " handle=" + v3.session.user.handle +
" user=" + typeof v3.user + " message=" + typeof v3.message + " file=" + typeof v3.file +
" oneliners=" + typeof v3.oneliners + "]"); v3.prompt.answer();`
		if err := os.WriteFile(filepath.Join(scripts, "guest.js"), []byte(guest), 0o644); err != nil {
			t.Fatal(err)
		}
		doors := f.e.DoorRegistry()
		doors["GUEST"] = config.DoorConfig{Type: "v3_script", Script: "guest.js", WorkingDirectory: scripts}
		f.e.SetDoorRegistry(doors)
		f.e.MessageMgr = nil // left nil by the fixture; stated so the check below means something
		out := f.pause(t, "!GUEST")
		want := "[guest id=0 handle= user=undefined message=undefined file=undefined oneliners=undefined]"
		if !strings.Contains(out, want) {
			t.Errorf("output=%q, want %q", out, want)
		}
	})

	// Other strings shown before login still print a !door value as text.
	t.Run("non-prompt string is unchanged", func(t *testing.T) {
		f := preLogin(newStringHookFixture(t, ""))
		if out := f.write(t, "!say"); !strings.Contains(out, "!say") || strings.Contains(out, "[script") {
			t.Errorf("output=%q", out)
		}
	})
}

// The signup screens a caller sees before login pause with the board's pause
// prompt: the closed-signups notice, the new-user message (NEWUSER.ANS) after
// "Apply for access?", and the pause once the account is made.
func TestPreLoginSignupPauseRunsScript(t *testing.T) {
	signup := func(t *testing.T, f *stringHookFixture, um *user.UserMgr) (*user.User, string) {
		var created *user.User
		out := f.pauseAt(t, func(tm *term.Terminal) {
			created, _ = f.e.handleNewUserApplication(f.s, tm, um, 3, ansi.OutputModeUTF8, 80, 0)
		})
		return created, out
	}

	t.Run("signups closed", func(t *testing.T) {
		f := preLogin(newStringHookFixture(t, "\r"))
		f.e.SetStrings(config.StringsConfig{PauseString: "!ANSWER"})
		if _, out := signup(t, f, nil); !strings.Contains(out, "[kind pause]") || strings.Contains(out, "!ANSWER") {
			t.Errorf("output=%q", out)
		}
	})

	t.Run("new-user message and account created", func(t *testing.T) {
		f := preLogin(newStringHookFixture(t, "Y\rFresh\rpw123\rpw123\rFresh Face\r\rHere\r"))
		if err := os.WriteFile(filepath.Join(f.e.MenuSetPath, "ansi", "NEWUSER.ANS"), []byte("[message to new users]"), 0o644); err != nil {
			t.Fatal(err)
		}
		f.e.SetStrings(config.StringsConfig{PauseString: "!ANSWER"})
		cfg := f.e.GetServerConfig()
		cfg.AllowNewUsers = true
		f.e.SetServerConfig(cfg)
		um, err := user.NewUserManager(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		created, out := signup(t, f, um)
		if created == nil {
			t.Fatalf("no account created; output=%q", out)
		}
		if !strings.Contains(out, "[message to new users]") {
			t.Errorf("NEWUSER.ANS not shown; output=%q", out)
		}
		if n := strings.Count(out, "[kind pause]"); n != 2 || strings.Contains(out, "!ANSWER") {
			t.Errorf("want 2 script pauses and no !ANSWER text, got %d; output=%q", n, out)
		}
	})
}

// The connection matrix's CHECKACCESS result screen is shown before login and
// ends with the pause prompt.
func TestMatrixCheckAccessPauseRunsScript(t *testing.T) {
	f := preLogin(newStringHookFixture(t, "Nobody\r\r"))
	f.e.SetStrings(config.StringsConfig{PauseString: "!ANSWER"})
	um, err := user.NewUserManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	out := f.pauseAt(t, func(tm *term.Terminal) {
		f.e.handleCheckAccess(f.s, tm, um, 3, ansi.OutputModeUTF8)
	})
	if !strings.Contains(out, "[kind pause]") || strings.Contains(out, "!ANSWER") {
		t.Errorf("output=%q", out)
	}
}

// The required-intro gate's NUEMAIL.ANS ("ONE LAST STEP") is shown at a new
// user's first login, before the session has a user, and pauses before the
// editor opens.
func TestIntroGatePauseRunsScript(t *testing.T) {
	f := preLogin(newStringHookFixture(t, "\r"))
	f.e.SetStrings(config.StringsConfig{PauseString: "!ANSWER"})
	if err := os.WriteFile(filepath.Join(f.e.MenuSetPath, "ansi", "NUEMAIL.ANS"), []byte("[one last step]"), 0o644); err != nil {
		t.Fatal(err)
	}
	mm, err := message.NewMessageManager(t.TempDir(), t.TempDir(), "TestBBS", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mm.AddArea(message.MessageArea{Tag: "PRIVMAIL", Name: "Private Mail", AreaType: "local"}); err != nil {
		t.Fatal(err)
	}
	f.e.MessageMgr = mm
	um, err := user.NewUserManager(t.TempDir()) // seeds the SysOp as user #1
	if err != nil {
		t.Fatal(err)
	}
	newcomer, err := um.AddUser("password", "Newcomer", "New Comer", "There")
	if err != nil {
		t.Fatal(err)
	}
	newcomer.IntroPending = true
	orig := introEditor
	t.Cleanup(func() { introEditor = orig })
	introEditor = func(string, io.Reader, io.Writer, ansi.OutputMode, string, string, string, bool,
		string, string, string, string, bool, []string, *editor.InputHandler, ...editor.EditorContext) (string, bool, error) {
		return "Hello.", true, nil
	}

	var sent bool
	out := f.pauseAt(t, func(tm *term.Terminal) {
		_, sent, _ = f.e.runNewUserIntroGate(f.s, tm, um, newcomer, 3, ansi.OutputModeUTF8, 80, 24)
	})
	if !sent {
		t.Errorf("message not sent; output=%q", out)
	}
	if !strings.Contains(out, "[one last step]") || !strings.Contains(out, "[kind pause]") || strings.Contains(out, "!ANSWER") {
		t.Errorf("output=%q", out)
	}
}

// The signup and CHECKACCESS pauses print a text pauseString where the cursor
// is, as written: a leading line break is kept, the prompt is not centred, and
// Enter ends the row.
func TestInPlacePauseKeepsTextPlacement(t *testing.T) {
	centred := regexp.MustCompile(`\x1b\[\d+C`)
	t.Run("signups closed", func(t *testing.T) {
		f := preLogin(newStringHookFixture(t, "\r"))
		f.e.SetStrings(config.StringsConfig{NewUsersClosedStr: "[closed]", PauseString: "\r\n[pause]"})
		out := f.pauseAt(t, func(tm *term.Terminal) {
			_, _ = f.e.handleNewUserApplication(f.s, tm, nil, 3, ansi.OutputModeUTF8, 80, 24)
		})
		if !strings.HasSuffix(out, "[closed]\r\r\n[pause]\r\r\n") || centred.MatchString(out) {
			t.Errorf("output=%q", out)
		}
	})
	t.Run("check access", func(t *testing.T) {
		f := preLogin(newStringHookFixture(t, "Nobody\r\r"))
		f.e.SetStrings(config.StringsConfig{MatrixUserNotFound: "[not found]", PauseString: "\r\n[pause]"})
		um, err := user.NewUserManager(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		out := f.pauseAt(t, func(tm *term.Terminal) {
			f.e.handleCheckAccess(f.s, tm, um, 3, ansi.OutputModeUTF8)
		})
		if !strings.HasSuffix(out, "[not found]\r\r\n[pause]\r\r\n") || centred.MatchString(out) {
			t.Errorf("output=%q", out)
		}
	})
}
