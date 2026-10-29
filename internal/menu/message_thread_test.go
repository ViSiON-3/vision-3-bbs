package menu

import (
	"bytes"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor/testterm"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"golang.org/x/term"
)

// sampleThreadMsg is one message of the design handoff's sample area.
type sampleThreadMsg struct {
	from, to, subject, body string
	parent                  int
	private, deleted        bool
}

// sampleThread is the handoff's sample data: a chain four deep
// (#1 > #3 > #5 > #7 > #10), siblings (#3 and #4 answer #1; #3 has #5 and
// #11), a deleted message (#4) with a live reply (#8), someone else's
// private reply (#9), a handle longer than 16 characters, and three more
// threads. #15 answers the private #9 and so has a hidden parent.
var sampleThread = []sampleThreadMsg{
	{from: "PhosphorDotMatrix", to: "All", subject: "Topaz font in SyncTERM", body: "Anyone got Topaz showing right in SyncTERM 1.5? I set the font in the dialing directory.\nbody-1"},
	{from: "GrimmTheTosser", to: "All", subject: "Binkd poll schedule", body: "Moving my hub poll to every 30 minutes.\nbody-2"},
	{from: "Kestrel_of_the_Wastes", to: "PhosphorDotMatrix", subject: "Re: Topaz font in SyncTERM", parent: 1, body: " Ph> Anyone got Topaz showing right in SyncTERM 1.5?\n\nIt's both. SyncTERM only switches fonts when the BBS asks for it.\nbody-3"},
	{from: "deadbeef", to: "PhosphorDotMatrix", subject: "Re: Topaz font in SyncTERM", parent: 1, deleted: true, body: "just use the web client\nbody-4"},
	{from: "PhosphorDotMatrix", to: "Kestrel_of_the_Wastes", subject: "Re: Topaz font in SyncTERM", parent: 3, body: " Ke> Check that the entry isn't set to 'Auto'.\n\nThat was it. Font sticks now, but the menus are shifted one column right.\nbody-5"},
	{from: "Lux", to: "All", subject: "Wanted: old .ANS packs", body: "Looking for ACiD and iCE packs from '94-'96.\nbody-6"},
	{from: "Mercury Retrograde", to: "PhosphorDotMatrix", subject: "Re: Topaz font in SyncTERM", parent: 5, body: " Ph> the menus are shifted one column right\n\nThat's the 80th column wrapping. The art is a full 80 wide.\nbody-7"},
	{from: "Lux", to: "deadbeef", subject: "Re: Topaz font in SyncTERM", parent: 4, body: " de> just use the web client\n\nIt does, but it can't do ZMODEM, so it's no good for the file areas.\nbody-8"},
	{from: "n0name", to: "Kestrel_of_the_Wastes", subject: "Re: Topaz font in SyncTERM", parent: 5, private: true, body: "private\nbody-9"},
	{from: "Kestrel_of_the_Wastes", to: "Mercury Retrograde", subject: "Re: Topaz font in SyncTERM", parent: 7, body: " Me> Turn off auto-wrap or trim the art to 79.\n\nOr save it with a SAUCE record that says 80 columns.\nbody-10"},
	{from: "GrimmTheTosser", to: "Kestrel_of_the_Wastes", subject: "Re: Topaz font in SyncTERM", parent: 3, body: " Ke> SyncTERM only switches fonts when the BBS asks for it\n\nWhich BBS packages actually send that sequence? Mine doesn't seem to.\nbody-11"},
	{from: "Mercury Retrograde", to: "All", subject: "Reddit gateway lag", body: "Posts from the Reddit areas are arriving about six hours late.\nbody-12"},
	{from: "Kestrel_of_the_Wastes", to: "Mercury Retrograde", subject: "Re: Reddit gateway lag", parent: 12, body: " Me> about six hours late since Monday\n\nSame here. Looks like the upstream poller is backing off.\nbody-13"},
	{from: "PhosphorDotMatrix", to: "Lux", subject: "Re: Topaz font in SyncTERM", parent: 8, body: " Lu> it can't do ZMODEM\n\nFair. Sticking with SyncTERM then.\nbody-14"},
	{from: "Lux", to: "n0name", subject: "Re: Topaz font in SyncTERM", parent: 9, body: " n0> (private)\n\nAnswering a private note in public, as one does.\nbody-15"},
}

// seedSampleThread posts sampleThread into GENERAL and flags #4 deleted
// without packing, as an imported deletion looks before the next pack.
func (env *menuEnv) seedSampleThread() {
	env.t.Helper()
	mm := env.e.MessageMgr
	for i, m := range sampleThread {
		n, err := mm.Post(generalAreaID, m.from, m.to, m.subject, m.body, message.PostOptions{
			Private: m.private, ReplyToNum: m.parent,
		})
		if err != nil {
			env.t.Fatalf("seed #%d: %v", i+1, err)
		}
		if n != i+1 {
			env.t.Fatalf("seed #%d landed as #%d", i+1, n)
		}
	}
	for i, m := range sampleThread {
		if m.deleted {
			if err := mm.DeleteMessage(generalAreaID, i+1); err != nil {
				env.t.Fatalf("delete #%d: %v", i+1, err)
			}
		}
	}
}

// sampleNodes is sampleThread's reply tree, as ReplyTree would build it.
func sampleNodes() map[int]*message.ReplyNode {
	nodes := map[int]*message.ReplyNode{}
	for i, m := range sampleThread {
		nodes[i+1] = &message.ReplyNode{Num: i + 1, Parent: m.parent, Deleted: m.deleted}
	}
	for i := range sampleThread {
		if p := nodes[i+1].Parent; p != 0 {
			nodes[p].Children = append(nodes[p].Children, i+1)
		}
	}
	return nodes
}

