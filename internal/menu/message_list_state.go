package menu

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/gliderlabs/ssh"
)

// MessageListEntry represents a single message in the list view
type MessageListEntry struct {
	MsgNum    int
	Subject   string
	From      string
	To        string
	Date      time.Time // When the message was written; blank in the list if zero
	IsPrivate bool
	IsRead    bool // Based on JAM lastread pointer

	// Threaded mode only (buildThreadedMessageList).
	Threaded    bool   // drawn as a threaded row: tree prefix and a Thread column
	Depth       int    // depth in the reply tree; 0 for a root
	Prefix      string // tree glyphs before the text, as threadRow.Prefix
	Excerpt     string // the message's first line of its own text
	OwnSubject  bool   // show the subject rather than the excerpt: a root, or a changed subject
	Placeholder bool   // a deleted message a reply hangs from: "#n deleted", never selectable
}

// MessageListState manages the list display and navigation
type MessageListState struct {
	AreaID        int
	TotalMessages int // rows in Entries, placeholders included; the page info counts messages
	Entries       []MessageListEntry
	CurrentPage   int
	ItemsPerPage  int
	SelectedIndex int // Index within current page (0-based)
	LastRead      int
	Threaded      bool // the list is in threaded mode
}

// defaultListStrings are the list's labels as shipped, for callers (tests)
// that draw the list without a loaded strings.json.
var defaultListStrings = config.StringsConfig{
	MsgListThreadedTitle:      "%s - Threaded List",
	MsgListThreadColumn:       "Thread",
	MsgListHelpThreaded:       "%s/%s: Navigate  Enter: Read  T: Flat  Q: Quit",
	MsgListHelpFlat:           "%s/%s: Navigate  Enter: Read  T: Threaded  Q: Quit",
	MsgListDeletedPlaceholder: "#%d deleted",
}

// listStrings returns str, or the shipped list labels when it is nil.
func listStrings(str *config.StringsConfig) *config.StringsConfig {
	if str == nil {
		return &defaultListStrings
	}
	return str
}

// selectable reports whether the entry at absolute index i can take the
// cursor: a message, not a deleted placeholder.
func (s *MessageListState) selectable(i int) bool {
	return i >= 0 && i < len(s.Entries) && !s.Entries[i].Placeholder
}

// nextSelectable is the first selectable index from i (inclusive) in
// direction dir, or -1.
func (s *MessageListState) nextSelectable(i, dir int) int {
	for ; i >= 0 && i < len(s.Entries); i += dir {
		if s.selectable(i) {
			return i
		}
	}
	return -1
}

// selectableCount counts the messages, leaving out placeholders.
func (s *MessageListState) selectableCount() int {
	n := 0
	for i := range s.Entries {
		if s.selectable(i) {
			n++
		}
	}
	return n
}

// selectableBefore counts the messages above absolute index i.
func (s *MessageListState) selectableBefore(i int) int {
	n := 0
	for j := 0; j < i && j < len(s.Entries); j++ {
		if s.selectable(j) {
			n++
		}
	}
	return n
}

// totalPages is the page count for the rows the list holds.
func (s *MessageListState) totalPages() int {
	if s.ItemsPerPage <= 0 {
		return 1
	}
	return max((len(s.Entries)+s.ItemsPerPage-1)/s.ItemsPerPage, 1)
}

// selectedAbsolute is the absolute index of the highlighted row.
func (s *MessageListState) selectedAbsolute() int {
	start, _ := calculatePagination(len(s.Entries), s.ItemsPerPage, s.CurrentPage)
	return start + s.SelectedIndex
}

// setSelected puts the cursor on absolute index i, turning to its page.
// It reports whether the page changed.
func (s *MessageListState) setSelected(i int) (pageChanged bool) {
	if s.ItemsPerPage <= 0 || i < 0 {
		return false
	}
	page := i/s.ItemsPerPage + 1
	pageChanged = page != s.CurrentPage
	s.CurrentPage = page
	s.SelectedIndex = i % s.ItemsPerPage
	return pageChanged
}

