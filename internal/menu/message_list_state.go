package menu

import (
	"fmt"
	"log/slog"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// MessageListEntry represents a single message in the list view
type MessageListEntry struct {
	MsgNum    int
	Subject   string
	From      string
	To        string
	IsPrivate bool
	IsRead    bool // Based on JAM lastread pointer
}

// MessageListState manages the list display and navigation
type MessageListState struct {
	AreaID        int
	TotalMessages int
	Entries       []MessageListEntry
	CurrentPage   int
	ItemsPerPage  int
	SelectedIndex int // Index within current page (0-based)
	LastRead      int
}

// truncateString truncates a string to maxLen, adding "..." if truncated
func truncateString(s string, maxLen int) string {
	// Measured and cut in runes, not bytes: subjects and handles can hold
	// multi-byte characters, and a byte-offset slice would emit a partial UTF-8
	// sequence and render as garbage.
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
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

	// Build list of message entries
	entries := make([]MessageListEntry, 0, totalCount)

	for msgNum := 1; msgNum <= totalCount; msgNum++ {
		msg, err := msgMgr.GetMessage(areaID, msgNum)
		if err != nil {
			// Skip deleted or unreadable messages
			slog.Warn("failed to read message", "msg", msgNum, "area", areaID, "error", err)
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
			IsPrivate: isPrivate,
			IsRead:    isRead,
		}
		entries = append(entries, entry)
	}

	return entries, lastRead, nil
}