// sampleLoad returns sampleThread's messages as viewer may read them.
func sampleLoad(viewer string) func(int) *message.DisplayMessage {
	return func(n int) *message.DisplayMessage {
		if n < 1 || n > len(sampleThread) {
			return nil
		}
		m := sampleThread[n-1]
		dm := &message.DisplayMessage{MsgNum: n, From: m.from, To: m.to, Subject: m.subject, Body: m.body,
			IsPrivate: m.private, IsDeleted: m.deleted, ReplyToNum: m.parent, DateTime: time.Date(2026, 10, 19+n/4, 8, 0, 0, 0, time.UTC)}
		if !dm.VisibleTo(viewer) {
			return nil
		}
		return dm
	}
}

func rowNums(rows []threadRow) []int {
	out := make([]int, len(rows))
	for i, r := range rows {
		out[i] = r.Num
	}
	return out
}

// The sample thread flattens depth-first with children ascending, the
// private #9 left out and its reply #15 hoisted under #5, and the deleted #4
// kept as a placeholder because #8 answers it.
func TestThreadRowsOrderAndPrefixes(t *testing.T) {
	rows := threadRows(sampleNodes(), []int{1}, sampleLoad("Caller"))
	want := []struct {
		num    int
		depth  int
		prefix string
	}{
		{1, 0, ""},
		{3, 1, "├─"},
		{5, 2, "│ ├─"},
		{7, 3, "│ │ ├─"},
		{10, 4, "│ │ │ └─"},
		{15, 3, "│ │ └─"},
		{11, 2, "│ └─"},
		{4, 1, "└─"},
		{8, 2, "  └─"},
		{14, 3, "    └─"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %v, want %d rows", rowNums(rows), len(want))
	}
	for i, w := range want {
		r := rows[i]
		if r.Num != w.num || r.Depth != w.depth || r.Prefix != w.prefix {
			t.Errorf("row %d = #%d depth %d %q, want #%d depth %d %q", i, r.Num, r.Depth, r.Prefix, w.num, w.depth, w.prefix)
		}
		if r.Deleted != (w.num == 4) {
			t.Errorf("row #%d deleted = %v", r.Num, r.Deleted)
		}
	}
	if rows[5].Parent != 5 {
		t.Errorf("#15 (reply to the hidden #9) has parent %d, want 5", rows[5].Parent)
	}
	for _, r := range rows {
		if r.Num == 9 {
			t.Errorf("the private #9 is shown")
		}
	}
	// The recipient of #9 sees it in place, and #15 under it.
	rows = threadRows(sampleNodes(), []int{1}, sampleLoad("Kestrel_of_the_Wastes"))
	if got := rowNums(rows); !reflect.DeepEqual(got, []int{1, 3, 5, 7, 10, 9, 15, 11, 4, 8, 14}) {
		t.Errorf("recipient's rows = %v", got)
	}
}

// A deleted message nothing readable descends from is not a row, and a
// hidden root's readable replies each start a thread of their own.
func TestThreadRowsPrunesAndReparents(t *testing.T) {
	nodes := map[int]*message.ReplyNode{
		1: {Num: 1, Children: []int{2, 3}},
		2: {Num: 2, Parent: 1, Deleted: true},
		3: {Num: 3, Parent: 1},
	}
	rows := threadRows(nodes, []int{1}, sampleLoad("Caller"))
	if got := rowNums(rows); !reflect.DeepEqual(got, []int{1, 3}) {
		t.Errorf("rows = %v, want [1 3]: the deleted leaf #2 is nobody's parent", got)
	}

	hidden := map[int]*message.ReplyNode{
		9:  {Num: 9, Children: []int{15, 16}},
		15: {Num: 15, Parent: 9},
		16: {Num: 16, Parent: 9},
	}
	tv := newThreadView(hidden, 15, sampleLoad("Caller"))
	if got := rowNums(tv.rows); !reflect.DeepEqual(got, []int{15}) {
		t.Errorf("rows around #15 under a hidden root = %v, want [15]", got)
	}
	if r := tv.row(15); r == nil || r.Parent != 0 {
		t.Errorf("#15 should be a root of its shown tree: %+v", r)
	}
}

// The thread view's helpers over the sample thread, seen by a caller who
// may not read #9.
func TestThreadViewNavigation(t *testing.T) {
	tv := newThreadView(sampleNodes(), 5, sampleLoad("Caller"))
	if got := tv.order(); !reflect.DeepEqual(got, []int{1, 3, 5, 7, 10, 15, 11, 8, 14}) {
		t.Errorf("order = %v", got)
	}
	if n := tv.directReplies(5); n != 2 {
		t.Errorf("directReplies(5) = %d, want 2 (#7 and the hoisted #15; not the private #9)", n)
	}
	if n := tv.directReplies(1); n != 1 {
		t.Errorf("directReplies(1) = %d, want 1 (#3; the deleted #4 is not a reply)", n)
	}
	if pos, count := tv.position(5); pos != 3 || count != 9 {
		t.Errorf("position(5) = %d of %d, want 3 of 9", pos, count)
	}
	if d := tv.depth(10); d != 4 {
		t.Errorf("depth(10) = %d, want 4", d)
	}
	if !tv.hasThread() {
		t.Error("hasThread should be true in a thread of nine")
	}
	alone := newThreadView(sampleNodes(), 2, sampleLoad("Caller"))
	if alone.hasThread() {
		t.Error("an unanswered root has no thread to show")
	}
}

// In the thread menu the arrows, Home and End name the readable message to
// read next, in display order, stepping over the #4 placeholder; at an end
// they go nowhere. Tab, Enter, T and Esc give focus back; other keys are
// left to the reader.
func TestThreadMenuKey(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cur, key int
		handled  bool
		target   int
		back     bool
	}{
		{"down", 11, editor.KeyArrowDown, true, 8, false},
		{"up", 11, editor.KeyArrowUp, true, 15, false},
		{"up at the top goes nowhere", 1, editor.KeyArrowUp, true, 0, false},
		{"down at the bottom goes nowhere", 14, editor.KeyArrowDown, true, 0, false},
		{"home", 10, editor.KeyHome, true, 1, false},
		{"end", 3, editor.KeyEnd, true, 14, false},
		{"Tab", 7, editor.KeyTab, true, 0, true},
		{"Enter", 7, editor.KeyEnter, true, 0, true},
		{"T", 7, 'T', true, 0, true},
		{"Esc", 7, editor.KeyEsc, true, 0, true},
		{"N is the reader's", 7, 'N', false, 0, false},
	} {
		tv := newThreadView(sampleNodes(), tc.cur, sampleLoad("Caller"))
		handled, target, back := threadMenuKey(tv, tc.key)
		if handled != tc.handled || target != tc.target || back != tc.back {
			t.Errorf("%s: handled %v target %d back %v; want %v %d %v", tc.name, handled, target, back, tc.handled, tc.target, tc.back)
		}
	}
}

