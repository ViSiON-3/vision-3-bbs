package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
	configtemplates "github.com/ViSiON-3/vision-3-bbs/templates/configs"
)

// argsEnv carries the v3mail command line to a re-executed test binary; see
// TestMain and runV3mail.
const argsEnv = "V3MAIL_TEST_ARGS"

// TestMain lets a test run the real main() in a child process, for the paths
// that end in os.Exit: when argsEnv is set the binary becomes v3mail itself.
func TestMain(m *testing.M) {
	if args, ok := os.LookupEnv(argsEnv); ok {
		os.Args = []string{"v3mail"}
		if args != "" {
			os.Args = append(os.Args, strings.Split(args, "\n")...)
		}
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runV3mail runs v3mail with args in a child process whose working directory
// is dir, returning its exit code and output.
func runV3mail(t *testing.T, dir string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), argsEnv+"="+strings.Join(args, "\n"), "V3MAIL_NO_CONSOLE_LOG=1")
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	default:
		t.Fatalf("running v3mail %v: %v", args, err)
	}
	return code, out.String(), errOut.String()
}

// capture runs fn with os.Stdout and os.Stderr redirected, returning what it
// wrote to each. The default slog logger is silenced for the duration so log
// records do not reach the real terminal.
func capture(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	outF, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	errF, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	origOut, origErr, origLog := os.Stdout, os.Stderr, slog.Default()
	os.Stdout, os.Stderr = outF, errF
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer func() {
		os.Stdout, os.Stderr = origOut, origErr
		slog.SetDefault(origLog)
		_ = outF.Close()
		_ = errF.Close()
	}()
	fn()
	o, _ := os.ReadFile(outF.Name())
	e, _ := os.ReadFile(errF.Name())
	return string(o), string(e)
}

// bbs is a throwaway BBS tree: root/configs seeded from the shipped
// templates, and root/data.
type bbs struct {
	root, configDir, dataDir string
}

// newBBS builds a BBS tree in a temp directory from the embedded config
// templates.
func newBBS(t *testing.T) *bbs {
	t.Helper()
	root := t.TempDir()
	b := &bbs{root: root, configDir: filepath.Join(root, "configs"), dataDir: filepath.Join(root, "data")}
	for _, d := range []string{b.configDir, filepath.Join(b.dataDir, "msgbases")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := configtemplates.FS.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := configtemplates.FS.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		b.writeConfig(t, e.Name(), string(data))
	}
	return b
}

// writeConfig replaces one file in the configs directory.
func (b *bbs) writeConfig(t *testing.T, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(b.configDir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeAreas replaces message_areas.json with areas.
func (b *bbs) writeAreas(t *testing.T, areas ...map[string]any) {
	t.Helper()
	data, err := json.Marshal(areas)
	if err != nil {
		t.Fatal(err)
	}
	b.writeConfig(t, "message_areas.json", string(data))
}

// flags returns the --config/--data flags pointing at this BBS.
func (b *bbs) flags() []string {
	return []string{"--config", b.configDir, "--data", b.dataDir}
}

// seedMsg describes one message for seedBase.
type seedMsg struct {
	subject string
	written time.Time // zero means now
	msgID   string
	replyID string
}

// seedBase creates a JAM base at path with msgs, and returns its path.
func seedBase(t *testing.T, path string, msgs ...seedMsg) string {
	t.Helper()
	b, err := jam.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	for _, m := range msgs {
		msg := jam.NewMessage()
		msg.From, msg.To, msg.Subject = "Sysop", "All", m.subject
		msg.Text = "body of " + m.subject
		msg.MsgID, msg.ReplyID = m.msgID, m.replyID
		if !m.written.IsZero() {
			msg.DateTime = m.written
		}
		if _, err := b.WriteMessage(msg); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// openBase opens the JAM base at path for inspection, closing it at cleanup.
func openBase(t *testing.T, path string) *jam.Base {
	t.Helper()
	b, err := jam.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// deleteMsgs marks the given 1-based messages deleted in the base at path.
func deleteMsgs(t *testing.T, path string, nums ...int) {
	t.Helper()
	b, err := jam.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	for _, n := range nums {
		if err := b.DeleteMessage(n); err != nil {
			t.Fatal(err)
		}
	}
}

// counts returns the total and active message counts of the base at path.
func counts(t *testing.T, path string) (total, active int) {
	t.Helper()
	b, err := jam.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	total, err = b.GetMessageCount()
	if err != nil {
		t.Fatal(err)
	}
	return total, b.GetActiveMessageCount()
}

// wantContains fails the test when s lacks any of subs.
func wantContains(t *testing.T, what, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("%s missing %q:\n%s", what, sub, s)
		}
	}
}
