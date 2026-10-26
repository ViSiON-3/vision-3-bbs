package qwkservice

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/conference"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)

func TestBuildPacket_PacksNewMessages(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	store.seed(1, dm(1, "a", "All", "s1", "b1"), dm(2, "b", "All", "s2", "b2"))

	svc := newTestService(t, store)
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

	svc := newTestService(t, store)
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

	svc := newTestService(t, store)
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

	svc := newTestService(t, store)
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

	svc := newTestService(t, store)
	res, _ := svc.BuildPacket(ExportOptions{Handle: "tester"}) // no tags
	if res.MessageCount != 2 {
		t.Errorf("MessageCount across all areas: want 2, got %d", res.MessageCount)
	}
}

func TestBuildPacket_DuplicateTagsDeduped(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	store.seed(1, dm(1, "a", "All", "s1", "b1"))

	svc := newTestService(t, store)
	res, _ := svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"GENERAL", "GENERAL"}})

	// A repeated tag must not double-count messages or last-read updates.
	if res.MessageCount != 1 {
		t.Errorf("MessageCount with duplicate tag: want 1, got %d", res.MessageCount)
	}
	if len(res.LastRead) != 1 {
		t.Errorf("last-read updates with duplicate tag: want 1, got %d", len(res.LastRead))
	}
}

func TestBuildPacket_DoesNotMutateCallerTags(t *testing.T) {
	store := newFakeStore()
	// Empty-but-non-nil slice with spare capacity: a careless append in the
	// fallback path could scribble into the caller's backing array.
	tags := make([]string, 0, 4)
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	store.seed(1, dm(1, "a", "All", "s1", "b1"))

	svc := newTestService(t, store)
	_, _ = svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: tags})

	if len(tags) != 0 {
		t.Errorf("caller TaggedTags was mutated: %v", tags)
	}
}

func TestBuildPacket_MaxPerArea(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	for i := 1; i <= 5; i++ {
		store.seed(1, dm(i, "a", "All", "s", "b"))
	}

	svc := newTestService(t, store)
	res, _ := svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"GENERAL"}, MaxPerArea: 3})
	if res.MessageCount != 3 {
		t.Errorf("MessageCount with MaxPerArea=3: want 3, got %d", res.MessageCount)
	}
	if res.LastRead[0].MsgNum != 3 {
		t.Errorf("last-read should stop at the cap (3), got %d", res.LastRead[0].MsgNum)
	}
}

// hasNDX reports whether the packet zip contains the given per-conference NDX.
func hasNDX(t *testing.T, packet []byte, name string) bool {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(packet), int64(len(packet)))
	if err != nil {
		t.Fatalf("packet not a zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			return true
		}
	}
	return false
}

func privMsg(num int, from, to string) *message.DisplayMessage {
	m := dm(num, from, to, "subj", "body")
	m.IsPrivate = true
	return m
}

func TestBuildPacket_PublicUsesStableNumber(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 5, Tag: "GENERAL", Name: "General"})
	store.seed(5, dm(1, "a", "All", "s", "b"))

	svc := newTestService(t, store)
	res, err := svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"GENERAL"}})
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	// Public area keeps its area.ID (5) as the conference number -> 005.NDX.
	if !hasNDX(t, res.Packet, "005.NDX") {
		t.Error("expected public area to export under conference 5 (005.NDX)")
	}
}

func TestBuildPacket_PrivateMailUsesConferenceZero(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 3, Tag: "PRIVMAIL", Name: "Private Mail"})
	store.seed(3, privMsg(1, "someone", "tester"))

	svc := newTestService(t, store)
	res, err := svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"PRIVMAIL"}})
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	if res.MessageCount != 1 {
		t.Fatalf("MessageCount: want 1, got %d", res.MessageCount)
	}
	if !hasNDX(t, res.Packet, "000.NDX") {
		t.Error("expected PRIVMAIL to export under conference 0 (000.NDX)")
	}
}

func TestBuildPacket_PrivateMailFiltersToUser(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 3, Tag: "PRIVMAIL", Name: "Private Mail"})
	store.seed(3,
		privMsg(1, "someone", "tester"), // to me -> included
		privMsg(2, "tester", "friend"),  // from me -> included
		privMsg(3, "alice", "bob"),      // neither -> excluded
	)

	svc := newTestService(t, store)
	res, err := svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"PRIVMAIL"}})
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	if res.MessageCount != 2 {
		t.Errorf("private-mail export should include only the user's own mail: want 2, got %d", res.MessageCount)
	}
}

func TestCommitExport_AppliesLastRead(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)
	res := &ExportResult{LastRead: []LastReadUpdate{{AreaID: 1, MsgNum: 7}, {AreaID: 2, MsgNum: 4}}}

	svc.CommitExport("tester", res)

	if len(store.setReads) != 2 {
		t.Fatalf("want 2 SetLastRead calls, got %d", len(store.setReads))
	}
	if store.setReads[0] != (setRead{1, "tester", 7}) {
		t.Errorf("first commit: got %+v", store.setReads[0])
	}
}

// firstMsgReplyRef parses the QWK reference field (positions 108-115) of the
// first message in a packet's MESSAGES.DAT.
func firstMsgReplyRef(t *testing.T, packet []byte) int {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(packet), int64(len(packet)))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != "MESSAGES.DAT" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		// Block 1 is the spacer; the first message header starts at offset 128.
		hdr := data[128:256]
		ref, _ := strconv.Atoi(strings.TrimSpace(string(hdr[108:116])))
		return ref
	}
	t.Fatal("MESSAGES.DAT not found")
	return 0
}

