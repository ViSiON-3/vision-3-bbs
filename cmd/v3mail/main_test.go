package main

import (
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runMain runs main() in-process from dir with the given arguments, for paths
// that return rather than exit, and restores the process-wide state main
// changes (logging, os.Args).
func runMain(t *testing.T, dir string, args ...string) (stdout, stderr string) {
	t.Helper()
	t.Chdir(dir)
	t.Setenv("V3MAIL_NO_CONSOLE_LOG", "1")
	origArgs, origSlog, origLog := os.Args, slog.Default(), log.Writer()
	os.Args = append([]string{"v3mail"}, args...)
	defer func() {
		os.Args = origArgs
		slog.SetDefault(origSlog)
		log.SetOutput(origLog)
	}()
	return capture(t, main)
}

// --version prints the banner and help prints the command list, both
// successfully.
func TestMainVersionAndHelp(t *testing.T) {
	dir := t.TempDir()
	_, errOut := runMain(t, dir, "--version")
	wantContains(t, "--version", errOut, "ViSiON/3 Mail Utility v", "MIT License")
	if strings.Contains(errOut, "Valid Commands") {
		t.Error("--version printed the usage")
	}

	for _, arg := range []string{"help", "-h", "--help"} {
		_, errOut = runMain(t, dir, arg)
		wantContains(t, arg, errOut, "Valid Commands Are As Follows", "STATS", "QWK-CONFERENCES", "--network NAME")
	}
}

// With no command, or an unknown one, v3mail prints usage and exits 1.
func TestMainUsageErrors(t *testing.T) {
	dir := t.TempDir()
	code, _, errOut := runV3mail(t, dir)
	if code != 1 || !strings.Contains(errOut, "Required Format: v3mail <command>") {
		t.Errorf("no command: exit %d, stderr %q", code, errOut)
	}
	code, _, errOut = runV3mail(t, dir, "bogus")
	if code != 1 || !strings.Contains(errOut, "Unknown command: bogus") {
		t.Errorf("unknown command: exit %d, stderr %q", code, errOut)
	}
}

// Every JAM command needs a base path or --all, and purge also a limit.
func TestMainMissingArguments(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []string{"stats", "pack", "fix", "link", "lastread"} {
		code, _, errOut := runV3mail(t, dir, c)
		if code != 1 || !strings.Contains(errOut, "Error: base path required (or use --all)") {
			t.Errorf("%s: exit %d, stderr %q", c, code, errOut)
		}
	}
	code, _, errOut := runV3mail(t, dir, "purge", "somebase")
	if code != 1 || !strings.Contains(errOut, "Error: --days or --keep is required") {
		t.Errorf("purge without limits: exit %d, stderr %q", code, errOut)
	}
	code, _, errOut = runV3mail(t, dir, "purge", "--keep", "5")
	if code != 1 || !strings.Contains(errOut, "Error: base path required") {
		t.Errorf("purge without path: exit %d, stderr %q", code, errOut)
	}
	code, _, errOut = runV3mail(t, dir, "stats", "--no-such-flag")
	if code != 2 || !strings.Contains(errOut, "flag provided but not defined: -no-such-flag") {
		t.Errorf("bad flag: exit %d, stderr %q", code, errOut)
	}
}

// main dispatches each command by name, run from a BBS root with the default
// configs/ and data/ directories.
func TestMainDispatch(t *testing.T) {
	b := newBBS(t)
	path := seedBase(t, filepath.Join(b.dataDir, "msgbases", "loc_general"), seedMsg{subject: "hello"})

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"stats", "-q", path}, "loc_general: total=1 active=1 deleted=0"},
		{[]string{"pack", "--all"}, "GENERAL: no deleted messages, skipping"},
		{[]string{"purge", "--keep", "5", path}, "loc_general: deleted 0 messages"},
		{[]string{"fix", path}, "OK: 1 messages, 1 active, no issues"},
		{[]string{"link", path}, "loc_general: 1 messages, all links current"},
		{[]string{"lastread", path}, "loc_general: no lastread records"},
		{[]string{"toss"}, "Toss complete: 0 packets"},
		{[]string{"scan"}, "Scan complete: 0 messages exported"},
		{[]string{"ftn-pack"}, "Pack complete: 0 bundles created"},
		{[]string{"poll"}, "FTN: no enabled networks"},
		{[]string{"qwk-poll"}, "No enabled QWK networks"},
		{[]string{"qwk-scan"}, ""},
		{[]string{"qwk-toss"}, ""},
	} {
		out, errOut := runMain(t, b.root, tc.args...)
		if !strings.Contains(out, tc.want) {
			t.Errorf("v3mail %v: stdout %q (stderr %q), want %q", tc.args, out, errOut, tc.want)
		}
	}
}
