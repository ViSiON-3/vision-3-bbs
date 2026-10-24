package v3net

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// OutboundStore is the message base side of the outbound sweep, satisfied
// by *message.MessageManager.
type OutboundStore interface {
	GetAreaByID(id int) (*message.MessageArea, bool)
	PendingOutbound(areaID int) ([]int, error)
	GetMessage(areaID, msgNum int) (*message.DisplayMessage, error)
	MarkMessageSent(areaID, msgNum int) error
}

// OutboundNetwork is the V3Net side of the outbound sweep, satisfied by
// *Service.
type OutboundNetwork interface {
	AreaBindings() map[int]AreaBinding
	NodeID() string
	SendMessage(network string, msg protocol.Message) error
}

// Outbound sends local posts in V3Net areas to their hub. Posts are written
// unprocessed (see message.MessageManager.IsV3NetArea); the sweep finds them,
// sends each one, and marks it sent. Because it is the only sender, a post
// made from the BBS, one written by another program such as `helper post`,
// and one whose first send failed because the hub was down all go out the
// same way. Every attempt at a post carries the same wire UUID (see
// outboundUUID), so a retry of a post the hub already has, after a lost
// response or a failed sent mark, is dropped by the hub as a duplicate.
//
// Run performs the sweep on a timer and whenever Nudge is called. It is safe
// to call Nudge from any goroutine.
type Outbound struct {
	store  OutboundStore
	net    OutboundNetwork
	board  func() string            // board name for OriginBoard, read at send time
	origin func(AreaBinding) string // origin line for a binding, read at send time
	every  time.Duration            // timer period
	nudge  chan struct{}            // buffered(1), so pending nudges merge

	mu     sync.Mutex      // serialises sweeps; guards failed
	failed map[string]bool // wire UUIDs already logged as failing
}

// NewOutbound returns a sweep over store sending through net every period.
// board and origin supply the board name and a binding's origin line when a
// message is sent, so a renamed board takes effect without a restart.
func NewOutbound(store OutboundStore, net OutboundNetwork, board func() string, origin func(AreaBinding) string, every time.Duration) *Outbound {
	return &Outbound{
		store:  store,
		net:    net,
		board:  board,
		origin: origin,
		every:  every,
		nudge:  make(chan struct{}, 1),
		failed: make(map[string]bool),
	}
}

// Nudge asks Run to sweep now, for a post just made from the BBS. It never
// blocks; nudges that arrive while one is pending are merged.
func (o *Outbound) Nudge() {
	select {
	case o.nudge <- struct{}{}:
	default:
	}
}

// Run sweeps once at start, then on every nudge and timer tick, until ctx is
// done.
func (o *Outbound) Run(ctx context.Context) {
	t := time.NewTicker(o.every)
	defer t.Stop()
	for {
		o.SweepNow()
		select {
		case <-ctx.Done():
			return
		case <-o.nudge:
		case <-t.C:
		}
	}
}

// SweepNow sends every pending post in every bound area and returns how many
// were sent. Errors are logged; a message that fails stays pending and is
// tried again on the next sweep.
func (o *Outbound) SweepNow() int {
	o.mu.Lock()
	defer o.mu.Unlock()

	bindings := o.net.AreaBindings()
	ids := make([]int, 0, len(bindings))
	for id := range bindings {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	sent := 0
	for _, areaID := range ids {
		sent += o.sweepArea(areaID, bindings[areaID])
	}
	return sent
}

func (o *Outbound) sweepArea(areaID int, binding AreaBinding) int {
	area, ok := o.store.GetAreaByID(areaID)
	if !ok {
		return 0
	}
	pending, err := o.store.PendingOutbound(areaID)
	if err != nil {
		slog.Warn("v3net outbound: scanning area", "area", area.Tag, "error", err)
		return 0
	}

	sent := 0
	for _, num := range pending {
		dm, err := o.store.GetMessage(areaID, num)
		if err != nil {
			o.logFailure(fmt.Sprintf("%d/%d", areaID, num), area.Tag, num, "reading message", err)
			continue
		}
		body := StripV3NetOrigin(dm.Body)
		msg := BuildWireMessage(binding.Network, area.Tag, o.net.NodeID(), o.board(),
			dm.From, dm.To, dm.Subject, body, o.origin(binding))
		msg.MsgUUID = outboundUUID(o.net.NodeID(), binding.Network, area.Tag, dm, body)
		msg.ThreadUUID = msg.MsgUUID
		if !dm.DateTime.IsZero() {
			msg.DateUTC = dm.DateTime.UTC().Format(time.RFC3339)
		}
		if err := o.net.SendMessage(binding.Network, msg); err != nil {
			o.logFailure(msg.MsgUUID, area.Tag, num, "sending to hub", err)
			continue
		}
		sent++
		delete(o.failed, msg.MsgUUID)
		if err := o.store.MarkMessageSent(areaID, num); err != nil {
			// The next sweep sends it again under the same UUID, which the
			// hub ignores, and tries the mark again.
			slog.Warn("v3net outbound: sent but could not mark as sent", "area", area.Tag, "msg", num, "error", err)
			continue
		}
		slog.Info("v3net outbound: sent", "area", area.Tag, "msg", num, "network", binding.Network, "uuid", msg.MsgUUID)
	}
	return sent
}

// outboundUUID derives a post's wire UUID from what identifies it: this
// node, the network and area, and the post's date, sender, recipient,
// subject and body. Every attempt at the same post gives the same UUID, and
// it survives a pack renumbering the base and a restart. The value is a
// SHA-256 digest in the version-4 UUID layout that protocol.Message requires
// (version nibble 4, variant 10xx).
func outboundUUID(nodeID, network, areaTag string, dm *message.DisplayMessage, body string) string {
	h := sha256.New()
	for _, part := range []string{nodeID, network, areaTag,
		strconv.FormatInt(dm.DateTime.Unix(), 10), dm.From, dm.To, dm.Subject, body} {
		_, _ = h.Write([]byte(part)) // hash.Hash writes never fail
		_, _ = h.Write([]byte{0})
	}
	var b [16]byte
	copy(b[:], h.Sum(nil))
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// logFailure logs a post's first failure at Warn and later ones at Debug, so
// a hub that stays down does not flood the log once a minute. key identifies
// the post across sweeps.
func (o *Outbound) logFailure(key, tag string, num int, what string, err error) {
	if o.failed[key] {
		slog.Debug("v3net outbound: still failing", "area", tag, "msg", num, "step", what, "error", err)
		return
	}
	o.failed[key] = true
	slog.Warn("v3net outbound: "+what+"; will retry", "area", tag, "msg", num, "error", err)
}

// StripV3NetOrigin removes the tearline and origin line the BBS appends to
// the local copy of a V3Net post (AppendV3NetOrigin), giving back the body as
// written. The receiving side appends them again from the message's Tearline
// and Origin fields. Bodies without that trailer are returned unchanged,
// apart from trailing blank lines.
func StripV3NetOrigin(body string) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	end := len(lines)
	if end > 0 && strings.HasPrefix(strings.TrimSpace(lines[end-1]), "* Origin:") {
		end--
	}
	if end > 0 && strings.HasPrefix(lines[end-1], "--- ") {
		end--
	}
	return strings.TrimRight(strings.Join(lines[:end], "\n"), "\n")
}
