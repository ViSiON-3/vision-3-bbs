package qwkservice

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

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

func TestBuildPacket_DuplicateTagsDeduped(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	store.seed(1, dm(1, "a", "All", "s1", "b1"))

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
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

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp")
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
