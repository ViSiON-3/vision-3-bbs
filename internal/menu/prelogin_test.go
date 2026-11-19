package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// preloginBoard points env at a board root holding the shipped matrix and
// connection check under menus/v3 and a theme "glass" with its own versions
// of both and its own matrix wording, and makes glass the board's theme.
// It returns the root.
func preloginBoard(t *testing.T, env *menuEnv) string {
	t.Helper()
	shipped := env.e.MenuSetPath
	root := t.TempDir()
	stock := filepath.Join(root, "menus", "v3")
	for _, f := range [][2]string{{"ansi", "PDMATRIX.ANS"}, {"bar", "PDMATRIX.BAR"}, {"cfg", "PDMATRIX.CFG"}, {"ansi", "BOTCHECK.ASC"}, {"mnu", "MAIN.MNU"}} {
		b, err := os.ReadFile(filepath.Join(shipped, f[0], f[1]))
		if err != nil {
			t.Fatal(err)
		}
		chatcovWriteFile(t, filepath.Join(stock, f[0], f[1]), string(b))
	}
	glass := filepath.Join(root, "menus", "glass")
	chatcovWriteFile(t, filepath.Join(glass, "ansi", "PDMATRIX.ANS"), "GLASS-MATRIX-ART\r\n")
	chatcovWriteFile(t, filepath.Join(glass, "ansi", "BOTCHECK.ASC"), "GLASS-GATE-ART\r\n")
	chatcovWriteFile(t, filepath.Join(glass, "mnu", "MAIN.MNU"), "{}")
	chatcovWriteFile(t, filepath.Join(glass, config.ThemeStringsFile), `{"matrixDisconnecting": "GLASS-BYE"}`)
	env.e.MenuSetPath = glass
	env.e.ReloadThemeStrings()
	if got := env.e.Strings().MatrixDisconnecting; got != "GLASS-BYE" {
		t.Fatalf("glass strings not loaded: matrixDisconnecting = %q", got)
	}
	return root
}

// preloginGate runs the connection check on e, passing it with one press of
// x, and returns what it drew.
func preloginGate(env *menuEnv) runResult {
	env.t.Helper()
	cfg := env.e.GetServerConfig()
	cfg.EnableChallengeGate = true
	cfg.ChallengeGateFile = "BOTCHECK.ASC"
	cfg.ChallengeGateKey = "x"
	cfg.ChallengeGateRequiredPresses = 1
	cfg.ChallengeGateTimeoutSeconds = 5
	env.e.SetServerConfig(cfg)
	return env.run(func(c *cmdCtx, _ string) (*user.User, string, error) {
		passed, err := c.e.RunChallengeGate(c.s, c.terminal, c.nodeNumber, c.outputMode, c.termWidth, c.termHeight)
		if !passed {
			return nil, "FAILED", err
		}
		return nil, "PASSED", err
	}, nil, "", "x")
}

// The board's theme has its own matrix and connection check; neither is
// shown. With no board-level copies the shipped ones are, worded by the
// board's strings, not the theme's.
func TestPreloginIgnoresBoardTheme(t *testing.T) {
	env := newMenuEnv(t)
	preloginBoard(t, env)

	r := chatcovMatrix(env, "D")
	if r.err != nil || r.next != "DISCONNECT" {
		t.Fatalf("matrix = (%q, %v)", r.next, r.err)
	}
	if strings.Contains(r.raw, "GLASS") {
		t.Errorf("the board theme's matrix or wording was shown:\n%s", r.text())
	}
	if !r.has(chatcovMatrixArt, "Disconnecting...") {
		t.Errorf("the shipped matrix was not shown:\n%s", r.text())
	}

	g := preloginGate(env)
	if g.err != nil || g.next != "PASSED" {
		t.Fatalf("gate = (%q, %v)", g.next, g.err)
	}
	// "CONNECTED!" is on the shipped BOTCHECK.ASC, not on the built-in
	// fallback prompt.
	if strings.Contains(g.raw, "GLASS-GATE-ART") || !strings.Contains(g.raw, "CONNECTED!") {
		t.Errorf("the gate did not show the shipped BOTCHECK.ASC:\n%s", g.text())
	}
}

// Files in the board-level prelogin/ directory win over the shipped ones,
// and its strings.json words the matrix.
func TestPreloginBoardCopiesWin(t *testing.T) {
	env := newMenuEnv(t)
	root := preloginBoard(t, env)
	pre := filepath.Join(root, "prelogin")
	chatcovWriteFile(t, filepath.Join(pre, "ansi", "PDMATRIX.ANS"), "BOARD-MATRIX-ART\r\n")
	chatcovWriteFile(t, filepath.Join(pre, "ansi", "BOTCHECK.ASC"), "BOARD-GATE-ART\r\n")
	chatcovWriteFile(t, filepath.Join(pre, config.ThemeStringsFile), `{"matrixDisconnecting": "BOARD-BYE"}`)

	r := chatcovMatrix(env, "D")
	if r.err != nil || r.next != "DISCONNECT" {
		t.Fatalf("matrix = (%q, %v)", r.next, r.err)
	}
	if !r.has("BOARD-MATRIX-ART", "BOARD-BYE") || strings.Contains(r.raw, "GLASS") || strings.Contains(r.raw, chatcovMatrixArt) {
		t.Errorf("the board-level matrix was not the one shown:\n%s", r.text())
	}
	if g := preloginGate(env); g.next != "PASSED" || !g.has("BOARD-GATE-ART") {
		t.Errorf("the board-level connection check was not shown:\n%s", g.text())
	}
}

// A caller's own theme reads the pre-login files and wording from the same
// place as the board does.
func TestPreloginSameForCallerTheme(t *testing.T) {
	env := newMenuEnv(t)
	root := preloginBoard(t, env)
	// The board runs stock; the caller has picked glass.
	env.e.MenuSetPath = filepath.Join(root, "menus", "v3")
	env.e.ReloadThemeStrings()
	view := env.e.ForUser(&user.User{Handle: "Caller", Theme: "glass"})
	if view.Menus().Base != filepath.Join(root, "menus", "glass") {
		t.Fatalf("caller's view is on %s, want glass", view.Menus().Base)
	}
	board, caller := env.e.preloginView(), view.preloginView()
	if board.Menus() != caller.Menus() {
		t.Errorf("pre-login files: board %+v, caller %+v", board.Menus(), caller.Menus())
	}
	if b, c := board.Strings().MatrixDisconnecting, caller.Strings().MatrixDisconnecting; b != c || c == "GLASS-BYE" {
		t.Errorf("matrixDisconnecting: board %q, caller %q", b, c)
	}
}