// isDarkGrey reports whether c is drawn in dark grey (pipe colour 8).
func isDarkGrey(c testterm.Cell) bool { return c.Fg == 90 || (c.Fg == 30 && c.Bold) }

// isBrightCyan reports whether c is drawn in bright cyan (pipe colour 11).
func isBrightCyan(c testterm.Cell) bool { return c.Fg == 96 || (c.Fg == 36 && c.Bold) }

// visibleWidth is the column count of a pipe-coded line as the terminal
// receives it: expanded, then put through the output writer for mode, which
// turns the UTF-8 box characters into single CP437 bytes on a CP437 session.
func visibleWidth(line string, mode ansi.OutputMode) int {
	var out bytes.Buffer
	if err := terminalio.WriteProcessedBytes(&out, ansi.ReplacePipeCodes([]byte(line)), mode); err != nil {
		panic(err)
	}
	stripped := testAnsiEscape.ReplaceAllString(out.String(), "")
	if mode == ansi.OutputModeCP437 {
		return len(stripped)
	}
	return utf8.RuneCountInString(stripped)
}

// The thread menu's key row fills exactly the row in both encodings.
func TestRenderThreadMenuKeys(t *testing.T) {
	for _, mode := range []ansi.OutputMode{ansi.OutputModeUTF8, ansi.OutputModeCP437} {
		row := renderThreadMenuKeys(79, "|08", "Message View", threadGlyphsFor(mode))
		if w := visibleWidth(row, mode); w != 79 {
			t.Errorf("%v: width = %d, want 79: %q", mode, w, row)
		}
	}
	for label, want := range map[string]string{
		"Close":        "─[↑↓] Move ─[Tab] Close ──",
		"Message View": "─[↑↓] Move ─[Tab] Message View ──",
	} {
		stripped := testAnsiEscape.ReplaceAllString(string(ansi.ReplacePipeCodes([]byte(renderThreadMenuKeys(79, "|08", label, threadGlyphsFor(ansi.OutputModeUTF8))))), "")
		if !strings.HasPrefix(stripped, want) {
			t.Errorf("keys = %q, want prefix %q", stripped, want)
		}
	}
	hint := testAnsiEscape.ReplaceAllString(string(ansi.ReplacePipeCodes([]byte(renderThreadViewHint(79)))), "")
	if !strings.HasPrefix(hint, "─[Tab] Thread View ──") || utf8.RuneCountInString(hint) != 79 {
		t.Errorf("hint = %q", hint)
	}
}

// The pane takes five rows, then three, then none, as the body shrinks:
// with its title, closing rule and blank row it needs eight rows for five,
// six for three, and the body keeps at least six. With a six-row header, a
// 24-row terminal has 15 body lines, a 21-row one 12 and a 20-row one 11.
func TestPaneRowsFor(t *testing.T) {
	for _, tc := range []struct{ termHeight, want int }{{25, 5}, {24, 5}, {23, 5}, {22, 3}, {21, 3}, {20, 0}, {16, 0}} {
		bodyAvail := tc.termHeight - 7 - 2
		if got := paneRowsFor(bodyAvail); got != tc.want {
			t.Errorf("termHeight %d (body %d): pane rows = %d, want %d", tc.termHeight, bodyAvail, got, tc.want)
		}
	}
}

// Every pane line is exactly the width asked for, in both encodings, with
// the sample's long handles and a deleted placeholder on screen.
func TestRenderThreadPaneWidth(t *testing.T) {
	for _, mode := range []ansi.OutputMode{ansi.OutputModeUTF8, ansi.OutputModeCP437} {
		g := threadGlyphsFor(mode)
		for _, cur := range []int{1, 5, 8, 14} {
			tv := newThreadView(sampleNodes(), cur, sampleLoad("Caller"))
			for _, rows := range []int{5, 3} {
				lines := renderThreadPane(tv, tv.cur, rows, 79, " Thread ", "Close", g, mode)
				if len(lines) != rows+2 {
					t.Fatalf("%v #%d rows %d: %d lines", mode, cur, rows, len(lines))
				}
				for i, line := range lines {
					if w := visibleWidth(line, mode); w != 79 {
						t.Errorf("%v #%d rows %d line %d: width %d, want 79: %q", mode, cur, rows, i, w, line)
					}
				}
			}
		}
	}
	// Depth past eight collapses to a count.
	deep := threadRow{Num: 99, Depth: 11, Prefix: strings.Repeat("│ ", 10) + "└─", Msg: sampleLoad("Caller")(10)}
	line := testAnsiEscape.ReplaceAllString(string(ansi.ReplacePipeCodes([]byte(renderTreeRow(deep, false, false, false, 79, threadGlyphsFor(ansi.OutputModeUTF8), ansi.OutputModeUTF8)))), "")
	if !strings.Contains(line, "              +4#99 ") {
		t.Errorf("deep row = %q, want the prefix collapsed to +4", line)
	}
	if utf8.RuneCountInString(line) != 79 {
		t.Errorf("deep row width = %d", utf8.RuneCountInString(line))
	}
}

