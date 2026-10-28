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
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
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
	if p, state := tv.parentOf(15); state != parentHidden || p != 0 {
		t.Errorf("parentOf(15) = %d, %v; want hidden and unnamed", p, state)
	}
	if _, isRoot := tv.nextSibling(15); !isRoot {
		t.Error("#15 should be a root of its shown tree")
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
	if f := tv.firstReply(5); f != 7 {
		t.Errorf("firstReply(5) = %d, want 7", f)
	}
	if x, _ := tv.nextSibling(7); x != 15 {
		t.Errorf("nextSibling(7) = %d, want 15: X skips the private #9", x)
	}
	if x, isRoot := tv.nextSibling(15); x != 0 || isRoot {
		t.Errorf("nextSibling(15) = %d, root %v; want none, not a root", x, isRoot)
	}
	if x, isRoot := tv.nextSibling(1); x != 0 || !isRoot {
		t.Errorf("nextSibling(1) = %d, root %v; want none on the root", x, isRoot)
	}
	if p, state := tv.parentOf(8); state != parentDeleted || p != 4 {
		t.Errorf("parentOf(8) = %d, %v; want the deleted #4", p, state)
	}
	if p, state := tv.parentOf(15); state != parentHidden || p != 0 {
		t.Errorf("parentOf(15) = %d, %v; want hidden", p, state)
	}
	if p, state := tv.parentOf(1); state != parentNone || p != 0 {
		t.Errorf("parentOf(1) = %d, %v; want none", p, state)
	}
	if p, state := tv.parentOf(5); state != parentShown || p != 3 {
		t.Errorf("parentOf(5) = %d, %v; want #3", p, state)
	}
	if n := tv.neighbour(10, 1); n != 15 {
		t.Errorf("after #10 in thread order = %d, want 15", n)
	}
	if n := tv.neighbour(1, -1); n != 0 {
		t.Errorf("before the root = %d, want 0", n)
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
	// Each key's target and notice.
	strs := env0Strings(t)
	for _, tc := range []struct {
		cur    int
		key    rune
		target int
		notice string
	}{
		{5, 'U', 3, ""},
		{1, 'U', 0, "Message |15#1|09 starts its thread; it isn't a reply."},
		{8, 'U', 0, "Message |15#4|09, the one this answers, has been deleted."},
		{15, 'U', 0, "The message this answers isn't available to you."},
		{1, 'F', 3, ""},
		{10, 'F', 0, "No replies to |15#10|09 yet."},
		{7, 'X', 15, ""},
		{1, 'X', 0, "Message |15#1|09 starts its thread; it has no sibling replies."},
		{11, 'X', 0, "No more replies to |15#3|09 after this one."},
		{10, ']', 15, ""},
		{14, ']', 0, "End of thread."},
		{3, '[', 1, ""},
		{1, '[', 0, "Start of thread."},
	} {
		tv := newThreadView(sampleNodes(), tc.cur, sampleLoad("Caller"))
		target, notice := threadNavTarget(tv, tc.key, strs)
		if target != tc.target || !strings.Contains(notice, tc.notice) || (tc.notice == "" && notice != "") {
			t.Errorf("#%d %c: target %d notice %q; want %d %q", tc.cur, tc.key, target, notice, tc.target, tc.notice)
		}
	}
}

// env0Strings loads the shipped strings, for notice text.
func env0Strings(t *testing.T) *config.StringsConfig {
	t.Helper()
	return newMenuEnv(t).e.Strings()
}

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

// The signposts always fill exactly the row, however long the handles: the
// fixed parts render whole and the handles share what is left.
func TestRenderSignpostsFitsWidth(t *testing.T) {
	long := "Kestrel_of_the_Wastes_and_Beyond"
	cases := []struct {
		name string
		segs []signpost
		want []string // substrings of the stripped row
	}{
		{"three long handles", []signpost{
			{key: 'U', arrow: "↑", target: 3, handle: long},
			{key: 'F', arrow: "↓", target: 7, handle: long + "_Two"},
			{key: 'X', arrow: "→", target: 11, handle: long + "_Three"},
		}, []string{"─[U]↑ #3 ", "─[F]↓ #7 ", "─[X]→ #11 ", "…"}},
		{"two long handles", []signpost{
			{key: 'U', arrow: "↑", target: 3, handle: long},
			{key: 'F', arrow: "↓", target: 7, handle: long},
			{key: 'X', arrow: "→"},
		}, []string{"none", "…"}},
		{"deleted target", []signpost{
			{key: 'U', arrow: "↑", target: 4, deleted: true},
			{key: 'F', arrow: "↓", target: 14, handle: "PhosphorDotMatrix"},
			{key: 'X', arrow: "→"},
		}, []string{"─[U]↑ #4 deleted ", "─[F]↓ #14 PhosphorDotMatrix ", "─[X]→ none ─"}},
		{"short handles leave a rule", []signpost{
			{key: 'U', arrow: "↑", target: 1, handle: "Lux"},
			{key: 'F', arrow: "↓"},
			{key: 'X', arrow: "→"},
		}, []string{"─[U]↑ #1 Lux ─[F]↓ none ─[X]→ none ─[V]list ────"}},
	}
	for _, mode := range []ansi.OutputMode{ansi.OutputModeUTF8, ansi.OutputModeCP437} {
		g := threadGlyphsFor(mode)
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s/%v", tc.name, mode), func(t *testing.T) {
				segs := make([]signpost, len(tc.segs))
				for i, s := range tc.segs {
					segs[i] = s
					segs[i].arrow = map[rune]string{'U': g.up, 'F': g.down, 'X': g.right}[s.key]
				}
				row := renderSignposts(segs, 79, g, mode)
				if w := visibleWidth(row, mode); w != 79 {
					t.Errorf("width = %d, want 79: %q", w, row)
				}
				if mode != ansi.OutputModeUTF8 {
					return
				}
				stripped := testAnsiEscape.ReplaceAllString(string(ansi.ReplacePipeCodes([]byte(row))), "")
				for _, want := range tc.want {
					if !strings.Contains(stripped, want) {
						t.Errorf("row %q lacks %q", stripped, want)
					}
				}
			})
		}
	}
}

