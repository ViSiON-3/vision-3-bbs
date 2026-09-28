package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// v3mailHelperEnv names the environment variable that turns the test binary
// into a v3mail process: TestV3mailHelperProcess runs the command it names.
const v3mailHelperEnv = "V3MAIL_TEST_HELPER"

// TestV3mailHelperProcess is not a real test. runV3mail re-executes the test
// binary with v3mailHelperEnv set, and this runs the named subcommand so a
// test can observe its output and exit status, since commands call os.Exit.
func TestV3mailHelperProcess(t *testing.T) {
	cmd := os.Getenv(v3mailHelperEnv)
	if cmd == "" {
		t.Skip("helper process only")
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	switch cmd {
	case "fix":
		cmdFix(args)
	case "link":
		cmdLink(args)
	default:
		t.Fatalf("unknown helper command %q", cmd)
	}
	os.Exit(0)
}

// runV3mail runs a v3mail subcommand in a child process and returns its
// stdout and exit code.
func runV3mail(t *testing.T, cmd string, args ...string) (string, int) {
	t.Helper()
	c := exec.Command(os.Args[0], append([]string{"-test.run=^TestV3mailHelperProcess$", "--"}, args...)...)
	c.Env = append(os.Environ(), v3mailHelperEnv+"="+cmd)
	var stdout bytes.Buffer
	c.Stdout = &stdout
	err := c.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return stdout.String(), 0
	case errors.As(err, &exitErr):
		return stdout.String(), exitErr.ExitCode()
	default:
		t.Fatalf("running v3mail %s: %v", cmd, err)
		return "", 0
	}
}

// seedReplyBase writes two messages from the same address and a third that
// replies to the second with the given ReplyID, returning the base path.
func seedReplyBase(t *testing.T, replyID string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "echo")
	b, err := jam.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, ids := range [][2]string{
		{"21:1/100 00000001", ""},
		{"21:1/100 00000002", ""},
		{"21:1/200 00000001", replyID},
	} {
		msg := jam.NewMessage()
		msg.From, msg.To, msg.Subject, msg.Text = "alice", "All", "Hi", "body"
		msg.MsgID, msg.ReplyID = ids[0], ids[1]
		if _, err := b.WriteMessage(msg); err != nil {
			t.Fatalf("WriteMessage: %v", err)
		}
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return path
}

// readMessage opens the base at path and returns message n.
func readMessage(t *testing.T, path string, n int) *jam.Message {
	t.Helper()
	b, err := jam.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = b.Close() }()
	msg, err := b.ReadMessage(n)
	if err != nil {
		t.Fatalf("ReadMessage(%d): %v", n, err)
	}
	return msg
}

func TestFixAcceptsAddressSerialReplyID(t *testing.T) {
	path := seedReplyBase(t, "21:1/100 00000002")

	out, code := runV3mail(t, "fix", path)
	if code != 0 {
		t.Errorf("fix exit code = %d, want 0\n%s", code, out)
	}
	if strings.Contains(out, "Malformed ReplyID") {
		t.Errorf("fix flagged a well-formed ReplyID:\n%s", out)
	}
}

func TestFixFlagsReplyIDWithExtraTokens(t *testing.T) {
	path := seedReplyBase(t, "21:1/100 00000002 21:1/100 00000003")

	out, code := runV3mail(t, "fix", path)
	if code != 1 {
		t.Errorf("fix exit code = %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, "Malformed ReplyID") {
		t.Errorf("fix did not flag the malformed ReplyID:\n%s", out)
	}
}

// A repair followed by link must still thread the reply onto the message it
// names, not the first message from the same address.
func TestFixRepairThenLinkKeepsParent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		replyID string
	}{
		{"well-formed", "21:1/100 00000002"},
		{"extra tokens", "21:1/100 00000002 21:1/100 00000003"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := seedReplyBase(t, tc.replyID)

			if out, code := runV3mail(t, "fix", "--repair", path); code != 0 {
				t.Fatalf("fix --repair exit code = %d, want 0\n%s", code, out)
			}
			if got := readMessage(t, path, 3).ReplyID; got != "21:1/100 00000002" {
				t.Errorf("ReplyID after repair = %q, want %q", got, "21:1/100 00000002")
			}

			if out, code := runV3mail(t, "link", path); code != 0 {
				t.Fatalf("link exit code = %d, want 0\n%s", code, out)
			}
			if got := readMessage(t, path, 3).Header.ReplyTo; got != 2 {
				t.Errorf("ReplyTo after link = %d, want 2", got)
			}
			if got := readMessage(t, path, 2).Header.Reply1st; got != 3 {
				t.Errorf("parent Reply1st after link = %d, want 3", got)
			}
		})
	}
}
