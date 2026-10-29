package menu

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// placeholderState is a threaded list of seven rows, three to a page, with
// placeholders at absolute indexes 1 and 3 (the last row of page 1 and the
// first of page 2) and a message on each side of them:
//
//	page 1: #10, [#9 deleted], #8
//	page 2: [#7 deleted], #6, #5
//	page 3: #4
func placeholderState() *MessageListState {
	nums := []int{10, 9, 8, 7, 6, 5, 4}
	entries := make([]MessageListEntry, len(nums))
	for i, n := range nums {
		entries[i] = MessageListEntry{MsgNum: n, Threaded: true, Placeholder: n == 9 || n == 7}
	}
	return &MessageListState{TotalMessages: len(entries), Entries: entries, CurrentPage: 1, ItemsPerPage: 3, Threaded: true}
}

// Every movement key steps over a placeholder, across a page boundary too,
// and Enter never lands on one.
func TestThreadedListNavigationSkipsPlaceholders(t *testing.T) {
	cases := []struct {
		name   string
		keys   string
		want   int // message Enter opens
		action string
	}{
		{"down over the placeholder onto #8", "\x1b[B\r", 8, "REFRESH_FULL"},
		{"down from #8 over #7 onto page 2's #6", "\x1b[B\x1b[B\r", 6, "REFRESH_FULL"},
		{"up from #6 over #7 onto #8", "\x1b[B\x1b[B\x1b[A\r", 8, "REFRESH_FULL"},
		{"up from #8 over #9 onto #10", "\x1b[B\x1b[A\r", 10, "REFRESH_FULL"},
		{"page down lands on page 2's first message", "\x1b[6~\r", 6, "REFRESH_FULL"},
		{"page down twice reaches page 3", "\x1b[6~\x1b[6~\r", 4, "REFRESH_FULL"},
		{"page up from page 2 lands on #10", "\x1b[6~\x1b[5~\r", 10, "REFRESH_FULL"},
		{"end lands on the last message", "\x1b[F\r", 4, "REFRESH_FULL"},
		{"end then home", "\x1b[F\x1b[H\r", 10, "REFRESH_FULL"},
		{"ctrl-x steps too", "\x18\r", 8, "REFRESH_FULL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := placeholderState()
			ih := editor.NewInputHandler(strings.NewReader(tc.keys))
			var action string
			var msg int
			for {
				var err error
				action, msg, err = runMessageListNavigation(ih, state)
				if err != nil {
					t.Fatalf("navigation: %v", err)
				}
				if action == "READ" {
					break
				}
			}
			if msg != tc.want {
				t.Errorf("Enter opened #%d, want #%d (page %d index %d)", msg, tc.want, state.CurrentPage, state.SelectedIndex)
			}
			if i := state.selectedAbsolute(); !state.selectable(i) {
				t.Errorf("cursor rests on a placeholder at index %d", i)
			}
		})
	}
	// Within a page the step is a line refresh; onto another page, a full one.
	state := placeholderState()
	ih := editor.NewInputHandler(strings.NewReader("\x1b[B\x1b[B"))
	if a, _, _ := runMessageListNavigation(ih, state); a != "REFRESH_LINE" || state.CurrentPage != 1 || state.SelectedIndex != 2 {
		t.Errorf("after one down: %s page %d index %d, want REFRESH_LINE page 1 index 2", a, state.CurrentPage, state.SelectedIndex)
	}
	if a, _, _ := runMessageListNavigation(ih, state); a != "REFRESH_FULL" || state.CurrentPage != 2 || state.SelectedIndex != 1 {
		t.Errorf("after two downs: %s page %d index %d, want REFRESH_FULL page 2 index 1", a, state.CurrentPage, state.SelectedIndex)
	}
}

