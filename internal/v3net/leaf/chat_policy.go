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

// chatAllowed is the policy check a session makes before a join or a post.
func (s *ChatSession) chatAllowed() error {
	if !s.leaf.ChatEnabled() {
		return ErrChatDisabled
	}
	return nil
}

// CloseChatSessions ends every chat session on this leaf: each is sent
// reason as a system message and then closed, so its Events channel
// delivers the reason and ends. It does not tell the hub the sessions
// left: the leaf is being stopped, and the hub drops the node's
// memberships when its stream closes.
func (l *Leaf) CloseChatSessions(reason string) {
	l.chatSessions.mu.RLock()
	sessions := make([]*ChatSession, 0, len(l.chatSessions.sessions))
	for s := range l.chatSessions.sessions {
		sessions = append(sessions, s)
	}
	l.chatSessions.mu.RUnlock()
	for _, s := range sessions {
		s.mu.Lock()
		if !s.closed && reason != "" {
			select {
			case s.events <- chat.ChatEvent{Type: chat.TypeSystem, Text: reason}:
			default:
			}
		}
		s.mu.Unlock()
		s.Close() //nolint:errcheck // always nil
	}
}
