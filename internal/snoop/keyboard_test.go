package snoop

import (
	"errors"
	"testing"
	"time"
)

func TestInjectDroppedWithoutKeyboard(t *testing.T) {
	tp := NewTap()
	if n := tp.Inject("sysop", []byte("x")); n != 0 {
		t.Fatalf("accepted %d bytes without the keyboard", n)
	}
}

func TestOnlyOneHolder(t *testing.T) {
	tp := newWatchedTap("a", "b")
	if err := tp.TakeKeyboard("a"); err != nil {
		t.Fatal(err)
	}
	if err := tp.TakeKeyboard("b"); !errors.Is(err, ErrKeyboardHeld) {
		t.Fatalf("second take = %v; want ErrKeyboardHeld", err)
	}
	if n := tp.Inject("b", []byte("x")); n != 0 {
		t.Fatal("non-holder injected")
	}
	if n := tp.Inject("a", []byte("hi")); n != 2 {
		t.Fatalf("holder injected %d", n)
	}
	if got := <-tp.Input(); string(got) != "hi" {
		t.Fatalf("Input = %q", got)
	}
	tp.ReleaseKeyboard("a")
	if err := tp.TakeKeyboard("b"); err != nil {
		t.Fatalf("take after release: %v", err)
	}
}

func TestReleaseKeyboardReportsHoldAndBytes(t *testing.T) {
	tp := newWatchedTap("a", "b")
	if held, n := tp.ReleaseKeyboard("a"); held != 0 || n != 0 {
		t.Fatalf("release without holding = %v, %d; want 0, 0", held, n)
	}
	if err := tp.TakeKeyboard("a"); err != nil {
		t.Fatal(err)
	}
	tp.Inject("a", []byte("abc"))
	tp.Inject("a", []byte("de"))
	tp.Inject("b", []byte("zzz"))
	time.Sleep(10 * time.Millisecond)
	if held, n := tp.ReleaseKeyboard("b"); held != 0 || n != 0 {
		t.Fatalf("non-holder release = %v, %d; want 0, 0", held, n)
	}
	held, n := tp.ReleaseKeyboard("a")
	if held < 10*time.Millisecond || n != 5 {
		t.Fatalf("release = %v, %d; want >=10ms, 5", held, n)
	}
	if err := tp.TakeKeyboard("a"); err != nil {
		t.Fatal(err)
	}
	if held, n := tp.ReleaseKeyboard("a"); n != 0 || held >= 10*time.Millisecond {
		t.Fatalf("second hold = %v, %d; counters were not reset", held, n)
	}
}

func TestRequestChatRefusedInDoorAndTransfer(t *testing.T) {
	for _, m := range []Mode{ModeDoor, ModeTransfer} {
		tp := newWatchedTap("a", "b")
		tp.SetMode(m)
		err := tp.RequestChat("a", 50*time.Millisecond)
		if !errors.Is(err, ErrBusy) {
			t.Fatalf("mode %v: err = %v; want ErrBusy", m, err)
		}
	}
}

func TestRequestChatTimesOutWhenNobodyServicesBreakIn(t *testing.T) {
	tp := newWatchedTap("a", "b")
	err := tp.RequestChat("a", 50*time.Millisecond)
	if !errors.Is(err, ErrChatNotStarted) {
		t.Fatalf("err = %v; want ErrChatNotStarted", err)
	}
	select {
	case <-tp.BreakIn():
		t.Fatal("stale break-in left pending after timeout")
	default:
	}
	if tp.KeyboardHolder() != "" {
		t.Fatal("keyboard not released after failed chat")
	}
}

func TestChatHandshakeAndRouting(t *testing.T) {
	tp := newWatchedTap("a", "b")
	go func() {
		<-tp.BreakIn()
		tp.ChatBegan()
	}()
	if err := tp.RequestChat("a", time.Second); err != nil {
		t.Fatal(err)
	}
	if !tp.Chatting() {
		t.Fatal("not chatting")
	}
	tp.Inject("a", []byte("yo"))
	if got := <-tp.ChatInput(); string(got) != "yo" {
		t.Fatalf("ChatInput = %q", got)
	}
	if err := tp.StopChat("a"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-tp.EndChat():
	case <-time.After(time.Second):
		t.Fatal("EndChat not closed")
	}
	tp.ChatEnded()
	if tp.Chatting() {
		t.Fatal("still chatting")
	}
}