// A handle that would get fewer than four characters is dropped rather
// than clipped to nothing.
func TestRenderSignpostsDropsTinyHandles(t *testing.T) {
	g := threadGlyphsFor(ansi.OutputModeUTF8)
	segs := []signpost{
		{key: 'U', arrow: g.up, target: 100000, handle: "Someone_Rather_Long"},
		{key: 'F', arrow: g.down, target: 100001, handle: "Another_Long_Handle"},
		{key: 'X', arrow: g.right, target: 100002, handle: "A_Third_Long_Handle"},
	}
	// The fixed parts take 51 columns with the list tail; a 53-column row
	// leaves two.
	row := renderSignposts(segs, 53, g, ansi.OutputModeUTF8)
	stripped := testAnsiEscape.ReplaceAllString(string(ansi.ReplacePipeCodes([]byte(row))), "")
	if utf8.RuneCountInString(stripped) != 53 {
		t.Errorf("width = %d, want 53: %q", utf8.RuneCountInString(stripped), stripped)
	}
	if strings.Contains(stripped, "Som") || strings.Contains(stripped, "Ano") || strings.Contains(stripped, "A_T") {
		t.Errorf("no handle fits a 53-column row with three six-digit targets: %q", stripped)
	}
	if !strings.Contains(stripped, "#100000 ─[F]↓ #100001 ─[X]→ #100002 ─[V]list") {
		t.Errorf("the fixed parts must still render whole: %q", stripped)
	}
}

