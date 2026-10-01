package wfcui

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

func pageEvent(node int, reason string) admin.Event {
	return admin.Event{Time: time.Now(), Type: admin.EventPage, NodeID: node, Handle: "caller", Message: reason}
}

func clearEvent(node int, why string) admin.Event {
	return admin.Event{Time: time.Now(), Type: admin.EventPageCleared, NodeID: node, Handle: "caller", Message: why}
}

func feed(t *testing.T, m Model, ev admin.Event) (Model, tea.Cmd) {
	t.Helper()
	return update(t, m, eventMsg{connID: m.connID, ch: make(chan admin.Event), ev: ev, ok: true})
}

func pageModel(t *testing.T) (Model, *fakeClient) {
	t.Helper()
	fc := newFakeClient()
	m, _ := newTestModel(fc, Options{NoBell: true})
	m.snapshot = &admin.SystemSnapshot{Time: time.Now(), Nodes: []admin.NodeState{
		{NodeID: 3, Handle: "caller", ConnectedAt: time.Unix(500, 0)},
		{NodeID: 4, Handle: "other", ConnectedAt: time.Unix(600, 0)},
	}}
	return m, fc
}

func TestPageEventAddsBadge(t *testing.T) {
	m, _ := pageModel(t)
	m, _ = feed(t, m, pageEvent(3, "help"))
	if len(m.pages) != 1 {
		t.Fatalf("pages %v", m.pages)
	}
	if !strings.Contains(m.View(), "PAGE 1") {
		t.Fatal("no badge")
	}
}

func pageAt(m Model, node int, reason string, age time.Duration) admin.Event {
	return admin.Event{Time: m.now().Add(-age), Type: admin.EventPage, NodeID: node, Handle: "caller", Message: reason}
}

func ringModel(rings *int, opts Options) Model {
	opts.bell = func() { *rings++ }
	m, _ := newTestModel(newFakeClient(), opts)
	return m
}

func feedRun(t *testing.T, m Model, ev admin.Event) Model {
	t.Helper()
	m, cmd := feed(t, m, ev)
	collect(cmd)
	return m
}

func TestPageBellOnlyForNewPendingPage(t *testing.T) {
	rings := 0
	m := ringModel(&rings, Options{})
	m = feedRun(t, m, pageAt(m, 3, "help", time.Second))
	if rings != 1 {
		t.Fatalf("fresh page rang %d times", rings)
	}
	m = feedRun(t, m, m.pages[0]) // same page again
	if len(m.pages) != 1 || rings != 1 {
		t.Fatalf("repeat rang or duplicated: rings=%d pages=%v", rings, m.pages)
	}
	m = feedRun(t, m, clearEvent(3, "answered"))
	m = feedRun(t, m, pageAt(m, 3, "again", 0))
	if rings != 2 {
		t.Fatalf("a new page after a clear must ring, rings=%d", rings)
	}

	quiet := 0
	mq := ringModel(&quiet, Options{NoBell: true})
	feedRun(t, mq, pageAt(mq, 3, "help", 0))
	if quiet != 0 {
		t.Fatal("NoBell must stay quiet")
	}
}

func TestOldPageIsListedButSilent(t *testing.T) {
	rings := 0
	m := ringModel(&rings, Options{})
	m = feedRun(t, m, pageAt(m, 3, "help", 5*time.Minute))
	if len(m.pages) != 1 || rings != 0 {
		t.Fatalf("pages=%v rings=%d", m.pages, rings)
	}
}

func TestReplayAfterReconnectIsSilent(t *testing.T) {
	rings := 0
	m := ringModel(&rings, Options{})
	pg := pageAt(m, 3, "help", time.Second)
	lg := clearEvent(3, "logoff")
	m = feedRun(t, m, pg)
	m = feedRun(t, m, lg)
	if rings != 1 || len(m.pages) != 0 {
		t.Fatalf("rings=%d pages=%v", rings, m.pages)
	}
	// A reconnect replays the ring: the page returns, then its clear.
	m = feedRun(t, m, pg)
	if len(m.pages) != 1 {
		t.Fatalf("replayed page not listed: %v", m.pages)
	}
	m = feedRun(t, m, lg)
	if rings != 1 || len(m.pages) != 0 {
		t.Fatalf("replay rang: rings=%d pages=%v", rings, m.pages)
	}
}

