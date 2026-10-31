package menu

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
	"github.com/ViSiON-3/vision-3-bbs/internal/toast"
)

// A replay that has overflowed cannot restore the screen, so with no redraw of
// its own the toast waits.
func TestToastHeldWhenReplayOverflowed(t *testing.T) {
	env := newToastEnv(t)
	flood := []byte(strings.Repeat("x", snoop.CatchupLimit+1))
	_, _ = env.s.Write(flood)
	keys := env.readKey()

	env.sess.AddPage("Held")
	time.Sleep(50 * time.Millisecond)
	if strings.Contains(env.term.Snapshot(), "Held") {
		t.Errorf("toast drawn with nothing to restore the screen from")
	}
	if _, ok := env.sess.PeekPage(); !ok {
		t.Error("toast dropped")
	}
	env.s.Send("x")
	<-keys
}

// The editor redraws from its buffer, so a long session can still show a toast
// and typed text survives it.
func TestToastOverEditorPastReplayLimitKeepsTypedText(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = 10 * time.Second
	ih := getSessionIH(env.s)
	ed := editor.NewFSEditor(env.s, env.s, 0, 80, 24, "", "", "", "", "", "", ih)
	type result struct {
		content string
		err     error
	}
	done := make(chan result, 1)
	go func() {
		content, _, err := ed.Run()
		done <- result{content, err}
	}()

	env.s.Send("typed so far")
	toastWaitFor(t, "the typed text", func() bool { return strings.Contains(env.term.Snapshot(), "typed so far") })
	_, _ = env.s.Write([]byte(strings.Repeat("x", snoop.CatchupLimit+1)))
	if _, overflowed := env.s.tap.Snapshot(); !overflowed {
		t.Fatal("catch-up buffer did not overflow")
	}

	env.sess.AddPage("Over the editor")
	toastWaitFor(t, "the overlay", func() bool { return strings.Contains(env.term.Snapshot(), ">> Over the editor") })

	env.s.Send(" and more\x1a")
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.content != "typed so far and more" {
		t.Errorf("content = %q", r.content)
	}
	if _, ok := env.sess.PeekPage(); ok {
		t.Error("toast still queued")
	}
}

// The redraw belongs to the editor's own wait. Saving an empty message reads a
// key under the editor's error text, a screen the redraw would wipe, so past the
// replay limit a toast waits there.
func TestEditorPromptDoesNotUseTheEditorRedraw(t *testing.T) {
	env := newToastEnv(t)
	toastDwell = 10 * time.Second
	ih := getSessionIH(env.s)
	ed := editor.NewFSEditor(env.s, env.s, 0, 80, 24, "", "", "", "", "", "", ih)
	done := make(chan string, 1)
	go func() {
		content, _, _ := ed.Run()
		done <- content
	}()
	before := func() string { return env.term.Snapshot() }
	toastWaitFor(t, "the editor", func() bool { return before() != "" })
	env.s.Send("\x1a") // nothing to save
	time.Sleep(50 * time.Millisecond)
	_, _ = env.s.Write([]byte(strings.Repeat("x", snoop.CatchupLimit+1)))

	env.sess.AddPage("Held at prompt")
	time.Sleep(50 * time.Millisecond)
	if strings.Contains(env.term.Snapshot(), "Held at prompt") {
		t.Error("toast drawn over the prompt with nothing to restore it from")
	}

	env.s.Send(" ") // any key leaves the error text
	// Back in the editor's own wait, the held toast needs no new wake.
	toastWaitFor(t, "the held toast", func() bool { return strings.Contains(env.term.Snapshot(), ">> Held at prompt") })
	env.s.Send("hi\x1a")
	if got := <-done; got != "hi" {
		t.Errorf("content = %q", got)
	}
	if ih.ScreenRedraw() != nil {
		t.Error("redraw left set after the editor ended")
	}
}

func startToastChat(t *testing.T) (*menuEnv, *chatLive, *snoop.Tap) {
	t.Helper()
	env := newMenuEnv(t)
	sess := chatcovNode(env, 1, env.caller)
	sess.SetToastsReady(true)
	tap := snoop.NewTap()
	t.Cleanup(tap.Close)
	live := startChatLiveWith(env, env.caller, func(s *chatLiveSession) {
		s.tap = tap
		env.e.AttachToasts(s, sess)
		t.Cleanup(func() { DetachToasts(s) })
	})
	live.waitHeader("[Users·001]")
	return env, live, tap
}

func TestTeleconferenceShowsToastAsARoomLine(t *testing.T) {
	env, live, _ := startToastChat(t)
	live.typeKeys("half")
	toastWaitFor(t, "the echo", func() bool { return strings.Contains(live.s.output(), "half") })

	store := env.e.toastStore()
	if err := env.e.SessionRegistry.SendToast(store, env.caller.ID, toast.Toast{Text: "Paged you"}); err != nil {
		t.Fatal(err)
	}
	toastWaitFor(t, "the room line", func() bool { return strings.Contains(live.s.output(), "Paged you") })
	toastWaitFor(t, "the queue to empty", func() bool {
		q, err := store.Peek(env.caller.ID)
		return err == nil && len(q) == 0
	})

	// What was being typed is still there to finish.
	live.typeKeys(" done\r")
	toastWaitFor(t, "the finished line", func() bool { return strings.Contains(live.s.output(), "half done") })
	live.typeKeys("/q\r")
}

