//go:build !windows

package menu

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/safego"
	"github.com/ViSiON-3/vision-3-bbs/internal/safego/safegotest"
	"github.com/ViSiON-3/vision-3-bbs/internal/toast"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Each test below makes one kind of goroutine a session starts panic, checks
// the panic is logged with its stack and that only that session is ended or
// degraded, then shows a second session served.

func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(handlerWait):
		t.Fatal(what)
	}
}

func TestTeleconferencePanicEndsOnlyItsChat(t *testing.T) {
	logs := safegotest.Capture(t)
	env := newMenuEnv(t)
	safegotest.Inject(t, safego.KindChat)

	first := startChatLive(env, env.caller)
	logs.WaitPanic(t, safego.KindChat, "menu/chat.go")
	// The caller can still leave; the keys go in on the side, as the chat
	// may already have ended.
	go func() { _, _ = first.keys.Write([]byte("/q\r")) }()
	waitClosed(t, first.done, "the first chat did not end after its event pump panicked")

	second := startChatLive(env, env.caller)
	second.typeKeys("hello there\r")
	second.waitOutput("hello there")
	second.typeKeys("/q\r")
	waitClosed(t, second.done, "the second chat did not end on /q")
}

func TestSysopChatPanicEndsOnlyItsChat(t *testing.T) {
	logs := safegotest.Capture(t)
	safegotest.Inject(t, safego.KindSysopChat)

	r := newChatRig(t, "")
	r.begin(t)
	done := r.run()
	logs.WaitPanic(t, safego.KindSysopChat, "menu/sysop_chat.go")
	waitDone(t, done, "the first chat did not end after its event forwarder panicked")

	r2 := newChatRig(t, "")
	r2.begin(t)
	done2 := r2.run()
	waitFor(t, func() bool { return strings.Contains(r2.term.Row(13), "chatting with") }, "the second chat screen was not drawn")
	_ = r2.tap.StopChat("SysOp")
	waitDone(t, done2, "the second chat ignored StopChat")
}

func TestLoginStepPanicEndsOnlyItsScript(t *testing.T) {
	logs := safegotest.Capture(t)
	env := newMenuEnv(t)
	script := doorcovScript(t, `echo READY; read line; echo "GOT=[$line]"`)
	safegotest.Inject(t, safego.KindLoginStep)

	// The script is stopped and the login goes on.
	r := doorcovRun(env, newDoorcovSession(), runLoginDoor, env.caller, script)
	logs.WaitPanic(t, safego.KindLoginStep, "executor_login_steps.go")
	if r.err != nil || r.user != env.caller || r.has("GOT=[hello]") {
		t.Fatalf("first login: err=%v output:\n%s", r.err, r.text())
	}

	s := newDoorcovSession()
	s.whenOutput("READY", func() { s.send("hello\n") })
	if r := doorcovRun(env, s, runLoginDoor, env.caller, script); r.err != nil || !r.has("GOT=[hello]") {
		t.Fatalf("second login: err=%v output:\n%s", r.err, r.text())
	}

}

func TestMailPollPanicDegradesOnlyItsPoll(t *testing.T) {
	logs := safegotest.Capture(t)
	env := newMenuEnv(t)
	if err := os.WriteFile(filepath.Join(env.root, "v3mail"), []byte("#!/bin/sh\necho POLLED\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	poll := func() string {
		t.Helper()
		ds := newDoorcovSession()
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		s := hookSession{doorcovSession: ds, ctx: &hookCtx{Context: ctx}}
		// Enter for the pause prompt once the stop-key watcher is done.
		ds.whenOutput("Poll complete", func() { ds.send("\r") })
		SetSessionOutputMode(s, env.outputMode)
		t.Cleanup(func() {
			resetSessionIH(s)
			ClearSessionOutputMode(s)
		})
		c := &cmdCtx{e: env.e, s: s, terminal: newDoorcovTerminal(ds), userManager: env.um,
			currentUser: env.sysop, nodeNumber: 1, sessionStartTime: time.Now(),
			outputMode: env.outputMode, termWidth: 80, termHeight: 24}
		done := make(chan error, 1)
		go func() {
			_, _, err := runMailPoll(c, "")
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("runMailPoll: %v", err)
			}
		case <-time.After(handlerWait):
			t.Fatalf("the poll did not finish; output:\n%s", ds.output())
		}
		return ds.output()
	}
	safegotest.Inject(t, safego.KindMailPoll)

	// Without its stop-key watcher the poll still runs to the end.
	if out := poll(); !strings.Contains(out, "POLLED") || !strings.Contains(out, "Poll complete") {
		t.Errorf("first poll:\n%s", out)
	}
	logs.WaitPanic(t, safego.KindMailPoll, "executor_admin_mailpoll.go")
	if out := poll(); !strings.Contains(out, "POLLED") || !strings.Contains(out, "Poll complete") {
		t.Errorf("second poll:\n%s", out)
	}
}

