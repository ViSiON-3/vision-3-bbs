package menu

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCRLFWriter(t *testing.T) {
	var out bytes.Buffer
	w := &crlfWriter{w: &out}
	// The split lands between a CR and its LF, which must not gain a second CR.
	for _, chunk := range []string{"one\ntwo\r", "\nthree\n"} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := out.String(), "one\r\ntwo\r\nthree\r\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// execMailPoll runs `v3mail poll` from the BBS root with the config and data
// directories, extra menu arguments and the quiet-log variable, and reports a
// failed poll as its exit code rather than as an error.
func TestExecMailPoll(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as a stand-in for v3mail")
	}
	root := t.TempDir()
	fake := filepath.Join(root, "v3mail")
	script := "#!/bin/sh\necho \"args: $*\"\necho \"quiet: $V3MAIL_NO_CONSOLE_LOG\"\necho \"cwd: $(pwd -P)\"\necho oops >&2\nexit 3\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(root, "configs")

	var out bytes.Buffer
	code, err := execMailPoll(fake, root, configDir, []string{"--network", "fsxnet"}, &out)
	if err != nil {
		t.Fatalf("execMailPoll: %v", err)
	}
	if code != 3 {
		t.Errorf("exit code %d, want 3", code)
	}
	got := out.String()
	realRoot, _ := filepath.EvalSymlinks(root)
	for _, want := range []string{
		"args: poll --config " + configDir + " --data " + filepath.Join(root, "data") + " --network fsxnet",
		"quiet: 1",
		"cwd: " + realRoot,
		"oops",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}
