package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// preloginBoard is a board root running stock with the shipped matrix and
// connection check in menus/v3, the connection check turned on.
func preloginBoard(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeThemeFiles(t, root, map[string]string{
		"configs/config.json":           `{"enableChallengeGate": true, "challengeGateFile": "BOTCHECK.ASC"}`,
		"menus/v3/theme.json":           `{}`,
		"menus/v3/mnu/MAIN.MNU":         `{}`,
		"menus/v3/ansi/PDMATRIX.ANS":    "matrix\r\n",
		"menus/v3/bar/PDMATRIX.BAR":     "1;1;15;7;L;Login\n",
		"menus/v3/cfg/PDMATRIX.CFG":     `[{"KEYS": "L", "CMD": "LOGIN"}]`,
		"menus/v3/ansi/BOTCHECK.ASC":    "gate\r\n",
		"menus/glass/theme.json":        `{}`,
		"menus/glass/mnu/MAIN.MNU":      `{}`,
		"menus/glass/ansi/BOTCHECK.ASC": "theme gate\r\n",
	})
	return root
}

func preloginChecks(t *testing.T, root string) []doctorCheck {
	t.Helper()
	var out []doctorCheck
	for _, c := range inspectBoard(root).Checks {
		if c.Name == "pre-login" {
			out = append(out, c)
		}
	}
	return out
}

func TestDoctorPrelogin(t *testing.T) {
	t.Run("the shipped files are reported where they come from", func(t *testing.T) {
		checks := preloginChecks(t, preloginBoard(t))
		if len(checks) != 1 || checks[0].Status != doctorOK || !strings.Contains(checks[0].Message, "menus/v3") {
			t.Errorf("checks:\n%s", dumpChecks(checks))
		}
	})

	t.Run("a missing matrix file is a warning", func(t *testing.T) {
		root := preloginBoard(t)
		if err := os.Remove(filepath.Join(root, "menus", "v3", "ansi", "PDMATRIX.ANS")); err != nil {
			t.Fatal(err)
		}
		checks := preloginChecks(t, root)
		if !hasCheckText(checks, doctorWarn, "ansi/PDMATRIX.ANS") {
			t.Errorf("no warning for the missing PDMATRIX.ANS:\n%s", dumpChecks(checks))
		}
	})

	t.Run("a missing connection check is a warning only with the check on", func(t *testing.T) {
		root := preloginBoard(t)
		if err := os.Remove(filepath.Join(root, "menus", "v3", "ansi", "BOTCHECK.ASC")); err != nil {
			t.Fatal(err)
		}
		// The theme's copy does not count.
		if !hasCheckText(preloginChecks(t, root), doctorWarn, "ansi/BOTCHECK.ASC") {
			t.Errorf("no warning for the missing BOTCHECK.ASC:\n%s", dumpChecks(preloginChecks(t, root)))
		}
		writeThemeFiles(t, root, map[string]string{"configs/config.json": `{"enableChallengeGate": false}`})
		if hasCheckText(preloginChecks(t, root), doctorWarn, "BOTCHECK.ASC") {
			t.Errorf("warned about the connection check with it off")
		}
	})

	t.Run("an unreadable board-level file is a warning", func(t *testing.T) {
		root := preloginBoard(t)
		dir := filepath.Join(root, "prelogin", "ansi")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "nowhere.png"), filepath.Join(dir, "PDMATRIX.PNG")); err != nil {
			t.Skipf("no symlinks here: %v", err)
		}
		if !hasCheckText(preloginChecks(t, root), doctorWarn, "PDMATRIX.PNG") {
			t.Errorf("no warning for the unreadable PDMATRIX.PNG:\n%s", dumpChecks(preloginChecks(t, root)))
		}
	})

	t.Run("a missing command list is a warning saying the choices don't act", func(t *testing.T) {
		root := preloginBoard(t)
		if err := os.Remove(filepath.Join(root, "menus", "v3", "cfg", "PDMATRIX.CFG")); err != nil {
			t.Fatal(err)
		}
		if !hasCheckText(preloginChecks(t, root), doctorWarn, "cfg/PDMATRIX.CFG") ||
			!hasCheckText(preloginChecks(t, root), doctorWarn, "none of its choices does anything") {
			t.Errorf("checks:\n%s", dumpChecks(preloginChecks(t, root)))
		}
	})

	t.Run("a board-level strings.json link to nothing is a warning", func(t *testing.T) {
		root := preloginBoard(t)
		if err := os.MkdirAll(filepath.Join(root, "prelogin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "gone.json"), filepath.Join(root, "prelogin", "strings.json")); err != nil {
			t.Skipf("no symlinks here: %v", err)
		}
		if !hasCheckText(preloginChecks(t, root), doctorWarn, "strings.json") {
			t.Errorf("no warning for the dangling strings.json:\n%s", dumpChecks(preloginChecks(t, root)))
		}
	})

	t.Run("a board-level strings.json that does not parse is a warning", func(t *testing.T) {
		root := preloginBoard(t)
		writeThemeFiles(t, root, map[string]string{"prelogin/strings.json": `{"matrixDisconnecting": `})
		if !hasCheckText(preloginChecks(t, root), doctorWarn, "strings.json") {
			t.Errorf("no warning for the broken strings.json:\n%s", dumpChecks(preloginChecks(t, root)))
		}
	})

	t.Run("board-level copies are counted", func(t *testing.T) {
		root := preloginBoard(t)
		writeThemeFiles(t, root, map[string]string{"prelogin/ansi/PDMATRIX.ANS": "board matrix\r\n"})
		checks := preloginChecks(t, root)
		if len(checks) != 1 || checks[0].Status != doctorOK || !strings.Contains(checks[0].Message, "1 from prelogin/") {
			t.Errorf("checks:\n%s", dumpChecks(checks))
		}
	})
}
