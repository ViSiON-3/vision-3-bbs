package main

import (
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func regWithNode(start time.Time) (*session.SessionRegistry, *session.BbsSession) {
	reg := session.NewSessionRegistry()
	bs := &session.BbsSession{NodeID: 4, StartTime: start, Width: 80, Height: 25, Tap: snoop.NewTap(),
		User: &user.User{Handle: "caller"}}
	reg.Register(bs)
	return reg, bs
}

func TestSnoopTargetRefusesReusedNode(t *testing.T) {
	start := time.Unix(100, 0)
	reg, _ := regWithNode(start)
	_, _, err := snoopTarget(reg)(admin.SnoopRequest{NodeID: 4, ConnectedAt: start.Add(time.Second)})
	if err == nil || !strings.Contains(err.Error(), "different caller") {
		t.Fatalf("err = %v", err)
	}
}

func TestSnoopTargetHeader(t *testing.T) {
	start := time.Unix(100, 0)
	reg, bs := regWithNode(start)
	bs.Tap.SetCP437(true)
	tap, hdr, err := snoopTarget(reg)(admin.SnoopRequest{NodeID: 4, ConnectedAt: start})
	if err != nil || tap != bs.Tap {
		t.Fatalf("tap %p err %v", tap, err)
	}
	if hdr.OutputMode != "cp437" || hdr.Width != 80 || hdr.Handle != "caller" {
		t.Fatalf("header %+v", hdr)
	}
}

func TestTypeInHookTakesAndReleases(t *testing.T) {
	start := time.Unix(100, 0)
	reg, bs := regWithNode(start)
	bs.Tap.AttachAs("jim")
	hook := typeInHook(reg)
	if err := hook("jim", 4, start, true); err != nil {
		t.Fatal(err)
	}
	if bs.Tap.KeyboardHolder() != "jim" {
		t.Fatal("not holding")
	}
	if err := hook("jim", 4, start, false); err != nil {
		t.Fatal(err)
	}
	if bs.Tap.KeyboardHolder() != "" {
		t.Fatal("still holding")
	}
}

func TestChatHookRefusedInDoor(t *testing.T) {
	start := time.Unix(100, 0)
	reg, bs := regWithNode(start)
	bs.Tap.AttachAs("jim")
	bs.Tap.SetMode(snoop.ModeDoor)
	err := chatHook(reg)("jim", 4, start, true)
	if err == nil || !strings.Contains(err.Error(), "door") {
		t.Fatalf("err = %v", err)
	}
}
