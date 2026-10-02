package admin

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
	"github.com/ViSiON-3/vision-3-bbs/internal/timeleft"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// RegistrySource is the read-only view of active sessions the builder needs.
// *session.SessionRegistry satisfies it via ListActive().
type RegistrySource interface {
	ListActive() []*session.BbsSession
}

// BuildSnapshot copies live session state into a serialization-safe snapshot.
// It reads each session under its RLock. counters supplies the header values
// the registry cannot derive (-1 where unavailable); ActiveNodes is always
// overwritten with the live count. timeLimit gives a caller's effective time
// limit in minutes (see ServerConfig.TimeLimit); nil uses the stored one.
// chatCredit gives the sysop chat time credited to a node's caller, added to
// the time left; nil means none.
func BuildSnapshot(reg RegistrySource, systemName string, startedAt, now time.Time, counters Counters, timeLimit func(*user.User) int, chatCredit func(nodeID int) time.Duration) *SystemSnapshot {
	if timeLimit == nil {
		timeLimit = func(u *user.User) int { return u.TimeLimit }
	}
	sessions := reg.ListActive()
	nodes := make([]NodeState, 0, len(sessions))
	for _, s := range sessions {
		var (
			limitMins int
			started   time.Time
		)
		s.Mutex.RLock()
		ns := NodeState{
			NodeID:       s.NodeID,
			CurrentMenu:  s.CurrentMenu,
			Activity:     s.Activity,
			Invisible:    s.Invisible,
			ConnectedAt:  s.StartTime,
			LastActivity: s.LastActivity,
			TimeLeftMins: -1,
		}
		if s.RemoteAddr != nil {
			ns.RemoteAddr = s.RemoteAddr.String()
		}
		if s.User != nil {
			ns.Handle = s.User.Handle
			ns.UserID = s.User.ID
			ns.AccessLevel = s.User.AccessLevel
			ns.Status = StatusOnline
			if s.Activity == "" && s.CurrentMenu != "" {
				ns.Status = StatusInMenu
			}
			if limit := timeLimit(s.User); limit <= 0 {
				ns.TimeUnlimited = true
			} else if !s.StartTime.IsZero() {
				ns.TimeLeftMins, _ = timeleft.Minutes(limit, s.StartTime, now, 0)
				limitMins, started = limit, s.StartTime
			}
		} else {
			ns.Status = StatusLogin
		}
		s.Mutex.RUnlock()
		// The hook takes the session's lock itself, so it runs after ours
		// is released.
		if limitMins > 0 && chatCredit != nil {
			ns.TimeLeftMins, _ = timeleft.Minutes(limitMins, started, now, chatCredit(ns.NodeID))
		}
		nodes = append(nodes, ns)
	}
	counters.ActiveNodes = len(nodes)
	return &SystemSnapshot{
		Time:       now,
		SystemName: systemName,
		UptimeSecs: int64(now.Sub(startedAt).Seconds()),
		Nodes:      nodes,
		Counters:   counters,
	}
}
