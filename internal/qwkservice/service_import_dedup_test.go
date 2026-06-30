package qwkservice

import (
	"archive/zip"
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)

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