// The pane takes five rows, then three, then none, as the body shrinks:
// with a six-row header, a 24-row terminal has 15 body lines, a 20-row one
// 11 and a 16-row one 7, and the body keeps at least six.
func TestPaneRowsFor(t *testing.T) {
	for _, tc := range []struct{ termHeight, want int }{{25, 5}, {24, 5}, {22, 5}, {21, 3}, {20, 3}, {19, 0}, {16, 0}} {
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
				lines := renderThreadPane(tv, rows, 79, " Thread ", g, mode)
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
	line := testAnsiEscape.ReplaceAllString(string(ansi.ReplacePipeCodes([]byte(renderTreeRow(deep, false, 79, threadGlyphsFor(ansi.OutputModeUTF8), ansi.OutputModeUTF8)))), "")
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
	fn, ok := env.e.RunRegistry[cmd]
	if !ok {
		env.t.Fatalf("no runnable registered as %q", cmd)
	}
	var opts []testterm.Option
	if env.outputMode == ansi.OutputModeCP437 {
		opts = append(opts, testterm.CP437())
	}
	screen := testterm.New(80, 24, opts...)
	sess := testterm.NewSession(screen, keys)
	SetSessionOutputMode(sess, env.outputMode)
	env.t.Cleanup(func() {
		resetSessionIH(sess)
		ClearSessionOutputMode(sess)
	})
	c := &cmdCtx{
		e: env.e, s: sess, terminal: term.NewTerminal(sess, ""), userManager: env.um, currentUser: u,
		nodeNumber: 1, sessionStartTime: time.Now(), outputMode: env.outputMode, termWidth: 80, termHeight: 24,
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
// and the separator carries the signposts.
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
	if !strings.Contains(screen.Row(14), "Ke> Check that the entry") {
		t.Errorf("body should start on row 14:\n%s", snap)
	}
	if marker := screen.Cell(10, 1); marker.Rune != '►' {
		t.Errorf("row 10 col 1 = %q, want the marker", marker.Rune)
	}
	if bg := screen.Cell(10, 40).Bg; bg != 44 {
		t.Errorf("current pane row background = %d, want blue (SGR 44)", bg)
	}
	if bg := screen.Cell(9, 40).Bg; bg == 44 {
		t.Error("a row that is not current should not be blue")
	}
	signs := screen.Row(23)
	for _, want := range []string{"─[U]↑ #3 Kestrel_of_t…", "─[F]↓ #7 Mercury Retr…", "─[X]→ #11 GrimmTheToss…", "─[V]list"} {
		if !strings.Contains(signs, want) {
			t.Errorf("signposts = %q, want %q", signs, want)
		}
	}
	// The row fills 79 columns (the last may be the final segment's
	// space) and never reaches the 80th, which can scroll some terminals.
	if n := utf8.RuneCountInString(strings.TrimRight(signs, " ")); n < 78 || n > 79 {
		t.Errorf("signposts width = %d: %q", n, signs)
	}
	if c := screen.Cell(23, 80).Rune; c != ' ' {
		t.Errorf("signposts reached column 80: %q", c)
	}
	if !strings.Contains(screen.Row(24), "Next") {
		t.Errorf("lightbar missing:\n%s", snap)
	}
}

// A message that starts a thread nobody answered shows no pane, and the
// body keeps the rows; its signposts all read none.
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
	if signs := screen.Row(23); !strings.Contains(signs, "─[U]↑ none ─[F]↓ none ─[X]→ none ─") {
		t.Errorf("signposts = %q", signs)
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
	if !strings.Contains(screen.Row(23), "─[U]↑ #4 deleted ─[F]↓ #14 PhosphorDotMatrix ─[X]→ none") {
		t.Errorf("signposts on #8 = %q", screen.Row(23))
	}
	screen = env.readerAt(14, "")
	if row := screen.Row(12); !strings.HasPrefix(row, "►     └─#14 Phosphor…") {
		t.Errorf("last row should be #14 marked: %q\n%s", row, screen.Snapshot())
	}
	if !strings.Contains(screen.Row(7), " 9 of 9 · depth 3 ") {
		t.Errorf("title on #14 = %q", screen.Row(7))
	}
}

// Each thread key moves the reader, or shows its notice on the bar row and
// stays, through the real reader and message base.
func TestMessageReaderThreadKeys(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	for _, tc := range []struct {
		name  string
		start int
		keys  string
		at    int    // message shown at the end
		seen  string // body marker or notice seen along the way
	}{
		{"U goes to the parent", 5, "U", 3, "body-3"},
		{"F goes to the first reply", 1, "F", 3, "body-3"},
		{"X goes to the next sibling, skipping private mail", 7, "X", 15, "body-15"},
		{"] walks the thread", 10, "]", 15, "body-15"},
		{"[ walks back", 3, "[", 1, "body-1"},
		{"U on a root", 1, "U", 1, "Message #1 starts its thread; it isn't a reply."},
		{"U under a deleted parent", 8, "U", 8, "Message #4, the one this answers, has been deleted."},
		{"U under a hidden parent", 15, "U", 15, "The message this answers isn't available to you."},
		{"F on a leaf", 10, "F", 10, "No replies to #10 yet."},
		{"X on a root", 1, "X", 1, "Message #1 starts its thread; it has no sibling replies."},
		{"X on the last sibling", 11, "X", 11, "No more replies to #3 after this one."},
		{"] at the end", 14, "]", 14, "End of thread."},
		{"[ at the start", 1, "[", 1, "Start of thread."},
		{"keys chain", 1, "FFF]", 10, "body-10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := env.sub(t)
			sub.markRead(generalAreaID, "Caller", len(sampleThread))
			r := sub.runCmd("READMSGS", sub.caller, "", fmt.Sprintf("%d\r%sQ", tc.start, tc.keys))
			txt := r.text()
			if !strings.Contains(txt, tc.seen) {
				t.Errorf("output lacks %q:\n%s", tc.seen, txt)
			}
			if last := strings.LastIndex(txt, "Reading message #"); last < 0 || !strings.HasPrefix(txt[last:], fmt.Sprintf("Reading message #%d ", tc.at)) {
				t.Errorf("reader should end on #%d; output:\n%s", tc.at, txt)
			}
		})
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
	if !strings.Contains(kestrelScreen.Row(23), "─[X]→ #9 n0name") {
		t.Errorf("recipient's signposts = %q, want X leading to #9", kestrelScreen.Row(23))
	}
}

// On a CP437 session the pane and signposts arrive as single bytes: the
// box characters, "~" for a clipped name, and the 0x10 marker and 0x18-0x1A
// arrows. The emulator draws nothing for those four control bytes (a CP437
// terminal draws glyphs), so they show up in its unhandled list and the
// text after each sits one column left of where a real terminal puts it.
func TestMessageReaderThreadPaneCP437(t *testing.T) {
	env := newMsgEnv(t)
	env.outputMode = ansi.OutputModeCP437
	env.seedSampleThread()
	screen := env.readerAt(5, "")
	snap := screen.Snapshot()
	if row := screen.Row(10); !strings.Contains(row, "│ ├─#5 Phosphor~ That was it. Font sticks now, but the menus are s~  ") {
		t.Errorf("current pane row = %q\n%s", row, snap)
	}
	if row := screen.Row(9); !strings.HasPrefix(row, "  ├─#3 Kestrel_~ ") {
		t.Errorf("pane row above = %q", row)
	}
	if signs := screen.Row(23); !strings.Contains(signs, "─[U] #3 Kestrel_of_t~ ─[F] #7 Mercury Retr~ ─[X] #11 GrimmTheToss~ ─[V]list") {
		t.Errorf("signposts = %q", signs)
	}
	if c := screen.Cell(23, 80).Rune; c != ' ' {
		t.Errorf("signposts reached column 80: %q", c)
	}
	sent := strings.Join(screen.Unhandled(), "")
	for _, b := range []string{"\x10", "\x18", "\x19", "\x1a"} {
		if !strings.Contains(sent, b) {
			t.Errorf("control glyph %q was not sent; unhandled = %q", b, screen.Unhandled())
		}
	}
}

// Text from a message cannot act on the terminal from inside the pane or
// the signposts: escape sequences and control characters are dropped, a
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
	// A row and a signpost built from such a message stay 79 columns and
	// carry no clear-screen.
	msg := &message.DisplayMessage{MsgNum: 7, From: "Evil|CL", Subject: "|CLsubject", Body: "\x1b[2J|16pwned\nbody", DateTime: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)}
	row := renderTreeRow(threadRow{Num: 7, Depth: 1, Prefix: "└─", Msg: msg}, true, 79, g, ansi.OutputModeUTF8)
	if w := visibleWidth(row, ansi.OutputModeUTF8); w != 79 {
		t.Errorf("row width = %d: %q", w, row)
	}
	if expanded := string(ansi.ReplacePipeCodes([]byte(row))); strings.Contains(expanded, "\x1b[2J") || !strings.Contains(expanded, "|16pwned") || !strings.Contains(expanded, "Evil|CL") {
		t.Errorf("row expands to %q", expanded)
	}
	signs := renderSignposts([]signpost{{key: 'U', arrow: g.up, target: 7, handle: "Evil|CL\x1b[2J"}, {key: 'F', arrow: g.down}, {key: 'X', arrow: g.right}}, 79, g, ansi.OutputModeUTF8)
	if w := visibleWidth(signs, ansi.OutputModeUTF8); w != 79 {
		t.Errorf("signposts width = %d: %q", w, signs)
	}
	if expanded := string(ansi.ReplacePipeCodes([]byte(signs))); strings.Contains(expanded, "\x1b[2J") || !strings.Contains(expanded, "Evil|CL ") {
		t.Errorf("signposts expand to %q", expanded)
	}
	// The pane title carries the root subject through the same cleaning.
	tv := newThreadView(map[int]*message.ReplyNode{7: {Num: 7, Children: []int{8}}, 8: {Num: 8, Parent: 7}}, 8,
		func(n int) *message.DisplayMessage {
			if n == 7 {
				return msg
			}
			return &message.DisplayMessage{MsgNum: 8, From: "x", Subject: "Re: s", Body: "b"}
		})
	title := string(ansi.ReplacePipeCodes([]byte(renderThreadPane(tv, 3, 79, " Thread ", g, ansi.OutputModeUTF8)[0])))
	if !strings.Contains(title, "|CLsubject") {
		t.Errorf("pane title expands to %q", title)
	}
}

