package wfcui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
)

// makeModel builds a plain-text model with explicit dimensions.
func makeModel(opts Options, w, h int) Model {
	opts.NoColor = true
	m, _ := newTestModel(nil, opts)
	m.width, m.height = w, h
	m.conn = connConnected // render as live unless a test says otherwise
	return m
}

func mockupSnapshot(now time.Time) *admin.SystemSnapshot {
	return &admin.SystemSnapshot{
		Schema:     admin.SnapshotSchema,
		SystemName: "Broken Bit Syndicate",
		Time:       now,
		UptimeSecs: 16*3600 + 32*60 + 24,
		MaxNodes:   4,
		Nodes: []admin.NodeState{
			{NodeID: 1, Handle: "J0hnny A1pha", Activity: "Scanning New Messages", RemoteAddr: "108.216.156.236:61234", ConnectedAt: now.Add(-4 * time.Minute), Status: admin.StatusOnline},
			{NodeID: 2, Handle: "The Wolverine", Activity: "Playing L.O.R.D", RemoteAddr: "192.168.1.214:32000", ConnectedAt: now.Add(-10 * time.Minute), Status: admin.StatusOnline},
			{NodeID: 3, Status: admin.StatusLogin, RemoteAddr: "45.33.32.156:40122", ConnectedAt: now.Add(-30 * time.Second), CurrentMenu: "LOGIN"},
		},
		Counters: admin.Counters{ActiveNodes: 3, TotalUsers: 1540, CallsToday: 123, NewUsers: 172, MailWaiting: 232},
		ScheduledEvents: []admin.ScheduledEvent{
			{ID: "nightly", Name: "Nightly toss", Schedule: "0 3 * * *", Enabled: true, NextRun: now.Add(9 * time.Hour), LastRun: now.Add(-15 * time.Hour), LastStatus: "success", LastDurationMs: 1234, RunCount: 40},
			{ID: "purge", Name: "Purge old files", Schedule: "@weekly", Enabled: false},
		},
	}
}

func rows(view string) []string { return strings.Split(view, "\n") }

// TestViewExactDimensions: every frame is exactly height rows of exactly
// width cells, at the minimum size and at the size the macOS launcher uses.
func TestViewExactDimensions(t *testing.T) {
	for _, size := range [][2]int{{80, 25}, {110, 34}, {200, 60}} {
		for _, ascii := range []bool{false, true} {
			m := makeModel(Options{ASCII: ascii}, size[0], size[1])
			m.snapshot = mockupSnapshot(m.now())
			m.events = []admin.Event{{Time: time.Now(), Type: admin.EventActivityChanged, Handle: "J0hnny A1pha", Message: "Scanning New Messages"}}
			for tab := groupLog; tab < groupCount; tab++ {
				m.tab = tab
				for _, mode := range []viewMode{modeList, modeDetails, modeConfirmKick} {
					m.mode = mode
					got := rows(m.View())
					if len(got) != size[1] {
						t.Fatalf("%dx%d ascii=%v tab=%v mode=%v: %d rows", size[0], size[1], ascii, tab, mode, len(got))
					}
					for i, r := range got {
						if n := runeCount(r); n != size[0] {
							t.Fatalf("%dx%d ascii=%v tab=%v mode=%v: row %d is %d wide:\n%q", size[0], size[1], ascii, tab, mode, i, n, r)
						}
					}
				}
			}
		}
	}
}