// selectMessage puts the cursor on message n, or on the nearest selectable
// row at or after the current position when n is not listed.
func (s *MessageListState) selectMessage(n int) {
	for i, e := range s.Entries {
		if e.MsgNum == n && !e.Placeholder {
			s.setSelected(i)
			return
		}
	}
	cur := min(s.selectedAbsolute(), len(s.Entries)-1)
	i := s.nextSelectable(cur, 1)
	if i < 0 {
		i = s.nextSelectable(cur, -1)
	}
	if i < 0 {
		s.CurrentPage, s.SelectedIndex = 1, 0
		return
	}
	s.setSelected(i)
}

// sessionListModes remembers, per session, whether the message list was
// last in threaded mode, so the mode a caller picked (with T, or by opening
// the list with V or L from the reader) holds for the rest of the call.
var sessionListModes sync.Map

func setSessionListThreaded(s ssh.Session, threaded bool) {
	sessionListModes.Store(s, threaded)
}

func sessionListThreaded(s ssh.Session) bool {
	v, ok := sessionListModes.Load(s)
	return ok && v.(bool)
}

// truncateString truncates a string to maxLen, adding "..." if truncated.
// Measured and cut in runes, not bytes: subjects and handles can hold
// multi-byte characters, and a byte-offset slice would emit a partial UTF-8
// sequence and render as garbage.
func truncateString(s string, maxLen int) string {
	return ansi.TruncateRunes(s, maxLen, "...")
}

// formatStatusChar returns the status character for a message entry
func formatStatusChar(entry MessageListEntry, isHighlighted bool) string {
	if isHighlighted {
		// When highlighted, use plain characters without color codes
		if !entry.IsRead {
			return "N"
		}
		if entry.IsPrivate {
			return "P"
		}
		return " "
	}
	// Normal (non-highlighted) display with colors
	if !entry.IsRead {
		return "|12N|07" // Bright red N for new/unread
	}
	if entry.IsPrivate {
		return "|12P|07" // Bright red P for private
	}
	return " " // Space for read messages
}

// calculatePagination calculates the start and end indices for a page
func calculatePagination(total, perPage, currentPage int) (start, end int) {
	if total == 0 || perPage == 0 {
		return 0, 0
	}
	start = (currentPage - 1) * perPage
	end = start + perPage
	if end > total {
		end = total
	}
	return start, end
}

// buildMessageList fetches message metadata from the current area
func buildMessageList(msgMgr *message.MessageManager, areaID int, username string, msgFilter msgOwnershipFilter) ([]MessageListEntry, int, error) {
	// Get total message count for area
	totalCount, err := msgMgr.GetMessageCountForArea(areaID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get message count: %w", err)
	}

	if totalCount == 0 {
		return []MessageListEntry{}, 0, nil
	}

	// Get lastread pointer
	lastRead, err := msgMgr.GetLastRead(areaID, username)
	if err != nil {
		slog.Warn("failed to get lastread", "area", areaID, "handle", username, "error", err)
		lastRead = 0 // Default to all unread
	}

	// Build list of message entries, newest first (descending message number)
	// so the most recent posts appear at the top of the list.
	entries := make([]MessageListEntry, 0, totalCount)

	for msgNum := totalCount; msgNum >= 1; msgNum-- {
		msg, err := msgMgr.GetMessage(areaID, msgNum)
		if err != nil {
			// Skip deleted or unreadable messages
			slog.Warn("failed to read message", "msg", msgNum, "area", areaID, "error", err)
			continue
		}

		// A message flagged deleted but not yet packed away is not shown: the
		// reader would only skip it.
		if msg.IsDeleted {
			continue
		}

		// Ownership boundary (e.g. PRIVMAIL): omit messages the user can't see.
		if msgFilter != nil && !msgFilter(msg) {
			continue
		}

		// Check if message is private
		isPrivate := msg.IsPrivate

		// Check if message is read
		isRead := msgNum <= lastRead

		entry := MessageListEntry{
			MsgNum:    msgNum,
			Subject:   msg.Subject,
			From:      msg.From,
			To:        msg.To,
			Date:      msg.DateTime,
			IsPrivate: isPrivate,
			IsRead:    isRead,
		}
		entries = append(entries, entry)
	}

	return entries, lastRead, nil
}