// A page made only of placeholders (a long chain of deleted ancestors
// above a readable reply) is passed over by Page Up as well as Page Down:
// Page Up from the page after it lands on the nearest message above it,
// not back on the page being left.
func TestThreadedListPageUpOverPlaceholderPage(t *testing.T) {
	nums := []int{10, 9, 8, 7, 6, 5, 4, 3, 2}
	entries := make([]MessageListEntry, len(nums))
	for i, n := range nums {
		// Page 2 (indexes 3-5) is all placeholders.
		entries[i] = MessageListEntry{MsgNum: n, Threaded: true, Placeholder: i >= 3 && i <= 5}
	}
	state := &MessageListState{TotalMessages: len(entries), Entries: entries, CurrentPage: 1, ItemsPerPage: 3, Threaded: true}
	ih := editor.NewInputHandler(strings.NewReader("\x1b[6~\x1b[5~\r"))
	if a, _, _ := runMessageListNavigation(ih, state); a != "REFRESH_FULL" || state.CurrentPage != 3 {
		t.Fatalf("page down: %s page %d, want page 3 (page 2 has no message)", a, state.CurrentPage)
	}
	if a, _, _ := runMessageListNavigation(ih, state); a != "REFRESH_FULL" || state.CurrentPage != 1 || state.SelectedIndex != 0 {
		t.Fatalf("page up from page 3: %s page %d index %d, want page 1 index 0", a, state.CurrentPage, state.SelectedIndex)
	}
	if a, msg, _ := runMessageListNavigation(ih, state); a != "READ" || msg != 10 {
		t.Errorf("enter: %s #%d, want READ #10", a, msg)
	}
}

// T and V come back as actions for the list runner; Esc quits like Q.
func TestThreadedListNavigationToggleAndQuitKeys(t *testing.T) {
	for key, want := range map[string]string{"T": "TOGGLE", "t": "TOGGLE", "\x1b": "QUIT", "Q": "QUIT"} {
		state := placeholderState()
		ih := editor.NewInputHandler(strings.NewReader(key))
		if a, _, err := runMessageListNavigation(ih, state); err != nil || a != want {
			t.Errorf("key %q: action %q err %v, want %q", key, a, err, want)
		}
	}
}

// The page info counts messages, not rows: placeholders are neither
// numbered nor counted.
func TestThreadedListPageInfoCountsMessages(t *testing.T) {
	state := placeholderState()
	strip := func(s string) string {
		return strings.TrimSpace(testAnsiEscape.ReplaceAllString(string(ansi.ReplacePipeCodes([]byte(s))), ""))
	}
	if got := strip(pageInfoLine(state)); !strings.Contains(got, "Page 1 of 3 [1-2 of 5 messages]") || utf8.RuneCountInString(got) != 79 {
		t.Errorf("page 1 info = %q", got)
	}
	state.CurrentPage = 2
	if got := strip(pageInfoLine(state)); !strings.Contains(got, "Page 2 of 3 [3-4 of 5 messages]") {
		t.Errorf("page 2 info = %q", got)
	}
	state.CurrentPage = 3
	if got := strip(pageInfoLine(state)); !strings.Contains(got, "Page 3 of 3 [5-5 of 5 messages]") {
		t.Errorf("page 3 info = %q", got)
	}
}