func TestReleaseKeyboardEndsChat(t *testing.T) {
	tp := newWatchedTap("a", "b")
	go func() { <-tp.BreakIn(); tp.ChatBegan() }()
	if err := tp.RequestChat("a", time.Second); err != nil {
		t.Fatal(err)
	}
	end := tp.EndChat()
	tp.ReleaseKeyboard("a")
	select {
	case <-end:
	case <-time.After(time.Second):
		t.Fatal("dropping the sysop must end chat")
	}
}

func TestLateChatBeganAfterTimeoutIsRefused(t *testing.T) {
	tp := newWatchedTap("a", "b")
	serviced := make(chan struct{})
	go func() { <-tp.BreakIn(); close(serviced) }()
	err := tp.RequestChat("a", 50*time.Millisecond)
	if !errors.Is(err, ErrChatNotStarted) {
		t.Fatalf("err = %v; want ErrChatNotStarted", err)
	}
	<-serviced
	if tp.ChatBegan() {
		t.Fatal("ChatBegan accepted a request that already timed out")
	}
	if tp.Chatting() {
		t.Fatal("chatting after a refused ChatBegan")
	}

	go func() { <-tp.BreakIn(); tp.ChatBegan() }()
	if err := tp.RequestChat("b", time.Second); err != nil {
		t.Fatalf("fresh request: %v", err)
	}
	if !tp.Chatting() || tp.KeyboardHolder() != "b" {
		t.Fatalf("chatting=%v holder=%q; want true, b", tp.Chatting(), tp.KeyboardHolder())
	}
}

func TestTimeoutClosesEndChat(t *testing.T) {
	tp := newWatchedTap("a", "b")
	done := make(chan error, 1)
	go func() { done <- tp.RequestChat("a", 50*time.Millisecond) }()
	<-tp.BreakIn()
	end := tp.EndChat()
	if err := <-done; !errors.Is(err, ErrChatNotStarted) {
		t.Fatalf("err = %v", err)
	}
	select {
	case <-end:
	default:
		t.Fatal("EndChat of the expired request not closed")
	}
}

func TestInputReadyFiresOnInject(t *testing.T) {
	tp := newWatchedTap("a", "b")
	_ = tp.TakeKeyboard("a")
	tp.Inject("a", []byte("x"))
	select {
	case <-tp.InputReady():
	default:
		t.Fatal("InputReady not signalled")
	}
}

// newWatchedTap returns a tap with one open watch per handle.
func newWatchedTap(handles ...string) *Tap {
	tp := NewTap()
	for _, h := range handles {
		tp.AttachAs(h)
	}
	return tp
}

func TestTakeKeyboardNeedsWatch(t *testing.T) {
	tp := NewTap()
	if err := tp.TakeKeyboard("a"); !errors.Is(err, ErrNotWatching) {
		t.Fatalf("TakeKeyboard err = %v, want ErrNotWatching", err)
	}
	if err := tp.RequestChat("a", 50*time.Millisecond); !errors.Is(err, ErrNotWatching) {
		t.Fatalf("RequestChat err = %v, want ErrNotWatching", err)
	}
	tp.Attach() // an anonymous watch does not count for a handle
	if err := tp.TakeKeyboard("a"); !errors.Is(err, ErrNotWatching) {
		t.Fatalf("TakeKeyboard with anonymous watch err = %v", err)
	}
}

func TestLastWatcherClosingReleasesKeyboardAndChat(t *testing.T) {
	tp := NewTap()
	w := tp.AttachAs("a")
	if err := tp.TakeKeyboard("a"); err != nil {
		t.Fatal(err)
	}
	end := tp.EndChat()
	w.Close()
	if h := tp.KeyboardHolder(); h != "" {
		t.Fatalf("holder = %q after last watcher closed", h)
	}
	select {
	case <-end:
	default:
		t.Fatal("EndChat not closed")
	}
}