// buildThreadedMessageList lists the area as reply threads: every thread the
// user may see, newest root first, each flattened depth-first with replies
// in ascending order (the order the reader's ] key walks). Hidden messages
// are left out, with their replies attached to the nearest shown ancestor;
// a deleted message a reply hangs from is a placeholder row. The entries
// carry the excerpt and tree prefix the threaded row draws.
func buildThreadedMessageList(msgMgr *message.MessageManager, areaID int, username string, msgFilter msgOwnershipFilter) ([]MessageListEntry, int, error) {
	totalCount, err := msgMgr.GetMessageCountForArea(areaID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get message count: %w", err)
	}
	if totalCount == 0 {
		return []MessageListEntry{}, 0, nil
	}
	lastRead, err := msgMgr.GetLastRead(areaID, username)
	if err != nil {
		slog.Warn("failed to get lastread", "area", areaID, "handle", username, "error", err)
		lastRead = 0
	}
	nodes, err := msgMgr.ReplyTree(areaID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to build reply tree: %w", err)
	}

	// Every message is read once, as the flat list reads them, and kept only
	// if the user may see it.
	msgs := make(map[int]*message.DisplayMessage, totalCount)
	for n := 1; n <= totalCount; n++ {
		m, err := msgMgr.GetMessage(areaID, n)
		if err != nil {
			slog.Warn("failed to read message", "msg", n, "area", areaID, "error", err)
			continue
		}
		if m.IsDeleted || (msgFilter != nil && !msgFilter(m)) {
			continue
		}
		msgs[n] = m
	}
	load := func(n int) *message.DisplayMessage { return msgs[n] }

	rows := threadForest(nodes, load)
	parentOf := make(map[int]int, len(rows))
	for _, r := range rows {
		parentOf[r.Num] = r.Parent
	}
	// nearestSubject is the subject of the closest ancestor that is a
	// message rather than a placeholder; ok is false at the top.
	nearestSubject := func(n int) (string, bool) {
		for p := parentOf[n]; p != 0; p = parentOf[p] {
			if m := msgs[p]; m != nil {
				return m.Subject, true
			}
		}
		return "", false
	}
	entries := make([]MessageListEntry, 0, len(rows))
	for _, r := range rows {
		e := MessageListEntry{MsgNum: r.Num, Threaded: true, Depth: r.Depth, Prefix: r.Prefix}
		if r.Deleted || r.Msg == nil {
			e.Placeholder = true
			entries = append(entries, e)
			continue
		}
		e.Subject, e.From, e.To, e.Date = r.Msg.Subject, r.Msg.From, r.Msg.To, r.Msg.DateTime
		e.IsPrivate, e.IsRead = r.Msg.IsPrivate, r.Num <= lastRead
		e.Excerpt = excerpt(r.Msg.Body)
		// A reply shows its own first line, not "Re: ..." again, unless it
		// changed the subject. Under a placeholder, the comparison is with
		// the nearest ancestor that is still a message.
		e.OwnSubject = true
		if above, ok := nearestSubject(r.Num); r.Depth > 0 && ok &&
			message.NormalizeThreadSubject(above) == message.NormalizeThreadSubject(r.Msg.Subject) {
			e.OwnSubject = false
		}
		entries = append(entries, e)
	}
	return entries, lastRead, nil
}
