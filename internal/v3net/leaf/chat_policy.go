package leaf

import (
	"errors"
	"slices"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Network chat is offered on a subscription only with the sysop's Chat
// switch on (which needs Central on). The switch is checked on the leaf,
// at each join and post, so a session that was handed out while the
// switch was on cannot carry on after a reload turned it off; and a
// reload ends the sessions of the leaf it stops (CloseChatSessions), so
// callers in a room are told rather than left on a dead stream.

// ErrChatDisabled is returned by a ChatSession when network chat is not
// offered on its subscription.
var ErrChatDisabled = errors.New("leaf: network chat is not offered on this network")

// ErrChatEnded is returned by a ChatSession on a leaf that a reload has
// stopped (see CloseChatSessions): a picker that kept the old leaf's
// session factory gets this rather than a session on a dead stream.
var ErrChatEnded = errors.New("leaf: this board's connection to the network was reset; try again")

// errChatSessionClosed is returned by a closed ChatSession.
var errChatSessionClosed = errors.New("chat session closed")

// ChatStatus is what a leaf knows about network chat on its network.
type ChatStatus struct {
	// Enabled is the sysop's switch for chat on this network, which needs
	// Central itself on.
	Enabled bool
	// SupportKnown is whether the hub's feature list has been read, and
	// Supported whether it names chat.
	SupportKnown bool
	Supported    bool
}

// ChatEnabled reports whether the sysop offers network chat on this
// subscription.
func (l *Leaf) ChatEnabled() bool { return l.cfg.CentralEnabled && l.cfg.CentralChat }

// ChatStatus returns what the leaf knows about network chat on its
// network.
func (l *Leaf) ChatStatus() ChatStatus {
	l.central.mu.Lock()
	defer l.central.mu.Unlock()
	return ChatStatus{
		Enabled:      l.ChatEnabled(),
		SupportKnown: l.central.featuresKnown,
		Supported:    slices.Contains(l.central.features, protocol.FeatureCentralChat),
	}
}

// chatAllowed is the policy check a session makes before a join, a post
// or a private message: the leaf still running, chat offered on it, and
// the session open.
func (s *ChatSession) chatAllowed() error {
	s.leaf.chatSessions.mu.RLock()
	retired := s.leaf.chatSessions.retired
	s.leaf.chatSessions.mu.RUnlock()
	if retired {
		return ErrChatEnded
	}
	if !s.leaf.ChatEnabled() {
		return ErrChatDisabled
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errChatSessionClosed
	}
	return nil
}

// CloseChatSessions ends every chat session on this leaf and retires the
// leaf for chat: each session is sent reason as a system message and then
// closed, so its Events channel delivers the reason and ends, and a
// session created afterwards (a caller's picker may still hold the leaf)
// is closed as it is made. It does not tell the hub the sessions left:
// the leaf is being stopped, and the hub drops the node's memberships
// when its stream closes.
func (l *Leaf) CloseChatSessions(reason string) {
	r := l.chatSessions
	r.mu.Lock()
	r.retired = true
	sessions := make([]*ChatSession, 0, len(r.sessions))
	for s := range r.sessions {
		sessions = append(sessions, s)
	}
	r.mu.Unlock()
	for _, s := range sessions {
		s.endWith(reason)
		r.deregister(s)
	}
}

// endWith closes the session with reason as its last event. The reason
// and the close happen under s.mu, which dispatch also takes to deliver,
// so nothing follows the reason; a full buffer gives up its oldest event
// to make room, so a slow reader still learns why the channel ended.
func (s *ChatSession) endWith(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if reason != "" {
		ev := chat.ChatEvent{Type: chat.TypeSystem, Text: reason}
		select {
		case s.events <- ev:
		default:
			select {
			case <-s.events:
			default:
			}
			select {
			case s.events <- ev:
			default:
			}
		}
	}
	s.closed = true
	close(s.events)
}
