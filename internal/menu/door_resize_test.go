//go:build !windows

package menu

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/winch"
	"github.com/gliderlabs/ssh"
)

// With the session loop owning the window channel through winch, as it does
// on a live board, every window change during a raw-terminal door reaches the
// loop, and the door keeps the caller's saved size (#629).
func TestNativeDoorLeavesWindowChangesForTheSessionLoop(t *testing.T) {
	env := newMenuEnv(t)
	s := newDoorcovSession()
	s.isPty = true
	s.winCh = make(chan ssh.Window) // unbuffered, as gliderlabs' is
	hub, loop := winch.Own(s, s.winCh)
	defer hub.Close()

	waiting := make(chan struct{})
	s.whenOutput("RESIZE?", func() { close(waiting) })
	ctx := doorcovCtx(env, s, config.DoorConfig{
		Commands:            []string{"/bin/sh", "-c", `printf 'RESIZE?'; read a; echo "SIZE:$(stty size)"`},
		RequiresRawTerminal: true,
	})
	done := make(chan error, 1)
	go func() { done <- executeNativeDoor(ctx) }()

	select {
	case <-waiting:
	case <-time.After(handlerWait):
		t.Fatalf("door never started; output so far:\n%s", s.output())
	}
	for _, size := range [][2]int{{40, 10}, {100, 30}, {120, 40}} {
		select {
		case s.winCh <- ssh.Window{Width: size[0], Height: size[1]}:
		case <-time.After(5 * time.Second):
			t.Fatalf("the %dx%d change was not taken", size[0], size[1])
		}
		select {
		case w := <-loop:
			if w.Width != size[0] || w.Height != size[1] {
				t.Errorf("session loop got %dx%d, want %dx%d", w.Width, w.Height, size[0], size[1])
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("session loop never saw the %dx%d change made during the door", size[0], size[1])
		}
	}
	s.send("\n")

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("executeNativeDoor: %v", err)
		}
	case <-time.After(handlerWait):
		t.Fatalf("door still running after %v; output so far:\n%s", handlerWait, s.output())
	}
	doorcovHas(t, s, "SIZE:37 132")
}
