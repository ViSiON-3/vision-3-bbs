package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

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

	code, out, _ := runV3mail(t, t.TempDir(), "fix", path)
	if code != 0 {
		t.Errorf("fix exit code = %d, want 0\n%s", code, out)
	}
	if strings.Contains(out, "Malformed ReplyID") {
		t.Errorf("fix flagged a well-formed ReplyID:\n%s", out)
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

			if code, out, _ := runV3mail(t, t.TempDir(), "fix", "--repair", path); code != 0 {
				t.Fatalf("fix --repair exit code = %d, want 0\n%s", code, out)
			}
			if got := readMessage(t, path, 3).ReplyID; got != "21:1/100 00000002" {
				t.Errorf("ReplyID after repair = %q, want %q", got, "21:1/100 00000002")
			}

			if code, out, _ := runV3mail(t, t.TempDir(), "link", path); code != 0 {
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

// A repair pack zeroes every thread pointer; fix --repair relinks the base
// itself, so the threads are intact without a separate link run.
func TestFixRepairRelinksThreads(t *testing.T) {
	dir := t.TempDir()
	path := seedBase(t, filepath.Join(dir, "echo"),
		seedMsg{subject: "parent", msgID: "21:1/100 00000001"},
		seedMsg{subject: "r1", msgID: "21:1/100 00000002", replyID: "21:1/100 00000001"},
		seedMsg{subject: "r2", replyID: "21:1/100 00000001 21:1/100 00000009"},
		seedMsg{subject: "r1a", replyID: "21:1/100 00000002"},
	)
	if code, out, errOut := runV3mail(t, dir, "link", path); code != 0 {
		t.Fatalf("link exit code = %d\n%s%s", code, out, errOut)
	}

	code, out, errOut := runV3mail(t, dir, "fix", "--repair", path)
	if code != 0 {
		t.Fatalf("fix --repair exit code = %d, want 0\n%s%s", code, out, errOut)
	}
	wantContains(t, "fix --repair", out,
		"REPAIR: Rebuilt message base with cleaned ReplyIDs",
		"REPAIR: Relinked reply threads: 4 messages, 4 links updated")
	if got, want := pointers(t, path), "1:0/2/0 2:1/4/3 3:1/0/0 4:2/0/0"; got != want {
		t.Errorf("pointers after fix --repair = %q, want %q", got, want)
	}

	// -q repairs and relinks just the same, without reporting the link pass
	// (the cleaned-ReplyID count is still printed, as before).
	setPointers(t, path, map[int][3]uint32{1: {}, 2: {}, 3: {}, 4: {}})
	seedBase(t, path, seedMsg{subject: "r3", replyID: "21:1/100 00000001 junk"})
	code, out, errOut = runV3mail(t, dir, "fix", "--repair", "-q", path)
	if code != 0 || out != "  Cleaned 1 malformed ReplyIDs\n" || errOut != "" {
		t.Errorf("fix --repair -q: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if got, want := pointers(t, path), "1:0/2/0 2:1/4/3 3:1/0/5 4:2/0/0 5:1/0/0"; got != want {
		t.Errorf("pointers after fix --repair -q = %q, want %q", got, want)
	}
}

// When the repair pack fails, fix reports it, exits non-zero and leaves the
// base alone: no link pass runs over the unrepaired base.
func TestFixRepairFailedPackDoesNotLink(t *testing.T) {
	dir := t.TempDir()
	path := seedBase(t, filepath.Join(dir, "echo"),
		seedMsg{subject: "parent", msgID: "21:1/100 00000001"},
		seedMsg{subject: "r1", replyID: "21:1/100 00000001"},
		seedMsg{subject: "r2", replyID: "21:1/100 00000001 21:1/100 00000009"},
	)
	// A directory where pack wants its temporary header file makes the pack
	// fail before it touches the base.
	if err := os.Mkdir(path+".jhr.tmp", 0o755); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := runV3mail(t, dir, "fix", "--repair", path)
	if code != 1 {
		t.Errorf("fix --repair exit code = %d, want 1\n%s%s", code, out, errOut)
	}
	wantContains(t, "fix --repair", out, "ERROR: Failed to rebuild message base")
	if strings.Contains(out, "Relinked") {
		t.Errorf("fix --repair linked after a failed pack:\n%s", out)
	}
	if got, want := pointers(t, path), "1:0/0/0 2:0/0/0 3:0/0/0"; got != want {
		t.Errorf("pointers after failed repair = %q, want %q (untouched)", got, want)
	}
	if got := readMessage(t, path, 3).ReplyID; got != "21:1/100 00000001 21:1/100 00000009" {
		t.Errorf("ReplyID after failed repair = %q, want it unchanged", got)
	}
}

// When fix cannot scan the base's messages it reports the scan error and
// exits non-zero; with --repair it then neither packs nor links. (Here the
// text-offset check also flags the truncated base, so the exit code alone
// would not show the scan error; the reported line does.)
func TestFixRepairScanFailureReported(t *testing.T) {
	for _, tc := range []struct {
		name       string
		checkOnly  bool // without --repair
		quiet      bool
		wantOut    string // in stdout
		wantErrOut string // in stderr
	}{
		{name: "verbose", wantOut: "ERROR: Failed to scan messages"},
		{name: "quiet", quiet: true, wantErrOut: "Error scanning"},
		{name: "check only", checkOnly: true, wantOut: "ERROR: Failed to scan messages"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := seedBase(t, filepath.Join(dir, "echo"),
				seedMsg{subject: "parent", msgID: "21:1/100 00000001"},
				seedMsg{subject: "r1", replyID: "21:1/100 00000001"},
				seedMsg{subject: "r2", replyID: "21:1/100 00000001 21:1/100 00000009"},
			)
			// With the message text gone every text read fails, so
			// ScanMessages returns an error; the headers stay readable.
			if err := os.Truncate(path+".jdt", 0); err != nil {
				t.Fatal(err)
			}

			args := []string{"fix"}
			if !tc.checkOnly {
				args = append(args, "--repair")
			}
			if tc.quiet {
				args = append(args, "-q")
			}
			code, out, errOut := runV3mail(t, dir, append(args, path)...)
			if code != 1 {
				t.Errorf("exit code = %d, want 1\n%s%s", code, out, errOut)
			}
			if tc.wantOut != "" {
				wantContains(t, "stdout", out, tc.wantOut)
			}
			if tc.wantErrOut != "" {
				wantContains(t, "stderr", errOut, tc.wantErrOut)
			}
			if strings.Contains(out, "REPAIR:") {
				t.Errorf("fix --repair repaired after a failed scan:\n%s", out)
			}
			if got, want := pointers(t, path), "1:0/0/0 2:0/0/0 3:0/0/0"; got != want {
				t.Errorf("pointers after failed scan = %q, want %q (untouched)", got, want)
			}
		})
	}
}