// The threaded columns and separators fill the frame, and every threaded
// row is exactly 79 visible columns in both encodings: long handles,
// multi-byte subjects, deep prefixes and placeholders included.
func TestThreadedListRowWidthIsExact(t *testing.T) {
	if sum := listStatusWidth + listNumWidth + listThreadWidth + listFromWidth + listDateWidth + listThreadSeparators; sum != listInteriorWidth {
		t.Errorf("threaded columns plus separators = %d, want %d", sum, listInteriorWidth)
	}
	sample := time.Date(2026, 10, 20, 7, 15, 0, 0, time.UTC)
	cases := []struct {
		name  string
		entry MessageListEntry
	}{
		{"root", MessageListEntry{MsgNum: 1, Threaded: true, Subject: "Topaz font in SyncTERM", OwnSubject: true, From: "PhosphorDotMatrix", Date: sample}},
		{"reply with excerpt", MessageListEntry{MsgNum: 3, Threaded: true, Depth: 1, Prefix: "├─", Excerpt: "It's both. SyncTERM only switches fonts when the BBS asks for it.", From: "Kestrel_of_the_Wastes", Date: sample}},
		{"deep reply", MessageListEntry{MsgNum: 10, Threaded: true, Depth: 4, Prefix: "│ │ │ └─", Excerpt: "Or save it with a SAUCE record that says 80 columns.", From: "Kestrel_of_the_Wastes", Date: sample}},
		{"deeper than eight collapses", MessageListEntry{MsgNum: 99, Threaded: true, Depth: 12, Prefix: strings.Repeat("│ ", 11) + "└─", Excerpt: "deep", From: "x", Date: sample}},
		{"multi-byte", MessageListEntry{MsgNum: 4242, Threaded: true, Depth: 2, Prefix: "│ ├─", Subject: "Ünïcödé sübjéct with åccents everywhere and then some more words", OwnSubject: true, From: "Jörg Müller von Überlingen", Date: sample}},
		{"placeholder", MessageListEntry{MsgNum: 4, Threaded: true, Depth: 1, Prefix: "└─", Placeholder: true}},
		{"placeholder, six digits, no date", MessageListEntry{MsgNum: 123456, Threaded: true, Depth: 3, Prefix: "│   └─", Placeholder: true}},
	}
	for _, mode := range []ansi.OutputMode{ansi.OutputModeUTF8, ansi.OutputModeCP437} {
		for _, tc := range cases {
			for _, hl := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%v/hl=%v", tc.name, mode, hl), func(t *testing.T) {
					ts := newTestSession("")
					terminal := newTestTerminal(ts)
					if err := drawMessageListLine(terminal, tc.entry, hl, mode, nil); err != nil {
						t.Fatalf("drawMessageListLine: %v", err)
					}
					line := strings.TrimRight(strings.Split(ts.output(), "\n")[0], "\r")
					stripped := testAnsiEscape.ReplaceAllString(line, "")
					width := utf8.RuneCountInString(stripped)
					if mode == ansi.OutputModeCP437 {
						width = len(stripped)
					}
					if width != 79 {
						t.Errorf("row width = %d, want 79\n%q", width, stripped)
					}
					if tc.entry.Placeholder && strings.Contains(line, "\x1b[7m") {
						t.Errorf("a placeholder must not be highlighted: %q", line)
					}
				})
			}
		}
	}
	// A placeholder reads "#n deleted" after its prefix; a reply shows its
	// excerpt, a root its subject.
	ts := newTestSession("")
	terminal := newTestTerminal(ts)
	for _, tc := range cases[:3] {
		_ = drawMessageListLine(terminal, tc.entry, false, ansi.OutputModeUTF8, nil)
	}
	_ = drawMessageListLine(terminal, cases[5].entry, false, ansi.OutputModeUTF8, nil)
	out := testAnsiEscape.ReplaceAllString(ts.output(), "")
	for _, want := range []string{
		"│N    1 Topaz font in SyncTERM                      PhosphorDotMa... 10/20/26 │",
		"│N    3 ├─It's both. SyncTERM only switches font... Kestrel_of_th... 10/20/26 │",
		"│N   10 │ │ │ └─Or save it with a SAUCE record t... Kestrel_of_th... 10/20/26 │",
		"│       └─#4 deleted                                                          │",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rows lack %q:\n%s", want, out)
		}
	}
}

