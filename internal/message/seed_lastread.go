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

	// Messages are stored chronologically. Walk backward to find the newest
	// message older than `since`; everything after it stays unread.
	seed := 0
	for i := total; i >= 1; i-- {
		hdr, err := b.ReadMessageHeader(i)
		if err != nil || hdr.Attribute&jam.MsgDeleted != 0 {
			continue
		}
		if time.Unix(int64(hdr.DateWritten), 0).Before(since) {
			seed = i
			break
		}
	}
	if seed == 0 {
		return nil // every message is recent — leave all unread, no record needed
	}
	return b.SetLastRead(username, uint32(seed), uint32(seed))
}