// TestViewMockupGeometry pins the 80x25 layout to the mockup: title, stats,
// the caller box sized to its two callers, the event log taking the rest,
// and the command bar.
func TestViewMockupGeometry(t *testing.T) {
	m := makeModel(Options{}, 80, 25)
	m.snapshot = mockupSnapshot(m.now())
	m.events = []admin.Event{{Time: time.Date(2026, 1, 1, 18, 21, 35, 0, time.Local), Type: admin.EventActivityChanged, Handle: "J0hnny A1pha", Message: "Scanning New Messages"}}
	r := rows(m.View())

	if !strings.Contains(r[0], "Broken Bit Syndicate") || !strings.Contains(r[0], "WFC ") {
		t.Errorf("title row: %q", r[0])
	}
	// Five full labels do not fit 74 columns, so the short set is used, centred.
	stats := " Users: 1540   New: 172   Mail: 232   Calls: 123   Uptime: 16:32:24 "
	if !strings.Contains(r[2], stats) || !centred(r[2], stats, 2) {
		t.Errorf("stats row: %q", r[2])
	}
	if !strings.HasPrefix(r[3], "   ┌") || !strings.HasSuffix(r[3], "┐   ") {
		t.Errorf("top box top: %q", r[3])
	}
	if !strings.Contains(r[4], "▄▀▄ Online Now ▄▀▄") {
		t.Errorf("caller box caption: %q", r[4])
	}
	if !strings.HasPrefix(r[5], "   │Handle          Activity                   On    Address           N#   │") {
		t.Errorf("node header: %q", r[5])
	}
	if !strings.HasPrefix(r[6], "   │J0hnny A1pha    Scanning New Messages      4m    108.216.156.236   1    │") {
		t.Errorf("node row 1: %q", r[6])
	}
	if !strings.Contains(r[7], "The Wolverine   Playing L.O.R.D            10m   192.168.1.214     2    │") {
		t.Errorf("node row 2: %q", r[7])
	}
	if strings.Contains(m.View(), "45.33.32.156") {
		t.Error("bot must not appear on the callers tab")
	}
	if !strings.HasPrefix(r[8], "   └") || !strings.HasSuffix(r[8], "┘   ") {
		t.Errorf("top box bottom (box must hug its two rows): %q", r[8])
	}
	if !strings.HasPrefix(r[9], "   ┌") || !strings.Contains(r[10], "▄▀▄ Callers  Bots 1  Events 2 ▄▀▄") {
		t.Errorf("lower box top/tabs: %q / %q", r[9], r[10])
	}
	if !strings.HasPrefix(r[11], "   │Time      User          Activity") {
		t.Errorf("event header: %q", r[11])
	}
	if !strings.HasPrefix(r[12], "   │18:21:35  J0hnny A1pha  Scanning New Messages") {
		t.Errorf("event row: %q", r[12])
	}
	if !strings.HasPrefix(r[23], "   └") {
		t.Errorf("event box bottom: %q", r[23])
	}
	want := "[↑/↓] select [TAB] view [PgUp/PgDn] scroll [ENTER] details [Q] quit [K] kick"
	if !strings.Contains(r[24], want) || !centred(r[24], want, 2) {
		t.Errorf("command bar:\n got %q\nwant centred %q", r[24], want)
	}
}

// centred reports whether sub sits within tol cells of the middle of row.
func centred(row, sub string, tol int) bool {
	cells := []rune(row)
	i := strings.Index(row, sub)
	if i < 0 {
		return false
	}
	left := runeCount(row[:i])
	right := len(cells) - left - runeCount(sub)
	d := left - right
	if d < 0 {
		d = -d
	}
	return d <= tol
}

func TestViewStatsUseFullLabelsWhenTheyFit(t *testing.T) {
	m := makeModel(Options{}, 110, 34)
	m.snapshot = mockupSnapshot(m.now())
	r := rows(m.View())
	stats := " Total Users: 1540   New Users: 172   Mail Waiting: 232   Calls Today: 123   Uptime: 16:32:24 "
	if !strings.Contains(r[2], stats) || !centred(r[2], stats, 2) {
		t.Errorf("stats row at 110 cols: %q", r[2])
	}
}