// The threaded header line is built from the same widths as its rows.
func TestThreadedListHeaderMatchesRowWidth(t *testing.T) {
	ts := newTestSession("")
	terminal := newTestTerminal(ts)
	state := &MessageListState{Entries: []MessageListEntry{}, CurrentPage: 1, ItemsPerPage: 5, Threaded: true}
	if err := drawMessageListScreen(terminal, state, "General", "Main", ansi.OutputModeUTF8, nil); err != nil {
		t.Fatalf("drawMessageListScreen: %v", err)
	}
	out := ts.output()
	for _, line := range strings.Split(out, "\n") {
		stripped := testAnsiEscape.ReplaceAllString(strings.TrimRight(line, "\r"), "")
		if stripped == "" {
			continue
		}
		if got := utf8.RuneCountInString(stripped); got != 79 {
			t.Errorf("screen line is %d runes, want 79\n%q", got, stripped)
		}
	}
	stripped := testAnsiEscape.ReplaceAllString(out, "")
	for _, want := range []string{"Main > General - Threaded List", "   N# Thread", "From             Date", "T: Flat  Q: Quit"} {
		if !strings.Contains(stripped, want) {
			t.Errorf("threaded screen lacks %q:\n%s", want, stripped)
		}
	}
	ts = newTestSession("")
	_ = drawMessageListScreen(newTestTerminal(ts), &MessageListState{Entries: []MessageListEntry{}, CurrentPage: 1, ItemsPerPage: 5}, "General", "Main", ansi.OutputModeUTF8, nil)
	if flat := testAnsiEscape.ReplaceAllString(ts.output(), ""); !strings.Contains(flat, "T: Threaded  Q: Quit") || !strings.Contains(flat, "Main > General - Message List") {
		t.Errorf("flat screen help/title changed:\n%s", flat)
	}
}

// A tree with a gap (a header that could not be read leaves no node) still
// lists every thread: the roots come from the keys, not the count.
func TestThreadForestWalksKeysNotCount(t *testing.T) {
	nodes := map[int]*message.ReplyNode{1: {Num: 1}, 3: {Num: 3}}
	rows := threadForest(nodes, sampleLoad("Caller"))
	if got := rowNums(rows); !reflect.DeepEqual(got, []int{3, 1}) {
		t.Errorf("rows = %v, want [3 1]", got)
	}
}

// buildThreadedMessageList over the sample area: threads newest root
// first, each in reader order, with the private #9 gone, #15 hoisted under
// #5, #4 a placeholder, and a reply showing its excerpt unless it changed
// the subject.
func TestBuildThreadedMessageList(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	env.markRead(generalAreaID, "Caller", 6)
	filter := withPrivacy(privateMailReaderFor(env.e, env.um, env.caller), nil)
	entries, lastRead, err := buildThreadedMessageList(env.e.MessageMgr, generalAreaID, "Caller", filter)
	if err != nil {
		t.Fatalf("buildThreadedMessageList: %v", err)
	}
	if lastRead != 6 {
		t.Errorf("lastRead = %d, want 6", lastRead)
	}
	var nums []int
	for _, e := range entries {
		nums = append(nums, e.MsgNum)
	}
	want := []int{12, 13, 6, 2, 1, 3, 5, 7, 10, 15, 11, 4, 8, 14}
	if !reflect.DeepEqual(nums, want) {
		t.Fatalf("order = %v, want %v", nums, want)
	}
	byNum := map[int]MessageListEntry{}
	for _, e := range entries {
		byNum[e.MsgNum] = e
	}
	if e := byNum[4]; !e.Placeholder || e.Prefix != "└─" || e.Depth != 1 {
		t.Errorf("#4 = %+v, want a depth-1 placeholder", e)
	}
	if e := byNum[15]; e.Prefix != "│ │ └─" || e.Depth != 3 || e.OwnSubject || e.Excerpt != "Answering a private note in public, as one does." {
		t.Errorf("#15 = %+v", e)
	}
	if e := byNum[13]; e.OwnSubject || e.Excerpt != "Same here. Looks like the upstream poller is backing off." {
		t.Errorf("#13 should show its excerpt: %+v", e)
	}
	if e := byNum[12]; !e.OwnSubject || e.IsRead {
		t.Errorf("#12 should be an unread root showing its subject: %+v", e)
	}
	if e := byNum[3]; !e.IsRead || e.Depth != 1 || e.Prefix != "├─" {
		t.Errorf("#3 = %+v", e)
	}
	if e := byNum[8]; e.Prefix != "  └─" || e.Depth != 2 {
		t.Errorf("#8 under the placeholder = %+v", e)
	}
	for _, e := range entries {
		if e.MsgNum == 9 {
			t.Error("the private #9 is listed")
		}
	}
	// A reply that changed the subject shows it rather than its excerpt.
	n, err := env.e.MessageMgr.Post(generalAreaID, "Lux", "All", "New topic from the Topaz thread", "Splitting this off.\nbody-16", message.PostOptions{ReplyToNum: 10})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	entries, _, err = buildThreadedMessageList(env.e.MessageMgr, generalAreaID, "Caller", filter)
	if err != nil {
		t.Fatalf("buildThreadedMessageList: %v", err)
	}
	for _, e := range entries {
		if e.MsgNum == n && (!e.OwnSubject || e.Depth != 5) {
			t.Errorf("#%d = %+v, want its own subject at depth 5", n, e)
		}
	}
}

