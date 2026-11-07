package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"regexp"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// centralActivitySource tells the V3Net leaves what this board may publish
// on network Who's Online and Last Callers (leaf.CentralActivitySource).
// It is where the callers' consent is applied: a session or call is only
// returned when its account has opted in, as the account stands now, and
// never for a session or call the caller made invisible. It is read fresh
// on every publish, so a caller who opts out is gone from the next
// snapshot, and a session that ended, however it ended, is gone from the
// next heartbeat at the latest.
type centralActivitySource struct {
	sessions interface {
		ListActive() []*session.BbsSession
	}
	users interface {
		GetUserByID(id int) (*user.User, bool)
		GetLastCallers() []user.CallRecord
	}
	// key derives each session's published ID. It is random per process
	// and never leaves it, so a session ID says nothing about the account
	// or node behind it, and IDs from two calls cannot be linked.
	key [32]byte
}

// newCentralActivitySource returns a source reading sessions and users,
// with a fresh random key for this process's session IDs.
func newCentralActivitySource(sessions interface {
	ListActive() []*session.BbsSession
}, users interface {
	GetUserByID(id int) (*user.User, bool)
	GetLastCallers() []user.CallRecord
}) *centralActivitySource {
	src := &centralActivitySource{sessions: sessions, users: users}
	if _, err := rand.Read(src.key[:]); err != nil {
		panic(fmt.Sprintf("central activity: random key: %v", err))
	}
	return src
}

// centralPipeCodeRe matches the colour codes a handle may hold, which are
// removed rather than shown as text on other boards.
var centralPipeCodeRe = regexp.MustCompile(`\|[0-9]{2}`)

// centralHandle returns a handle as a snapshot carries it.
func centralHandle(h string) string {
	return protocol.CleanCentralText(centralPipeCodeRe.ReplaceAllString(h, ""), protocol.MaxCentralHandleLen)
}

// sessionID returns the published ID of the session on node that started
// at start.
func (c *centralActivitySource) sessionID(node int, start time.Time) string {
	m := hmac.New(sha256.New, c.key[:])
	_, _ = fmt.Fprintf(m, "%d:%d", node, start.UnixNano()) // hash writes do not fail
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:16])
}

// CentralPresence returns one entry per logged-in, visible session whose
// account has opted in to network Who's Online.
func (c *centralActivitySource) CentralPresence() []protocol.CentralActivityEntry {
	var out []protocol.CentralActivityEntry
	for _, sess := range c.sessions.ListActive() {
		sess.Mutex.RLock()
		u, invisible, node, start := sess.User, sess.Invisible, sess.NodeID, sess.StartTime
		sess.Mutex.RUnlock()
		// A session is eligible only once its login sequence is done,
		// when the caller has answered whether the login is visible.
		if u == nil || invisible || !sess.CentralReady() {
			continue
		}
		acct, ok := c.users.GetUserByID(u.ID)
		if !ok || acct.DeletedUser || !acct.CentralPresence {
			continue
		}
		h := centralHandle(acct.Handle)
		if h == "" {
			continue
		}
		avail := acct.CentralAvailability
		if avail != protocol.CentralAvailable && avail != protocol.CentralBusy {
			avail = ""
		}
		out = append(out, protocol.CentralActivityEntry{Handle: h, Session: c.sessionID(node, start), Availability: avail})
	}
	return out
}

// CentralLastCalls returns the visible calls since since by accounts that
// have opted in to network Last Callers, each made after the account
// opted in.
func (c *centralActivitySource) CentralLastCalls(since time.Time) []protocol.CentralActivityEntry {
	var out []protocol.CentralActivityEntry
	accounts := make(map[int]*user.User)
	for _, rec := range c.users.GetLastCallers() {
		if rec.Invisible || !rec.ConnectTime.After(since) {
			continue
		}
		acct, seen := accounts[rec.UserID]
		if !seen {
			if u, ok := c.users.GetUserByID(rec.UserID); ok {
				acct = u
			}
			accounts[rec.UserID] = acct
		}
		if acct == nil || acct.DeletedUser || acct.CentralLastCallsSince == nil || rec.ConnectTime.Before(*acct.CentralLastCallsSince) {
			continue
		}
		h := centralHandle(acct.Handle)
		if h == "" {
			continue
		}
		e := protocol.CentralActivityEntry{Handle: h, At: rec.ConnectTime.UTC().Format(time.RFC3339)}
		if !rec.DisconnectTime.IsZero() {
			e.Until = rec.DisconnectTime.UTC().Format(time.RFC3339)
		}
		out = append(out, e)
	}
	return out
}