// A deleted message is a placeholder only for a caller who could have read
// it. With the private #9 deleted, its public reply #15 still hangs from #5
// for everyone else, with no "#9 deleted" row and U saying only that the
// parent is unavailable; its recipient sees the placeholder.
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
	if p, state := tv.parentOf(15); state != parentHidden || p != 0 {
		t.Errorf("parentOf(15) = %d, %v; want hidden and unnamed", p, state)
	}
	if _, notice := threadNavTarget(tv, 'U', env0Strings(t)); strings.Contains(notice, "#9") {
		t.Errorf("U notice names the hidden message: %q", notice)
	}
	segs := readerSignposts(tv, threadGlyphsFor(ansi.OutputModeUTF8))
	if segs[0].target != 0 {
		t.Errorf("U signpost names the hidden parent: %+v", segs[0])
	}

	kestrel := newThreadView(nodes, 15, deleted9("Kestrel_of_the_Wastes"))
	if r := kestrel.row(9); r == nil || !r.Deleted {
		t.Errorf("the recipient should see #9 as a placeholder: %v", rowNums(kestrel.rows))
	}
	if p, state := kestrel.parentOf(15); state != parentDeleted || p != 9 {
		t.Errorf("recipient's parentOf(15) = %d, %v; want deleted #9", p, state)
	}
}