func TestOtherWatcherClosingKeepsKeyboard(t *testing.T) {
	tp := NewTap()
	w1, w2 := tp.AttachAs("a"), tp.AttachAs("a")
	other := tp.AttachAs("b")
	if err := tp.TakeKeyboard("a"); err != nil {
		t.Fatal(err)
	}
	w1.Close()
	other.Close()
	if h := tp.KeyboardHolder(); h != "a" {
		t.Fatalf("holder = %q, want a", h)
	}
	w2.Close()
	if h := tp.KeyboardHolder(); h != "" {
		t.Fatalf("holder = %q after last watcher closed", h)
	}
}

func TestTapCloseDropsKeyboardAndEndsChat(t *testing.T) {
	tp := NewTap()
	tp.AttachAs("a")
	if err := tp.TakeKeyboard("a"); err != nil {
		t.Fatal(err)
	}
	end := tp.EndChat()
	tp.Close()
	if h := tp.KeyboardHolder(); h != "" {
		t.Fatalf("holder = %q after Close", h)
	}
	select {
	case <-end:
	default:
		t.Fatal("EndChat not closed")
	}
}

func beginChat(t *testing.T, tp *Tap, handle string) {
	t.Helper()
	go func() { <-tp.BreakIn(); tp.ChatBegan() }()
	if err := tp.RequestChat(handle, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestChatEndedReleasesKeyboardChatTook(t *testing.T) {
	tp := newWatchedTap("a")
	beginChat(t, tp, "a")
	tp.ChatEnded()
	if h := tp.KeyboardHolder(); h != "" {
		t.Fatalf("holder after chat = %q; want none", h)
	}
	if n := tp.Inject("a", []byte("x")); n != 0 {
		t.Fatal("sysop keys became type-in after chat")
	}
}

func TestChatEndedByHolderKeepsTypeInHold(t *testing.T) {
	tp := newWatchedTap("a")
	if err := tp.TakeKeyboard("a"); err != nil {
		t.Fatal(err)
	}
	beginChat(t, tp, "a")
	if err := tp.StopChat("a"); err != nil {
		t.Fatal(err)
	}
	tp.ChatEnded()
	if h := tp.KeyboardHolder(); h != "a" {
		t.Fatalf("holder after chat = %q; want a", h)
	}
}

func TestChatEndedByCallerDropsTypeInHold(t *testing.T) {
	tp := newWatchedTap("a")
	if err := tp.TakeKeyboard("a"); err != nil {
		t.Fatal(err)
	}
	beginChat(t, tp, "a")
	tp.ChatEnded() // the caller pressed ESC ESC
	if h := tp.KeyboardHolder(); h != "" {
		t.Fatalf("holder after caller ended chat = %q; want none", h)
	}
	if n := tp.Inject("a", []byte("ok, bye\r")); n != 0 {
		t.Fatalf("Inject after caller ended chat = %d; want 0", n)
	}
	select {
	case b := <-tp.Input():
		t.Fatalf("type-in queued after caller ended chat: %q", b)
	default:
	}
}

func TestChatBeganDropsQueuedTypeIn(t *testing.T) {
	tp := newWatchedTap("a")
	if err := tp.TakeKeyboard("a"); err != nil {
		t.Fatal(err)
	}
	tp.Inject("a", []byte("queued"))
	beginChat(t, tp, "a")
	select {
	case b := <-tp.Input():
		t.Fatalf("type-in still queued in chat: %q", b)
	default:
	}
	select {
	case <-tp.InputReady():
		t.Fatal("ready token left after drain")
	default:
	}
}

func TestRequestChatRefusedInTeleconference(t *testing.T) {
	tp := newWatchedTap("a")
	tp.SetMode(ModeTeleconf)
	err := tp.RequestChat("a", 50*time.Millisecond)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v; want ErrBusy", err)
	}
	if want := "caller is busy: in a teleconference"; err.Error() != want {
		t.Fatalf("err = %q; want %q", err, want)
	}
	if err := tp.TakeKeyboard("a"); err != nil {
		t.Fatalf("type-in refused in teleconference: %v", err)
	}
}

func TestChatEndedLeavesLaterHolder(t *testing.T) {
	tp := newWatchedTap("a", "b")
	beginChat(t, tp, "a")
	tp.ReleaseKeyboard("a")
	if err := tp.TakeKeyboard("b"); err != nil {
		t.Fatal(err)
	}
	tp.ChatEnded()
	if h := tp.KeyboardHolder(); h != "b" {
		t.Fatalf("holder after chat = %q; want b", h)
	}
}

func TestChatBeganDropsStaleChatInput(t *testing.T) {
	tp := newWatchedTap("a")
	if err := tp.TakeKeyboard("a"); err != nil {
		t.Fatal(err)
	}
	beginChat(t, tp, "a")
	tp.Inject("a", []byte("stale"))
	tp.ChatEnded()
	beginChat(t, tp, "a")
	tp.Inject("a", []byte("fresh"))
	if got := <-tp.ChatInput(); string(got) != "fresh" {
		t.Fatalf("ChatInput = %q; want fresh", got)
	}
}

func TestChatEndedKeepsHoldRetakenForTypeIn(t *testing.T) {
	tp := newWatchedTap("a")
	beginChat(t, tp, "a")
	tp.ReleaseKeyboard("a")
	if err := tp.TakeKeyboard("a"); err != nil {
		t.Fatal(err)
	}
	tp.ChatEnded()
	if h := tp.KeyboardHolder(); h != "a" {
		t.Fatalf("holder after chat = %q; want a", h)
	}
}

func TestChatsCountsStartedChats(t *testing.T) {
	tp := newWatchedTap("a", "b")
	if tp.Chats() != 0 {
		t.Fatal("fresh tap has chats")
	}
	if tp.ChatBegan() {
		t.Fatal("ChatBegan true with no request")
	}
	if tp.Chats() != 0 {
		t.Fatal("refused ChatBegan was counted")
	}
	go func() { <-tp.BreakIn(); tp.ChatBegan() }()
	if err := tp.RequestChat("a", time.Second); err != nil {
		t.Fatal(err)
	}
	if tp.Chats() != 1 {
		t.Fatalf("chats %d, want 1", tp.Chats())
	}
}

func TestChatEndedReportsHoldDroppedWhenCallerEnds(t *testing.T) {
	tp := newWatchedTap("a")
	if err := tp.TakeKeyboard("a"); err != nil {
		t.Fatal(err)
	}
	if n := tp.Inject("a", []byte("hello")); n != 5 {
		t.Fatalf("Inject = %d", n)
	}
	beginChat(t, tp, "a")
	dropped, held, injected := tp.ChatEnded()
	if dropped != "a" || injected != 5 || held <= 0 {
		t.Fatalf("ChatEnded = %q, %v, %d; want a, >0, 5", dropped, held, injected)
	}
}

func TestChatEndedReportsNothingForChatOnlyHold(t *testing.T) {
	tp := newWatchedTap("a")
	beginChat(t, tp, "a") // RequestChat took the free keyboard
	if dropped, _, _ := tp.ChatEnded(); dropped != "" {
		t.Fatalf("dropped = %q for a hold taken only for chat", dropped)
	}
	if h := tp.KeyboardHolder(); h != "" {
		t.Fatalf("holder = %q", h)
	}
}

func TestChatEndedReportsNothingWhenHolderEnds(t *testing.T) {
	tp := newWatchedTap("a")
	if err := tp.TakeKeyboard("a"); err != nil {
		t.Fatal(err)
	}
	beginChat(t, tp, "a")
	if err := tp.StopChat("a"); err != nil {
		t.Fatal(err)
	}
	if dropped, _, _ := tp.ChatEnded(); dropped != "" {
		t.Fatalf("dropped = %q after the holder ended chat", dropped)
	}
}