func TestExcerpt(t *testing.T) {
	for body, want := range map[string]string{
		" Ke> quoted\n\nThat was it. Font sticks now.\nmore":    "That was it. Font sticks now.",
		"> bare quote\n--- tear\n * Origin: here (1:2/3)\nText": "Text",
		"\r\n  \r\nFirst real line\r\n":                         "First real line",
		" Ke> only a quote":                                     "",
		"":                                                      "",
	} {
		if got := excerpt(body); got != want {
			t.Errorf("excerpt(%q) = %q, want %q", body, got, want)
		}
	}
}

// screenRun runs a RUN: command for u on an 80x24 emulated terminal,
// feeding keys, and returns the screen as it was left.
func (env *menuEnv) screenRun(cmd string, u *user.User, keys string) *testterm.Term {
	env.t.Helper()
	return env.screenRunHeight(cmd, u, keys, 24)
}

// screenRunHeight is screenRun on an 80-column terminal of height rows.
func (env *menuEnv) screenRunHeight(cmd string, u *user.User, keys string, height int) *testterm.Term {
	env.t.Helper()
	fn, ok := env.e.RunRegistry[cmd]
	if !ok {
		env.t.Fatalf("no runnable registered as %q", cmd)
	}
	var opts []testterm.Option
	if env.outputMode == ansi.OutputModeCP437 {
		opts = append(opts, testterm.CP437())
	}
	screen := testterm.New(80, height, opts...)
	sess := testterm.NewSession(screen, keys)
	SetSessionOutputMode(sess, env.outputMode)
	env.t.Cleanup(func() {
		resetSessionIH(sess)
		ClearSessionOutputMode(sess)
	})
	c := &cmdCtx{
		e: env.e, s: sess, terminal: term.NewTerminal(sess, ""), userManager: env.um, currentUser: u,
		nodeNumber: 1, sessionStartTime: time.Now(), outputMode: env.outputMode, termWidth: 80, termHeight: height,
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = fn(c, "")
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		env.t.Fatalf("%s still running 10s after its input ran out", cmd)
	}
	return screen
}

// readerAt opens READMSGS on message n of the sample thread (all read, so
// the reader asks which) and then feeds keys. The screen is as the last
// redraw left it: the reader's Q leaves the screen alone.
func (env *menuEnv) readerAt(n int, keys string) *testterm.Term {
	env.t.Helper()
	env.markRead(generalAreaID, "Caller", len(sampleThread))
	return env.screenRun("READMSGS", env.caller, fmt.Sprintf("%d\r%sQ", n, keys))
}

// The reader on #5 of the sample thread: the header counts two direct
// replies (#7 and the hoisted #15), the pane sits under the header with
// the current row marked and windowed two from the top, the body follows,
// and the separator names Tab while the thread menu is closed.
func TestMessageReaderThreadPaneLayout(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	screen := env.readerAt(5, "")
	snap := screen.Snapshot()

	// Header rows 1-6 (MSGHDR.2), pane title at 7, tree rows 8-12, rule 13.
	if !strings.Contains(screen.Row(2), "Reading message #5 ") {
		t.Fatalf("not on #5:\n%s", snap)
	}
	title := screen.Row(7)
	if !strings.HasPrefix(title, "── Thread ─ Topaz font in SyncTERM ") || !strings.HasSuffix(strings.TrimRight(title, " "), " 3 of 9 · depth 2 ──") {
		t.Errorf("pane title = %q", title)
	}
	wantRows := []string{
		"  #1 Phosphor… Topaz font in SyncTERM  ",
		"  ├─#3 Kestrel_… It's both. SyncTERM only switches fonts when the BB…  ",
		"► │ ├─#5 Phosphor… That was it. Font sticks now, but the menus are s…  ",
		"  │ │ ├─#7 Mercury … That's the 80th column wrapping. The art is a f…  ",
		"  │ │ │ └─#10 Kestrel_… Or save it with a SAUCE record that says 80 …  ",
	}
	for i, want := range wantRows {
		if row := screen.Row(8 + i); !strings.HasPrefix(row, want) {
			t.Errorf("pane row %d = %q\nwant prefix %q", i, row, want)
		}
	}
	for i := 0; i < 5; i++ {
		if row := screen.Row(8 + i); !strings.HasSuffix(strings.TrimRight(row, " "), "/26") {
			t.Errorf("pane row %d has no date: %q", i, row)
		}
	}
	if rule := strings.TrimRight(screen.Row(13), " "); rule != strings.Repeat("─", 79) {
		t.Errorf("closing rule = %q", rule)
	}
	if blank := strings.TrimSpace(screen.Row(14)); blank != "" {
		t.Errorf("row 14 should be blank between the pane and the body: %q", blank)
	}
	if !strings.Contains(screen.Row(15), "Ke> Check that the entry") {
		t.Errorf("body should start on row 15:\n%s", snap)
	}
	if marker := screen.Cell(10, 1); marker.Rune != '►' {
		t.Errorf("row 10 col 1 = %q, want the marker", marker.Rune)
	}
	// With the menu closed no row is barred: the blue bar is the menu's
	// cursor, and the frame is dark grey.
	for row := 8; row <= 12; row++ {
		if bg := screen.Cell(row, 40).Bg; bg == 44 {
			t.Errorf("row %d is barred blue with the menu closed", row)
		}
	}
	// Every drawn character of the closed pane, frame, text and marker,
	// is dark grey.
	for row := 7; row <= 13; row++ {
		for col := 1; col <= 79; col++ {
			if c := screen.Cell(row, col); c.Rune != ' ' && !isDarkGrey(c) {
				t.Fatalf("row %d col %d %q drawn fg %d bold %v, want dark grey", row, col, c.Rune, c.Fg, c.Bold)
			}
		}
	}
	if sep := screen.Row(23); !strings.HasPrefix(sep, "─[Tab] Thread View ──") {
		t.Errorf("separator = %q, want the Tab hint", sep)
	}
	if !strings.Contains(screen.Row(24), "Next") {
		t.Errorf("lightbar missing:\n%s", snap)
	}
}

