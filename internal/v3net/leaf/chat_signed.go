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
// before. This board's own chat always passes. A room's member list, which
// the hub reports, keeps only members whose board is on the roster. Leaves
// and topic changes pass as the hub reports them. Without a roster, or
// with an open one, chat is shown as before, signed or not.

// chatSeenSize is how many live signed chat IDs a leaf remembers, to drop
// a replayed line.
const chatSeenSize = 4096

// chatSeen remembers the IDs of live signed chat actions acted on.
type chatSeen struct {
	mu    sync.Mutex
	seen  map[string]bool
	order []string
}

// claim records node's id and reports whether it is new.
func (c *chatSeen) claim(node, id string) bool {
	key := node + "\x00" + id
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen[key] {
		return false
	}
	if c.seen == nil {
		c.seen = make(map[string]bool)
	}
	if len(c.order) >= chatSeenSize {
		delete(c.seen, c.order[0])
		c.order = c.order[1:]
	}
	c.seen[key] = true
	c.order = append(c.order, key)
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
	if !l.rosterFiltering() || c.fromNode == l.cfg.Keystore.NodeID() {
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
	}
	if c.live {
		age := time.Since(signed.Issued())
		if age > protocol.MaxChatSignedAge || age < -protocol.MaxCentralClockSkew || !l.chatSeen.claim(signed.NodeID, signed.ID) {
			return false
		}
	}
	return l.rosterAdmits(signed.NodeID, signed.OriginPubKeyB64)
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

// chatAdmitsMembers keeps the room members whose board the roster names,
// and this board's own.
func (l *Leaf) chatAdmitsMembers(users []chat.ChatUser) []chat.ChatUser {
	if !l.rosterFiltering() {
		return users
	}
	l.rosterState.mu.Lock()
	r := l.rosterState.roster
	l.rosterState.mu.Unlock()
	var out []chat.ChatUser
	for _, u := range users {
		if u.Node == l.cfg.Keystore.NodeID() || rosterNamesNode(r, u.Node) {
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
