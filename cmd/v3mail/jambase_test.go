package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// Positional base paths are used as given and tagged by their base name;
// with none and no --all, the caller is told what is missing.
func TestResolveBasePaths(t *testing.T) {
	got, err := resolveBasePaths(false, "", "", []string{"/bbs/data/msgbases/general", "other"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Path != "/bbs/data/msgbases/general" || got[0].Tag != "general" || got[1].Tag != "other" {
		t.Errorf("paths = %+v", got)
	}
	if _, err := resolveBasePaths(false, "", "", nil); err == nil || !strings.Contains(err.Error(), "base path required") {
		t.Errorf("no args: err = %v, want base path required", err)
	}
}

// --all reads message_areas.json: base_path is resolved under the data
// directory, a blank one defaults to msgbases/<lowercased tag>, and per-area
// purge limits are carried through.
func TestLoadAllBasePaths(t *testing.T) {
	b := newBBS(t)
	b.writeAreas(t,
		map[string]any{"id": 1, "tag": "GENERAL", "name": "General", "base_path": "msgbases/loc_general", "max_messages": 50, "max_age": 30},
		map[string]any{"id": 2, "tag": "NoPath", "name": "Defaulted"},
	)
	got, err := resolveBasePaths(true, b.configDir, b.dataDir, []string{"ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d areas, want 2: %+v", len(got), got)
	}
	want0 := baseMeta{Path: filepath.Join(b.dataDir, "msgbases/loc_general"), Tag: "GENERAL", Name: "General", MaxMsgs: 50, MaxAge: 30}
	if got[0] != want0 {
		t.Errorf("area 0 = %+v, want %+v", got[0], want0)
	}
	if want := filepath.Join(b.dataDir, "msgbases", "nopath"); got[1].Path != want {
		t.Errorf("defaulted path = %q, want %q", got[1].Path, want)
	}

	if _, err := loadAllBasePaths(t.TempDir(), b.dataDir); err == nil || !strings.Contains(err.Error(), "failed to read message_areas.json") {
		t.Errorf("missing file: err = %v", err)
	}
	b.writeConfig(t, "message_areas.json", "{not json")
	if _, err := loadAllBasePaths(b.configDir, b.dataDir); err == nil || !strings.Contains(err.Error(), "failed to parse message_areas.json") {
		t.Errorf("bad JSON: err = %v", err)
	}
}

// formatBytes switches units at 1 KB and 1 MB.
func TestFormatBytes(t *testing.T) {
	for in, want := range map[int64]string{
		0:               "0 bytes",
		1023:            "1023 bytes",
		1024:            "1.0 KB",
		1536:            "1.5 KB",
		1024 * 1024:     "1.0 MB",
		5 * 1024 * 1024: "5.0 MB",
	} {
		if got := formatBytes(in); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

// stats reports total/active/deleted counts, in full or as one quiet line,
// labels --all areas by tag and name, and skips a base it cannot open.
func TestCmdStats(t *testing.T) {
	b := newBBS(t)
	path := seedBase(t, filepath.Join(b.dataDir, "msgbases", "loc_general"),
		seedMsg{subject: "one"}, seedMsg{subject: "two"}, seedMsg{subject: "three"})
	deleteMsgs(t, path, 2)

	out, _ := capture(t, func() { cmdStats([]string{path}) })
	wantContains(t, "stats", out, "=== "+path+" ===", "Messages:   3 total, 2 active, 1 deleted", ".jhr:", ".jdt:", "BaseMsgNum: 1")

	out, _ = capture(t, func() { cmdStats([]string{"-q", path}) })
	if want := "loc_general: total=3 active=2 deleted=1\n"; out != want {
		t.Errorf("quiet stats = %q, want %q", out, want)
	}

	// The shipped message_areas.json names GENERAL at msgbases/loc_general.
	out, _ = capture(t, func() { cmdStats(append([]string{"--all"}, b.flags()...)) })
	wantContains(t, "stats --all", out, "=== GENERAL (General Discussion) ===", "=== PRIVMAIL (Private Mail) ===")

	blocker := filepath.Join(b.root, "afile")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// An unopenable base now fails the command (exit 1, #496), which would
	// end this test process, so run it as a child.
	code, out, errOut := runV3mail(t, b.root, "stats", filepath.Join(blocker, "base"))
	if code != 1 {
		t.Errorf("exit for unopenable base = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout for unopenable base = %q", out)
	}
	wantContains(t, "stderr", errOut, "Error opening")
}

// pack reports without touching the base on --dry-run, skips a base with
// nothing deleted, and otherwise rewrites it without the deleted messages.
func TestCmdPack(t *testing.T) {
	dir := t.TempDir()
	path := seedBase(t, filepath.Join(dir, "general"),
		seedMsg{subject: "a"}, seedMsg{subject: "b"}, seedMsg{subject: "c"}, seedMsg{subject: "d"})
	clean := seedBase(t, filepath.Join(dir, "clean"), seedMsg{subject: "keep"})
	deleteMsgs(t, path, 1, 3)

	out, _ := capture(t, func() { cmdPack([]string{"--dry-run", path}) })
	wantContains(t, "dry run", out, "general: 4 total, 2 active, 2 deleted (would remove 2)")
	if total, _ := counts(t, path); total != 4 {
		t.Errorf("dry run changed the base: total = %d", total)
	}

	out, _ = capture(t, func() { cmdPack([]string{path, clean}) })
	wantContains(t, "pack", out, "Packing general...", "Before: 4 messages (2 active, 2 deleted)", "After:  2 messages",
		"Reclaimed:", "clean: no deleted messages, skipping")
	if total, active := counts(t, path); total != 2 || active != 2 {
		t.Errorf("after pack: total=%d active=%d, want 2/2", total, active)
	}
	msg, err := openBase(t, path).ReadMessage(1)
	if err != nil || msg.Subject != "b" {
		t.Errorf("first surviving message = %+v, %v; want subject b", msg, err)
	}

	out, _ = capture(t, func() { cmdPack([]string{"-q", path}) })
	if out != "" {
		t.Errorf("quiet pack printed %q", out)
	}
}

// purge --keep deletes the oldest messages beyond the limit and --days those
// older than the cutoff; --dry-run only reports.
func TestCmdPurgeManual(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-90 * 24 * time.Hour)
	path := seedBase(t, filepath.Join(dir, "general"),
		seedMsg{subject: "old1", written: old}, seedMsg{subject: "old2", written: old},
		seedMsg{subject: "new1"}, seedMsg{subject: "new2"}, seedMsg{subject: "new3"})

	out, _ := capture(t, func() { cmdPurge([]string{"--dry-run", "--days", "30", path}) })
	wantContains(t, "dry run", out, "general: would delete 2 messages (age>30d, keep<=0)")
	if _, active := counts(t, path); active != 5 {
		t.Fatalf("dry run deleted messages: active = %d", active)
	}

	out, _ = capture(t, func() { cmdPurge([]string{"--days", "30", path}) })
	wantContains(t, "purge --days", out, "general: deleted 2 messages (run 'pack' to reclaim space)")
	base := openBase(t, path)
	for n, wantDeleted := range map[int]bool{1: true, 2: true, 3: false, 4: false, 5: false} {
		msg, err := base.ReadMessage(n)
		if err != nil {
			t.Fatal(err)
		}
		if msg.IsDeleted() != wantDeleted {
			t.Errorf("msg %d (%s) deleted = %v, want %v", n, msg.Subject, msg.IsDeleted(), wantDeleted)
		}
	}
	_ = base.Close()

	out, _ = capture(t, func() { cmdPurge([]string{"--keep", "1", path}) })
	wantContains(t, "purge --keep", out, "general: deleted 2 messages")
	if _, active := counts(t, path); active != 1 {
		t.Errorf("after --keep 1: active = %d, want 1", active)
	}
	msg, err := openBase(t, path).ReadMessage(5)
	if err != nil || msg.IsDeleted() {
		t.Errorf("newest message was purged: %+v, %v", msg, err)
	}
}

// With --all, each area's own max_age/max_messages wins over the flags, and
// an area with no limit at all is skipped with a note.
func TestCmdPurgeAllUsesAreaLimits(t *testing.T) {
	b := newBBS(t)
	b.writeAreas(t,
		map[string]any{"id": 1, "tag": "CAPPED", "name": "Capped", "base_path": "msgbases/capped", "max_messages": 1},
		map[string]any{"id": 2, "tag": "OPEN", "name": "Open", "base_path": "msgbases/open"},
	)
	capped := seedBase(t, filepath.Join(b.dataDir, "msgbases", "capped"),
		seedMsg{subject: "a"}, seedMsg{subject: "b"}, seedMsg{subject: "c"})
	open := seedBase(t, filepath.Join(b.dataDir, "msgbases", "open"), seedMsg{subject: "a"}, seedMsg{subject: "b"})

	out, _ := capture(t, func() { cmdPurge(append([]string{"--all"}, b.flags()...)) })
	wantContains(t, "purge --all", out, "CAPPED: deleted 2 messages", "OPEN: no purge limits configured, skipping")
	if _, active := counts(t, capped); active != 1 {
		t.Errorf("CAPPED active = %d, want 1", active)
	}
	if _, active := counts(t, open); active != 2 {
		t.Errorf("OPEN active = %d, want 2 (untouched)", active)
	}

	// --keep is the fallback for areas that set no limit of their own.
	out, _ = capture(t, func() { cmdPurge(append([]string{"--all", "--keep", "1"}, b.flags()...)) })
	wantContains(t, "purge --all --keep", out, "OPEN: deleted 1 messages", "CAPPED: deleted 0 messages")
	if _, active := counts(t, open); active != 1 {
		t.Errorf("OPEN active after fallback = %d, want 1", active)
	}
}

// fix passes a healthy base, and --repair cuts a malformed ReplyID (more
// than "address serial") back to its first two tokens.
func TestCmdFix(t *testing.T) {
	dir := t.TempDir()
	good := seedBase(t, filepath.Join(dir, "good"), seedMsg{subject: "a"}, seedMsg{subject: "b"})
	deleteMsgs(t, good, 2)
	out, _ := capture(t, func() { cmdFix([]string{good}) })
	wantContains(t, "fix", out, "Checking good...", "OK: 2 messages, 1 active, no issues")

	out, _ = capture(t, func() { cmdFix([]string{"-q", good}) })
	if out != "" {
		t.Errorf("quiet fix of a healthy base printed %q", out)
	}

	bad := seedBase(t, filepath.Join(dir, "bad"),
		seedMsg{subject: "parent", msgID: "21:1/100 0000abcd"},
		seedMsg{subject: "child", replyID: "21:1/100 0000abcd junk"})
	out, _ = capture(t, func() { cmdFix([]string{"--repair", bad}) })
	wantContains(t, "fix --repair", out, `REPAIR: Cleaned ReplyID "21:1/100 0000abcd junk" -> "21:1/100 0000abcd"`,
		"REPAIR: Rebuilt message base with cleaned ReplyIDs", "Cleaned 1 malformed ReplyIDs")
	msg, err := openBase(t, bad).ReadMessage(2)
	if err != nil || msg.ReplyID != "21:1/100 0000abcd" {
		t.Errorf("repaired ReplyID = %q, %v; want 21:1/100 0000abcd", msg.ReplyID, err)
	}
}

// fix exits non-zero when it finds a problem: an unopenable base, a
// truncated index file, or a malformed ReplyID it was not asked to repair.
func TestCmdFixReportsIssues(t *testing.T) {
	dir := t.TempDir()
	path := seedBase(t, filepath.Join(dir, "general"),
		seedMsg{subject: "a"}, seedMsg{subject: "b", replyID: "21:1/100 0000abcd junk"})

	code, out, _ := runV3mail(t, dir, "fix", path)
	if code != 1 {
		t.Errorf("fix exit = %d, want 1", code)
	}
	wantContains(t, "fix", out, `ISSUE: Malformed ReplyID: "21:1/100 0000abcd junk" (use --repair to fix)`, "Found 1 issue(s)")

	f, err := os.OpenFile(path+".jdx", os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	code, out, _ = runV3mail(t, dir, "fix", path)
	if code != 1 {
		t.Errorf("fix exit = %d, want 1", code)
	}
	wantContains(t, "fix", out, fmt.Sprintf(".jdx size %d not divisible by %d", 2*jam.IndexRecordSize+3, jam.IndexRecordSize))

	blocker := filepath.Join(dir, "afile")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runV3mail(t, dir, "fix", filepath.Join(blocker, "base"))
	if code != 1 || !strings.Contains(errOut, "Error opening") {
		t.Errorf("unopenable base: exit %d, stderr %q", code, errOut)
	}
}

// lastread lists each user's pointers and --reset zeroes one user's.
func TestCmdLastread(t *testing.T) {
	dir := t.TempDir()
	path := seedBase(t, filepath.Join(dir, "general"), seedMsg{subject: "a"}, seedMsg{subject: "b"})
	empty := seedBase(t, filepath.Join(dir, "empty"))

	out, _ := capture(t, func() { cmdLastread([]string{empty}) })
	wantContains(t, "lastread", out, "empty: no lastread records")

	base := openBase(t, path)
	if err := base.SetLastRead("alice", 2, 2); err != nil {
		t.Fatal(err)
	}
	_ = base.Close()

	crc := jam.CRC32String("alice")
	out, _ = capture(t, func() { cmdLastread([]string{path}) })
	wantContains(t, "lastread", out, "=== general ===", fmt.Sprintf("UserCRC=0x%08X  LastRead=2 ", crc), "HighRead=2")

	out, _ = capture(t, func() { cmdLastread([]string{"--reset", "alice", path}) })
	wantContains(t, "lastread --reset", out, `general: reset lastread for "alice"`)
	lr, err := openBase(t, path).GetLastRead("alice")
	if err != nil || lr.LastReadMsg != 0 || lr.HighReadMsg != 0 {
		t.Errorf("after reset: %+v, %v; want zeroed pointers", lr, err)
	}
}

// A .jlr whose size is not a whole number of records is reported, not
// listed.
func TestCmdLastreadCorruptFile(t *testing.T) {
	path := seedBase(t, filepath.Join(t.TempDir(), "general"), seedMsg{subject: "a"})
	if err := os.WriteFile(path+".jlr", []byte{1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut := capture(t, func() { cmdLastread([]string{path}) })
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
	wantContains(t, "stderr", errOut, "Error reading lastread for general")
}

// link threads replies to their parent by MSGID, chains siblings through
// ReplyNext, matches a REPLY stored without its serial, and is a no-op when
// run again.
func TestCmdLink(t *testing.T) {
	dir := t.TempDir()
	path := seedBase(t, filepath.Join(dir, "general"),
		seedMsg{subject: "parent", msgID: "21:1/100 0000abcd"},
		seedMsg{subject: "reply1", msgID: "21:1/200 00000001", replyID: "21:1/100 0000abcd"},
		seedMsg{subject: "reply2", replyID: "21:1/100 0000abcd"},
		seedMsg{subject: "loose", replyID: "21:1/200"},
	)
	empty := seedBase(t, filepath.Join(dir, "empty"))

	out, _ := capture(t, func() { cmdLink([]string{path, empty}) })
	wantContains(t, "link", out, "general: 4 messages, 4 links updated", "empty: no messages",
		"Total: 4 links updated across 2 areas")

	base := openBase(t, path)
	hdr := func(n int) *jam.MessageHeader {
		t.Helper()
		h, err := base.ReadMessageHeader(n)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	if h := hdr(1); h.Reply1st != 2 || h.ReplyTo != 0 {
		t.Errorf("parent: Reply1st=%d ReplyTo=%d, want 2/0", h.Reply1st, h.ReplyTo)
	}
	if h := hdr(2); h.ReplyTo != 1 || h.ReplyNext != 3 || h.Reply1st != 4 {
		t.Errorf("reply1: ReplyTo=%d ReplyNext=%d Reply1st=%d, want 1/3/4", h.ReplyTo, h.ReplyNext, h.Reply1st)
	}
	if h := hdr(3); h.ReplyTo != 1 || h.ReplyNext != 0 {
		t.Errorf("reply2: ReplyTo=%d ReplyNext=%d, want 1/0", h.ReplyTo, h.ReplyNext)
	}
	if h := hdr(4); h.ReplyTo != 2 {
		t.Errorf("loose: ReplyTo=%d, want 2 (matched by address prefix)", h.ReplyTo)
	}
	_ = base.Close()

	out, _ = capture(t, func() { cmdLink([]string{path}) })
	wantContains(t, "second link", out, "general: 4 messages, all links current")
}

// A base whose messages are all deleted has nothing to link, and a Reply1st
// left pointing at a reply that no longer exists is cleared.
func TestLinkBaseClearsStalePointers(t *testing.T) {
	dir := t.TempDir()
	gone := seedBase(t, filepath.Join(dir, "gone"), seedMsg{subject: "a"})
	deleteMsgs(t, gone, 1)
	out, _ := capture(t, func() { cmdLink([]string{gone}) })
	wantContains(t, "link", out, "gone: no active messages")

	path := seedBase(t, filepath.Join(dir, "stale"),
		seedMsg{subject: "parent", msgID: "21:1/100 0000abcd"},
		seedMsg{subject: "sole", replyID: "21:1/999 00000009"})
	base := openBase(t, path)
	for n, set := range map[int]func(*jam.MessageHeader){
		1: func(h *jam.MessageHeader) { h.Reply1st = 7 },
		2: func(h *jam.MessageHeader) { h.ReplyNext = 9 },
	} {
		h, err := base.ReadMessageHeader(n)
		if err != nil {
			t.Fatal(err)
		}
		set(h)
		if err := base.UpdateMessageHeader(n, h); err != nil {
			t.Fatal(err)
		}
	}
	var updated int
	var err error
	out, _ = capture(t, func() { updated, err = linkBase(base, false, "stale") })
	if err != nil || updated != 2 {
		t.Fatalf("linkBase = %d, %v; want 2 updates", updated, err)
	}
	wantContains(t, "linkBase", out, "stale: 2 messages, 2 links updated")
	for n := 1; n <= 2; n++ {
		h, _ := base.ReadMessageHeader(n)
		if h.Reply1st != 0 || h.ReplyNext != 0 {
			t.Errorf("msg %d: Reply1st=%d ReplyNext=%d, want both cleared", n, h.Reply1st, h.ReplyNext)
		}
	}
}