// A message that starts a thread nobody answered shows no pane, and the
// body keeps the rows; the separator row is a plain rule.
func TestMessageReaderNoPaneWithoutThread(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	screen := env.readerAt(2, "")
	if strings.Contains(screen.Snapshot(), "── Thread ─") {
		t.Errorf("pane shown for an unanswered root:\n%s", screen.Snapshot())
	}
	if !strings.Contains(screen.Row(7), "Moving my hub poll") {
		t.Errorf("body should start right under the header:\n%s", screen.Snapshot())
	}
	if signs := strings.TrimRight(screen.Row(23), " "); signs != strings.Repeat("─", 79) {
		t.Errorf("separator = %q, want a plain rule", signs)
	}
}

// The only reply to a deleted root has nothing else to show: no pane, and
// T does not open the thread menu.
func TestMessageReaderLoneReplyToDeletedRoot(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	if err := env.e.MessageMgr.DeleteMessage(generalAreaID, 12); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	screen := env.readerAt(13, "T")
	if strings.Contains(screen.Snapshot(), "── Thread ─") || strings.Contains(screen.Snapshot(), "Close") {
		t.Errorf("pane or menu shown with nothing else to show:\n%s", screen.Snapshot())
	}
}

// A thread shorter than the pane takes only the rows it fills: #12 and its
// one reply draw a title, two rows and the closing rule, then the blank
// row, and the body starts right under it, with no blank pane rows.
func TestMessageReaderPaneShrinksToThread(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	screen := env.readerAt(13, "")
	if !strings.Contains(screen.Row(7), "── Thread ─") {
		t.Fatalf("pane title not under the header:\n%s", screen.Snapshot())
	}
	if !strings.Contains(screen.Row(8), "#12 ") || !strings.Contains(screen.Row(9), "#13 ") {
		t.Errorf("tree rows = %q, %q", screen.Row(8), screen.Row(9))
	}
	if rule := strings.TrimRight(screen.Row(10), " "); rule != strings.Repeat("─", 79) {
		t.Errorf("row 10 should close the pane: %q", rule)
	}
	if strings.TrimSpace(screen.Row(11)) != "" || !strings.Contains(screen.Row(12), "six hours late") {
		t.Errorf("body should start right under the pane:\n%s", screen.Snapshot())
	}
}

// The pane rows follow the current message: on #14 (last in the thread)
// the window ends with it; on #8 the deleted #4 shows as a placeholder.
func TestMessageReaderPaneWindowAndPlaceholder(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	screen := env.readerAt(8, "")
	snap := screen.Snapshot()
	if !strings.Contains(snap, "└─#4 deleted") {
		t.Errorf("deleted #4 should be a placeholder row:\n%s", snap)
	}
	screen = env.readerAt(14, "")
	if row := screen.Row(12); !strings.HasPrefix(row, "►     └─#14 Phosphor…") {
		t.Errorf("last row should be #14 marked: %q\n%s", row, screen.Snapshot())
	}
	if !strings.Contains(screen.Row(7), " 9 of 9 · depth 3 ") {
		t.Errorf("title on #14 = %q", screen.Row(7))
	}
}

// The thread menu through the real reader and message base: T or Tab
// gives it focus, each arrow reads the message it moves to with the menu
// left open, and Tab, Enter, T or Esc give focus back to the message
// without leaving the reader.
func TestMessageReaderThreadMenu(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	for _, tc := range []struct {
		name  string
		start int
		keys  string
		at    int // message shown at the end
	}{
		{"down reads the next in the thread", 5, "T\x1b[B", 7},
		{"the menu stays open: down twice", 5, "T\x1b[B\x1b[B", 10},
		{"up steps over the #4 placeholder", 8, "T\x1b[A", 11},
		{"End then Home", 5, "T\x1b[F\x1b[H", 1},
		{"up at the top stays", 1, "T\x1b[A", 1},
		{"Enter gives focus back; Enter is Next again", 5, "T\r\r", 6},
		{"T gives focus back", 5, "TT\r", 6},
		{"Esc gives focus back without leaving the reader", 5, "T\x1bN", 6},
		{"Tab there and back", 5, "\t\x1b[B\t\r", 8},
		{"T on a message with no thread does nothing", 2, "T\r", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := env.sub(t)
			sub.markRead(generalAreaID, "Caller", len(sampleThread))
			r := sub.runCmd("READMSGS", sub.caller, "", fmt.Sprintf("%d\r%sQ", tc.start, tc.keys))
			txt := r.text()
			if last := strings.LastIndex(txt, "Reading message #"); last < 0 || !strings.HasPrefix(txt[last:], fmt.Sprintf("Reading message #%d ", tc.at)) {
				t.Errorf("reader should end on #%d; output:\n%s", tc.at, txt)
			}
		})
	}
}