// The threaded list on screen, as design 2d: the frame, the tree column
// with the placeholder, excerpts for replies, and the page info counting
// twelve messages for thirteen rows.
func TestListMsgsThreadedScreen(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	// From the menu the list opens threaded. Q leaves the screen as drawn.
	screen := env.screenRun("LISTMSGS", env.caller, "Q")
	snap := screen.Snapshot()
	if !strings.Contains(screen.Row(2), "General Discussion - Threaded List") {
		t.Errorf("title = %q", screen.Row(2))
	}
	if !strings.HasPrefix(screen.Row(4), "│    N# Thread                                      From             Date") {
		t.Errorf("header = %q", screen.Row(4))
	}
	wantRows := []string{
		"│N   12 Reddit gateway lag                          Mercury Retro... 10/",
		"│N   13 └─Same here. Looks like the upstream pol... Kestrel_of_th... 10/",
		"│N    6 Wanted: old .ANS packs                      Lux              10/",
		"│N    2 Binkd poll schedule                         GrimmTheTosser   10/",
		"│N    1 Topaz font in SyncTERM                      PhosphorDotMa... 10/",
		"│N    3 ├─It's both. SyncTERM only switches font... Kestrel_of_th... 10/",
		"│N    5 │ ├─That was it. Font sticks now, but th... PhosphorDotMa... 10/",
		"│N    7 │ │ ├─That's the 80th column wrapping. T... Mercury Retro... 10/",
		"│N   10 │ │ │ └─Or save it with a SAUCE record t... Kestrel_of_th... 10/",
		"│N   15 │ │ └─Answering a private note in public... Lux              10/",
		"│N   11 │ └─Which BBS packages actually send tha... GrimmTheTosser   10/",
		"│       └─#4 deleted",
		"│N    8   └─It does, but it can't do ZMODEM, so ... Lux              10/",
		"│N   14     └─Fair. Sticking with SyncTERM then.    PhosphorDotMa... 10/",
	}
	for i, want := range wantRows {
		if row := screen.Row(6 + i); !strings.HasPrefix(row, want) {
			t.Errorf("row %d = %q\nwant prefix %q", 6+i, row, want)
		}
	}
	for i := range wantRows {
		if row := screen.Row(6 + i); utf8.RuneCountInString(row) != 79 || !strings.HasSuffix(row, "│") {
			t.Errorf("row %d is %d wide: %q", 6+i, utf8.RuneCountInString(row), row)
		}
	}
	if !strings.Contains(snap, "Page 1 of 1 [1-13 of 13 messages]") {
		t.Errorf("page info should count 13 messages for 14 rows:\n%s", snap)
	}
	if !strings.Contains(snap, "↑/↓: Navigate  Enter: Read  T: Flat  Q: Quit") {
		t.Errorf("help line missing:\n%s", snap)
	}
	if !strings.Contains(snap, " 9 of 15") && strings.Contains(snap, "#9") {
		t.Errorf("the private #9 is listed:\n%s", snap)
	}
}

