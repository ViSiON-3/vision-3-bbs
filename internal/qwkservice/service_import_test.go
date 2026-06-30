package qwkservice

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)

func TestImportREP_PostsReplies(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})

	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 1, Number: 1, From: "tester", To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "Reply body"},
	})

	svc := newTestService(t, store)
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
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

	svc := newTestService(t, store)
	_, err := svc.ImportREP(rep, ImportOptions{Handle: "tester", Signature: "-- sig"})
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

	svc := newTestService(t, store)
	res, _ := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
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

	svc := newTestService(t, store)
	res, _ := svc.ImportREP(rep, ImportOptions{
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
	svc := newTestService(t, store)
	_, err := svc.ImportREP(rep, ImportOptions{
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

	svc := newTestService(t, store)
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("ImportREP should not fail wholesale on a single post error: %v", err)
	}
	if res.Posted != 0 || res.Skipped != 1 {
		t.Errorf("want posted=0 skipped=1, got %+v", res)
	}
}

func TestImportREP_BadPacketErrors(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)
	_, err := svc.ImportREP([]byte("not a zip"), ImportOptions{Handle: "tester"})
	if err == nil {
		t.Fatal("expected error for a non-zip REP packet")
	}
}

func TestImportREP_PrivateConferenceRoutesToPrivate(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 3, Tag: "PRIVMAIL", Name: "Private Mail"})

	// Conference 0 is the private-mail conference after Sync.
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 0, Number: 1, From: "tester", To: "friend", Subject: "Hey", DateTime: time.Now(), Body: "private reply"},
	})

	svc := newTestService(t, store)
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("ImportREP: %v", err)
	}
	if res.Posted != 1 {
		t.Errorf("want posted=1, got %+v", res)
	}
	if len(store.privPosted) != 1 {
		t.Fatalf("want 1 private post, got %d", len(store.privPosted))
	}
	if len(store.posted) != 0 {
		t.Errorf("private reply must not go through the public path, got %d public posts", len(store.posted))
	}
	if store.privPosted[0].to != "friend" {
		t.Errorf("private post To: want 'friend', got %q", store.privPosted[0].to)
	}
}

func TestImportREP_PublicConferenceRoutesToPublic(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 5, Tag: "GENERAL", Name: "General"})

	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 5, Number: 1, To: "All", Subject: "Hi", DateTime: time.Now(), Body: "public reply"},
	})

	svc := newTestService(t, store)
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("ImportREP: %v", err)
	}
	if res.Posted != 1 || len(store.posted) != 1 || len(store.privPosted) != 0 {
		t.Errorf("public reply should use AddMessage: res=%+v public=%d priv=%d", res, len(store.posted), len(store.privPosted))
	}
}

func TestImportREP_UnmappedNumberFallsBackToPublic(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 5, Tag: "GENERAL", Name: "General"})

	// Pre-seed a map that assigns GENERAL a number (9) different from its ID (5),
	// so a REP using the old area.ID (5) misses the map and exercises the
	// GetAreaByID fallback.
	dir := t.TempDir()
	preMap := `[{"qwk_number":9,"area_tag":"GENERAL","kind":"public"}]` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "qwk_conferences.json"), []byte(preMap), 0o644); err != nil {
		t.Fatal(err)
	}

	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 5, Number: 1, To: "All", Subject: "Hi", DateTime: time.Now(), Body: "legacy-numbered reply"},
	})

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp", dir)
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("ImportREP: %v", err)
	}
	if res.Posted != 1 || len(store.posted) != 1 {
		t.Errorf("unmapped number should fall back to GetAreaByID and post public: res=%+v public=%d", res, len(store.posted))
	}
}

func TestImportREP_MappedButStaleAreaSkipped(t *testing.T) {
	// An area exists with ID 7, but the map maps conference 7 to a different,
	// now-deleted tag ("GONE"). A reply to conference 7 must be skipped, NOT
	// posted into the ID-7 area via the legacy fallback.
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 7, Tag: "OTHER", Name: "Other"})

	dir := t.TempDir()
	preMap := `[{"qwk_number":7,"area_tag":"GONE","kind":"public"}]` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "qwk_conferences.json"), []byte(preMap), 0o644); err != nil {
		t.Fatal(err)
	}

	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 7, Number: 1, To: "All", Subject: "Hi", DateTime: time.Now(), Body: "stale-target reply"},
	})

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp", dir)
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("ImportREP: %v", err)
	}
	if res.Posted != 0 || res.Skipped != 1 {
		t.Errorf("mapped-but-stale conference should be skipped, not posted: res=%+v", res)
	}
	if len(store.posted) != 0 || len(store.privPosted) != 0 {
		t.Errorf("nothing should be posted for a stale mapped conference: public=%d priv=%d", len(store.posted), len(store.privPosted))
	}
}

func TestImportREP_WrongBBSRejected(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	// REP built for a different BBS ID; the service's bbsID is VISION3.
	rep := makeREP(t, "OTHERBBS", []qwk.PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "x"},
	})

	svc := newTestService(t, store)
	_, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if !errors.Is(err, ErrWrongBBS) {
		t.Fatalf("want ErrWrongBBS, got %v", err)
	}
	if len(store.posted) != 0 {
		t.Error("nothing should be posted for a wrong-BBS packet")
	}
}

func TestImportREP_EmptyFirstBlockAccepted(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})

	// Build a REP whose first block is blank, but whose .MSG filename matches.
	first := make([]byte, 128)
	for i := range first {
		first[i] = ' '
	}
	rep := buildBlankFirstBlockREP(t, "VISION3", first)

	svc := newTestService(t, store)
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("blank first block should be accepted: %v", err)
	}
	if res.Posted != 1 {
		t.Errorf("want posted=1, got %+v", res)
	}
}

func TestImportREP_DuplicateNotReposted(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "x"},
	})

	svc := newTestService(t, store)
	first, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Posted != 1 {
		t.Fatalf("first import: want posted=1, got %+v", first)
	}

	second, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Duplicate != 1 || second.Posted != 0 {
		t.Errorf("second import: want duplicate=1 posted=0, got %+v", second)
	}
	if len(store.posted) != 1 {
		t.Errorf("duplicate upload must not double-post: posts=%d", len(store.posted))
	}
}

func TestImportREP_DuplicatePerHandle(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "x"},
	})

	svc := newTestService(t, store)
	if _, err := svc.ImportREP(rep, ImportOptions{Handle: "alice"}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Posted != 1 || res.Duplicate != 0 {
		t.Errorf("a different handle must not be a duplicate: %+v", res)
	}
}

// buildBlankFirstBlockREP builds a REP zip whose <bbsID>.MSG has the given first
// block followed by one message, bypassing WriteREP (which writes the BBS ID).
func buildBlankFirstBlockREP(t *testing.T, bbsID string, firstBlock []byte) []byte {
	t.Helper()
	var msgBuf bytes.Buffer
	msgBuf.Write(firstBlock)
	// One minimal message block: set block-count=1 at 116-121, conference=1.
	blk := make([]byte, 128)
	for i := range blk {
		blk[i] = ' '
	}
	copy(blk[116:122], []byte("     1"))
	blk[123] = 1 // conference number low byte = 1
	blk[124] = 0 // conference number high byte = 0 (space would be 0x20, giving wrong number)
	msgBuf.Write(blk)

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	w, err := zw.Create(bbsID + ".MSG")
	if err != nil {
		t.Fatal(err)
	}
	w.Write(msgBuf.Bytes())
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return zipBuf.Bytes()
}