// Any reader command but Thread closes the menu: after N from #7 the
// threaded #8 opens with the menu closed, so Enter is Next again (to #10,
// past the private #9); after ? the help returns to a closed menu.
func TestMessageReaderOtherCommandsCloseThreadMenu(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	for _, tc := range []struct {
		name  string
		start int
		keys  string
		at    int
	}{
		{"N", 7, "TN\r", 10},
		{"?", 5, "T?\r", 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := env.sub(t)
			sub.markRead(generalAreaID, "Caller", len(sampleThread))
			r := sub.runCmd("READMSGS", sub.caller, "", fmt.Sprintf("%d\r%sQ", tc.start, tc.keys))
			txt := r.text()
			if last := strings.LastIndex(txt, "Reading message #"); last < 0 || !strings.HasPrefix(txt[last:], fmt.Sprintf("Reading message #%d ", tc.at)) {
				t.Errorf("reader should end on #%d; output:\n%s", tc.at, txt)
			}
		})
	}
}

// With no room for the pane (a 20-row screen under a six-row header), Thread
// is greyed out and T does not open an invisible menu: Enter stays Next.
func TestMessageReaderNoThreadMenuWithoutRoom(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	env.markRead(generalAreaID, "Caller", len(sampleThread))
	screen := env.screenRunHeight("READMSGS", env.caller, "5\rT\rQ", 20)
	snap := screen.Snapshot()
	if strings.Contains(snap, "── Thread ") || strings.Contains(snap, "[Tab]") {
		t.Errorf("the pane or menu was drawn without room for it:\n%s", snap)
	}
	if !strings.Contains(screen.Row(2), "Reading message #6 ") {
		t.Errorf("T should do nothing, leaving Enter as Next to #6:\n%s", snap)
	}
}

// With the menu open the message being read is barred blue and marked,
// the frame is bright cyan with the same title, the pane's closing row
// lists the menu's keys, and the bar below is greyed out. Down from #5
// reads #7, two from the top of a window that now starts at #3.
func TestMessageReaderThreadMenuScreen(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	screen := env.readerAt(5, "T\x1b[B")
	if title := screen.Row(7); !strings.HasPrefix(title, "── Thread ─ ") {
		t.Errorf("title = %q", title)
	}
	if c := screen.Cell(7, 1); !isBrightCyan(c) {
		t.Errorf("open pane frame fg %d bold %v, want bright cyan", c.Fg, c.Bold)
	}
	if keys := screen.Row(13); !strings.HasPrefix(keys, "─[↑↓] Move ─[Tab] Message View ──") {
		t.Errorf("pane's closing row = %q, want the menu's keys", keys)
	}
	if sep := strings.TrimRight(screen.Row(23), " "); sep != strings.Repeat("─", 79) {
		t.Errorf("separator = %q, want a plain rule", sep)
	}
	col := strings.Index(screen.Row(24), "Next")
	if col < 0 {
		t.Fatalf("no Next on the bar: %q", screen.Row(24))
	}
	if c := screen.Cell(24, utf8.RuneCountInString(screen.Row(24)[:col])+1); !isDarkGrey(c) || c.Bg == 44 {
		t.Errorf("Next drawn fg %d bold %v bg %d, want greyed and unselected", c.Fg, c.Bold, c.Bg)
	}
	if !strings.Contains(screen.Row(2), "Reading message #7 ") {
		t.Errorf("down should read #7 straight away:\n%s", screen.Snapshot())
	}
	if marker := screen.Cell(10, 1); marker.Rune != '►' || !strings.Contains(screen.Row(10), "#7 ") || screen.Cell(10, 40).Bg != 44 {
		t.Errorf("row 10 = %q, want #7 marked and barred", screen.Row(10))
	}
	if bg := screen.Cell(9, 40).Bg; bg == 44 {
		t.Error("#5 should no longer be barred")
	}
}

// With Thread View off in Konfig there is no pane until T opens the menu,
// and closing the menu hides it again.
func TestMessageReaderThreadMenuWithViewClosed(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	env.markRead(generalAreaID, "Caller", len(sampleThread))
	caller := *env.caller
	caller.ThreadViewClosed = true
	for _, tc := range []struct {
		keys  string
		panes int
	}{
		{"", 0},
		{"T", 1},
		{"TT", 1},
		{"\t", 1},
		{"\t\t", 1},
		// Opened on #5, drawn again on #7, then closed by Enter.
		{"T\x1b[B\r", 2},
	} {
		r := env.sub(t).runCmd("READMSGS", &caller, "", "5\r"+tc.keys+"Q")
		if n := strings.Count(r.text(), "── Thread "); n != tc.panes {
			t.Errorf("%q: pane drawn %d times, want %d", tc.keys, n, tc.panes)
		}
	}
	screen := env.screenRun("READMSGS", &caller, "5\rTTQ")
	if strings.Contains(screen.Row(7), "── Thread ") {
		t.Errorf("the pane should be gone after T closes the menu:\n%s", screen.Snapshot())
	}
	screen = env.screenRun("READMSGS", &caller, "5\r\tQ")
	if !strings.HasPrefix(screen.Row(7), "── Thread ─") || screen.Cell(10, 40).Bg != 44 {
		t.Errorf("Tab should open the pane as the thread menu:\n%s", screen.Snapshot())
	}
	// Tab would close the pane here, and says so.
	if keys := screen.Row(13); !strings.HasPrefix(keys, "─[↑↓] Move ─[Tab] Close ──") {
		t.Errorf("key row = %q, want Tab to say Close", keys)
	}
	// With the pane hidden, the separator still names Tab.
	screen = env.screenRun("READMSGS", &caller, "5\rQ")
	if sep := screen.Row(23); !strings.HasPrefix(sep, "─[Tab] Thread View ──") {
		t.Errorf("separator = %q, want the Tab hint", sep)
	}
}

