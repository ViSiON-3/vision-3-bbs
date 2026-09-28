package session

import (
	"sort"
	"sync"
)

// SessionRegistry tracks all active BBS sessions.
type SessionRegistry struct {
	mu       sync.RWMutex
	sessions map[int]*BbsSession
}

// NewSessionRegistry returns an empty registry, ready for use.
func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{
		sessions: make(map[int]*BbsSession),
	}
}

// Register adds s to the registry under s.NodeID, replacing any session
// already registered for that node. Safe for concurrent use.
func (r *SessionRegistry) Register(s *BbsSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s.NodeID] = s
}

// Unregister removes the session for nodeID; it is a no-op if none is
// registered. Safe for concurrent use.
func (r *SessionRegistry) Unregister(nodeID int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, nodeID)
}

// Get returns the session registered for nodeID, or nil if that node is not
// in use. The returned session is shared; guard access to its mutable fields
// with its Mutex.
func (r *SessionRegistry) Get(nodeID int) *BbsSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sessions[nodeID]
}

// ActiveCount returns the number of currently active sessions.
func (r *SessionRegistry) ActiveCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sessions)
}

// ListActive returns a snapshot of the registered sessions sorted by NodeID.
// The slice is the caller's to keep, but the sessions it points to are
// shared and may change or disconnect after it returns.
func (r *SessionRegistry) ListActive() []*BbsSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*BbsSession, 0, len(r.sessions))
	for _, s := range r.sessions {
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].NodeID < result[j].NodeID
	})
	return result
}