func TestPKeyWithoutPagesOnlySetsStatus(t *testing.T) {
	m, _ := pageModel(t)
	m, _ = update(t, m, keyRune('p'))
	if m.mode == modePages || m.status != "No pages" {
		t.Fatalf("mode=%v status=%q", m.mode, m.status)
	}
}

func TestPageListReturnsToOpeningMode(t *testing.T) {
	m, _ := pageModel(t)
	m, _ = feed(t, m, pageEvent(3, "help"))
	m.mode = modeDetails
	m, _ = update(t, m, keyRune('p'))
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeDetails {
		t.Fatalf("mode %v", m.mode)
	}
}

func TestSecondSnoopRefusedWhileOpening(t *testing.T) {
	sc := &snoopClient{fakeClient: newFakeClient(), openErr: errors.New("gone")}
	m, _ := newTestModel(sc, Options{})
	m = withNodes(m)
	m, cmd := update(t, m, keyRune('s'))
	if cmd == nil || !m.snoopPending {
		t.Fatal("first S must start an open")
	}
	m2, cmd2 := update(t, m, keyRune('s'))
	if cmd2 != nil || !m2.statusErr {
		t.Fatalf("second S not refused, status %q", m2.status)
	}
	m = run(t, m, cmd) // open fails
	if m.snoopPending {
		t.Fatal("pending not cleared after failed open")
	}
	_, cmd = update(t, m, keyRune('s'))
	if cmd == nil {
		t.Fatal("S must work again after the failure")
	}
	// A stale-link open result also clears the flag.
	m.snoopPending = true
	m, _ = update(t, m, snoopOpenedMsg{connID: m.connID + 1})
	if m.snoopPending {
		t.Fatal("pending not cleared on stale result")
	}
	// A finished snoop clears it too.
	m.snoopPending = true
	m, _ = update(t, m, snoopResult{node: 1})
	if m.snoopPending {
		t.Fatal("pending not cleared after snoop")
	}
}

func TestPageClearedStates(t *testing.T) {
	m, _ := pageModel(t)
	m, _ = feed(t, m, pageEvent(3, "help"))
	m, _ = feed(t, m, clearEvent(3, "timeout"))
	if len(m.pages) != 1 || !strings.Contains(m.pages[0].Message, "(timeout)") {
		t.Fatalf("pages %v", m.pages)
	}
	if strings.Contains(m.View(), "PAGE") {
		t.Fatal("badge counts only pending pages")
	}
	m, _ = feed(t, m, clearEvent(3, "logoff"))
	if len(m.pages) != 0 {
		t.Fatalf("pages %v", m.pages)
	}
}

func TestPageDropsWhenNodeLeavesSnapshot(t *testing.T) {
	m, _ := pageModel(t)
	m, _ = feed(t, m, pageEvent(3, "help"))
	snap := &admin.SystemSnapshot{Time: time.Now().Add(time.Second), Nodes: []admin.NodeState{{NodeID: 4, Handle: "other"}}}
	m, _ = update(t, m, snapshotMsg{connID: m.connID, snap: snap})
	if len(m.pages) != 0 {
		t.Fatalf("pages %v", m.pages)
	}
}

