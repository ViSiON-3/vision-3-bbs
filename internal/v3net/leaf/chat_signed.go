package leaf

import (
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/chat"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// Signed network chat on a leaf (#1008, see protocol.ChatSigned).
//
// Sending: every room join, room line and private message this board sends
// is signed with its node key and sent with the request.
//
// Receiving: while the board holds a board roster that is not open, chat
// reaches its callers only from boards on it. A line, join or private
// message must carry a signed action that verifies, says what the hub says
// it does (kind, board, handle, room, text, recipient), and is signed with
// a key the roster admits; a live one must also be fresh and not seen
// before. This board's own chat is held to the same signature checks (the
// hub only says where a message came from), and passes the roster as this
// board's. A room's member list, which the hub reports, keeps only members
// whose board is on the roster, and of this board's own only the handles a
// session here really has in that room. Leaves and topic changes pass as
// the hub reports them. Without a roster, or with an open one, chat is
// shown as before, signed or not.

// chatSeenSize is the most live signed chat IDs a leaf holds at once.
// Each is kept until its action can no longer pass as fresh, so a replay
// is always caught; when that many are all still fresh, a new action is
// refused rather than one forgotten that could then be replayed.
const chatSeenSize = 16384

// chatSeen remembers the IDs of live signed chat actions acted on, each
// until the action it names is too old to be taken as live.
type chatSeen struct {
	mu   sync.Mutex
	seen map[string]time.Time // node \x00 id -> when it stops mattering
}

// claim records node's action id, issued at issued, and reports whether it
// may be acted on: it is new, and there is room to remember it for as long
// as it could be replayed.
func (c *chatSeen) claim(node, id string, issued, now time.Time) bool {
	key := node + "\x00" + id
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.seen[key]; ok {
		return false
	}
	if c.seen == nil {
		c.seen = make(map[string]time.Time)
	}
	if len(c.seen) >= chatSeenSize {
		for k, until := range c.seen {
			if !until.After(now) {
				delete(c.seen, k)
			}
		}
		if len(c.seen) >= chatSeenSize {
			return false
		}
	}
	// Fresh until MaxChatSignedAge after issue; a clock running ahead by
	// up to the allowed skew stretches that.
	c.seen[key] = issued.Add(protocol.MaxChatSignedAge + protocol.MaxCentralClockSkew)
	return true
}

// rosterFiltering reports whether the board holds a roster that filters:
// held, and not open.
func (l *Leaf) rosterFiltering() bool {
	l.rosterState.mu.Lock()
	defer l.rosterState.mu.Unlock()
	return l.rosterState.roster != nil && !l.rosterState.roster.Open
}

// chatClaim is what the hub says a chat action is, to hold its signed form
// to.
type chatClaim struct {
	kind, fromNode, handle, room, text, toHandle string
	live                                         bool
}

// chatAdmits reports whether chat the hub reports as c, carrying signed,
// may reach this board's callers (see the notes above).
func (l *Leaf) chatAdmits(c chatClaim, signed *protocol.ChatSigned) bool {
	// A claim to come from this board is the hub's word like any other, so
	// it is checked as any other; only the roster's membership test treats
	// this board as admitted (rosterAdmits).
	if !l.rosterFiltering() {
		return true
	}
	if signed == nil || signed.Validate(l.cfg.Network) != nil || signed.Verify() != nil {
		return false
	}
	if signed.Kind != c.kind || signed.NodeID != c.fromNode || signed.Handle != c.handle {
		return false
	}
	switch c.kind {
	case protocol.ChatSignedLine:
		if signed.Room != c.room || signed.Text != c.text {
			return false
		}
	case protocol.ChatSignedJoin:
		if signed.Room != c.room {
			return false
		}
	case protocol.ChatSignedPrivate:
		if signed.ToHandle != c.toHandle || signed.Text != c.text {
			return false
		}
		// Addressed to a board, it must be this one: the hub cannot send
		// on a message signed for someone else's Bob. An empty node is one
		// the hub was asked to resolve.
		if signed.ToNode != "" && signed.ToNode != l.cfg.Keystore.NodeID() {
			return false
		}
	}
	// The roster is checked before the replay cache records the action, so
	// off-roster boards cannot fill it and crowd out admitted ones.
	if !l.rosterAdmits(signed.NodeID, signed.OriginPubKeyB64) {
		return false
	}
	if c.live {
		age := time.Since(signed.Issued())
		if age > protocol.MaxChatSignedAge || age < -protocol.MaxCentralClockSkew ||
			!l.chatSeen.claim(signed.NodeID, signed.ID, signed.Issued(), time.Now()) {
			return false
		}
	}
	return true
}

// chatAdmitsHistory keeps the room history lines chatAdmits passes.
func (l *Leaf) chatAdmitsHistory(room string, msgs []protocol.ChatMsgPayload) []protocol.ChatMsgPayload {
	if !l.rosterFiltering() {
		return msgs
	}
	var out []protocol.ChatMsgPayload
	for _, m := range msgs {
		if l.chatAdmits(chatClaim{kind: protocol.ChatSignedLine, fromNode: m.FromNode, handle: m.FromHandle, room: room, text: m.Text}, m.Signed) {
			out = append(out, m)
		}
	}
	return out
}

// chatAdmitsMembers keeps a room's members whose board the roster names,
// and of this board's own only those in local, the handles sessions here
// have in the room: the hub could otherwise list callers of this board who
// are not there. It takes no session lock, so callers may hold one.
func (l *Leaf) chatAdmitsMembers(local map[string]bool, users []chat.ChatUser) []chat.ChatUser {
	if !l.rosterFiltering() {
		return users
	}
	l.rosterState.mu.Lock()
	r := l.rosterState.roster
	l.rosterState.mu.Unlock()
	var out []chat.ChatUser
	for _, u := range users {
		if u.Node == l.cfg.Keystore.NodeID() {
			if local[u.Handle] {
				out = append(out, u)
			}
			continue
		}
		if rosterNamesNode(r, u.Node) {
			out = append(out, u)
		}
	}
	return out
}

// rosterNamesNode reports whether r names node, whatever its key: the hub's
// member list carries no key to check.
func rosterNamesNode(r *protocol.CentralRoster, node string) bool {
	if r == nil {
		return false
	}
	for _, m := range r.Members {
		if m.NodeID == node {
			return true
		}
	}
	return false
}

// signChat signs a chat action of kind by handle as this board.
func (l *Leaf) signChat(kind, handle string, fill func(c *protocol.ChatSigned)) (*protocol.ChatSigned, error) {
	return protocol.NewChatSigned(l.cfg.Keystore, kind, l.cfg.Network, handle, fill)
}
