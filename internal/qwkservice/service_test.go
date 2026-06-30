package qwkservice

import (
	"archive/zip"
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)

// fakeStore is an in-memory MessageStore for service tests.
type fakeStore struct {
	areas    []*message.MessageArea
	byTag    map[string]*message.MessageArea
	byID     map[int]*message.MessageArea
	msgs     map[int][]*message.DisplayMessage // areaID -> 1-based messages
	lastRead map[string]int                    // "areaID/user" -> num

	posted   []postedMessage
	setReads []setRead
	addErr   map[int]error // areaID -> error to return from AddMessage
}

type postedMessage struct {
	areaID                       int
	from, to, subject, body, rep string
}

type setRead struct {
	areaID int
	user   string
	num    int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		byTag:    map[string]*message.MessageArea{},
		byID:     map[int]*message.MessageArea{},
		msgs:     map[int][]*message.DisplayMessage{},
		lastRead: map[string]int{},
		addErr:   map[int]error{},
	}
}

func (f *fakeStore) addArea(a *message.MessageArea) {
	f.areas = append(f.areas, a)
	f.byTag[a.Tag] = a
	f.byID[a.ID] = a
}

func (f *fakeStore) ListAreas() []*message.MessageArea { return f.areas }

func (f *fakeStore) GetAreaByTag(tag string) (*message.MessageArea, bool) {
	a, ok := f.byTag[tag]
	return a, ok
}

func (f *fakeStore) GetAreaByID(id int) (*message.MessageArea, bool) {
	a, ok := f.byID[id]
	return a, ok
}

func (f *fakeStore) GetLastRead(areaID int, username string) (int, error) {
	return f.lastRead[key(areaID, username)], nil
}

func (f *fakeStore) SetLastRead(areaID int, username string, msgNum int) error {
	f.setReads = append(f.setReads, setRead{areaID, username, msgNum})
	f.lastRead[key(areaID, username)] = msgNum
	return nil
}

func (f *fakeStore) GetMessageCountForArea(areaID int) (int, error) {
	return len(f.msgs[areaID]), nil
}

func (f *fakeStore) GetMessage(areaID, msgNum int) (*message.DisplayMessage, error) {
	list := f.msgs[areaID]
	if msgNum < 1 || msgNum > len(list) {
		return nil, fmt.Errorf("no message %d in area %d", msgNum, areaID)
	}
	return list[msgNum-1], nil
}

func (f *fakeStore) AddMessage(areaID int, from, to, subject, body, replyToMsgID string) (int, error) {
	if err := f.addErr[areaID]; err != nil {
		return 0, err
	}
	f.posted = append(f.posted, postedMessage{areaID, from, to, subject, body, replyToMsgID})
	return len(f.posted), nil
}

func key(areaID int, user string) string { return fmt.Sprintf("%d/%s", areaID, user) }

func (f *fakeStore) seed(areaID int, msgs ...*message.DisplayMessage) {
	f.msgs[areaID] = append(f.msgs[areaID], msgs...)
}

func dm(num int, from, to, subj, body string) *message.DisplayMessage {
	return &message.DisplayMessage{
		MsgNum: num, From: from, To: to, Subject: subj, Body: body,
		DateTime: time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC),
	}
}

// ---- Export tests ----

func TestBuildPacket_PacksNewMessages(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	store.seed(1, dm(1, "a", "All", "s1", "b1"), dm(2, "b", "All", "s2", "b2"))

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
	res, err := svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"GENERAL"}})
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	if res.MessageCount != 2 {
		t.Errorf("MessageCount: want 2, got %d", res.MessageCount)
	}
	if len(res.Packet) == 0 {
		t.Error("expected non-empty packet bytes")
	}
	// Packet must be a valid zip with MESSAGES.DAT.
	zr, err := zip.NewReader(bytes.NewReader(res.Packet), int64(len(res.Packet)))
	if err != nil {
		t.Fatalf("packet not a zip: %v", err)
	}
	hasMessages := false
	for _, fl := range zr.File {
		if fl.Name == "MESSAGES.DAT" {
			hasMessages = true
		}
	}
	if !hasMessages {
		t.Error("packet missing MESSAGES.DAT")
	}

	// last-read should be pending but NOT yet committed.
	if len(res.LastRead) != 1 || res.LastRead[0].MsgNum != 2 {
		t.Errorf("pending last-read: want [{1 2}], got %+v", res.LastRead)
	}
	if len(store.setReads) != 0 {
		t.Errorf("BuildPacket must not commit last-read, got %+v", store.setReads)
	}
}