var bodyMarkerRe = regexp.MustCompile(`body-(\d+)`)

// listOpenedBody reports which sample message a LISTMSGS run opened, by
// its body marker, failing if it opened more than one.
func listOpenedBody(t *testing.T, r runResult) int {
	t.Helper()
	opened := 0
	for _, m := range bodyMarkerRe.FindAllStringSubmatch(r.text(), -1) {
		n, _ := strconv.Atoi(m[1])
		if opened != 0 && opened != n {
			t.Fatalf("reader opened both #%d and #%d", opened, n)
		}
		opened = n
	}
	return opened
}

// T keeps the cursor on the same message when switching either way, even
// when it lands on another page: flat lists #1 last (page 2 of 2); the
// threaded list has it fifth (page 1). The list opens threaded, on the
// newest root, #12.
func TestListMsgsToggleKeepsSelection(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	cases := []struct {
		name string
		keys string
		want int
	}{
		{"threaded opens on the newest root", "", 12},
		{"T keeps #12", "T", 12},
		{"down lands on #13", "\x1b[B", 13},
		{"down past #11 skips the placeholder to #8", strings.Repeat("\x1b[B", 11), 8},
		{"end (#14), T to flat keeps #14", "\x1b[FT", 14},
		{"end, T, T is still #14", "\x1b[FTT", 14},
		{"T, end (#1, page 2), T back to threaded keeps #1", "T\x1b[FT", 1},
		{"T, home (#15), T back to threaded keeps #15", "T\x1b[HT", 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub := env.sub(t)
			r := sub.runCmd("LISTMSGS", sub.caller, "", tc.keys+"\rQQ")
			if r.err != nil {
				t.Fatalf("LISTMSGS: %v", r.err)
			}
			if got := listOpenedBody(t, r); got != tc.want {
				t.Errorf("opened #%d, want #%d\n%s", got, tc.want, r.text())
			}
		})
	}
}

// The mode a caller picks holds for the session: after T in one list, the
// next list from the menu opens flat.
func TestListMsgsModePersistsForSession(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	// One session, two lists: the first run's T and Q, then the second
	// run's Q, all read through the session's one input handler.
	ts := newTestSession("TQQ")
	c := &cmdCtx{e: env.e, s: ts, terminal: newTestTerminal(ts), userManager: env.um, currentUser: env.caller, nodeNumber: 1, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24}
	t.Cleanup(func() { resetSessionIH(ts); sessionListModes.Delete(ts) })
	if _, _, err := runListMsgs(c, ""); err != nil {
		t.Fatalf("first list: %v", err)
	}
	if sessionListThreaded(ts) {
		t.Fatal("T should record flat mode for the session")
	}
	// The same session again: it opens flat.
	if _, _, err := runListMsgs(c, ""); err != nil {
		t.Fatalf("second list: %v", err)
	}
	if out := testAnsiEscape.ReplaceAllString(ts.output(), ""); strings.Count(out, "- Message List") < 2 {
		t.Errorf("second list should open flat; output:\n%s", out)
	}
}

