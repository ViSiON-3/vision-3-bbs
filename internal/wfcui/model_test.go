package wfcui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

// fakeClient is an in-memory AdminClient with an optional liveness channel.
type fakeClient struct {
	mu      sync.Mutex
	snap    *admin.SystemSnapshot
	snapErr error
	events  chan admin.Event
	done    chan struct{}
	err     error
	execs   []admin.AdminCommand
	execErr error
	closed  bool
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		snap:   &admin.SystemSnapshot{SystemName: "Fake", Time: time.Unix(1000, 0)},
		events: make(chan admin.Event, 8),
		done:   make(chan struct{}),
	}
}

func (f *fakeClient) Snapshot(context.Context) (*admin.SystemSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap, f.snapErr
}
func (f *fakeClient) Subscribe(context.Context) (<-chan admin.Event, error) { return f.events, nil }
func (f *fakeClient) Execute(_ context.Context, cmd admin.AdminCommand) (*admin.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execs = append(f.execs, cmd)
	if f.execErr != nil {
		return nil, f.execErr
	}
	return &admin.Result{OK: true, Message: "ok"}, nil
}
func (f *fakeClient) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}
func (f *fakeClient) Done() <-chan struct{} { return f.done }
func (f *fakeClient) Err() error            { return f.err }
func (f *fakeClient) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// clock is a settable test clock.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// newTestModel builds a connected model on a fake clock.
func newTestModel(client admin.AdminClient, opts Options) (Model, *clock) {
	ck := &clock{t: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)}
	opts.now = ck.now
	m := New(client, opts)
	m.width, m.height = 100, 30
	return m, ck
}

// run executes a Cmd synchronously (batches included) and feeds every
// resulting message back through Update, returning the final model.
func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, msg := range collect(cmd) {
		mi, next := m.Update(msg)
		m = mi.(Model)
		_ = next // one level is enough for these tests
	}
	return m
}

