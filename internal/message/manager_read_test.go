package message

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// post adds a public message from alice to an area and returns its number.
func post(t *testing.T, mm *MessageManager, areaID int, to, subject string) int {
	t.Helper()
	n, err := mm.AddMessage(areaID, "alice", to, subject, "body", "")
	if err != nil {
		t.Fatalf("AddMessage(%q): %v", subject, err)
	}
	return n
}

// newBrokenBaseManager builds a manager whose only area cannot be opened: its
// base path runs through a regular file, so jam.Open fails with an I/O error
// rather than ErrAreaNotFound.
func newBrokenBaseManager(t *testing.T) *MessageManager {
	t.Helper()
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "blocker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMsgAreas(t, cfg, `[{"id":1,"tag":"BROKEN","name":"Broken","base_path":"blocker/sub/base","area_type":"local"}]`)
	mm, err := NewMessageManager(tmp, cfg, "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	return mm
}

func TestMessageCounts(t *testing.T) {
	mm := newReplyTestManager(t)
	post(t, mm, 1, "All", "one")
	post(t, mm, 1, "All", "two")
	post(t, mm, 2, "bob", "three")

	for areaID, want := range map[int]int{1: 2, 2: 1, 99: 0} {
		got, err := mm.GetMessageCountForArea(areaID)
		if err != nil || got != want {
			t.Errorf("GetMessageCountForArea(%d) = %d, %v; want %d, nil", areaID, got, err, want)
		}
	}
	if got := mm.GetTotalMessageCount(); got != 3 {
		t.Errorf("GetTotalMessageCount() = %d, want 3", got)
	}
}

func TestGetAreaCounts(t *testing.T) {
	mm := newReplyTestManager(t)
	post(t, mm, 1, "All", "one")
	post(t, mm, 1, "bob", "two")
	post(t, mm, 1, "bob", "three")
	post(t, mm, 1, "carol", "four")
	if err := mm.SetLastRead(1, "bob", 1); err != nil {
		t.Fatalf("SetLastRead: %v", err)
	}

	got, err := mm.GetAreaCounts(1, "bob")
	if err != nil {
		t.Fatalf("GetAreaCounts: %v", err)
	}
	if want := (AreaCounts{Total: 4, New: 3, Personal: 2}); got != want {
		t.Errorf("GetAreaCounts(1, bob) = %+v, want %+v", got, want)
	}

	// Without a user only the total is worked out.
	got, err = mm.GetAreaCounts(1, "")
	if err != nil || got != (AreaCounts{Total: 4}) {
		t.Errorf("GetAreaCounts(1, \"\") = %+v, %v; want {Total:4}, nil", got, err)
	}

	// An unconfigured area reads as empty rather than failing.
	got, err = mm.GetAreaCounts(99, "bob")
	if err != nil || got != (AreaCounts{}) {
		t.Errorf("GetAreaCounts(99) = %+v, %v; want zero counts, nil", got, err)
	}
}

func TestLastReadPointer(t *testing.T) {
	mm := newReplyTestManager(t)
	for _, s := range []string{"one", "two", "three"} {
		post(t, mm, 1, "All", s)
	}

	// A user with no lastread record has read nothing: the first message is next.
	if lr, err := mm.GetLastRead(1, "bob"); err != nil || lr != 0 {
		t.Errorf("GetLastRead before any read = %d, %v; want 0, nil", lr, err)
	}
	if next, err := mm.GetNextUnreadMessage(1, "bob"); err != nil || next != 1 {
		t.Errorf("GetNextUnreadMessage before any read = %d, %v; want 1, nil", next, err)
	}

	if err := mm.AdvanceLastRead(1, "bob", 2); err != nil {
		t.Fatalf("AdvanceLastRead(2): %v", err)
	}
	if next, err := mm.GetNextUnreadMessage(1, "bob"); err != nil || next != 3 {
		t.Errorf("GetNextUnreadMessage after reading 2 = %d, %v; want 3, nil", next, err)
	}

	// Viewing an older message must not rewind the pointer.
	if err := mm.AdvanceLastRead(1, "bob", 1); err != nil {
		t.Fatalf("AdvanceLastRead(1): %v", err)
	}
	if lr, _ := mm.GetLastRead(1, "bob"); lr != 2 {
		t.Errorf("lastread after AdvanceLastRead to an older message = %d, want 2", lr)
	}

	// SetLastRead, unlike AdvanceLastRead, moves the pointer either way.
	if err := mm.SetLastRead(1, "bob", 1); err != nil {
		t.Fatalf("SetLastRead(1): %v", err)
	}
	if lr, _ := mm.GetLastRead(1, "bob"); lr != 1 {
		t.Errorf("lastread after SetLastRead(1) = %d, want 1", lr)
	}

	if err := mm.AdvanceLastRead(1, "bob", 3); err != nil {
		t.Fatalf("AdvanceLastRead(3): %v", err)
	}
	if next, err := mm.GetNextUnreadMessage(1, "bob"); err != nil || next != 0 {
		t.Errorf("GetNextUnreadMessage with everything read = %d, %v; want 0, nil", next, err)
	}
}

func TestGetNextUnreadMessage_EmptyArea(t *testing.T) {
	mm := newReplyTestManager(t)
	if next, err := mm.GetNextUnreadMessage(1, "bob"); err != nil || next != 0 {
		t.Errorf("GetNextUnreadMessage on an empty area = %d, %v; want 0, nil", next, err)
	}
}

// The design note in manager.go: reads of an unconfigured area come back empty,
// writes and direct access report ErrAreaNotFound.
func TestMissingAreaHandling(t *testing.T) {
	mm := newReplyTestManager(t)
	const missing = 99

	reads := map[string]func() (int, error){
		"GetMessageCountForArea": func() (int, error) { return mm.GetMessageCountForArea(missing) },
		"GetThreadReplyCount":    func() (int, error) { return mm.GetThreadReplyCount(missing, 1, "s") },
		"GetNewMessageCount":     func() (int, error) { return mm.GetNewMessageCount(missing, "bob") },
		"GetLastRead":            func() (int, error) { return mm.GetLastRead(missing, "bob") },
		"GetNextUnreadMessage":   func() (int, error) { return mm.GetNextUnreadMessage(missing, "bob") },
	}
	for name, fn := range reads {
		if n, err := fn(); n != 0 || err != nil {
			t.Errorf("%s on a missing area = %d, %v; want 0, nil", name, n, err)
		}
	}

	writes := map[string]func() error{
		"SetLastRead":     func() error { return mm.SetLastRead(missing, "bob", 1) },
		"AdvanceLastRead": func() error { return mm.AdvanceLastRead(missing, "bob", 1) },
		"MarkMessageSent": func() error { return mm.MarkMessageSent(missing, 1) },
		"DeleteMessage":   func() error { return mm.DeleteMessage(missing, 1) },
		"PackAndLinkArea": func() error { return mm.PackAndLinkArea(missing) },
		"GetMessage":      func() error { _, err := mm.GetMessage(missing, 1); return err },
		"GetBase":         func() error { _, err := mm.GetBase(missing); return err },
		"AddMessage":      func() error { _, err := mm.AddMessage(missing, "a", "b", "s", "b", ""); return err },
	}
	for name, fn := range writes {
		if err := fn(); !errors.Is(err, ErrAreaNotFound) {
			t.Errorf("%s on a missing area: err = %v, want ErrAreaNotFound", name, err)
		}
	}
}

// A base that exists in the config but cannot be opened is a real failure and
// must not be reported as an empty area.
func TestUnopenableBasePropagatesError(t *testing.T) {
	mm := newBrokenBaseManager(t)

	calls := map[string]func() error{
		"GetMessageCountForArea": func() error { _, err := mm.GetMessageCountForArea(1); return err },
		"GetThreadReplyCount":    func() error { _, err := mm.GetThreadReplyCount(1, 1, "s"); return err },
		"GetNewMessageCount":     func() error { _, err := mm.GetNewMessageCount(1, "bob"); return err },
		"GetLastRead":            func() error { _, err := mm.GetLastRead(1, "bob"); return err },
		"GetNextUnreadMessage":   func() error { _, err := mm.GetNextUnreadMessage(1, "bob"); return err },
		"GetAreaCounts":          func() error { _, err := mm.GetAreaCounts(1, "bob"); return err },
	}
	for name, fn := range calls {
		err := fn()
		if err == nil || errors.Is(err, ErrAreaNotFound) {
			t.Errorf("%s on an unopenable base: err = %v, want an I/O error", name, err)
		}
	}

	// The site-wide total skips the area it cannot read instead of failing.
	if got := mm.GetTotalMessageCount(); got != 0 {
		t.Errorf("GetTotalMessageCount() = %d, want 0", got)
	}
	// The MSGID lookup has no error return; it reports nothing found.
	if got := mm.FindMessageByMSGID(1, "1:2/3 abcd"); got != 0 {
		t.Errorf("FindMessageByMSGID on an unopenable base = %d, want 0", got)
	}
}

func TestMarkMessageSent(t *testing.T) {
	mm := newReplyTestManager(t)
	n := post(t, mm, 1, "All", "one")

	before, err := mm.GetMessage(1, n)
	if err != nil {
		t.Fatal(err)
	}
	if before.Attributes&jam.MsgSent != 0 {
		t.Fatal("a fresh message is already flagged sent")
	}

	if err := mm.MarkMessageSent(1, n); err != nil {
		t.Fatalf("MarkMessageSent: %v", err)
	}
	after, err := mm.GetMessage(1, n)
	if err != nil {
		t.Fatal(err)
	}
	if after.Attributes&jam.MsgSent == 0 {
		t.Errorf("Attributes = %#x, want MsgSent set", after.Attributes)
	}
	if after.Subject != "one" || after.From != "alice" {
		t.Errorf("header rewrite changed the message: %+v", after)
	}

	err = mm.MarkMessageSent(1, n+5)
	if err == nil || !strings.Contains(err.Error(), "mark sent") {
		t.Errorf("MarkMessageSent on a missing message: err = %v, want a \"mark sent\" error", err)
	}
}

func TestDeleteThenPackAndLink(t *testing.T) {
	mm := newReplyTestManager(t)
	post(t, mm, 1, "All", "one")
	doomed := post(t, mm, 1, "All", "two")
	post(t, mm, 1, "All", "three")

	if err := mm.DeleteMessage(1, doomed); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	// Deleting only flags the message; it keeps its slot until the base is packed.
	msg, err := mm.GetMessage(1, doomed)
	if err != nil {
		t.Fatal(err)
	}
	if !msg.IsDeleted {
		t.Error("deleted message is not flagged IsDeleted")
	}
	if n, _ := mm.GetMessageCountForArea(1); n != 3 {
		t.Errorf("count after delete = %d, want 3 (not yet packed)", n)
	}

	if err := mm.PackAndLinkArea(1); err != nil {
		t.Fatalf("PackAndLinkArea: %v", err)
	}
	if n, _ := mm.GetMessageCountForArea(1); n != 2 {
		t.Errorf("count after pack = %d, want 2", n)
	}
	// Packing renumbers: "three" moves down into the freed slot.
	msg, err = mm.GetMessage(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Subject != "three" || msg.IsDeleted {
		t.Errorf("message 2 after pack = %q (deleted=%v), want \"three\"", msg.Subject, msg.IsDeleted)
	}

	if err := mm.DeleteMessage(1, 50); err == nil {
		t.Error("DeleteMessage on a missing message returned nil")
	}
}

func TestGetBaseGivesDirectAccess(t *testing.T) {
	mm := newReplyTestManager(t)
	post(t, mm, 1, "All", "one")

	b, err := mm.GetBase(1)
	if err != nil {
		t.Fatalf("GetBase: %v", err)
	}
	defer b.Close()
	if n, err := b.GetMessageCount(); err != nil || n != 1 {
		t.Errorf("base message count = %d, %v; want 1, nil", n, err)
	}
}

func TestGetMessageStripsKludgesAndNormalizesLineEndings(t *testing.T) {
	mm := newReplyTestManager(t)
	n, err := mm.AddMessage(1, "alice", "All", "s", "first\r\x01V3NETUUID: abc\rsecond\r\nthird", "")
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mm.GetMessage(1, n)
	if err != nil {
		t.Fatal(err)
	}
	if want := "first\nsecond\nthird"; msg.Body != want {
		t.Errorf("Body = %q, want %q", msg.Body, want)
	}
	if msg.MsgNum != n || msg.AreaID != 1 {
		t.Errorf("MsgNum/AreaID = %d/%d, want %d/1", msg.MsgNum, msg.AreaID, n)
	}

	if _, err := mm.GetMessage(1, n+1); err == nil {
		t.Error("GetMessage past the end of the base returned nil error")
	}
}
