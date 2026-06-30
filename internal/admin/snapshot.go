package admin

import (
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/session"
)

// RegistrySource is the read-only view of active sessions the builder needs.
// *session.SessionRegistry satisfies it via ListActive().
type RegistrySource interface {
	ListActive() []*session.BbsSession
}

// BuildSnapshot copies live session state into a serialization-safe snapshot.
// It reads each session under its RLock. callsToday may be -1 if unavailable.
func BuildSnapshot(reg RegistrySource, systemName string, startedAt, now time.Time, callsToday int) *SystemSnapshot {
	sessions := reg.ListActive()
	nodes := make([]NodeState, 0, len(sessions))
	for _, s := range sessions {
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
			if s.User.TimeLimit > 0 && !s.StartTime.IsZero() {
				used := int(now.Sub(s.StartTime).Minutes())
				left := s.User.TimeLimit - used
				if left < 0 {
					left = 0
				}
				ns.TimeLeftMins = left
			}
		} else {
			ns.Status = StatusLogin
		}
		s.Mutex.RUnlock()
		nodes = append(nodes, ns)
	}
	return &SystemSnapshot{
		Time:       now,
		SystemName: systemName,
		UptimeSecs: int64(now.Sub(startedAt).Seconds()),
		Nodes:      nodes,
		Counters:   Counters{ActiveNodes: len(nodes), CallsToday: callsToday},
	}
}