var replyCountRe = regexp.MustCompile(`Reply#: 3 +Replies: 2 `)

// Nobody but its recipient sees the private #9 or any count of it: the
// caller reading #5 gets Replies: 2 (#7 and the hoisted #15) with no #9 in
// the pane.
func TestMessageReaderDirectReplyCountRespectsVisibility(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	users := defaultTestUsers()
	kestrel := &user.User{ID: 3, Handle: "Kestrel_of_the_Wastes", RealName: "K W", AccessLevel: 30, Validated: true, TimeLimit: 60}
	for _, u := range append(users, kestrel) {
		u.MsgHdr = 1
		u.CurrentMsgConferenceID = 1
		u.CurrentMsgConferenceTag = "LOCAL"
		u.CurrentMessageAreaID = generalAreaID
		u.CurrentMessageAreaTag = "GENERAL"
	}
	users[1].AccessLevel = 30
	env.writeUsers(append(users, kestrel)...)
	env.markRead(generalAreaID, "Caller", len(sampleThread))
	env.markRead(generalAreaID, kestrel.Handle, len(sampleThread))

	callerScreen := env.screenRun("READMSGS", env.caller, "5\rQ")
	if row := callerScreen.Row(1); !replyCountRe.MatchString(row) {
		t.Errorf("caller's header row = %q, want Reply#: 3 and Replies: 2", row)
	}
	if snap := callerScreen.Snapshot(); strings.Contains(snap, "#9 ") || strings.Contains(snap, "n0name") {
		t.Errorf("the caller's pane names the private #9:\n%s", snap)
	}
	// Its recipient sees #9 in place, with #15 under it rather than #5, so
	// #5 still has two direct replies.
	if !strings.Contains(callerScreen.Row(7), " 3 of 9 ") {
		t.Errorf("caller's pane title = %q, want 3 of 9", callerScreen.Row(7))
	}
	kestrelScreen := env.screenRun("READMSGS", kestrel, "5\rQ")
	if row := kestrelScreen.Row(1); !replyCountRe.MatchString(row) {
		t.Errorf("recipient's header row = %q, want Replies: 2", row)
	}
	if !strings.Contains(kestrelScreen.Row(7), " 3 of 10 ") {
		t.Errorf("recipient's pane title = %q, want 3 of 10", kestrelScreen.Row(7))
	}
	// On #7 the window reaches #9, shown as the last reply under #5.
	kestrelScreen = env.screenRun("READMSGS", kestrel, "7\rQ")
	if snap := kestrelScreen.Snapshot(); !strings.Contains(snap, "│ │ └─#9 n0name") {
		t.Errorf("the recipient's pane should show #9:\n%s", snap)
	}
}

// On a CP437 session the pane and the menu's keys arrive as single bytes:
// the box characters, "~" for a clipped name, and the 0x10 marker and the
// 0x18 and 0x19 arrows. The emulator draws nothing for those control bytes (a CP437
// terminal draws glyphs), so they show up in its unhandled list and the
// text after each sits one column left of where a real terminal puts it.
func TestMessageReaderThreadPaneCP437(t *testing.T) {
	env := newMsgEnv(t)
	env.outputMode = ansi.OutputModeCP437
	env.seedSampleThread()
	screen := env.readerAt(5, "T")
	snap := screen.Snapshot()
	if row := screen.Row(10); !strings.Contains(row, "│ ├─#5 Phosphor~ That was it. Font sticks now, but the menus are s~  ") {
		t.Errorf("current pane row = %q\n%s", row, snap)
	}
	if row := screen.Row(9); !strings.HasPrefix(row, "  ├─#3 Kestrel_~ ") {
		t.Errorf("pane row above = %q", row)
	}
	if keys := screen.Row(13); !strings.Contains(keys, "] Move ─[Tab] Message View ─") {
		t.Errorf("pane's closing row = %q", keys)
	}
	if c := screen.Cell(13, 80).Rune; c != ' ' {
		t.Errorf("key row reached column 80: %q", c)
	}
	sent := strings.Join(screen.Unhandled(), "")
	for _, b := range []string{"\x10", "\x18", "\x19"} {
		if !strings.Contains(sent, b) {
			t.Errorf("control glyph %q was not sent; unhandled = %q", b, screen.Unhandled())
		}
	}
}