func TestViewBotsTab(t *testing.T) {
	m := makeModel(Options{}, 80, 25)
	m.snapshot = mockupSnapshot(m.now())
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.Local)
	m.events = []admin.Event{
		{Time: base, Type: admin.EventCallerConnected, NodeID: 3, Addr: "45.33.32.156:40122", Message: "connected"},
		{Time: base.Add(time.Second), Type: admin.EventMenuChanged, NodeID: 1, Handle: "J0hnny A1pha", Message: "MAIN"},
		{Time: base.Add(2 * time.Second), Type: admin.EventCallerDisconnected, NodeID: 7, Message: "disconnected"}, // old daemon: no addr
	}
	m.tab = groupBots
	r := rows(m.View())
	// Callers stay on top; the bot log replaces the callers' log below.
	if !strings.Contains(r[6], "J0hnny A1pha") || !strings.HasPrefix(r[8], "   └") {
		t.Errorf("caller box: %q / %q", r[6], r[8])
	}
	if !strings.Contains(r[10], "▄▀▄ Callers  Bots 1  Events 2 ▄▀▄") {
		t.Errorf("tabs: %q", r[10])
	}
	if !strings.HasPrefix(r[11], "   │Time      Address           Event") {
		t.Errorf("bot log header: %q", r[11])
	}
	if !strings.HasPrefix(r[12], "   │09:00:00  45.33.32.156      Connected") || !strings.Contains(r[12], " 3    │") {
		t.Errorf("bot row: %q", r[12])
	}
	if !strings.HasPrefix(r[13], "   │09:00:02  node 7            Disconnected") {
		t.Errorf("addr-less bot row: %q", r[13])
	}
	if strings.Contains(m.View(), "Menu: MAIN") {
		t.Error("caller activity must not show in the bot log")
	}
	// And the callers' log hides the bots.
	m.tab = groupLog
	v := m.View()
	if strings.Contains(v, "45.33.32.156") || strings.Contains(v, "node 7") || !strings.Contains(v, "Menu: MAIN") {
		t.Errorf("callers log leaked bot rows:\n%s", v)
	}
	m.events = nil
	m.tab = groupBots
	if !strings.Contains(m.View(), "(no bot activity yet)") {
		t.Error("empty bot log placeholder missing")
	}
}

func TestViewEventsTab(t *testing.T) {
	m := makeModel(Options{}, 80, 25)
	m.snapshot = mockupSnapshot(m.now())
	m.tab = groupEvents
	r := rows(m.View())
	if !strings.HasPrefix(r[11], "   │Event               Schedule       Next Run     Last Run     Status") {
		t.Errorf("events header: %q", r[11])
	}
	if !strings.Contains(r[12], "Nightly toss        0 3 * * *      ") || !strings.Contains(r[12], "ok 1.2s") {
		t.Errorf("event row 1: %q", r[12])
	}
	if !strings.Contains(r[13], "Purge old files     @weekly        -            -            disabled") {
		t.Errorf("event row 2: %q", r[13])
	}
	if strings.Contains(r[24], "kick") {
		t.Errorf("events tab must not offer kick: %q", r[24])
	}
	// The cursor lives in the lower box now, so the caller rows lose their highlight
	// and Enter opens the event.
	m.mode = modeDetails
	v := m.View()
	for _, want := range []string{"Event Nightly toss", "0 3 * * *", "Runs:", "40", "success"} {
		if !strings.Contains(v, want) {
			t.Errorf("event details missing %q:\n%s", want, v)
		}
	}
}

func TestViewEmptyCallersShowsWaiting(t *testing.T) {
	m := makeModel(Options{}, 80, 25)
	m.snapshot = mockupSnapshot(m.now())
	m.snapshot.Nodes = nil
	r := rows(m.View())
	if !strings.Contains(r[6], "...waiting...") || !strings.HasPrefix(r[7], "   └") {
		t.Errorf("empty callers: %q / %q", r[6], r[7])
	}
}

func TestViewLogKeepsMinimumRowsAndTableScrolls(t *testing.T) {
	m := makeModel(Options{}, 80, 25)
	snap := mockupSnapshot(m.now())
	snap.Nodes = nil
	for i := 1; i <= 15; i++ {
		snap.Nodes = append(snap.Nodes, admin.NodeState{NodeID: i, Handle: "User" + string(rune('A'+i-1)), Status: admin.StatusOnline, ConnectedAt: m.now()})
	}
	m.snapshot = snap
	g := layout(80, 25, 15)
	if g.eventRows != minLogRows || g.topRows != 10 {
		t.Fatalf("layout with 15 callers: %+v", g)
	}
	m.selected = 12 // UserM, below the 10 visible rows
	v := m.View()
	if !strings.Contains(v, "UserM") || strings.Contains(v, "UserA ") {
		t.Errorf("expected the table to scroll to the selection:\n%s", v)
	}
}