// The same through the reader and base: with #9 deleted, the caller reading
// #15 sees neither its number nor a placeholder for it.
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
	if !strings.Contains(screen.Row(23), "─[U]↑ none ") {
		t.Errorf("signposts = %q, want U leading nowhere", screen.Row(23))
	}
	r := env.sub(t).runCmd("READMSGS", env.caller, "", "15\rUQ")
	if !r.has("The message this answers isn't available to you.") || strings.Contains(r.text(), "#9") {
		t.Errorf("U should give the hidden-parent notice without naming #9; output:\n%s", r.text())
	}
}

// After R posts a reply, the reader reloads the message, so the thread
// view includes the reply: F on a leaf that was just answered goes to it.
func TestMessageReaderReplyRefreshesThread(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	env.markRead(generalAreaID, "Caller", len(sampleThread))
	r := env.runCmd("READMSGS", env.caller, "", "10\rRbody-16 fresh reply\x1aFQ")
	txt := r.text()
	if !r.has("Reply posted successfully") {
		t.Fatalf("reply not posted; output:\n%s", txt)
	}
	if last := strings.LastIndex(txt, "Reading message #"); last < 0 || !strings.HasPrefix(txt[last:], "Reading message #16 ") {
		t.Errorf("F after the reply should open the new reply #16; output:\n%s", txt)
	}
	if r.has("No replies to #10 yet.") {
		t.Errorf("the thread view was not rebuilt after the reply; output:\n%s", txt)
	}
}

// The thread keys work from inside the lightbar too: Right or Left enters
// the bar, and the key is handed back rather than swallowed.
func TestMessageReaderThreadKeysPassThroughLightbar(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	env.markRead(generalAreaID, "Caller", len(sampleThread))
	for _, tc := range []struct {
		keys string
		at   int
	}{
		{"\x1b[CU", 3},
		{"\x1b[Cf", 7},
		{"\x1b[D]", 7},
	} {
		r := env.sub(t).runCmd("READMSGS", env.caller, "", "5\r"+tc.keys+"Q")
		txt := r.text()
		if last := strings.LastIndex(txt, "Reading message #"); last < 0 || !strings.HasPrefix(txt[last:], fmt.Sprintf("Reading message #%d ", tc.at)) {
			t.Errorf("%q should end on #%d; output:\n%s", tc.keys, tc.at, txt)
		}
	}
}
