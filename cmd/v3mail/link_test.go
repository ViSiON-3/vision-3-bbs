package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// linkFixture seeds, in dir, the bases the link behaviour tests share: a
// threaded base, an empty one, one with every message deleted, one carrying
// stale pointers, and one whose thread has a deleted member. It returns
// their paths in that order.
func linkFixture(t *testing.T, dir string) []string {
	t.Helper()
	general := seedBase(t, filepath.Join(dir, "general"),
		seedMsg{subject: "parent", msgID: "21:1/100 0000abcd"},
		seedMsg{subject: "reply1", msgID: "21:1/200 00000001", replyID: "21:1/100 0000abcd"},
		seedMsg{subject: "reply2", replyID: "21:1/100 0000abcd"},
		seedMsg{subject: "loose", replyID: "21:1/200"},
	)
	empty := seedBase(t, filepath.Join(dir, "empty"))
	gone := seedBase(t, filepath.Join(dir, "gone"), seedMsg{subject: "a"})
	deleteMsgs(t, gone, 1)
	stale := seedBase(t, filepath.Join(dir, "stale"),
		seedMsg{subject: "parent", msgID: "21:1/100 0000abcd"},
		seedMsg{subject: "sole", replyID: "21:1/999 00000009"})
	setPointers(t, stale, map[int][3]uint32{1: {0, 7, 0}, 2: {5, 0, 9}})
	holes := seedBase(t, filepath.Join(dir, "holes"),
		seedMsg{subject: "parent", msgID: "21:1/100 00000001"},
		seedMsg{subject: "r1", msgID: "21:1/100 00000002", replyID: "21:1/100 00000001"},
		seedMsg{subject: "r2", msgID: "21:1/100 00000003", replyID: "21:1/100 00000001"},
		seedMsg{subject: "r3", replyID: "21:1/100 00000001"},
		seedMsg{subject: "r2a", replyID: "21:1/100 00000003"},
	)
	deleteMsgs(t, holes, 3)
	return []string{general, empty, gone, stale, holes}
}

// setPointers overwrites ReplyTo/Reply1st/ReplyNext of the given messages in
// the base at path.
func setPointers(t *testing.T, path string, ptrs map[int][3]uint32) {
	t.Helper()
	b, err := jam.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	for n, p := range ptrs {
		h, err := b.ReadMessageHeader(n)
		if err != nil {
			t.Fatal(err)
		}
		h.ReplyTo, h.Reply1st, h.ReplyNext = p[0], p[1], p[2]
		if err := b.UpdateMessageHeader(n, h); err != nil {
			t.Fatal(err)
		}
	}
}

// pointers lists every message's ReplyTo/Reply1st/ReplyNext in the base at
// path as space-separated "n:to/1st/next" entries.
func pointers(t *testing.T, path string) string {
	t.Helper()
	b, err := jam.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	total, err := b.GetMessageCount()
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for n := 1; n <= total; n++ {
		h, err := b.ReadMessageHeader(n)
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, fmt.Sprintf("%d:%d/%d/%d", n, h.ReplyTo, h.Reply1st, h.ReplyNext))
	}
	return strings.Join(parts, " ")
}

// linkFixturePointers is what link leaves in linkFixture's bases. The stale
// ReplyTo=5 in "stale" survives: a reply whose parent is not in the base
// keeps whatever ReplyTo it had.
var linkFixturePointers = map[string]string{
	"general": "1:0/2/0 2:1/4/3 3:1/0/0 4:2/0/0",
	"empty":   "",
	"gone":    "1:0/0/0",
	"stale":   "1:0/0/0 2:5/0/0",
	"holes":   "1:0/2/0 2:1/0/4 3:0/0/0 4:1/0/0 5:0/0/0",
}

// v3mail link's output, exit status and the pointers it writes, pinned to
// what the command produced before it was moved onto jam.Base.Link.
func TestCmdLinkOutputAndPointers(t *testing.T) {
	dir := t.TempDir()
	paths := linkFixture(t, dir)

	checkPointers := func(when string) {
		t.Helper()
		for _, p := range paths {
			if got, want := pointers(t, p), linkFixturePointers[filepath.Base(p)]; got != want {
				t.Errorf("%s: %s pointers = %q, want %q", when, filepath.Base(p), got, want)
			}
		}
	}
	run := func(when, wantOut string) {
		t.Helper()
		code, out, errOut := runV3mail(t, dir, append([]string{"link"}, paths...)...)
		if code != 0 || errOut != "" {
			t.Errorf("%s: exit %d, stderr %q", when, code, errOut)
		}
		if out != wantOut {
			t.Errorf("%s: stdout =\n%s\nwant\n%s", when, out, wantOut)
		}
		checkPointers(when)
	}

	run("first run", `general: 4 messages, 4 links updated
empty: no messages
gone: no active messages
stale: 2 messages, 2 links updated
holes: 4 messages, 3 links updated

Total: 9 links updated across 5 areas
`)
	run("second run", `general: 4 messages, all links current
empty: no messages
gone: no active messages
stale: 2 messages, all links current
holes: 4 messages, all links current

Total: 0 links updated across 5 areas
`)

	// -q links just the same, silently.
	dir = t.TempDir()
	paths = linkFixture(t, dir)
	code, out, errOut := runV3mail(t, dir, append([]string{"link", "-q"}, paths...)...)
	if code != 0 || out != "" || errOut != "" {
		t.Errorf("link -q: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	checkPointers("link -q")
}