// Round trips between the reader and the list never nest a second reader:
// L from the reader opens the list on the current message, Enter there
// returns to the same reader on the chosen message, and Q/Esc in the list
// return to the message it was opened from. With a nested reader
// the trailing Q would leave the caller in the list, and the exhausted
// input would log the session off instead of ending with the reader's Q.
func TestReaderListRoundTrip(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	env.markRead(generalAreaID, "Caller", len(sampleThread))
	cases := []struct {
		name string
		keys string
		at   int
	}{
		{"L, Enter reads the highlighted (same) message", "5\rL\rQ", 5},
		{"L opens the threaded list; down is #7 there", "5\rL\x1b[B\rQ", 7},
		{"L, Q comes back to the message it left", "5\rL\x1b[B\x1b[BQQ", 5},
		{"L, Esc comes back too", "5\rL\x1b[B\x1bQ", 5},
		{"L, T, down, Enter reads the next in flat order", "5\rLT\x1b[B\rQ", 3},
		{"L, T, Enter, L, Enter, Q: two round trips, one reader, still flat", "5\rLT\x1b[B\rL\x1b[B\rQ", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub := env.sub(t)
			r := sub.runCmd("READMSGS", sub.caller, "", tc.keys)
			if r.err != nil || r.next != "QUIT_NEWSCAN" {
				t.Fatalf("next = %q err = %v, want the reader's own Q to end the command; output:\n%s", r.next, r.err, r.text())
			}
			txt := r.text()
			if last := strings.LastIndex(txt, "Reading message #"); last < 0 || !strings.HasPrefix(txt[last:], fmt.Sprintf("Reading message #%d ", tc.at)) {
				t.Errorf("reader should end on #%d; output:\n%s", tc.at, txt)
			}
		})
	}
}

// From a list opened on the menu, the nested reader's L returns to that
// list, in the mode it was in, with the cursor on the message that was
// being read; Q returns to it too.
func TestListMsgsReaderReturnsToList(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	// The threaded list opens on #12, then #13, #6, #2, #1, #3...; T keeps
	// #12, which the flat list has fourth, followed by #11, #10, #8.
	cases := []struct {
		name string
		keys string
		want int    // message the second Enter opens
		mode string // title text expected after the return
	}{
		{"Enter, L, Enter reopens the same message in threaded mode", "\x1b[B\rL\r", 13, "Threaded List"},
		{"T, Enter, L, Enter reopens the same message in flat mode", "T\x1b[B\rL\r", 11, "Message List"},
		{"Enter, Q keeps the cursor on the message read", "\x1b[B\x1b[B\rQ\r", 6, "Threaded List"},
		{"Enter, ], L follows the thread then lists on the new message", strings.Repeat("\x1b[B", 4) + "\r]L\r", 3, "Threaded List"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub := env.sub(t)
			r := sub.runCmd("LISTMSGS", sub.caller, "", tc.keys+"QQ")
			if r.err != nil {
				t.Fatalf("LISTMSGS: %v", r.err)
			}
			txt := r.text()
			if last := strings.LastIndex(txt, "Reading message #"); last < 0 || !strings.HasPrefix(txt[last:], fmt.Sprintf("Reading message #%d ", tc.want)) {
				t.Errorf("second read should be #%d; output:\n%s", tc.want, txt)
			}
			if lastTitle := strings.LastIndex(txt, " - "); lastTitle < 0 || !strings.HasPrefix(txt[lastTitle:], " - "+tc.mode) {
				t.Errorf("list after the reader should be a %s; output tail:\n%s", tc.mode, txt[max(0, len(txt)-600):])
			}
		})
	}
}

// Message text cannot act on the terminal from a list row: a reply whose
// first line is a clear-screen code or an escape is printed inert, and the
// row keeps its width.
func TestThreadedListRowIsInert(t *testing.T) {
	entry := MessageListEntry{MsgNum: 7, Threaded: true, Depth: 1, Prefix: "└─", Excerpt: "|CLpwned \x1b[2Jtext", From: "Evil|16", Date: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)}
	for _, hl := range []bool{false, true} {
		ts := newTestSession("")
		if err := drawMessageListLine(newTestTerminal(ts), entry, hl, ansi.OutputModeUTF8, nil); err != nil {
			t.Fatal(err)
		}
		out := ts.output()
		if strings.Contains(out, "\x1b[2J") || !strings.Contains(out, "|CLpwned text") || !strings.Contains(out, "Evil|16") {
			t.Errorf("hl=%v: row expands to %q", hl, out)
		}
		stripped := testAnsiEscape.ReplaceAllString(strings.TrimRight(strings.Split(out, "\n")[0], "\r"), "")
		if w := utf8.RuneCountInString(stripped); w != 79 {
			t.Errorf("hl=%v: width %d: %q", hl, w, stripped)
		}
	}
}

