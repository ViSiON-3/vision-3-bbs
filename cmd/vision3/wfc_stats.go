package main

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/admin"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/scheduler"
	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// Data sources for the WFC console's header counters, Events tab, and kick
// command. Each is a small pure function over the daemon's existing state so
// it can be unit-tested without a running board.

// countCallsToday counts logins since local midnight: calls already
// recorded in the history plus callers who are still online (their record
// is only written at disconnect). The history keeps a bounded number of
// records, so on a very busy board the count is a floor, not an exact
// figure.
func countCallsToday(records []user.CallRecord, active []*session.BbsSession, now time.Time) int {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	n := 0
	for _, r := range records {
		if !r.ConnectTime.Before(midnight) {
			n++
		}
	}
	for _, s := range active {
		s.Mutex.RLock()
		loggedIn := s.User != nil && !s.StartTime.Before(midnight)
		s.Mutex.RUnlock()
		if loggedIn {
			n++
		}
	}
	return n
}

// countTotalUsers counts registered accounts, leaving out soft-deleted ones.
func countTotalUsers(users []*user.User) int {
	n := 0
	for _, u := range users {
		if u != nil && !u.DeletedUser {
			n++
		}
	}
	return n
}

// countNewUsers counts accounts awaiting sysop validation: not yet
// validated, not soft-deleted, and not banned (a banned account is level 0
// and unvalidated, and is not waiting for anything).
func countNewUsers(users []*user.User) int {
	n := 0
	for _, u := range users {
		if u == nil || u.Validated || u.DeletedUser || u.AccessLevel <= 0 {
			continue
		}
		n++
	}
	return n
}

// countSysopMailWaiting counts unread private mail addressed to the SysOp
// account (user #1, the same convention the new-user intro mail uses).
// Returns -1 when there is no SysOp account or no PRIVMAIL area.
func countSysopMailWaiting(mm *message.MessageManager, um *user.UserMgr) int {
	if mm == nil || um == nil {
		return -1
	}
	sysop, ok := um.GetUserByID(1)
	if !ok || sysop == nil {
		return -1
	}
	area, ok := mm.GetAreaByTag("PRIVMAIL")
	if !ok || area == nil {
		return -1
	}
	total, err := mm.GetMessageCountForArea(area.ID)
	if err != nil {
		slog.Debug("wfc: mail waiting: message count", "error", err)
		return -1
	}
	if total == 0 {
		return 0
	}
	lastRead, err := mm.GetLastRead(area.ID, sysop.Handle)
	if err != nil {
		// A missing lastread record is already (0, nil); an error is an I/O
		// failure, and counting from zero would report every message as
		// waiting.
		slog.Debug("wfc: mail waiting: lastread", "error", err)
		return -1
	}
	base, err := mm.GetBase(area.ID)
	if err != nil {
		slog.Debug("wfc: mail waiting: open base", "error", err)
		return -1
	}
	defer func() {
		if cerr := base.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()
	n := 0
	for num := lastRead + 1; num <= total; num++ {
		msg, err := base.ReadMessage(num)
		if err != nil || msg.IsDeleted() {
			continue
		}
		if msg.IsPrivate() && strings.EqualFold(msg.To, sysop.Handle) {
			n++
		}
	}
	return n
}

// schedulerEvents converts the scheduler's status report into the admin
// wire type. A nil scheduler yields nil (the console then has no Events tab
// content rather than an empty list that looks like "nothing configured").
func schedulerEvents(s *scheduler.Scheduler, now time.Time) []admin.ScheduledEvent {
	if s == nil {
		return nil
	}
	statuses := s.Status(now)
	out := make([]admin.ScheduledEvent, 0, len(statuses))
	for _, st := range statuses {
		out = append(out, admin.ScheduledEvent{
			ID:             st.ID,
			Name:           st.Name,
			Schedule:       st.Schedule,
			Enabled:        st.Enabled,
			RunAtStartup:   st.RunAtStartup,
			Running:        st.Running,
			NextRun:        st.NextRun,
			LastRun:        st.LastRun,
			LastStatus:     st.LastStatus,
			LastDurationMs: st.LastDurationMs,
			RunCount:       st.RunCount,
			FailureCount:   st.FailureCount,
		})
	}
	return out
}

// kickNoticeTimeout bounds the courtesy notice written before a kick. A
// caller whose connection has stalled may never drain its flow-control
// window, and the admin command must not hang behind that write.
const kickNoticeTimeout = 2 * time.Second

// kickNode disconnects the caller on nodeID by closing their SSH channel.
// connectedAt names the session the sysop selected: node numbers are reused,
// so if a different session now holds the slot the kick is refused instead of
// dropping the wrong caller (a zero connectedAt skips the check). The session
// loop sees EOF on its next read and unwinds exactly as it does for a caller
// who hangs up, so the disconnect is logged and the node is freed through the
// normal path. A short notice is written first so the caller knows this was
// deliberate.
func kickNode(reg *session.SessionRegistry, nodeID int, connectedAt time.Time) error {
	if reg == nil {
		return fmt.Errorf("session registry unavailable")
	}
	s := reg.Get(nodeID)
	if s == nil {
		return fmt.Errorf("no active session on node %d", nodeID)
	}
	s.Mutex.RLock()
	ch := s.Channel
	started := s.StartTime
	s.Mutex.RUnlock()
	if !connectedAt.IsZero() && !started.Equal(connectedAt) {
		return fmt.Errorf("node %d now has a different caller; select again", nodeID)
	}
	if ch == nil {
		return fmt.Errorf("node %d has no channel to close", nodeID)
	}
	// From here on only ch (the selected session's own channel) is touched,
	// so a slot reuse after the check above cannot redirect the close.
	written := make(chan struct{})
	go func() {
		defer close(written)
		_, _ = ch.Write([]byte("\r\n\r\nYou have been disconnected by the SysOp.\r\n")) // best-effort notice
	}()
	select {
	case <-written:
	case <-time.After(kickNoticeTimeout):
		// Close unblocks the stalled writer; do not wait for it.
	}
	return ch.Close()
}