func TestBuildPacket_RespectsLastRead(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	store.seed(1, dm(1, "a", "All", "s1", "b1"), dm(2, "b", "All", "s2", "b2"), dm(3, "c", "All", "s3", "b3"))
	store.lastRead[key(1, "tester")] = 2 // already read 1 and 2

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
	res, err := svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"GENERAL"}})
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	if res.MessageCount != 1 {
		t.Errorf("MessageCount: want 1 (only msg 3), got %d", res.MessageCount)
	}
	if len(res.LastRead) != 1 || res.LastRead[0].MsgNum != 3 {
		t.Errorf("pending last-read: want msg 3, got %+v", res.LastRead)
	}
}

func TestBuildPacket_SkipsDeleted(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	deleted := dm(2, "b", "All", "s2", "b2")
	deleted.IsDeleted = true
	store.seed(1, dm(1, "a", "All", "s1", "b1"), deleted)

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
	res, _ := svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"GENERAL"}})
	if res.MessageCount != 1 {
		t.Errorf("MessageCount: want 1 (deleted skipped), got %d", res.MessageCount)
	}
	// last-read advances only to the highest message actually packed (1).
	// A trailing deleted message does not advance the pointer past itself; this
	// mirrors the original menu behavior and is harmless (it is merely
	// re-examined and skipped on the next newscan).
	if res.LastRead[0].MsgNum != 1 {
		t.Errorf("last-read should be 1 (highest packed), got %d", res.LastRead[0].MsgNum)
	}
}

func TestBuildPacket_NoNewMessages(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	store.seed(1, dm(1, "a", "All", "s1", "b1"))
	store.lastRead[key(1, "tester")] = 1

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
	res, err := svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"GENERAL"}})
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	if res.MessageCount != 0 {
		t.Errorf("MessageCount: want 0, got %d", res.MessageCount)
	}
	if res.Packet != nil {
		t.Error("packet should be nil when there are no new messages")
	}
	if len(res.LastRead) != 0 {
		t.Errorf("no pending last-read expected, got %+v", res.LastRead)
	}
}

func TestBuildPacket_EmptyTagsFallsBackToAllAreas(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	store.addArea(&message.MessageArea{ID: 2, Tag: "TECH", Name: "Tech"})
	store.seed(1, dm(1, "a", "All", "s1", "b1"))
	store.seed(2, dm(1, "a", "All", "s2", "b2"))

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
	res, _ := svc.BuildPacket(ExportOptions{Handle: "tester"}) // no tags
	if res.MessageCount != 2 {
		t.Errorf("MessageCount across all areas: want 2, got %d", res.MessageCount)
	}
}

func TestBuildPacket_MaxPerArea(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	for i := 1; i <= 5; i++ {
		store.seed(1, dm(i, "a", "All", "s", "b"))
	}

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
	res, _ := svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"GENERAL"}, MaxPerArea: 3})
	if res.MessageCount != 3 {
		t.Errorf("MessageCount with MaxPerArea=3: want 3, got %d", res.MessageCount)
	}
	if res.LastRead[0].MsgNum != 3 {
		t.Errorf("last-read should stop at the cap (3), got %d", res.LastRead[0].MsgNum)
	}
}