// A deleted message is a placeholder in the list only for a caller who
// could have read it: with the private #9 deleted, the caller's list hangs
// #15 from #5 with no "#9 deleted" row.
func TestThreadedListDeletedPrivateStaysHidden(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	if err := env.e.MessageMgr.DeleteMessage(generalAreaID, 9); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	filter := withPrivacy(privateMailReaderFor(env.e, env.um, env.caller), nil)
	entries, _, err := buildThreadedMessageList(env.e.MessageMgr, generalAreaID, "Caller", filter)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.MsgNum == 9 {
			t.Errorf("the deleted private #9 is listed (placeholder=%v)", e.Placeholder)
		}
		if e.MsgNum == 15 && (e.Depth != 3 || e.Prefix != "│ │ └─") {
			t.Errorf("#15 = %+v, want it hanging from #5", e)
		}
		found = found || (e.MsgNum == 4 && e.Placeholder)
	}
	if !found {
		t.Error("the deleted public #4 should still be a placeholder")
	}
}

// T opens and closes the thread pane, from inside the lightbar too, and the
// choice holds for the next message of the session.
func TestReaderTTogglesThreadPane(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	env.markRead(generalAreaID, "Caller", len(sampleThread))
	for _, tc := range []struct {
		name   string
		closed bool // the caller's Konfig default
		keys   string
		panes  int // pane draws in the output
	}{
		{"open by default", false, "5\rQ", 1},
		{"T closes it", false, "5\rTQ", 1},
		{"T twice reopens it", false, "5\rTTQ", 2},
		{"Right then T closes it from the bar", false, "5\r\x1b[CTQ", 1},
		{"closed stays closed on the next message", false, "5\rT]Q", 1},
		{"Konfig closed", true, "5\r]Q", 0},
		{"Konfig closed, T opens it for the session", true, "5\rT]Q", 2},
		{"T does nothing without a thread", false, "2\rTTQ", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := env.sub(t)
			caller := *sub.caller
			caller.ThreadViewClosed = tc.closed
			r := sub.runCmd("READMSGS", &caller, "", tc.keys)
			if r.err != nil || r.next != "QUIT_NEWSCAN" {
				t.Fatalf("next = %q err = %v; output:\n%s", r.next, r.err, r.text())
			}
			if n := strings.Count(r.text(), "── Thread ─"); n != tc.panes {
				t.Errorf("pane drawn %d times, want %d; output:\n%s", n, tc.panes, r.text())
			}
			if r.has("Message Threading") {
				t.Errorf("T asked for a subject search; output:\n%s", r.text())
			}
		})
	}
}

// The bar's Thread item is in the bar's usual blue on a message with a
// thread, and greyed out on one without.
func TestReaderThreadItemGreyedWithoutThread(t *testing.T) {
	env := newMsgEnv(t)
	env.seedSampleThread()
	for _, tc := range []struct {
		msg  int
		grey bool
	}{{5, false}, {2, true}} {
		screen := env.readerAt(tc.msg, "")
		row := 24
		col := strings.Index(screen.Row(row), "Thread")
		if col < 0 {
			t.Fatalf("#%d: no Thread item on the bar: %q", tc.msg, screen.Row(row))
		}
		col = utf8.RuneCountInString(screen.Row(row)[:col]) + 1
		c := screen.Cell(row, col)
		grey := c.Fg == 90 || (c.Fg == 30 && c.Bold)
		blue := c.Fg == 94 || (c.Fg == 34 && c.Bold)
		if grey != tc.grey || blue == tc.grey {
			t.Errorf("#%d: Thread drawn with fg %d bold %v, want grey=%v", tc.msg, c.Fg, c.Bold, tc.grey)
		}
	}
}
