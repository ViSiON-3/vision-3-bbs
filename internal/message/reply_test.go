package message

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newReplyTestManager(t *testing.T) *MessageManager {
	t.Helper()
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	areas := `[{"id":1,"tag":"GENERAL","name":"General","base_path":"general","area_type":"local"},
	           {"id":2,"tag":"PRIVMAIL","name":"Private Mail","base_path":"privmail","area_type":"local"}]`
	if err := os.WriteFile(filepath.Join(cfg, "message_areas.json"), []byte(areas), 0o644); err != nil {
		t.Fatal(err)
	}
	mm, err := NewMessageManager(tmp, cfg, "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	return mm
}

// Characterization: lock the existing Add* behavior before consolidating.
func TestAddFamily_Characterization(t *testing.T) {
	mm := newReplyTestManager(t)

	posted := 0
	mm.OnMessagePosted = func(area *MessageArea, msgNum int, from, to, subject, body string) { posted++ }

	if _, err := mm.AddMessage(1, "a", "All", "s", "b", ""); err != nil {
		t.Fatal(err)
	}
	if posted != 1 {
		t.Errorf("AddMessage should fire OnMessagePosted once, got %d", posted)
	}

	pn, err := mm.AddPrivateMessage(2, "a", "b", "s", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	if posted != 1 {
		t.Errorf("AddPrivateMessage must NOT fire OnMessagePosted; count=%d", posted)
	}
	pm, err := mm.GetMessage(2, pn)
	if err != nil {
		t.Fatal(err)
	}
	if !pm.IsPrivate {
		t.Error("AddPrivateMessage should set IsPrivate")
	}

	when := time.Date(2020, 1, 2, 3, 4, 0, 0, time.UTC)
	dn, err := mm.AddMessageWithDate(1, "a", "All", "s3", "b3", "", when)
	if err != nil {
		t.Fatal(err)
	}
	dmsg, err := mm.GetMessage(1, dn)
	if err != nil {
		t.Fatal(err)
	}
	if !dmsg.DateTime.Equal(when) {
		t.Errorf("AddMessageWithDate: want %v, got %v", when, dmsg.DateTime)
	}
}

func TestAddPrivateMessage_SkipsBodyTransform(t *testing.T) {
	mm := newReplyTestManager(t)
	mm.BodyTransform = func(areaID int, body string) string { return body + "\n[TRANSFORMED]" }

	pubNum, err := mm.AddMessage(1, "a", "All", "s", "public body", "")
	if err != nil {
		t.Fatal(err)
	}
	pub, err := mm.GetMessage(1, pubNum)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pub.Body, "[TRANSFORMED]") {
		t.Errorf("AddMessage should apply BodyTransform; body=%q", pub.Body)
	}

	privNum, err := mm.AddPrivateMessage(2, "a", "b", "s", "private body", "")
	if err != nil {
		t.Fatal(err)
	}
	priv, err := mm.GetMessage(2, privNum)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(priv.Body, "[TRANSFORMED]") {
		t.Errorf("AddPrivateMessage must NOT apply BodyTransform; body=%q", priv.Body)
	}
}

func TestAddReply_SetsReplyTo(t *testing.T) {
	mm := newReplyTestManager(t)

	parent, err := mm.AddMessage(1, "alice", "All", "Topic", "first", "")
	if err != nil {
		t.Fatal(err)
	}
	reply, err := mm.AddReply(1, "bob", "alice", "Re: Topic", "second", "", parent)
	if err != nil {
		t.Fatal(err)
	}
	dm, err := mm.GetMessage(1, reply)
	if err != nil {
		t.Fatal(err)
	}
	if dm.ReplyToNum != parent {
		t.Errorf("ReplyToNum: want %d, got %d", parent, dm.ReplyToNum)
	}
}

func TestAddReply_AlsoSetsReplyID(t *testing.T) {
	mm := newReplyTestManager(t)
	parent, err := mm.AddMessage(1, "alice", "All", "Topic", "first", "")
	if err != nil {
		t.Fatal(err)
	}
	reply, err := mm.AddReply(1, "bob", "alice", "Re: Topic", "second", "PARENTMSGID", parent)
	if err != nil {
		t.Fatal(err)
	}
	dm, err := mm.GetMessage(1, reply)
	if err != nil {
		t.Fatal(err)
	}
	if dm.ReplyID != "PARENTMSGID" {
		t.Errorf("ReplyID: want PARENTMSGID, got %q", dm.ReplyID)
	}
	if dm.ReplyToNum != parent {
		t.Errorf("ReplyToNum: want %d, got %d", parent, dm.ReplyToNum)
	}
}

func TestAddPrivateReply_StaysPrivateAndThreads(t *testing.T) {
	mm := newReplyTestManager(t) // area 2 is PRIVMAIL
	parent, err := mm.AddPrivateMessage(2, "alice", "bob", "Topic", "first", "")
	if err != nil {
		t.Fatal(err)
	}
	reply, err := mm.AddPrivateReply(2, "bob", "alice", "Re: Topic", "second", "", parent)
	if err != nil {
		t.Fatal(err)
	}
	dm, err := mm.GetMessage(2, reply)
	if err != nil {
		t.Fatal(err)
	}
	if !dm.IsPrivate {
		t.Error("AddPrivateReply should keep the reply private")
	}
	if dm.ReplyToNum != parent {
		t.Errorf("ReplyToNum: want %d, got %d", parent, dm.ReplyToNum)
	}
}