func TestViewShowsPendingReloads(t *testing.T) {
	m := makeModel(Options{}, 120, 30)
	m.snapshot = mockupSnapshot(m.now())
	m.snapshot.PendingReloads = []string{"file_areas.json"}
	got := m.View()
	if !strings.Contains(got, "RELOAD PENDING") || !strings.Contains(got, "file_areas.json") {
		t.Errorf("header missing pending-reload notice; got:\n%s", got)
	}
	m.snapshot.PendingReloads = nil
	if strings.Contains(m.View(), "RELOAD PENDING") {
		t.Error("pending-reload notice shown with an empty queue")
	}
}

func TestViewNilSnapshotNoPanic(t *testing.T) {
	m := makeModel(Options{}, 100, 30)
	got := m.View()
	if !strings.Contains(got, "ViSiON/3 WFC") || !strings.Contains(got, "waiting for first snapshot") {
		t.Errorf("nil snapshot view:\n%s", got)
	}
	for _, mode := range []viewMode{modeDetails, modeConfirmKick} {
		m.mode = mode
		if m.View() == "" {
			t.Errorf("mode %v with nil snapshot must still render", mode)
		}
	}
}

func TestViewOfflineState(t *testing.T) {
	m := makeModel(Options{Dial: func(context.Context) (admin.AdminClient, error) { return nil, nil }}, 100, 30)
	m.snapshot = mockupSnapshot(m.now())
	m.conn = connLost
	m.nextRetryAt = m.now().Add(4 * time.Second)
	r := rows(m.View())
	if !strings.Contains(r[0], "OFFLINE - retry in 4s") {
		t.Errorf("title: %q", r[0])
	}
	if !strings.Contains(r[29], "[R] retry now") || strings.Contains(r[29], "kick") {
		t.Errorf("offline command bar: %q", r[29])
	}
	m.conn = connConnecting
	if !strings.Contains(rows(m.View())[0], "connecting...") {
		t.Error("connecting state not shown")
	}
	// Without a dialer there is no retry to offer.
	m.opts.Dial = nil
	m.conn = connLost
	r = rows(m.View())
	if !strings.Contains(r[0], "OFFLINE") || strings.Contains(r[29], "retry") {
		t.Errorf("no-dialer offline: %q / %q", r[0], r[29])
	}
}

func TestViewStaleFeedFlagged(t *testing.T) {
	m := makeModel(Options{}, 100, 30)
	m.snapshot = mockupSnapshot(m.now())
	m.lastSnapAt = m.now().Add(-7 * time.Second)
	if !strings.Contains(rows(m.View())[0], "stale 7s") {
		t.Errorf("title: %q", rows(m.View())[0])
	}
}

func TestViewReadOnlyHidesKick(t *testing.T) {
	m := makeModel(Options{ReadOnly: true}, 100, 30)
	m.snapshot = mockupSnapshot(m.now())
	if strings.Contains(m.View(), "kick") {
		t.Error("read-only console must not advertise kick")
	}
	m.mode = modeDetails
	if strings.Contains(m.View(), "kick") {
		t.Error("read-only details bar must not advertise kick")
	}
}

func TestViewKickConfirmBar(t *testing.T) {
	m := makeModel(Options{}, 100, 30)
	m.snapshot = mockupSnapshot(m.now())
	m.selected = 1
	m.kickTarget = m.snapshot.Nodes[1]
	m.mode = modeConfirmKick
	last := rows(m.View())[29]
	if !strings.Contains(last, "Disconnect The Wolverine on node 2? [Y] yes [N] no") {
		t.Errorf("confirm bar: %q", last)
	}
}

func TestViewStatusLineReplacesBar(t *testing.T) {
	m := makeModel(Options{}, 100, 30)
	m.setStatus("Kicked X (node 1)", false)
	last := rows(m.View())[29]
	if !strings.Contains(last, "Kicked X (node 1)") || strings.Contains(last, "select") {
		t.Errorf("status bar: %q", last)
	}
}

