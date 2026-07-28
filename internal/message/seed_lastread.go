// Package message: newscan last-read seeding.
package message

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// NewscanSeedDays is how far back a freshly seeded last-read pointer leaves
// messages unread, so a newly joined user has recent messages to scan
// without facing the area's entire history.
const NewscanSeedDays = 7

// SeedLastRead sets a user's last-read pointer so only messages newer than
// `since` appear unread. A quiet area (nothing newer than `since`) ends up
// fully read. It is a no-op if the user already has a last-read record or
// the area is empty; it never overwrites real reading progress.
//
// The seed is found with a forward scan for the first message dated
// `since` or later (same pattern as determineStartMessage in
// internal/menu/message_scan_run.go), not a backward walk: messages are
// appended in arrival order, not strictly by date, so a late-arriving
// echomail message with an old date at a high message number would
// otherwise become the seed and silently mark lower-numbered recent
// messages as read.
//
// Deleted messages are NOT skipped while scanning: their headers keep
// their dates, and JAM's unread count is pure index arithmetic
// (count - LastReadMsg), so counting deleted messages toward the seed
// keeps seeding consistent with how "unread" is actually computed.
func (mm *MessageManager) SeedLastRead(areaID int, username string, since time.Time) error {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()

	if _, err := b.GetLastRead(username); err == nil {
		return nil // existing progress — never overwrite
	} else if err != jam.ErrNotFound {
		return fmt.Errorf("seed lastread: %w", err)
	}

	total, err := b.GetMessageCount()
	if err != nil {
		return fmt.Errorf("seed lastread: %w", err)
	}
	if total == 0 {
		return nil
	}

	seed := total
	for i := 1; i <= total; i++ {
		hdr, err := b.ReadMessageHeader(i)
		if err != nil {
			continue // unreadable header: treat as old
		}
		if !time.Unix(int64(hdr.DateWritten), 0).Before(since) {
			seed = i - 1
			break
		}
	}
	if seed == 0 {
		return nil // every message is recent — leave all unread, no record needed
	}
	return b.SetLastRead(username, uint32(seed), uint32(seed))
}
