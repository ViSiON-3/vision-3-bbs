package message

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// newIndexTestManager builds a manager with an echomail area (whose posts are
// stamped with a MSGID) and a QWK network area.
func newIndexTestManager(t *testing.T) *MessageManager {
	t.Helper()
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMsgAreas(t, cfg, `[
		{"id":1,"tag":"FSX_GEN","name":"General","base_path":"fsx_gen","area_type":"echomail",
		 "echo_tag":"FSX_GEN","origin_addr":"21:4/158","network":"fsxnet"},
		{"id":2,"tag":"DOVE_GEN","name":"Dove","base_path":"dove_gen","area_type":"qwknet",
		 "echo_tag":"General","network":"dovenet","qwk_conference":2001},
		{"id":3,"tag":"EMPTY","name":"Empty","base_path":"empty","area_type":"local"}]`)
	mm, err := NewMessageManager(tmp, cfg, "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	return mm
}

// msgIDOf returns the MSGID stored with a message.
func msgIDOf(t *testing.T, mm *MessageManager, areaID, msgNum int) string {
	t.Helper()
	msg, err := mm.GetMessage(areaID, msgNum)
	if err != nil {
		t.Fatalf("GetMessage(%d, %d): %v", areaID, msgNum, err)
	}
	if msg.MsgID == "" {
		t.Fatalf("message %d in area %d has no MSGID", msgNum, areaID)
	}
	return msg.MsgID
}

func TestFindMessageByMSGID(t *testing.T) {
	mm := newIndexTestManager(t)
	first := post(t, mm, 1, "All", "one")
	second := post(t, mm, 1, "All", "two")
	firstID, secondID := msgIDOf(t, mm, 1, first), msgIDOf(t, mm, 1, second)
	if firstID == secondID {
		t.Fatalf("both posts got MSGID %q", firstID)
	}

	if got := mm.FindMessageByMSGID(1, firstID); got != first {
		t.Errorf("FindMessageByMSGID(%q) = %d, want %d", firstID, got, first)
	}
	if got := mm.FindMessageByMSGID(1, secondID); got != second {
		t.Errorf("FindMessageByMSGID(%q) = %d, want %d", secondID, got, second)
	}

	for name, id := range map[string]string{
		"unknown MSGID": "9:9/9 deadbeef",
		"empty MSGID":   "",
	} {
		if got := mm.FindMessageByMSGID(1, id); got != 0 {
			t.Errorf("%s: FindMessageByMSGID = %d, want 0", name, got)
		}
	}
	if got := mm.FindMessageByMSGID(3, firstID); got != 0 {
		t.Errorf("lookup in an empty area = %d, want 0", got)
	}
	if got := mm.FindMessageByMSGID(99, firstID); got != 0 {
		t.Errorf("lookup in a missing area = %d, want 0", got)
	}
}

// Some tossers write a REPLY kludge without the serial, so the address part of
// a MSGID is indexed too. It resolves to the first message from that address.
func TestFindMessageByMSGID_AddressPrefix(t *testing.T) {
	mm := newIndexTestManager(t)
	first := post(t, mm, 1, "All", "one")
	post(t, mm, 1, "All", "two")

	full := msgIDOf(t, mm, 1, first)
	cut := strings.LastIndex(full, " ")
	if cut <= 0 {
		t.Fatalf("MSGID %q is not in \"address serial\" form", full)
	}
	if got := mm.FindMessageByMSGID(1, full[:cut]); got != first {
		t.Errorf("FindMessageByMSGID(%q) = %d, want %d", full[:cut], got, first)
	}
}

func TestFindMessagesByMSGID_Batch(t *testing.T) {
	mm := newIndexTestManager(t)
	first := post(t, mm, 1, "All", "one")
	second := post(t, mm, 1, "All", "two")
	firstID, secondID := msgIDOf(t, mm, 1, first), msgIDOf(t, mm, 1, second)

	got := mm.FindMessagesByMSGID(1, []string{firstID, "", "9:9/9 deadbeef", secondID})
	if len(got) != 2 || got[firstID] != first || got[secondID] != second {
		t.Errorf("FindMessagesByMSGID = %v, want {%q:%d, %q:%d}", got, firstID, first, secondID, second)
	}

	if got := mm.FindMessagesByMSGID(1, nil); len(got) != 0 {
		t.Errorf("FindMessagesByMSGID with no IDs = %v, want empty", got)
	}
}

// The index is cached per area, so it has to notice the base changing under it.
func TestMSGIDIndexFollowsBaseChanges(t *testing.T) {
	mm := newIndexTestManager(t)
	first := post(t, mm, 1, "All", "one")
	firstID := msgIDOf(t, mm, 1, first)
	if got := mm.FindMessageByMSGID(1, firstID); got != first {
		t.Fatalf("FindMessageByMSGID = %d, want %d", got, first)
	}

	// A message posted after the index was built is found.
	second := post(t, mm, 1, "All", "two")
	secondID := msgIDOf(t, mm, 1, second)
	if got := mm.FindMessageByMSGID(1, secondID); got != second {
		t.Errorf("message posted after the index was built: got %d, want %d", got, second)
	}

	// A deleted message drops out.
	if err := mm.DeleteMessage(1, first); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	if got := mm.FindMessageByMSGID(1, firstID); got != 0 {
		t.Errorf("deleted message still resolves to %d, want 0", got)
	}
	if got := mm.FindMessageByMSGID(1, secondID); got != second {
		t.Errorf("surviving message = %d, want %d", got, second)
	}
}

func TestGetThreadReplyCount(t *testing.T) {
	mm := newReplyTestManager(t)
	root := post(t, mm, 1, "All", "Topic")
	post(t, mm, 1, "All", "Re: Topic")
	pascal := post(t, mm, 1, "All", "RE: re: topic -Re: #1-")
	post(t, mm, 1, "All", "Other")

	for subject, want := range map[string]int{
		"Topic":        2, // two other messages share the thread
		"re: TOPIC":    2, // the key ignores case and reply prefixes
		"Other":        0, // a thread of one has no replies
		"Never posted": 0,
	} {
		got, err := mm.GetThreadReplyCount(1, root, subject)
		if err != nil || got != want {
			t.Errorf("GetThreadReplyCount(%q) = %d, %v; want %d, nil", subject, got, err, want)
		}
	}

	// A new post and a deletion both have to show up in the cached counts.
	post(t, mm, 1, "All", "Re: Other")
	if got, _ := mm.GetThreadReplyCount(1, root, "Other"); got != 1 {
		t.Errorf("after a reply to Other: count = %d, want 1", got)
	}
	if err := mm.DeleteMessage(1, pascal); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	if got, _ := mm.GetThreadReplyCount(1, root, "Topic"); got != 1 {
		t.Errorf("after deleting a reply: count = %d, want 1", got)
	}
}

func TestQWKIDIsNormalized(t *testing.T) {
	mm := newIndexTestManager(t)
	if got := mm.QWKID(); got != "" {
		t.Errorf("QWKID() before SetQWKID = %q, want empty", got)
	}
	mm.SetQWKID("  VERT ")
	if got := mm.QWKID(); got != "vert" {
		t.Errorf("QWKID() = %q, want %q", got, "vert")
	}
}

// A post in a QWK network area carries no FTN address, so the manager mints
// the Message-ID itself from the area tag and the system's QWK ID.
func TestQWKNetPostGetsMessageID(t *testing.T) {
	mm := newIndexTestManager(t)
	mm.SetQWKID("VERT")

	n := post(t, mm, 2, "All", "hello dove")
	id := msgIDOf(t, mm, 2, n)
	if !regexp.MustCompile(`^<[0-9a-f]+\.dove_gen@vert>$`).MatchString(id) {
		t.Errorf("MsgID = %q, want <time-hex.dove_gen@vert>", id)
	}
	if got := mm.FindMessageByMSGID(2, id); got != n {
		t.Errorf("FindMessageByMSGID(%q) = %d, want %d", id, got, n)
	}
}

func TestQWKNetPostWithoutQWKIDHasNoMessageID(t *testing.T) {
	mm := newIndexTestManager(t)

	n := post(t, mm, 2, "All", "hello dove")
	msg, err := mm.GetMessage(2, n)
	if err != nil {
		t.Fatal(err)
	}
	if msg.MsgID != "" {
		t.Errorf("MsgID = %q, want empty when no QWK ID is set", msg.MsgID)
	}
}