// Text from a message cannot act on the terminal from inside the pane: escape sequences and control characters are dropped, a
// pipe code is printed rather than expanded, and a body stored as raw
// CP437 (0x82 is é) is decoded rather than mangled. Cells keep their width.
func TestThreadCellsAreInert(t *testing.T) {
	if got := excerpt("Caf\x82 au lait\nmore"); got != "Café au lait" {
		t.Errorf("excerpt of a raw CP437 body = %q", got)
	}
	g := threadGlyphsFor(ansi.OutputModeUTF8)
	for _, tc := range []struct{ in, want string }{
		{"|CLgone", "||CLgone"},
		{"a|15b", "a||15b"},
		{"x\x1b[2Jy\x1b[1;1Hz", "xyz"},
		{"tab\there\x7f", "tabhere"},
		{"Caf\x82", "Café"},
	} {
		got := threadCell(tc.in, 12, g, ansi.OutputModeUTF8)
		if !strings.HasPrefix(got, tc.want) {
			t.Errorf("threadCell(%q) = %q, want prefix %q", tc.in, got, tc.want)
		}
		expanded := string(ansi.ReplacePipeCodes([]byte(got)))
		if strings.Contains(expanded, "\x1b") {
			t.Errorf("threadCell(%q) expands to an escape: %q", tc.in, expanded)
		}
		if w := utf8.RuneCountInString(expanded); w != 12 {
			t.Errorf("threadCell(%q) is %d columns after expansion, want 12: %q", tc.in, w, expanded)
		}
		if t2 := threadText(tc.in, ansi.OutputModeUTF8); !strings.HasPrefix(t2, tc.want) {
			t.Errorf("threadText(%q) = %q", tc.in, t2)
		}
	}
	// A row built from such a message stays 79 columns and carries no
	// clear-screen.
	msg := &message.DisplayMessage{MsgNum: 7, From: "Evil|CL", Subject: "|CLsubject", Body: "\x1b[2J|16pwned\nbody", DateTime: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)}
	row := renderTreeRow(threadRow{Num: 7, Depth: 1, Prefix: "└─", Msg: msg}, true, true, false, 79, g, ansi.OutputModeUTF8)
	if w := visibleWidth(row, ansi.OutputModeUTF8); w != 79 {
		t.Errorf("row width = %d: %q", w, row)
	}
	if expanded := string(ansi.ReplacePipeCodes([]byte(row))); strings.Contains(expanded, "\x1b[2J") || !strings.Contains(expanded, "|16pwned") || !strings.Contains(expanded, "Evil|CL") {
		t.Errorf("row expands to %q", expanded)
	}
	// The pane title carries the root subject through the same cleaning.
	tv := newThreadView(map[int]*message.ReplyNode{7: {Num: 7, Children: []int{8}}, 8: {Num: 8, Parent: 7}}, 8,
		func(n int) *message.DisplayMessage {
			if n == 7 {
				return msg
			}
			return &message.DisplayMessage{MsgNum: 8, From: "x", Subject: "Re: s", Body: "b"}
		})
	title := string(ansi.ReplacePipeCodes([]byte(renderThreadPane(tv, tv.cur, 3, 79, " Thread ", "Close", g, ansi.OutputModeUTF8)[0])))
	if !strings.Contains(title, "|CLsubject") {
		t.Errorf("pane title expands to %q", title)
	}
}

// A deleted message is a placeholder only for a caller who could have read
// it. With the private #9 deleted, its public reply #15 still hangs from #5
// for everyone else, with no "#9 deleted" row; its recipient sees the
// placeholder.
func TestDeletedPrivateMessageStaysHidden(t *testing.T) {
	nodes := sampleNodes()
	nodes[9].Deleted = true
	deleted9 := func(viewer string) func(int) *message.DisplayMessage {
		base := sampleLoad(viewer)
		return func(n int) *message.DisplayMessage {
			m := base(n)
			if m != nil && n == 9 {
				m.IsDeleted = true
			}
			return m
		}
	}
	tv := newThreadView(nodes, 15, deleted9("Caller"))
	for _, r := range tv.rows {
		if r.Num == 9 {
			t.Errorf("caller's rows include the deleted private #9: %v", rowNums(tv.rows))
		}
	}
	if r := tv.row(15); r == nil || r.Parent != 5 {
		t.Errorf("#15 should hang from #5 for the caller: %+v", r)
	}
	kestrel := newThreadView(nodes, 15, deleted9("Kestrel_of_the_Wastes"))
	if r := kestrel.row(9); r == nil || !r.Deleted {
		t.Errorf("the recipient should see #9 as a placeholder: %v", rowNums(kestrel.rows))
	}
}

// The same through the reader and base: with #9 deleted, the caller reading
// #15 sees neither its number nor a placeholder for it, menu open or not.
func TestMessageReaderDeletedPrivateParentStaysHidden(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	if err := env.e.MessageMgr.DeleteMessage(generalAreaID, 9); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	screen := env.readerAt(15, "")
	snap := screen.Snapshot()
	if strings.Contains(snap, "#9") {
		t.Errorf("the deleted private #9 is named:\n%s", snap)
	}
	if snap := env.readerAt(15, "T").Snapshot(); strings.Contains(snap, "#9") {
		t.Errorf("the thread menu names the deleted private #9:\n%s", snap)
	}
}

// After R posts a reply, the reader reloads the message, so the thread
// view includes the reply: the thread menu reaches it from the leaf that
// was just answered.
func TestMessageReaderReplyRefreshesThread(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	env.markRead(generalAreaID, "Caller", len(sampleThread))
	r := env.runCmd("READMSGS", env.caller, "", "10\rRbody-16 fresh reply\x1aT\x1b[B\rQ")
	txt := r.text()
	if !r.has("Reply posted successfully") {
		t.Fatalf("reply not posted; output:\n%s", txt)
	}
	if last := strings.LastIndex(txt, "Reading message #"); last < 0 || !strings.HasPrefix(txt[last:], "Reading message #16 ") {
		t.Errorf("down from #10 in the menu should open the new reply #16; output:\n%s", txt)
	}
}

// Thread on the bar opens the menu like the T key: Right enters the bar, T
// picks Thread, and the arrows then move the menu's cursor.
func TestMessageReaderThreadMenuFromLightbar(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	env.markRead(generalAreaID, "Caller", len(sampleThread))
	for _, tc := range []struct {
		keys string
		at   int
	}{
		{"\x1b[CT\x1b[B\r", 7},
		{"\x1b[Dt\x1b[A\r", 3},
	} {
		r := env.sub(t).runCmd("READMSGS", env.caller, "", "5\r"+tc.keys+"Q")
		txt := r.text()
		if last := strings.LastIndex(txt, "Reading message #"); last < 0 || !strings.HasPrefix(txt[last:], fmt.Sprintf("Reading message #%d ", tc.at)) {
			t.Errorf("%q should end on #%d; output:\n%s", tc.keys, tc.at, txt)
		}
	}
}