func TestDetailsOverlayShowsNodeFields(t *testing.T) {
	m := makeModel(Options{}, 100, 30)
	m.snapshot = &admin.SystemSnapshot{Time: time.Now(), MaxNodes: 4, Nodes: []admin.NodeState{{
		NodeID: 3, Handle: "SysopJoe", Status: admin.StatusOnline, RemoteAddr: "192.168.1.42:2323",
		CurrentMenu: "MAIN", Activity: "reading messages", AccessLevel: 255,
		ConnectedAt: m.now().Add(-30 * time.Minute), LastActivity: m.now().Add(-5 * time.Minute), TimeLeftMins: 60,
	}}}
	m.mode = modeDetails
	got := m.View()
	for _, want := range []string{"Node 3", "SysopJoe", "192.168.1.42:2323", "MAIN", "reading messages", "255", "30m", "60m", "[ESC] back", "[K] kick"} {
		if !strings.Contains(got, want) {
			t.Errorf("details missing %q; got:\n%s", want, got)
		}
	}
}

func TestViewEventsNewestLastAndLabelled(t *testing.T) {
	m := makeModel(Options{}, 80, 25)
	m.snapshot = mockupSnapshot(m.now())
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.Local)
	m.events = []admin.Event{
		{Time: base, Type: admin.EventCallerLoggedIn, NodeID: 2, Handle: "Zed", Message: "logged in"},
		{Time: base.Add(time.Second), Type: admin.EventMenuChanged, Handle: "Zed", Message: "MAIN"},
		{Time: base.Add(2 * time.Second), Type: admin.EventNodeKicked, Handle: "Zed", Message: "kicked by sysop"},
		{Time: base.Add(3 * time.Second), Type: eventConsole, Handle: "WFC", Message: "Kicked Zed (node 1)"},
	}
	v := m.View()
	for _, want := range []string{"Zed           Logged on", "Zed           Menu: MAIN", "Zed           Kicked by sysop", "WFC           Kicked Zed (node 1)"} {
		if !strings.Contains(v, want) {
			t.Errorf("event feed missing %q:\n%s", want, v)
		}
	}
	if strings.Index(v, "Logged on") > strings.Index(v, "Kicked Zed") {
		t.Error("events must be oldest first")
	}
}

func TestTitleStateNeverOverwritesLongBoardName(t *testing.T) {
	m := makeModel(Options{Version: "1.0.0", Dial: func(context.Context) (admin.AdminClient, error) { return nil, nil }}, 80, 25)
	m.snapshot = mockupSnapshot(m.now())
	m.snapshot.SystemName = strings.Repeat("Broken Bit Syndicate ", 3) + "BBS" // 66 columns
	m.nextRetryAt = m.now().Add(4 * time.Second)
	m.drops = 3
	states := []struct {
		name string
		set  func()
	}{
		{"connecting", func() { m.conn = connConnecting }},
		{"offline", func() { m.conn = connLost }},
		{"offline-no-dial", func() { m.conn = connLost; m.opts.Dial = nil }},
		{"stale", func() { m.conn = connConnected; m.lastSnapAt = m.now().Add(-9 * time.Second) }},
		{"drops", func() { m.conn = connConnected; m.lastSnapAt = m.now() }},
		{"plain", func() { m.drops = 0 }},
	}
	for _, st := range states {
		st.set()
		r0 := rows(m.View())[0]
		if !strings.Contains(r0, m.snapshot.SystemName) || runeCount(r0) != 80 {
			t.Errorf("%s: board name damaged: %q", st.name, r0)
		}
	}
}

func TestTitleShowsDropCount(t *testing.T) {
	m := makeModel(Options{Version: "1.0.0"}, 80, 25)
	m.snapshot = mockupSnapshot(m.now())
	if !strings.Contains(rows(m.View())[0], "WFC v1.0.0 ") {
		t.Errorf("title: %q", rows(m.View())[0])
	}
	m.drops = 1
	m.lastDropAt = time.Date(2026, 9, 17, 7, 1, 0, 0, time.Local)
	// At 80 columns the full wording would touch the name, so it shortens.
	r0 := rows(m.View())[0]
	if !strings.Contains(r0, "Broken Bit Syndicate") || !strings.HasSuffix(r0, "WFC v1.0.0 - 1 drop ") || strings.Contains(r0, "last") {
		t.Errorf("title with drops at 80 cols: %q", r0)
	}
	m.drops = 12
	m.width, m.height = 110, 34
	r0 = rows(m.View())[0]
	if !strings.HasSuffix(r0, "WFC v1.0.0 - 12 drops, last 07:01 ") || !strings.Contains(r0, "Broken Bit Syndicate") {
		t.Errorf("title with drops at 110 cols: %q", r0)
	}
	// A very long board name leaves only the bare tally.
	m.width, m.height = 80, 25
	m.snapshot.SystemName = "The Very Long Name Of A Bulletin Board System"
	r0 = rows(m.View())[0]
	if !strings.Contains(r0, m.snapshot.SystemName) || !strings.HasSuffix(r0, "12 drops ") || strings.Contains(r0, "WFC") || runeCount(r0) != 80 {
		t.Errorf("title clipping: %q", r0)
	}
}