func TestBuildPacket_WritesReplyReference(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	m := dm(1, "a", "All", "s", "b")
	m.ReplyToNum = 7
	store.seed(1, m)

	svc := newTestService(t, store)
	res, err := svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"GENERAL"}})
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	if got := firstMsgReplyRef(t, res.Packet); got != 7 {
		t.Errorf("exported reply reference: want 7, got %d", got)
	}
}

// TestBuildPacket_OmitsOthersPrivateMailInPublicAreas pins #465 for QWK: a
// private message in an ordinary conference is packed only for its sender or
// recipient by handle, never for another caller, and addressing it to the
// caller's real name does not make it theirs.
func TestBuildPacket_OmitsOthersPrivateMailInPublicAreas(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 5, Tag: "GENERAL", Name: "General"})
	store.seed(5,
		dm(1, "alice", "All", "s", "b"), // public -> included
		privMsg(2, "alice", "bob"),      // someone else's -> excluded
		privMsg(3, "alice", "Tess Ter"), // a real name, not a handle -> excluded
		privMsg(4, "alice", "TESTER"),   // to my handle -> included
		privMsg(5, "tester", "carol"),   // from my handle -> included
	)

	svc := newTestService(t, store)
	res, err := svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"GENERAL"}})
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	pkt, err := qwk.ReadPacket(bytes.NewReader(res.Packet), int64(len(res.Packet)))
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	var got []int
	for _, m := range pkt.Messages {
		got = append(got, m.Number)
	}
	if want := []int{1, 4, 5}; !slices.Equal(got, want) {
		t.Errorf("packed message numbers = %v, want %v", got, want)
	}
}

type fakeConfs []*conference.Conference

func (f fakeConfs) ListConferences() []*conference.Conference { return f }

func readAreaIndex(t *testing.T, packet []byte) *qwk.AreaIndex {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(packet), int64(len(packet)))
	if err != nil {
		t.Fatalf("packet not a zip: %v", err)
	}
	for _, fl := range zr.File {
		if fl.Name != qwk.AreaIndexFile {
			continue
		}
		rc, err := fl.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		raw, _ := io.ReadAll(rc)
		var idx qwk.AreaIndex
		if err := json.Unmarshal(raw, &idx); err != nil {
			t.Fatalf("%s: %v", qwk.AreaIndexFile, err)
		}
		return &idx
	}
	t.Fatalf("packet missing %s", qwk.AreaIndexFile)
	return nil
}

func TestBuildPacket_WritesAreaIndexWithBoardConferences(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General Discussion", ConferenceID: 1, AreaType: "local"})
	store.addArea(&message.MessageArea{ID: 2, Tag: "FSX_GEN", Name: "General", ConferenceID: 2, AreaType: "echomail", Network: "fsxnet"})
	store.addArea(&message.MessageArea{ID: 3, Tag: "LORD", Name: "LORD: Legend of the Red Dragon", AreaType: "echomail", Network: "fidonet"}) // ungrouped
	store.seed(1, dm(1, "a", "All", "s", "b"))
	store.seed(2, dm(1, "a", "All", "s", "b"))
	store.seed(3, dm(1, "a", "All", "s", "b"))

	svc := newTestService(t, store)
	svc.SetConferenceLister(fakeConfs{
		{ID: 2, Position: 1, Tag: "FSXNET", Name: "fsxNet"},
		{ID: 1, Position: 2, Tag: "LOCAL", Name: "Local Areas"},
		{ID: 9, Position: 3, Tag: "UNUSED", Name: "No areas exported here"},
	})
	res, err := svc.BuildPacket(ExportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	idx := readAreaIndex(t, res.Packet)

	if idx.Version != qwk.AreaIndexVersion {
		t.Errorf("version: %d", idx.Version)
	}
	// Only referenced conferences, in board display order.
	gotConfs := make([]string, 0, len(idx.Conferences))
	for _, c := range idx.Conferences {
		gotConfs = append(gotConfs, c.Tag)
	}
	if !slices.Equal(gotConfs, []string{"FSXNET", "LOCAL"}) {
		t.Errorf("conferences: %v", gotConfs)
	}
	byTag := map[string]qwk.AreaIndexArea{}
	for _, a := range idx.Areas {
		byTag[a.Tag] = a
	}
	if a := byTag["FSX_GEN"]; a.QWK != 2 || a.Conference != "FSXNET" || a.Type != "echomail" || a.Network != "fsxnet" {
		t.Errorf("FSX_GEN: %+v", a)
	}
	if a := byTag["GENERAL"]; a.QWK != 1 || a.Conference != "LOCAL" || a.Type != "local" {
		t.Errorf("GENERAL: %+v", a)
	}
	// An area with no (or unknown) conference stays ungrouped; its name is
	// passed through untouched so no client has to parse it.
	if a := byTag["LORD"]; a.QWK != 3 || a.Conference != "" || a.Name != "LORD: Legend of the Red Dragon" {
		t.Errorf("LORD: %+v", a)
	}
}

func TestBuildPacket_AreaIndexWithoutConferenceLister(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General", ConferenceID: 1})
	store.seed(1, dm(1, "a", "All", "s", "b"))
	svc := newTestService(t, store)
	res, err := svc.BuildPacket(ExportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	idx := readAreaIndex(t, res.Packet)
	if len(idx.Conferences) != 0 {
		t.Errorf("conferences without lister: %+v", idx.Conferences)
	}
	if len(idx.Areas) != 1 || idx.Areas[0].Conference != "" || idx.Areas[0].QWK != 1 {
		t.Errorf("areas: %+v", idx.Areas)
	}
}
