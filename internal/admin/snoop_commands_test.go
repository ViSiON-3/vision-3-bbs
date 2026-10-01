package admin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
)

func newTestServer(cfg ServerConfig) *Server {
	if cfg.Reg == nil {
		cfg.Reg = &fakeRegistry{}
	}
	cfg.MaxEvents = 50
	return NewServer(cfg)
}

func TestTypeInCommandCallsHookWithSysop(t *testing.T) {
	var gotSysop string
	var gotOn bool
	srv := newTestServer(ServerConfig{TypeIn: func(sysop string, node int, at time.Time, on bool) error {
		gotSysop, gotOn = sysop, on
		return nil
	}})
	_, err := srv.ExecuteAs("jim", AdminCommand{Command: CommandTypeIn, NodeID: 2, Payload: map[string]any{"on": true}})
	if err != nil {
		t.Fatal(err)
	}
	if gotSysop != "jim" || !gotOn {
		t.Fatalf("hook got %q %v", gotSysop, gotOn)
	}
}

func TestTypeInRefusesEmptySysop(t *testing.T) {
	srv := newTestServer(ServerConfig{TypeIn: func(string, int, time.Time, bool) error { return nil }})
	if _, err := srv.Execute(AdminCommand{Command: CommandTypeIn, NodeID: 2}); err != errNoSysop {
		t.Fatalf("err = %v, want errNoSysop", err)
	}
}

func TestChatCommandWithoutHookIsUnsupported(t *testing.T) {
	srv := newTestServer(ServerConfig{})
	_, err := srv.ExecuteAs("jim", AdminCommand{Command: CommandChat, NodeID: 2, Payload: map[string]any{"start": true}})
	if err == nil {
		t.Fatal("want unsupported error")
	}
}

func TestChatCommandEmitsState(t *testing.T) {
	srv := newTestServer(ServerConfig{Chat: func(string, int, time.Time, bool) error { return nil }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := srv.Subscribe(ctx)
	if _, err := srv.ExecuteAs("jim", AdminCommand{Command: CommandChat, NodeID: 2, Payload: map[string]any{"start": true}}); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-ch:
		if ev.Type != EventChatState || ev.Message != "on jim" {
			t.Fatalf("event %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no chat state event")
	}
}

func TestRaisePageReachesSubscribers(t *testing.T) {
	srv := newTestServer(ServerConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := srv.Subscribe(ctx)
	if srv.Consoles() != 1 {
		t.Fatalf("Consoles = %d", srv.Consoles())
	}
	srv.RaisePage(3, "caller", "help with door")
	select {
	case ev := <-ch:
		if ev.Type != EventPage || ev.NodeID != 3 || ev.Message != "help with door" {
			t.Fatalf("event %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no page event")
	}
}

func TestInProcessOpenSnoop(t *testing.T) {
	tap := snoop.NewTap()
	tap.Output([]byte("hello"))
	srv := newTestServer(ServerConfig{Snoop: func(req SnoopRequest) (*snoop.Tap, SnoopHeader, error) {
		return tap, SnoopHeader{OutputMode: "utf8", Width: 80, Height: 25}, nil
	}})
	c := NewInProcessClient(srv)
	st, err := c.OpenSnoop(context.Background(), 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	buf := make([]byte, 16)
	n, _ := st.Read(buf)
	if !strings.Contains(string(buf[:n]), "hello") {
		t.Fatalf("got %q", buf[:n])
	}
}

var (
	_ Snooper = (*InProcessClient)(nil)
	_ Snooper = (*SSHChannelClient)(nil)
)

// nextEvent returns the next event on ch, or fails after a short wait.
func nextEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(time.Second):
		t.Fatal("no event")
		return Event{}
	}
}

func noEvent(t *testing.T, ch <-chan Event) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestClearPageWithoutPageIsSilent(t *testing.T) {
	srv := newTestServer(ServerConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := srv.Subscribe(ctx)
	srv.ClearPage(3, "caller", "logoff")
	srv.ClearPage(3, "caller", "answered")
	noEvent(t, ch)
}

func TestClearPageClearsOutstandingPageOnce(t *testing.T) {
	srv := newTestServer(ServerConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := srv.Subscribe(ctx)
	srv.RaisePage(3, "caller", "help")
	if ev := nextEvent(t, ch); ev.Type != EventPage {
		t.Fatalf("event %+v", ev)
	}
	srv.ClearPage(4, "other", "logoff")
	srv.ClearPage(3, "caller", "answered")
	if ev := nextEvent(t, ch); ev.Type != EventPageCleared || ev.NodeID != 3 || ev.Message != "answered" {
		t.Fatalf("event %+v", ev)
	}
	srv.ClearPage(3, "caller", "logoff")
	noEvent(t, ch)
}
