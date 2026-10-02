package main

import (
	"errors"
	"strings"
	"sync/atomic"
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

func TestSnoopTargetHeaderUsesLiveSize(t *testing.T) {
	start := time.Unix(100, 0)
	reg, bs := regWithNode(start)
	w, h := 80, 25
	bs.Size = func() (int, int) { return w, h }
	w, h = 132, 50 // the caller resized after login
	_, hdr, err := snoopTarget(reg)(admin.SnoopRequest{NodeID: 4, ConnectedAt: start})
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Width != 132 || hdr.Height != 50 {
		t.Fatalf("header size %dx%d, want 132x50", hdr.Width, hdr.Height)
	}
}

func TestLiveTermSizeUsesPhysicalWidth(t *testing.T) {
	var phys, termW, termH atomic.Int32
	phys.Store(132)
	termW.Store(80) // the user's saved width preference
	termH.Store(50)
	if w, h := liveTermSize(&phys, &termH)(); w != 132 || h != 50 {
		t.Fatalf("size %dx%d, want 132x50", w, h)
	}
	phys.Store(100) // window resize
	if w, _ := liveTermSize(&phys, &termH)(); w != 100 {
		t.Fatalf("width %d after resize, want 100", w)
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
	_, err := chatHook(reg)("jim", 4, start, true)
	if err == nil || !strings.Contains(err.Error(), "door") {
		t.Fatalf("err = %v", err)
	}
}

func TestTypeInHookOffWithoutHoldIsRefused(t *testing.T) {
	start := time.Unix(100, 0)
	reg, _ := regWithNode(start)
	if err := typeInHook(reg)("jim", 4, start, false); !errors.Is(err, snoop.ErrNotHolder) {
		t.Fatalf("err = %v, want ErrNotHolder", err)
	}
}

func TestChatCreditHook(t *testing.T) {
	reg := session.NewSessionRegistry()
	reg.Register(&session.BbsSession{NodeID: 1})
	reg.Register(&session.BbsSession{NodeID: 2, ChatCredit: func() time.Duration { return 7 * time.Minute }})
	hook := chatCreditHook(reg)

	if got := hook(9); got != 0 {
		t.Errorf("missing node = %v, want 0", got)
	}
	if got := hook(1); got != 0 {
		t.Errorf("nil credit func = %v, want 0", got)
	}
	if got := hook(2); got != 7*time.Minute {
		t.Errorf("credit = %v, want 7m", got)
	}
}