func TestViewTooSmall(t *testing.T) {
	m := makeModel(Options{}, minWidth-1, minHeight-1)
	got := m.View()
	if !strings.Contains(got, "Terminal too small") {
		t.Errorf("too-small view: %q", got)
	}
	if len(rows(got)) != minHeight-1 {
		t.Errorf("too-small view rows = %d", len(rows(got)))
	}
}

func TestViewASCIIModeIsSevenBit(t *testing.T) {
	m := makeModel(Options{ASCII: true}, 100, 30)
	m.snapshot = mockupSnapshot(m.now())
	m.events = []admin.Event{{Time: time.Now(), Type: admin.EventCallerConnected, Handle: "Zoë"}}
	for tab := groupLog; tab < groupCount; tab++ {
		m.tab = tab
		for _, mode := range []viewMode{modeList, modeDetails} {
			m.mode = mode
			for i, r := range m.View() {
				if r > 0x7e && r != '\n' {
					t.Fatalf("tab %v mode %v: non-ASCII rune %U at byte %d", tab, mode, r, i)
				}
			}
		}
	}
	m.tab, m.mode = groupLog, modeList
	if !strings.Contains(m.View(), "[^/v] select") {
		t.Error("ASCII arrows missing from the command bar")
	}
}

func TestWideAndControlRunesCannotShiftColumns(t *testing.T) {
	m := makeModel(Options{}, 80, 25)
	m.snapshot = mockupSnapshot(m.now())
	m.snapshot.Nodes[0].Handle = "漢字\x1b[2Jbad"
	m.snapshot.Nodes[0].Activity = strings.Repeat("x", 200)
	r := rows(m.View())
	for i, row := range r {
		if runeCount(row) != 80 {
			t.Fatalf("row %d width %d: %q", i, runeCount(row), row)
		}
	}
	if !strings.Contains(r[6], "??[2Jbad") || strings.Contains(r[6], "\x1b") {
		t.Errorf("hostile handle not neutralised: %q", r[6])
	}
	if cells := []rune(r[6]); string(cells[71:76]) != "1    " {
		t.Errorf("long activity pushed the node column: %q", r[6])
	}
}

func TestColumnsAlwaysFillInnerWidth(t *testing.T) {
	for w := 72; w <= 240; w++ {
		c := nodeColumnsFor(w)
		if c.handle+c.activity+c.on+c.addr+c.node != w {
			t.Fatalf("node columns for %d sum to %d", w, c.handle+c.activity+c.on+c.addr+c.node)
		}
		e := eventColumnsFor(w)
		if e.name+e.schedule+e.next+e.last+e.status != w {
			t.Fatalf("event columns for %d sum to %d", w, e.name+e.schedule+e.next+e.last+e.status)
		}
		l := logColumnsFor(w)
		if l.time+l.user+l.text != w {
			t.Fatalf("log columns for %d sum to %d", w, l.time+l.user+l.text)
		}
		b := botColumnsFor(w)
		if b.time+b.addr+b.text+b.node != w {
			t.Fatalf("bot columns for %d sum to %d", w, b.time+b.addr+b.text+b.node)
		}
	}
}