func TestPKeyOpensPageList(t *testing.T) {
	m, _ := pageModel(t)
	m, _ = feed(t, m, pageEvent(3, "help"))
	m, _ = update(t, m, keyRune('p'))
	if m.mode != modePages {
		t.Fatalf("mode %v", m.mode)
	}
	if !strings.Contains(m.View(), "help") {
		t.Fatal("page reason not listed")
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeList {
		t.Fatalf("mode %v", m.mode)
	}
}

func TestSKeyRefusals(t *testing.T) {
	m, _ := pageModel(t) // fakeClient is not a Snooper
	m, _ = update(t, m, keyRune('s'))
	if m.status == "" || !m.statusErr {
		t.Fatal("no refusal for a client that cannot snoop")
	}

	sc := &snoopClient{fakeClient: newFakeClient()}
	m, _ = newTestModel(sc, Options{ReadOnly: true})
	m = withNodes(m)
	m, cmd := update(t, m, keyRune('s'))
	if cmd != nil || !strings.Contains(m.status, "Read-only") {
		t.Fatalf("read-only: status %q", m.status)
	}

	m, _ = newTestModel(nil, Options{})
	m, cmd = update(t, m, keyRune('s'))
	if cmd != nil || m.status == "" {
		t.Fatal("disconnected must refuse")
	}
}

// snoopClient is a fakeClient that can open snoops.
type snoopClient struct {
	*fakeClient
	opened  []admin.NodeState
	openErr error
}

func (s *snoopClient) OpenSnoop(_ context.Context, node int, at time.Time) (*admin.SnoopStream, error) {
	s.opened = append(s.opened, admin.NodeState{NodeID: node, ConnectedAt: at})
	return nil, s.openErr
}

func TestSKeyTargetsSelectedSession(t *testing.T) {
	sc := &snoopClient{fakeClient: newFakeClient(), openErr: errors.New("caller has gone")}
	m, _ := newTestModel(sc, Options{})
	m.snapshot = &admin.SystemSnapshot{Time: time.Now(), Nodes: []admin.NodeState{
		{NodeID: 3, Handle: "a", ConnectedAt: time.Unix(500, 0)},
		{NodeID: 4, Handle: "b", ConnectedAt: time.Unix(600, 0)},
	}}
	m.selected = 1
	m, cmd := update(t, m, keyRune('s'))
	if cmd == nil {
		t.Fatalf("no command, status %q", m.status)
	}
	m = run(t, m, cmd)
	if len(sc.opened) != 1 || sc.opened[0].NodeID != 4 || !sc.opened[0].ConnectedAt.Equal(time.Unix(600, 0)) {
		t.Fatalf("opened %+v", sc.opened)
	}
	if !m.statusErr || !strings.Contains(m.status, "caller has gone") {
		t.Fatalf("status %q", m.status)
	}
}

func TestEnterOnPageAnswersWithChat(t *testing.T) {
	sc := &snoopClient{fakeClient: newFakeClient(), openErr: errors.New("x")}
	m, _ := newTestModel(sc, Options{NoBell: true})
	m.snapshot = &admin.SystemSnapshot{Time: time.Now(), Nodes: []admin.NodeState{
		{NodeID: 3, Handle: "caller", ConnectedAt: time.Unix(500, 0)},
	}}
	m, _ = feed(t, m, pageEvent(3, "help"))
	m, _ = update(t, m, keyRune('p'))
	m, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("no command, status %q", m.status)
	}
	m = run(t, m, cmd)
	if len(sc.opened) != 1 || sc.opened[0].NodeID != 3 {
		t.Fatalf("opened %+v", sc.opened)
	}
}

func TestRPCControlSendsCommands(t *testing.T) {
	fc := newFakeClient()
	at := time.Unix(500, 0)
	ctl := rpcControl{client: fc, node: admin.NodeState{NodeID: 3, ConnectedAt: at}}
	if err := ctl.TypeIn(true); err != nil {
		t.Fatal(err)
	}
	if err := ctl.Chat(false); err != nil {
		t.Fatal(err)
	}
	if len(fc.execs) != 2 {
		t.Fatalf("execs %+v", fc.execs)
	}
	e := fc.execs[0]
	if e.Command != admin.CommandTypeIn || e.NodeID != 3 || !e.ConnectedAt.Equal(at) || e.Payload["on"] != true {
		t.Fatalf("typein %+v", e)
	}
	e = fc.execs[1]
	if e.Command != admin.CommandChat || e.Payload["start"] != false {
		t.Fatalf("chat %+v", e)
	}
	fc.execErr = errors.New("refused")
	if err := ctl.TypeIn(true); err == nil {
		t.Fatal("error not returned")
	}
}

func TestSnoopResultSetsStatusAndRefreshes(t *testing.T) {
	fc := newFakeClient()
	m, _ := newTestModel(fc, Options{})
	m, cmd := update(t, m, snoopResult{node: 3, reason: "node 3 disconnected"})
	if m.status != "node 3 disconnected" {
		t.Fatalf("status %q", m.status)
	}
	if cmd == nil {
		t.Fatal("no refresh")
	}
}

func TestSnoopStartsInChatWhenAnsweringPage(t *testing.T) {
	a, server := net.Pipe()
	defer server.Close()
	ctl := &fakeCtl{}
	out := &syncBuf{}
	cmd := newSnoopCmd(admin.NewSnoopStream(utf8Hdr, a), ctl, 3, fakeRaw{w: 80, h: 26})
	cmd.startChat = true
	inR, inW := io.Pipe()
	defer inW.Close()
	cmd.SetStdin(inR)
	cmd.SetStdout(out)
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	out.waitFor(t, "CHAT")
	if _, err := inW.Write([]byte("\x1bx")); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := ctl.got(); got != "chat+,chat-" {
		t.Fatalf("calls %q", got)
	}
}