func TestCommitExport_AppliesLastRead(t *testing.T) {
	store := newFakeStore()
	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
	res := &ExportResult{LastRead: []LastReadUpdate{{AreaID: 1, MsgNum: 7}, {AreaID: 2, MsgNum: 4}}}

	svc.CommitExport("tester", res)

	if len(store.setReads) != 2 {
		t.Fatalf("want 2 SetLastRead calls, got %d", len(store.setReads))
	}
	if store.setReads[0] != (setRead{1, "tester", 7}) {
		t.Errorf("first commit: got %+v", store.setReads[0])
	}
}

// ---- Import tests ----

func makeREP(t *testing.T, bbsID string, msgs []qwk.PacketMessage) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := qwk.WriteREP(&buf, bbsID, msgs); err != nil {
		t.Fatalf("WriteREP: %v", err)
	}
	return buf.Bytes()
}

func TestImportREP_PostsReplies(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})

	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 1, Number: 1, From: "tester", To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "Reply body"},
	})

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
	res, err := svc.ImportREP(rep, "VISION3", ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("ImportREP: %v", err)
	}
	if res.Posted != 1 || res.Skipped != 0 {
		t.Errorf("want posted=1 skipped=0, got %+v", res)
	}
	if len(store.posted) != 1 {
		t.Fatalf("want 1 posted message, got %d", len(store.posted))
	}
	if store.posted[0].from != "tester" || store.posted[0].to != "SysOp" {
		t.Errorf("posted from/to wrong: %+v", store.posted[0])
	}
}

func TestImportREP_AppendsSignature(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "Body"},
	})

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
	_, err := svc.ImportREP(rep, "VISION3", ImportOptions{Handle: "tester", Signature: "-- sig"})
	if err != nil {
		t.Fatalf("ImportREP: %v", err)
	}
	if got := store.posted[0].body; got != "Body\n\n-- sig" {
		t.Errorf("signature not appended: %q", got)
	}
}

func TestImportREP_SkipsUnknownConference(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 99, Number: 1, To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "Body"},
	})

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
	res, _ := svc.ImportREP(rep, "VISION3", ImportOptions{Handle: "tester"})
	if res.Posted != 0 || res.Skipped != 1 {
		t.Errorf("want posted=0 skipped=1, got %+v", res)
	}
}

func TestImportREP_AuthorizeGate(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "READONLY", Name: "Read Only"})
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "Body"},
	})

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
	res, _ := svc.ImportREP(rep, "VISION3", ImportOptions{
		Handle:    "tester",
		Authorize: func(*message.MessageArea) bool { return false },
	})
	if res.Posted != 0 || res.Skipped != 1 {
		t.Errorf("want posted=0 skipped=1 (denied), got %+v", res)
	}
	if len(store.posted) != 0 {
		t.Error("nothing should be posted when Authorize denies")
	}
}

func TestImportREP_NotifyCalled(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "Body"},
	})

	var notified []string
	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
	_, err := svc.ImportREP(rep, "VISION3", ImportOptions{
		Handle: "tester",
		Notify: func(a *message.MessageArea) { notified = append(notified, a.Name) },
	})
	if err != nil {
		t.Fatalf("ImportREP: %v", err)
	}
	if len(notified) != 1 || notified[0] != "General" {
		t.Errorf("Notify not called as expected: %v", notified)
	}
}

func TestImportREP_PostErrorCounted(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	store.addErr[1] = fmt.Errorf("disk full")
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "Body"},
	})

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
	res, err := svc.ImportREP(rep, "VISION3", ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("ImportREP should not fail wholesale on a single post error: %v", err)
	}
	if res.Posted != 0 || res.Skipped != 1 {
		t.Errorf("want posted=0 skipped=1, got %+v", res)
	}
}

func TestImportREP_BadPacketErrors(t *testing.T) {
	store := newFakeStore()
	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
	_, err := svc.ImportREP([]byte("not a zip"), "VISION3", ImportOptions{Handle: "tester"})
	if err == nil {
		t.Fatal("expected error for a non-zip REP packet")
	}
}