// collect runs cmd and flattens batches into the messages they produce.
// Commands that block (the heartbeat tick, the liveness watcher) are
// abandoned after a short wait so a test never hangs on them.
func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(200 * time.Millisecond):
		return nil
	}
	switch v := msg.(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range v {
			out = append(out, collect(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func update(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	mi, cmd := m.Update(msg)
	return mi.(Model), cmd
}

func keyRune(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func lastEvent(m Model) string {
	if len(m.events) == 0 {
		return ""
	}
	return m.events[len(m.events)-1].Message
}

func TestNewStartsConnectedOnlyWithClient(t *testing.T) {
	m, _ := newTestModel(newFakeClient(), Options{})
	if m.conn != connConnected {
		t.Fatalf("with client: conn = %v, want connected", m.conn)
	}
	m2, _ := newTestModel(nil, Options{})
	if m2.conn != connLost {
		t.Fatalf("nil client: conn = %v, want lost", m2.conn)
	}
}

func TestSnapshotAppliedAndNavigation(t *testing.T) {
	m, _ := newTestModel(nil, Options{})
	snap := &admin.SystemSnapshot{SystemName: "T", Time: time.Now(), Nodes: []admin.NodeState{
		{NodeID: 1, Handle: "A"}, {NodeID: 2, Handle: "B"},
	}}
	m, _ = update(t, m, snapshotMsg{connID: m.connID, snap: snap})
	if m.snapshot == nil || len(m.snapshot.Nodes) != 2 {
		t.Fatalf("snapshot not applied: %+v", m.snapshot)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selected != 1 {
		t.Fatalf("selected = %d, want 1", m.selected)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.selected != 1 {
		t.Fatalf("selected past end = %d, want 1", m.selected)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeDetails {
		t.Fatalf("mode = %v, want details", m.mode)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeList {
		t.Fatalf("mode = %v, want list", m.mode)
	}
	// Selection clamps when nodes disappear.
	m.selected = 1
	m, _ = update(t, m, snapshotMsg{connID: m.connID, snap: &admin.SystemSnapshot{Nodes: []admin.NodeState{{NodeID: 1}}}})
	if m.selected != 0 {
		t.Fatalf("selected after shrink = %d, want 0", m.selected)
	}
}

func TestStaleConnIDMessagesIgnored(t *testing.T) {
	m, _ := newTestModel(newFakeClient(), Options{})
	old := m.connID
	m.connID++
	m, _ = update(t, m, snapshotMsg{connID: old, err: errors.New("late failure")})
	if m.conn != connConnected {
		t.Fatal("stale snapshot error must not drop the live link")
	}
	m, _ = update(t, m, snapshotMsg{connID: old, snap: &admin.SystemSnapshot{SystemName: "stale"}})
	if m.snapshot != nil {
		t.Fatal("stale snapshot must not be applied")
	}
}

func TestSnapshotErrorLosesConnectionAndSchedulesRetry(t *testing.T) {
	fc := newFakeClient()
	m, ck := newTestModel(fc, Options{})
	m, cmd := update(t, m, snapshotMsg{connID: m.connID, err: errors.New("EOF")})
	if m.conn != connLost {
		t.Fatalf("conn = %v, want lost", m.conn)
	}
	if m.attempt != 1 || !m.nextRetryAt.Equal(ck.t.Add(time.Second)) {
		t.Fatalf("attempt=%d next=%v; want 1 and +1s", m.attempt, m.nextRetryAt)
	}
	if !strings.HasPrefix(lastEvent(m), "Connection lost: EOF") {
		t.Fatalf("event log = %q", lastEvent(m))
	}
	if m.client != nil {
		t.Fatal("dead client must be dropped")
	}
	collect(cmd) // runs the deferred Close
	if !fc.isClosed() {
		t.Fatal("dead client must be closed")
	}
	// A second failure report for the same (now stale) link is a no-op.
	before := m.attempt
	m, _ = update(t, m, connLostMsg{connID: m.connID - 1})
	if m.attempt != before {
		t.Fatal("duplicate loss report must not advance the backoff")
	}
}

func TestTickDialsWhenRetryDue(t *testing.T) {
	replacement := newFakeClient()
	dialed := 0
	dial := func(context.Context) (admin.AdminClient, error) { dialed++; return replacement, nil }
	m, ck := newTestModel(newFakeClient(), Options{Dial: dial})
	m, _ = update(t, m, connLostMsg{connID: m.connID, err: errors.New("gone")})

	// Not yet due: the tick must not dial.
	m, _ = update(t, m, tickMsg{at: ck.t})
	if m.conn != connLost {
		t.Fatalf("early tick: conn = %v, want lost", m.conn)
	}
	// Due: the tick starts a dial.
	ck.t = ck.t.Add(2 * time.Second)
	m, cmd := update(t, m, tickMsg{at: ck.t})
	if m.conn != connConnecting {
		t.Fatalf("due tick: conn = %v, want connecting", m.conn)
	}
	msgs := collect(cmd)
	var res dialResultMsg
	for _, msg := range msgs {
		if r, ok := msg.(dialResultMsg); ok {
			res = r
		}
	}
	if dialed != 1 || res.client != replacement {
		t.Fatalf("dial not run: dialed=%d res=%+v", dialed, res)
	}
	m, cmd = update(t, m, res)
	if m.conn != connConnected || m.client != replacement || m.attempt != 0 {
		t.Fatalf("after dial: conn=%v client=%v attempt=%d", m.conn, m.client, m.attempt)
	}
	if lastEvent(m) != "Reconnected" {
		t.Fatalf("event = %q, want Reconnected", lastEvent(m))
	}
	// The new link is serviced: its snapshot arrives via the link commands.
	m = run(t, m, cmd)
	if m.snapshot == nil || m.snapshot.SystemName != "Fake" {
		t.Fatalf("new link snapshot not read: %+v", m.snapshot)
	}
}

func TestDialFailureBacksOffAndCaps(t *testing.T) {
	m, ck := newTestModel(nil, Options{Dial: func(context.Context) (admin.AdminClient, error) { return nil, errors.New("refused") }})
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	for i, d := range want {
		m.conn = connConnecting
		m, _ = update(t, m, dialResultMsg{connID: m.connID, err: errors.New("refused")})
		if got := m.nextRetryAt.Sub(ck.t); got != d {
			t.Fatalf("attempt %d: backoff = %v, want %v", i+1, got, d)
		}
		if m.conn != connLost || m.lastErr == nil {
			t.Fatalf("attempt %d: conn=%v err=%v", i+1, m.conn, m.lastErr)
		}
	}
}

func TestStaleDialResultIsClosedNotAdopted(t *testing.T) {
	late := newFakeClient()
	m, _ := newTestModel(nil, Options{})
	m, cmd := update(t, m, dialResultMsg{connID: m.connID - 1, client: late})
	if m.client != nil {
		t.Fatal("stale dial result must not be adopted")
	}
	collect(cmd)
	if !late.isClosed() {
		t.Fatal("stale dial result must be closed to avoid a leak")
	}
}

func TestEventStreamClosedLosesConnection(t *testing.T) {
	fc := newFakeClient()
	m, _ := newTestModel(fc, Options{})
	m, cmd := update(t, m, subscribedMsg{connID: m.connID, ch: fc.events})
	fc.events <- admin.Event{Type: admin.EventCallerConnected, Handle: "Zed", Message: "connected"}
	m = run(t, m, cmd)
	if len(m.events) != 1 || m.events[0].Handle != "Zed" {
		t.Fatalf("event not appended: %+v", m.events)
	}
	close(fc.events)
	m, _ = update(t, m, eventMsg{connID: m.connID, ch: fc.events, ok: false})
	if m.conn != connLost || !errors.Is(m.lastErr, errEventStreamClosed) {
		t.Fatalf("conn=%v err=%v", m.conn, m.lastErr)
	}
}

func TestLivenessDoneLosesConnection(t *testing.T) {
	fc := newFakeClient()
	fc.err = errors.New("keepalive timed out")
	m, _ := newTestModel(fc, Options{})
	cmd := m.watchDone()
	if cmd == nil {
		t.Fatal("client with Liveness must be watched")
	}
	close(fc.done)
	msg := cmd()
	m, _ = update(t, m, msg)
	if m.conn != connLost || m.lastErr == nil || !strings.Contains(m.lastErr.Error(), "keepalive") {
		t.Fatalf("conn=%v err=%v", m.conn, m.lastErr)
	}
}

func TestStalledSnapshotFeedReconnects(t *testing.T) {
	m, ck := newTestModel(newFakeClient(), Options{})
	m.lastSnapAt = ck.t.Add(-staleLostAfter - time.Second)
	m, _ = update(t, m, tickMsg{at: ck.t})
	if m.conn != connLost || !errors.Is(m.lastErr, errSnapshotStalled) {
		t.Fatalf("conn=%v err=%v", m.conn, m.lastErr)
	}
}

func TestRKeyWhileOfflineDialsImmediately(t *testing.T) {
	dialed := false
	m, _ := newTestModel(nil, Options{Dial: func(context.Context) (admin.AdminClient, error) { dialed = true; return newFakeClient(), nil }})
	m.nextRetryAt = m.now().Add(time.Hour)
	m, cmd := update(t, m, keyRune('r'))
	if m.conn != connConnecting {
		t.Fatalf("conn = %v, want connecting", m.conn)
	}
	collect(cmd)
	if !dialed {
		t.Fatal("R while offline must dial at once")
	}
	// Without a dialer R is inert.
	m2, _ := newTestModel(nil, Options{})
	m2, cmd = update(t, m2, keyRune('r'))
	if m2.conn != connLost || cmd != nil {
		t.Fatal("R without a dialer must do nothing")
	}
}

func TestRKeyWhileConnectedRefreshes(t *testing.T) {
	fc := newFakeClient()
	m, _ := newTestModel(fc, Options{})
	m, cmd := update(t, m, keyRune('R'))
	m = run(t, m, cmd)
	if len(fc.execs) != 1 || fc.execs[0].Command != admin.CommandRefresh {
		t.Fatalf("execs = %+v, want one refresh", fc.execs)
	}
	if m.snapshot == nil {
		t.Fatal("refresh must read the snapshot")
	}
}

func withNodes(m Model) Model {
	m.snapshot = &admin.SystemSnapshot{Time: time.Now(), Nodes: []admin.NodeState{
		{NodeID: 1, Handle: "J0hnny A1pha"}, {NodeID: 2, Handle: "The Wolverine"},
	}}
	return m
}

func TestKickFlow(t *testing.T) {
	fc := newFakeClient()
	m, _ := newTestModel(fc, Options{})
	m = withNodes(m)
	m.selected = 1

	m, _ = update(t, m, keyRune('k'))
	if m.mode != modeConfirmKick {
		t.Fatalf("mode = %v, want confirm", m.mode)
	}
	m, _ = update(t, m, keyRune('n'))
	if m.mode != modeList || len(fc.execs) != 0 {
		t.Fatalf("N must cancel: mode=%v execs=%d", m.mode, len(fc.execs))
	}
	m, _ = update(t, m, keyRune('K'))
	m, cmd := update(t, m, keyRune('y'))
	if m.mode != modeList {
		t.Fatalf("mode after Y = %v, want list", m.mode)
	}
	m = run(t, m, cmd)
	if len(fc.execs) != 1 || fc.execs[0].Command != admin.CommandKick || fc.execs[0].NodeID != 2 {
		t.Fatalf("execs = %+v", fc.execs)
	}
	if !strings.Contains(m.status, "Kicked The Wolverine") || m.statusErr {
		t.Fatalf("status = %q err=%v", m.status, m.statusErr)
	}
	if !strings.Contains(lastEvent(m), "Kicked The Wolverine (node 2)") {
		t.Fatalf("event = %q", lastEvent(m))
	}
	// Details mode returns to details after the prompt.
	m.mode = modeDetails
	m, _ = update(t, m, keyRune('k'))
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeDetails {
		t.Fatalf("cancel from details: mode = %v", m.mode)
	}
}

func TestKickFailureReported(t *testing.T) {
	fc := newFakeClient()
	fc.execErr = errors.New("admin: kick not supported by this server")
	m, _ := newTestModel(fc, Options{})
	m = withNodes(m)
	m, _ = update(t, m, keyRune('k'))
	m, cmd := update(t, m, keyRune('Y'))
	m = run(t, m, cmd)
	if !m.statusErr || !strings.Contains(m.status, "not supported") {
		t.Fatalf("status = %q err=%v", m.status, m.statusErr)
	}
}

func TestKickGuards(t *testing.T) {
	m, _ := newTestModel(newFakeClient(), Options{ReadOnly: true})
	m = withNodes(m)
	m, _ = update(t, m, keyRune('k'))
	if m.mode != modeList || !strings.Contains(m.status, "Read-only") {
		t.Fatalf("read-only: mode=%v status=%q", m.mode, m.status)
	}
	m2, _ := newTestModel(newFakeClient(), Options{})
	m2, _ = update(t, m2, keyRune('k'))
	if m2.mode != modeList || !strings.Contains(m2.status, "No caller") {
		t.Fatalf("no nodes: mode=%v status=%q", m2.mode, m2.status)
	}
	m3, _ := newTestModel(nil, Options{})
	m3 = withNodes(m3)
	m3, _ = update(t, m3, keyRune('k'))
	if m3.mode != modeList || !strings.Contains(m3.status, "Not connected") {
		t.Fatalf("offline: mode=%v status=%q", m3.mode, m3.status)
	}
}

func TestStatusExpiresOnTick(t *testing.T) {
	m, ck := newTestModel(nil, Options{})
	m.setStatus("hello", false)
	m, _ = update(t, m, tickMsg{at: ck.t.Add(statusTTL - time.Second)})
	if m.status == "" {
		t.Fatal("status expired early")
	}
	m, _ = update(t, m, tickMsg{at: ck.t.Add(statusTTL)})
	if m.status != "" {
		t.Fatal("status should expire after its TTL")
	}
}

func TestEventRingIsBounded(t *testing.T) {
	m, _ := newTestModel(nil, Options{MaxEvents: 3})
	for i := 0; i < 10; i++ {
		m.pushLocalEvent("x")
	}
	if len(m.events) != 3 {
		t.Fatalf("events = %d, want 3", len(m.events))
	}
}

func TestBackoffTable(t *testing.T) {
	cases := map[int]time.Duration{0: time.Second, 1: time.Second, 2: 2 * time.Second, 5: 16 * time.Second, 6: 30 * time.Second, 40: 30 * time.Second}
	for n, want := range cases {
		if got := backoff(n); got != want {
			t.Errorf("backoff(%d) = %v, want %v", n, got, want)
		}
	}
}

func TestOptionDefaults(t *testing.T) {
	m := New(nil, Options{})
	if m.opts.Refresh != time.Second || m.opts.MaxEvents != 200 || m.opts.now == nil {
		t.Fatalf("defaults not applied: %+v", m.opts)
	}
}

func TestInitStartsLinkCommands(t *testing.T) {
	fc := newFakeClient()
	m, _ := newTestModel(fc, Options{})
	var sawSnap, sawSub bool
	for _, msg := range collect(m.Init()) {
		switch msg.(type) {
		case snapshotMsg:
			sawSnap = true
		case subscribedMsg:
			sawSub = true
		}
	}
	if !sawSnap || !sawSub {
		t.Fatalf("Init: snapshot=%v subscribe=%v", sawSnap, sawSub)
	}
}

func TestTabsSeparateCallersBotsAndEvents(t *testing.T) {
	m, _ := newTestModel(nil, Options{})
	m.snapshot = &admin.SystemSnapshot{Time: time.Now(), Nodes: []admin.NodeState{
		{NodeID: 1, Handle: "Alice", Status: admin.StatusOnline},
		{NodeID: 2, Status: admin.StatusLogin, RemoteAddr: "10.0.0.9:5555"},
		{NodeID: 3, Handle: "Bob", Status: admin.StatusInMenu},
	}, ScheduledEvents: []admin.ScheduledEvent{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}}

	if got := len(m.callers()); got != 2 {
		t.Fatalf("callers = %d, want 2", got)
	}
	if got := m.bots(); len(got) != 1 || got[0].NodeID != 2 {
		t.Fatalf("bots = %+v", got)
	}
	// Selection is scoped to the tab and resets on switch.
	m.selected = 1
	if n, _ := m.selectedNode(); n.Handle != "Bob" {
		t.Fatalf("callers tab selection = %+v", n)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.tab != groupBots || m.selected != 0 {
		t.Fatalf("after TAB: tab=%v selected=%d", m.tab, m.selected)
	}
	// The bot log is not selectable, so the cursor stays with the callers.
	if n, ok := m.selectedNode(); !ok || n.Handle != "Alice" {
		t.Fatalf("bots tab selection = %+v ok=%v", n, ok)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.tab != groupEvents {
		t.Fatalf("after second TAB: tab=%v", m.tab)
	}
	if _, ok := m.selectedNode(); ok {
		t.Fatal("events tab must not expose a node selection")
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	if ev, ok := m.selectedEvent(); !ok || ev.ID != "d" || m.selected != 3 {
		t.Fatalf("events End: %+v selected=%d", ev, m.selected)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.tab != groupLog {
		t.Fatalf("TAB must wrap: tab=%v", m.tab)
	}
	if n, ok := m.selectedNode(); !ok || n.Handle != "Alice" {
		t.Fatalf("log tab must hand the cursor back to the callers: %+v", n)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.tab != groupEvents {
		t.Fatalf("Shift-TAB must go back: tab=%v", m.tab)
	}
	// K on the events tab is refused.
	m.client = newFakeClient()
	m.conn = connConnected
	m, _ = update(t, m, keyRune('k'))
	if m.mode != modeList || !strings.Contains(m.status, "No caller") {
		t.Fatalf("kick on events tab: mode=%v status=%q", m.mode, m.status)
	}
}

func TestPageKeysScrollLogsAndPageEvents(t *testing.T) {
	m, ck := newTestModel(nil, Options{})
	m.width, m.height = 80, 25 // 12 log rows with one placeholder caller row
	for i := 0; i < 30; i++ {
		m.appendEvent(admin.Event{Time: ck.t, Type: admin.EventMenuChanged, Handle: "Zed", Message: fmt.Sprintf("M%02d", i)})
	}
	rows := m.lowerRows()
	if rows != 12 {
		t.Fatalf("lowerRows = %d, want 12", rows)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.scrollBack != rows-1 {
		t.Fatalf("PgUp: scrollBack = %d, want %d", m.scrollBack, rows-1)
	}
	for i := 0; i < 5; i++ {
		m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	}
	if m.scrollBack != 30-rows {
		t.Fatalf("PgUp clamps at the oldest entry: scrollBack = %d, want %d", m.scrollBack, 30-rows)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.scrollBack != 0 {
		t.Fatalf("PgDn returns to the tail: scrollBack = %d", m.scrollBack)
	}
	// Switching tabs resets the scroll; on Events, paging moves the cursor.
	m.scrollBack = 3
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.tab != groupEvents || m.scrollBack != 0 {
		t.Fatalf("tab switch: tab=%v scrollBack=%d", m.tab, m.scrollBack)
	}
	m.snapshot = &admin.SystemSnapshot{Schema: admin.SnapshotSchema, Time: ck.t}
	for i := 0; i < 40; i++ {
		m.snapshot.ScheduledEvents = append(m.snapshot.ScheduledEvents, admin.ScheduledEvent{ID: fmt.Sprintf("e%d", i)})
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.selected != rows {
		t.Fatalf("PgDn on events: selected = %d, want %d", m.selected, rows)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.selected != 0 {
		t.Fatalf("PgUp on events: selected = %d, want 0", m.selected)
	}
}

func TestReconnectReplayDoesNotDuplicateEvents(t *testing.T) {
	fc := newFakeClient()
	m, ck := newTestModel(fc, Options{})
	history := []admin.Event{
		{Time: ck.t.Add(-3 * time.Second), Type: admin.EventCallerLoggedIn, NodeID: 1, Handle: "Zed", Message: "logged in"},
		{Time: ck.t.Add(-2 * time.Second), Type: admin.EventMenuChanged, NodeID: 1, Handle: "Zed", Message: "MAIN"},
		{Time: ck.t.Add(-2 * time.Second), Type: admin.EventActivityChanged, NodeID: 1, Handle: "Zed", Message: "Reading"},
	}
	feed := func(evs []admin.Event) {
		for _, ev := range evs {
			m, _ = update(t, m, eventMsg{connID: m.connID, ch: fc.events, ev: ev, ok: true})
		}
	}
	feed(history)
	// Link drops and comes back; the daemon replays its ring buffer.
	m, _ = update(t, m, connLostMsg{connID: m.connID, err: errors.New("gone")})
	m, _ = update(t, m, dialResultMsg{connID: m.connID, client: newFakeClient()})
	feed(history)
	newer := admin.Event{Time: ck.t.Add(time.Second), Type: admin.EventMenuChanged, NodeID: 1, Handle: "Zed", Message: "DOORS"}
	feed([]admin.Event{newer})

	var menus, logins int
	for _, ev := range m.events {
		switch ev.Type {
		case admin.EventMenuChanged:
			menus++
		case admin.EventCallerLoggedIn:
			logins++
		}
	}
	if logins != 1 || menus != 2 {
		t.Fatalf("replayed history duplicated: logins=%d menus=%d events=%+v", logins, menus, m.events)
	}
	if !strings.Contains(lastEvent(m), "DOORS") {
		t.Fatalf("new event after replay must still be appended: %q", lastEvent(m))
	}
}

func TestStaleKickResultIgnored(t *testing.T) {
	m, _ := newTestModel(newFakeClient(), Options{})
	m, _ = update(t, m, kickResultMsg{connID: m.connID - 1, nodeID: 2, handle: "Old", res: &admin.Result{OK: true}})
	if m.status != "" || len(m.events) != 0 {
		t.Fatalf("stale kick result must not touch the model: status=%q events=%d", m.status, len(m.events))
	}
}

func TestKickCommandCarriesConnectTime(t *testing.T) {
	fc := newFakeClient()
	m, _ := newTestModel(fc, Options{})
	started := time.Date(2026, 9, 16, 11, 0, 0, 0, time.UTC)
	m.snapshot = &admin.SystemSnapshot{Time: time.Now(), Nodes: []admin.NodeState{{NodeID: 4, Handle: "Zed", ConnectedAt: started}}}
	m, _ = update(t, m, keyRune('k'))
	m, cmd := update(t, m, keyRune('y'))
	run(t, m, cmd)
	if len(fc.execs) != 1 || !fc.execs[0].ConnectedAt.Equal(started) {
		t.Fatalf("kick command = %+v, want ConnectedAt %v", fc.execs, started)
	}
}