func TestV3NetOnlineRefreshPanicDegradesOnlyThatRefresh(t *testing.T) {
	logs := safegotest.Capture(t)
	presence := func(node string, handles ...string) protocol.CentralActivityBoard {
		var es []protocol.CentralActivityEntry
		for i, h := range handles {
			es = append(es, protocol.CentralActivityEntry{Handle: h, Session: node + string(rune('a'+i))})
		}
		return activityBoardOf(node, node, protocol.CentralKindPresence, es...)
	}
	fake := &centralFake{
		node: "self",
		nets: []CentralNetworkInfo{activityNet("one")},
		acts: map[string]map[string][]protocol.CentralActivityBoard{
			"one": {protocol.CentralKindPresence: {presence("beta", "Alice")}},
		},
	}
	e := &MenuExecutor{V3NetCentral: fake}
	if n := e.v3netOnlineCount(); n != 1 {
		t.Fatalf("first count %d, want 1", n)
	}
	v, _ := netOnlineCounts.Load(e)
	c := v.(*netOnlineCache)
	stale := func() {
		c.mu.Lock()
		c.at = time.Now().Add(-2 * netOnlineTTL)
		c.mu.Unlock()
	}
	refreshing := func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.refreshing
	}

	safegotest.Inject(t, safego.KindV3NetOnline)
	stale()
	if n := e.v3netOnlineCount(); n != 1 {
		t.Errorf("stale count %d, want the last one, 1", n)
	}
	logs.WaitPanic(t, safego.KindV3NetOnline, "v3net_central_online.go")
	waitFor(t, func() bool { return !refreshing() }, "the panicked refresh left the cache refreshing")

	// The next caller's screen refreshes it.
	fake.acts["one"][protocol.CentralKindPresence] = []protocol.CentralActivityBoard{presence("beta", "Alice", "Bob")}
	stale()
	e.v3netOnlineCount()
	waitFor(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.count == 2 && !c.refreshing
	}, "the next refresh did not count 2")
}

func TestNoticeWatcherPanicDegradesOnlyItsMenu(t *testing.T) {
	logs := safegotest.Capture(t)
	safegotest.Inject(t, safego.KindNotice)
	env := newNoticeEnv(t)
	done := env.run(t)
	logs.WaitPanic(t, safego.KindNotice, "menu_script_notice.go")
	// Without its watcher the menu goes on answering.
	env.finish(t, done)

	env2 := newNoticeEnv(t)
	done2 := env2.run(t)
	if err := env2.e.SessionRegistry.SendToast(env2.e.toastStore(), env2.u.ID, toast.Toast{Text: "Hello there"}); err != nil {
		t.Fatal(err)
	}
	toastWaitFor(t, "the second menu's toast", func() bool { return strings.Contains(env2.out(), ">> Hello there") })
	env2.finish(t, done2)
}

// watched runs a native door as executeDoor does, under a door watch.
func watched(c *DoorCtx) error { return runDoorWatched(c, executeNativeDoor) }

func TestDoorIOPanicEndsOnlyItsDoor(t *testing.T) {
	logs := safegotest.Capture(t)
	env := newMenuEnv(t)
	safegotest.Inject(t, safego.KindDoorIO)

	s := newDoorcovSession() // input stays open
	ctx := doorcovCtx(env, s, config.DoorConfig{Commands: []string{"/bin/sh", "-c", "cat; echo LEFT"}})
	_ = doorcovExec(t, s, watched, ctx)
	if log := logs.WaitPanic(t, safego.KindDoorIO, "menu/door_stdio.go"); !strings.Contains(log, "node=3") {
		t.Errorf("panic not logged with the node:\n%s", log)
	}

	s2 := newDoorcovSession() // input stays open, as a caller's does
	s2.send("one\n")
	ctx2 := doorcovCtx(env, s2, config.DoorConfig{Commands: []string{"/bin/sh", "-c", `echo "GOT:$(head -n 1)"`}})
	if err := doorcovExec(t, s2, watched, ctx2); err != nil {
		t.Fatalf("second door: %v", err)
	}
	doorcovHas(t, s2, "GOT:one")
}

func TestDoorWatchPanicEndsOnlyItsDoor(t *testing.T) {
	logs := safegotest.Capture(t)
	env := newMenuEnv(t)
	safegotest.Inject(t, safego.KindDoorWatch)

	s := newDoorcovSession()
	ctx := doorcovCtx(env, s, config.DoorConfig{Commands: []string{"/bin/sh", "-c", "sleep 5; echo LATE"}})
	start := time.Now()
	_ = doorcovExec(t, s, watched, ctx)
	logs.WaitPanic(t, safego.KindDoorWatch, "menu/door_watch.go")
	if took := time.Since(start); took > 4*time.Second || strings.Contains(s.output(), "LATE") {
		t.Errorf("first door ran on for %v without its watch; output:\n%s", took, s.output())
	}

	s2 := newDoorcovSession()
	ctx2 := doorcovCtx(env, s2, config.DoorConfig{Commands: []string{"echo", "second"}})
	if err := doorcovExec(t, s2, watched, ctx2); err != nil {
		t.Fatalf("second door: %v", err)
	}
	doorcovHas(t, s2, "second")
}
