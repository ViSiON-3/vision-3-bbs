package menu

import (
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// --- calculatePagination ---

func TestCalculatePagination(t *testing.T) {
	tests := []struct {
		name                 string
		total, perPage, page int
		wantStart, wantEnd   int
	}{
		{"first page", 25, 10, 1, 0, 10},
		{"middle page", 25, 10, 2, 10, 20},
		{"last partial page", 25, 10, 3, 20, 25},
		{"total zero", 0, 10, 1, 0, 0},
		{"perPage larger than total", 5, 10, 1, 0, 5},
		// currentPage past the last page is NOT clamped by calculatePagination:
		// start keeps advancing by perPage while end is clamped to total, so
		// start can end up greater than end. Callers only ever loop start..end
		// (a no-op range in that case), so this is harmless in practice, but
		// the raw return values look odd in isolation.
		{"currentPage past the end", 25, 10, 5, 40, 25},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := calculatePagination(tt.total, tt.perPage, tt.page)
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("calculatePagination(%d,%d,%d) = (%d,%d), want (%d,%d)",
					tt.total, tt.perPage, tt.page, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

// --- truncateString ---

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{"empty input", "", 10, ""},
		{"shorter than max", "abc", 10, "abc"},
		{"exactly max", "Hello", 5, "Hello"},
		{"longer, ellipsis applied", "Hello World", 8, "Hello..."},
		// maxLen <= 3 hard-truncates with NO ellipsis — there's no room for
		// "..." once it's subtracted, so the function just slices instead.
		{"maxLen exactly 3, string longer", "Hello", 3, "Hel"},
		{"maxLen 0", "Hello", 0, ""},
		// Multi-byte input must be measured and cut on rune boundaries: a
		// subject line can contain accented or CJK characters, and slicing by
		// byte offset would emit a partial UTF-8 sequence.
		{"multi-byte fits by runes but not bytes", "héllo", 5, "héllo"},
		{"multi-byte truncation lands on a rune boundary", "héllo wörld", 8, "héllo..."},
		{"CJK truncation lands on a rune boundary", "日本語のメッセージ", 5, "日本..."},
		{"maxLen 3 with multi-byte", "日本語のメッセージ", 3, "日本語"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateString(tt.s, tt.maxLen); got != tt.want {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
		})
	}
}

// --- formatStatusChar ---

func TestFormatStatusChar(t *testing.T) {
	tests := []struct {
		name          string
		entry         MessageListEntry
		isHighlighted bool
		want          string
	}{
		{"highlighted unread", MessageListEntry{IsRead: false, IsPrivate: false}, true, "N"},
		{"highlighted read private", MessageListEntry{IsRead: true, IsPrivate: true}, true, "P"},
		{"highlighted read public", MessageListEntry{IsRead: true, IsPrivate: false}, true, " "},
		// Unread takes priority over private in both display modes.
		{"highlighted unread private", MessageListEntry{IsRead: false, IsPrivate: true}, true, "N"},
		{"plain unread", MessageListEntry{IsRead: false, IsPrivate: false}, false, "|12N|07"},
		{"plain read private", MessageListEntry{IsRead: true, IsPrivate: true}, false, "|12P|07"},
		{"plain read public", MessageListEntry{IsRead: true, IsPrivate: false}, false, " "},
		{"plain unread private", MessageListEntry{IsRead: false, IsPrivate: true}, false, "|12N|07"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatStatusChar(tt.entry, tt.isHighlighted); got != tt.want {
				t.Errorf("formatStatusChar(%+v, %v) = %q, want %q", tt.entry, tt.isHighlighted, got, tt.want)
			}
		})
	}
}

// --- buildMessageList ---

// newMessageListTestManager builds a real *message.MessageManager over
// t.TempDir() with a single local message area, mirroring the real-manager
// pattern used elsewhere (see internal/message/reply_test.go,
// internal/menu/file_lightbar_test.go).
func newMessageListTestManager(t *testing.T) (*message.MessageManager, int) {
	t.Helper()
	dataDir, cfgDir := t.TempDir(), t.TempDir()
	mm, err := message.NewMessageManager(dataDir, cfgDir, "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	areaID, err := mm.AddArea(message.MessageArea{Tag: "GENERAL", Name: "General", AreaType: "local"})
	if err != nil {
		t.Fatalf("AddArea: %v", err)
	}
	return mm, areaID
}

func TestBuildMessageList_CountOrderingAndLastRead(t *testing.T) {
	mm, areaID := newMessageListTestManager(t)

	for i, subj := range []string{"first", "second", "third"} {
		if _, err := mm.AddMessage(areaID, "Alice", "All", subj, "body", ""); err != nil {
			t.Fatalf("AddMessage %d: %v", i, err)
		}
	}
	if err := mm.SetLastRead(areaID, "Alice", 2); err != nil {
		t.Fatalf("SetLastRead: %v", err)
	}

	entries, lastRead, err := buildMessageList(mm, areaID, "Alice", nil)
	if err != nil {
		t.Fatalf("buildMessageList: %v", err)
	}
	if lastRead != 2 {
		t.Errorf("lastRead = %d, want 2", lastRead)
	}
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}

	// Ascending by message number, matching post order.
	wantSubjects := []string{"first", "second", "third"}
	for i, e := range entries {
		if e.MsgNum != i+1 {
			t.Errorf("entries[%d].MsgNum = %d, want %d", i, e.MsgNum, i+1)
		}
		if e.Subject != wantSubjects[i] {
			t.Errorf("entries[%d].Subject = %q, want %q", i, e.Subject, wantSubjects[i])
		}
	}
	if !entries[0].IsRead || !entries[1].IsRead {
		t.Error("messages 1 and 2 should be read (msgNum <= lastRead)")
	}
	if entries[2].IsRead {
		t.Error("message 3 should be unread (msgNum > lastRead)")
	}
}

func TestBuildMessageList_FilterExcludesMessages(t *testing.T) {
	mm, areaID := newMessageListTestManager(t)
	if _, err := mm.AddMessage(areaID, "Alice", "All", "keep", "body", ""); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if _, err := mm.AddMessage(areaID, "Alice", "All", "skip", "body", ""); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	filter := func(m *message.DisplayMessage) bool { return m.Subject != "skip" }
	entries, _, err := buildMessageList(mm, areaID, "Alice", filter)
	if err != nil {
		t.Fatalf("buildMessageList: %v", err)
	}
	if len(entries) != 1 || entries[0].Subject != "keep" {
		t.Fatalf("entries = %+v, want a single 'keep' entry", entries)
	}
}

// --- runMessageListNavigation ---

// fiveEntryState returns a fresh MessageListState with 5 entries and 3 items
// per page (2 pages: [1,2,3] and [4,5]), selection at the top of page 1.
func fiveEntryState() *MessageListState {
	entries := make([]MessageListEntry, 5)
	for i := range entries {
		entries[i] = MessageListEntry{MsgNum: i + 1}
	}
	return &MessageListState{
		TotalMessages: 5,
		Entries:       entries,
		CurrentPage:   1,
		ItemsPerPage:  3,
		SelectedIndex: 0,
	}
}

func TestRunMessageListNavigation_DownArrowAdvancesSelection(t *testing.T) {
	state := fiveEntryState()
	ih := editor.NewInputHandler(strings.NewReader("\x1b[B"))
	action, _, err := runMessageListNavigation(ih, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "REFRESH_LINE" {
		t.Errorf("action = %q, want REFRESH_LINE", action)
	}
	if state.SelectedIndex != 1 {
		t.Errorf("SelectedIndex = %d, want 1", state.SelectedIndex)
	}
}

func TestRunMessageListNavigation_UpArrowAtTopClamps(t *testing.T) {
	state := fiveEntryState()
	// Up-arrow at the very top of page 1 matches no case in the switch and
	// falls through to the next key; 'q' terminates so the call returns.
	ih := editor.NewInputHandler(strings.NewReader("\x1b[Aq"))
	action, _, err := runMessageListNavigation(ih, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "QUIT" {
		t.Errorf("action = %q, want QUIT", action)
	}
	if state.SelectedIndex != 0 || state.CurrentPage != 1 {
		t.Errorf("state changed by clamped up-arrow: index=%d page=%d", state.SelectedIndex, state.CurrentPage)
	}
}

func TestRunMessageListNavigation_PageDownAdvancesPage(t *testing.T) {
	state := fiveEntryState()
	ih := editor.NewInputHandler(strings.NewReader("\x1b[6~"))
	action, _, err := runMessageListNavigation(ih, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "REFRESH_FULL" {
		t.Errorf("action = %q, want REFRESH_FULL", action)
	}
	if state.CurrentPage != 2 {
		t.Errorf("CurrentPage = %d, want 2", state.CurrentPage)
	}
	if state.SelectedIndex != 0 {
		t.Errorf("SelectedIndex = %d, want 0", state.SelectedIndex)
	}
}

func TestRunMessageListNavigation_QuitKeyReturnsAction(t *testing.T) {
	state := fiveEntryState()
	ih := editor.NewInputHandler(strings.NewReader("q"))
	action, _, err := runMessageListNavigation(ih, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "QUIT" {
		t.Errorf("action = %q, want QUIT", action)
	}
}

func TestRunMessageListNavigation_EnterReturnsSelectedMessage(t *testing.T) {
	state := fiveEntryState()
	state.SelectedIndex = 1
	ih := editor.NewInputHandler(strings.NewReader("\r"))
	action, selectedMsg, err := runMessageListNavigation(ih, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != "READ" {
		t.Errorf("action = %q, want READ", action)
	}
	if selectedMsg != state.Entries[1].MsgNum {
		t.Errorf("selectedMsg = %d, want %d", selectedMsg, state.Entries[1].MsgNum)
	}
}

func TestRunMessageListNavigation_ExhaustedReaderReturnsError(t *testing.T) {
	state := fiveEntryState()
	ih := editor.NewInputHandler(strings.NewReader(""))
	action, _, err := runMessageListNavigation(ih, state)
	if action != "ERROR" {
		t.Errorf("action = %q, want ERROR", action)
	}
	if !errors.Is(err, io.EOF) {
		t.Errorf("err = %v, want io.EOF", err)
	}
}

// testAnsiEscape strips CSI sequences so a rendered line can be measured in
// visible runes. Test-local on purpose: calculateVisibleWidth counts bytes,
// which is exactly the measurement bug this test guards against.
var testAnsiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

// The help line sits inside a 77-column frame, so the full line is 79 visible
// runes. Its padding must be computed in runes: in UTF-8 mode the up/down
// arrows are 3 bytes each, and a byte-length measurement pads 4 columns short,
// pulling the frame's right edge inward.
func TestDrawMessageListScreenHelpLineCenteringIsRuneBased(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode ansi.OutputMode
	}{
		{"cp437 arrows", ansi.OutputModeCP437},
		{"utf8 arrows", ansi.OutputModeUTF8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := newTestSession("")
			terminal := newTestTerminal(ts)
			state := &MessageListState{
				Entries:       []MessageListEntry{},
				CurrentPage:   1,
				ItemsPerPage:  10,
				TotalMessages: 0,
			}

			if err := drawMessageListScreen(terminal, state, "General", "Main", tc.mode); err != nil {
				t.Fatalf("drawMessageListScreen: %v", err)
			}

			var helpLine string
			for _, line := range strings.Split(ts.output(), "\n") {
				if strings.Contains(line, "Navigate") && strings.Contains(line, "Q:") {
					helpLine = strings.TrimRight(line, "\r")
					break
				}
			}
			if helpLine == "" {
				t.Fatal("help line not found in rendered output")
			}

			stripped := testAnsiEscape.ReplaceAllString(helpLine, "")
			if got := utf8.RuneCountInString(stripped); got != 79 {
				t.Errorf("help line visible width = %d runes, want 79 (line %q)", got, stripped)
			}
		})
	}
}

// A non-ASCII area or conference name must not panic the screen draw. The title
// is clamped to 75 runes, but the padding arithmetic measured bytes, so a
// multi-byte title exceeded the 77-column frame and strings.Repeat got a
// negative count.
func TestDrawMessageListScreenNonASCIITitleDoesNotPanic(t *testing.T) {
	ts := newTestSession("")
	terminal := newTestTerminal(ts)
	state := &MessageListState{
		Entries:       []MessageListEntry{},
		CurrentPage:   1,
		ItemsPerPage:  10,
		TotalMessages: 0,
	}

	// 40 CJK runes: well under the 75-rune clamp, but 120 bytes.
	areaName := strings.Repeat("日", 40)

	if err := drawMessageListScreen(terminal, state, areaName, "Main", ansi.OutputModeUTF8); err != nil {
		t.Fatalf("drawMessageListScreen: %v", err)
	}

	var titleLine string
	for _, line := range strings.Split(ts.output(), "\n") {
		if strings.Contains(line, "Message List") {
			titleLine = strings.TrimRight(line, "\r")
			break
		}
	}
	if titleLine == "" {
		t.Fatal("title line not found in rendered output")
	}
	stripped := testAnsiEscape.ReplaceAllString(titleLine, "")
	if got := utf8.RuneCountInString(stripped); got != 79 {
		t.Errorf("title line visible width = %d runes, want 79 (line %q)", got, stripped)
	}
}