func TestLayoutFlexesLogAroundTopBox(t *testing.T) {
	g := layout(80, 25, 2)
	if g.topRows != 2 || g.eventRows != 11 || g.eventY != 9 || g.cmdY != 24 {
		t.Fatalf("80x25 two callers: %+v", g)
	}
	g = layout(80, 25, 0)
	if g.topRows != 1 || g.eventRows != 12 {
		t.Fatalf("80x25 empty: %+v", g)
	}
	g = layout(80, 25, 40)
	if g.topRows != 10 || g.eventRows != minLogRows {
		t.Fatalf("80x25 many callers: %+v", g)
	}
	g = layout(110, 34, 5)
	if g.topRows != 5 || g.eventRows != 17 {
		t.Fatalf("110x34: %+v", g)
	}
}

func TestViewScrolledLogShowsOlderRowsAndMarker(t *testing.T) {
	m := makeModel(Options{}, 80, 25)
	m.snapshot = mockupSnapshot(m.now())
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.Local)
	for i := 0; i < 30; i++ {
		m.events = append(m.events, admin.Event{Time: base.Add(time.Duration(i) * time.Second), Type: admin.EventMenuChanged, Handle: "Zed", Message: fmt.Sprintf("M%02d", i)})
	}
	v := m.View()
	if !strings.Contains(v, "Menu: M29") || strings.Contains(v, "Menu: M05") || strings.Contains(v, "newer") {
		t.Errorf("tail view wrong:\n%s", v)
	}
	m.scrollBack = 10 // 11 rows: shows M09..M19
	v = m.View()
	if !strings.Contains(v, "Menu: M09") || !strings.Contains(v, "Menu: M19") || strings.Contains(v, "Menu: M20") {
		t.Errorf("scrolled view wrong:\n%s", v)
	}
	if !strings.Contains(v, "10 newer - PgDn") {
		t.Errorf("scroll marker missing:\n%s", v)
	}
}

func TestFormatters(t *testing.T) {
	if got := formatUptime(16*3600 + 32*60 + 24); got != "16:32:24" {
		t.Errorf("formatUptime = %q", got)
	}
	if got := formatUptime(2*86400 + 5); got != "2d 00:00:05" {
		t.Errorf("formatUptime days = %q", got)
	}
	cases := map[time.Duration]string{30 * time.Second: "<1m", 4 * time.Minute: "4m", 70 * time.Minute: "1h10m", 49 * time.Hour: "2d1h"}
	for d, want := range cases {
		if got := formatOnline(d); got != want {
			t.Errorf("formatOnline(%v) = %q, want %q", d, got, want)
		}
	}
	if counterText(-1) != "-" || counterText(7) != "7" {
		t.Error("counterText")
	}
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	when := map[time.Time]string{
		{}:                            "-",
		now.Add(3 * time.Hour):        "15:00",
		now.Add(48 * time.Hour):       "Fri 12:00",
		now.Add(-30 * 24 * time.Hour): "Aug 17 12:00",
	}
	for t0, want := range when {
		if got := formatWhen(t0, now); got != want {
			t.Errorf("formatWhen(%v) = %q, want %q", t0, got, want)
		}
	}
}

func TestViewOldDaemonShowsDashesAndEventsNote(t *testing.T) {
	m := makeModel(Options{}, 80, 25)
	m.snapshot = mockupSnapshot(m.now())
	m.snapshot.Schema = 0 // a daemon from before these fields existed
	m.snapshot.ScheduledEvents = nil
	r := rows(m.View())
	for _, want := range []string{" Users: - ", " New: - ", " Mail: - ", " Calls: 123 "} {
		if !strings.Contains(r[2], want) {
			t.Errorf("old-daemon stats missing %q: %q", want, r[2])
		}
	}
	m.tab = groupEvents
	if !strings.Contains(m.View(), "predates the Events tab") {
		t.Error("events tab should explain that the daemon is too old")
	}
	m.snapshot.Schema = admin.SnapshotSchema
	if !strings.Contains(m.View(), "(no scheduled events)") {
		t.Error("current daemon with no events should say so")
	}
}

func TestHostOnly(t *testing.T) {
	cases := map[string]string{
		"108.216.156.236:49354": "108.216.156.236",
		"[2001:db8::1]:2222":    "2001:db8::1",
		"10.0.0.5":              "10.0.0.5",
		"":                      "",
	}
	for in, want := range cases {
		if got := hostOnly(in); got != want {
			t.Errorf("hostOnly(%q) = %q, want %q", in, got, want)
		}
	}
}