func TestTeleconferenceHoldsToastWhilePagedBack(t *testing.T) {
	env, live, _ := startToastChat(t)
	for range 30 {
		live.typeKeys("filler\r")
	}
	live.typeKeys("\x1b[5~")
	hint := string(ansi.ReplacePipeCodes([]byte(env.e.Strings().ChatScrollbackHint)))
	toastWaitFor(t, "the paged view", func() bool { return strings.Contains(live.s.output(), hint) })

	store := env.e.toastStore()
	if err := env.e.SessionRegistry.SendToast(store, env.caller.ID, toast.Toast{Text: "Wait for me"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if strings.Contains(live.s.output(), "Wait for me") {
		t.Error("toast written while paged back")
	}
	if q, err := store.Peek(env.caller.ID); err != nil || len(q) != 1 {
		t.Errorf("queue = %+v, %v, want the toast kept", q, err)
	}

	live.typeKeys("\r") // a blank line returns to the live view
	toastWaitFor(t, "the room line", func() bool { return strings.Contains(live.s.output(), "Wait for me") })
	live.typeKeys("/q\r")
}

func TestTeleconferenceToastCannotRunScreenCodes(t *testing.T) {
	env, live, _ := startToastChat(t)
	mark := len(live.s.output())
	store := env.e.toastStore()
	if err := env.e.SessionRegistry.SendToast(store, env.caller.ID, toast.Toast{Text: "a|CLb|PPc"}); err != nil {
		t.Fatal(err)
	}
	toastWaitFor(t, "the room line", func() bool { return strings.Contains(live.s.output()[mark:], "a|CLb|PPc") })

	// The scrollback replays the line through the same expander.
	for range 30 {
		live.typeKeys("filler\r")
	}
	mark = len(live.s.output())
	live.typeKeys("\x1b[5~")
	toastWaitFor(t, "the line replayed", func() bool { return strings.Contains(live.s.output()[mark:], "a|CLb|PPc") })
	if strings.Contains(live.s.output()[mark:], "\x1b[2J") {
		t.Error("replaying the toast cleared the screen")
	}
	live.typeKeys("\r/q\r")
}

// A page taller than the room shows its newest lines and keeps the rest in the
// scrollback, instead of writing over the header.
func TestTeleconferenceLongToastStaysInTheRoom(t *testing.T) {
	env, live, _ := startToastChat(t)
	mark := len(live.s.output())
	text := "FIRSTWORD " + strings.Repeat("word ", 800) + "LASTWORD"
	if err := env.e.SessionRegistry.SendToast(env.e.toastStore(), env.caller.ID, toast.Toast{Text: text}); err != nil {
		t.Fatal(err)
	}
	toastWaitFor(t, "the end of the toast", func() bool { return strings.Contains(live.s.output()[mark:], "LASTWORD") })
	if strings.Contains(live.s.output()[mark:], "FIRSTWORD") {
		t.Error("lines above the room were written to the screen")
	}
	for range 5 {
		live.typeKeys("\x1b[5~")
	}
	toastWaitFor(t, "the start in the scrollback", func() bool { return strings.Contains(live.s.output()[mark:], "FIRSTWORD") })
	live.typeKeys("\r/q\r")
}

// A failed write leaves the toast queued, unclaimed, for the next login.
func TestToastLineWriteFailureKeepsTheToast(t *testing.T) {
	env := newToastEnv(t)
	env.s.tap.SetMode(snoop.ModeTeleconf)
	store := env.e.toastStore()
	if err := store.Enqueue(env.u.ID, toast.Toast{ID: "a", Text: "keep me"}); err != nil {
		t.Fatal(err)
	}
	defer setToastLineScreen(env.s, nil, toastLineScreen{
		ready: func() bool { return true },
		write: func(string) error { return errors.New("gone") },
	})()
	env.e.serviceToasts(env.s, getSessionIH(env.s), env.sess, 0)
	if q := mustPeek(t, store, env.u.ID); len(q) != 1 {
		t.Fatalf("queue = %+v, want the toast kept", q)
	}
	if !store.Claim(env.u.ID, "a") {
		t.Error("claim left held after the failed write")
	}
	store.Release("a")
}

// Teleconference's writer reports a dropped connection to the toast path.
func TestTeleconferenceWriteFailureKeepsTheToast(t *testing.T) {
	env, live, _ := startToastChat(t)
	store := env.e.toastStore()
	live.s.failWrites.Store(true)
	if err := env.e.SessionRegistry.SendToast(store, env.caller.ID, toast.Toast{Text: "keep me"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if q := mustPeek(t, store, env.caller.ID); len(q) != 1 {
		t.Errorf("queue = %+v, want the toast kept", q)
	}
	live.s.failWrites.Store(false)
	env.e.nodeSession(1).WakeToasts()
	toastWaitFor(t, "the room line", func() bool { return strings.Contains(live.s.output(), "keep me") })
	live.typeKeys("/q\r")
}
