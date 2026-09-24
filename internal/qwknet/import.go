package qwknet

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)

// TossResult reports one import pass.
type TossResult struct {
	Packets    int // packets processed
	Imported   int
	Duplicates int
	Unmapped   int // messages for conferences no local area mirrors
	Skipped    int // votes, private mail (conference 0), routing loops
	Errors     []string
}

// Toss imports every waiting packet from the hub. A packet whose messages
// all land is deleted; one the reader cannot parse, or that came from a
// different hub, is set aside as <name>.bad; one with a message that could
// not be written is kept as .bad too, since the dupe database already
// protects the messages that did land from being posted twice on a retry.
func (n *Node) Toss() TossResult {
	var res TossResult
	areas := n.areasByConference()
	for _, path := range n.inboundPackets() {
		res.Packets++
		pr, err := n.tossPacket(path, areas)
		res.Imported += pr.Imported
		res.Duplicates += pr.Duplicates
		res.Unmapped += pr.Unmapped
		res.Skipped += pr.Skipped
		res.Errors = append(res.Errors, pr.Errors...)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", path, err))
			setAside(path)
			continue
		}
		if len(pr.Errors) > 0 {
			setAside(path)
			continue
		}
		if err := os.Remove(path); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("remove %s: %v", path, err))
		}
	}
	if n.dupes != nil {
		if err := n.dupes.Save(); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("save dupe db: %v", err))
		}
	}
	if res.Packets > 0 {
		slog.Info("qwknet toss complete", "network", n.Key, "packets", res.Packets, "imported", res.Imported,
			"duplicates", res.Duplicates, "unmapped", res.Unmapped, "skipped", res.Skipped, "errors", len(res.Errors))
	}
	return res
}

func setAside(path string) {
	bad := path + ".bad"
	if err := os.Rename(path, bad); err != nil {
		slog.Warn("could not set aside packet", "path", path, "error", err)
		return
	}
	slog.Warn("packet set aside for inspection", "path", bad)
}

// tossPacket imports one packet. The returned error means the packet as a
// whole was unusable; per-message failures go in the result's Errors.
func (n *Node) tossPacket(path string, areas map[int]*message.MessageArea) (TossResult, error) {
	var res TossResult
	f, err := os.Open(path)
	if err != nil {
		return res, err
	}
	defer func() { _ = f.Close() }() // read-only
	st, err := f.Stat()
	if err != nil {
		return res, err
	}
	p, err := qwk.ReadPacket(f, st.Size())
	if err != nil {
		return res, err
	}
	if p.BBSID != "" && !strings.EqualFold(p.BBSID, n.hubID) {
		return res, fmt.Errorf("packet is from %s, not hub %s", p.BBSID, n.hubID)
	}
	if p.ParseError != nil {
		slog.Warn("packet ended early; importing what was readable", "path", path, "error", p.ParseError)
	}

	for _, m := range p.Messages {
		switch {
		case m.Status == 'V':
			res.Skipped++ // poll/vote records live in VOTING.DAT; not supported
			continue
		case m.Conference == 0:
			res.Skipped++
			slog.Info("qwknet private mail not supported, skipped", "network", n.Key, "from", m.From, "to", m.To)
			continue
		case m.Via != "" && routeContains(m.Via, n.nodeID):
			res.Skipped++
			slog.Warn("qwknet message already passed through this node, dropped", "network", n.Key, "via", m.Via)
			continue
		}
		area, ok := areas[m.Conference]
		if !ok {
			res.Unmapped++
			slog.Warn("qwknet message for a conference no area mirrors", "network", n.Key, "conference", m.Conference, "subject", m.Subject)
			continue
		}
		key := dupeKey(m)
		if n.dupes != nil && n.dupes.IsDupe(key) {
			res.Duplicates++
			continue
		}
		if err := n.importMessage(area, m); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("conference %d -> %s: %v", m.Conference, area.Tag, err))
			continue
		}
		if n.dupes != nil {
			n.dupes.Add(key)
		}
		res.Imported++
	}
	return res, nil
}

// dupeKey identifies a message for duplicate detection: its Message-ID when
// the hub sent one, else a digest of the fields that make it the same post.
func dupeKey(m qwk.NetMessage) string {
	if m.MessageID != "" {
		return m.MessageID
	}
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%d\x00%s\x00%s\x00%s\x00%d\x00%s", m.Conference, m.From, m.To, m.Subject, m.DateTime.Unix(), m.Body) // hash.Hash never fails
	return "fp:" + hex.EncodeToString(h.Sum(nil))
}

// importMessage writes one hub message into its local area, marking it
// processed so the export scan never sends it back to the hub.
func (n *Node) importMessage(area *message.MessageArea, m qwk.NetMessage) (retErr error) {
	// Resolve the parent before opening the base: the manager opens its own
	// handle for the index and a nested open on the same base is avoided.
	replyTo := 0
	if m.ReplyID != "" {
		replyTo = n.msgMgr.FindMessageByMSGID(area.ID, m.ReplyID)
	}

	base, err := n.msgMgr.GetBase(area.ID)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := base.Close(); cerr != nil && retErr == nil {
			retErr = fmt.Errorf("closing base: %w", cerr)
		}
	}()

	jm := jam.NewMessage()
	jm.From = toCP437(m.From, m.UTF8)
	jm.To = toCP437(m.To, m.UTF8)
	jm.Subject = toCP437(m.Subject, m.UTF8)
	jm.Text = toCP437(m.Body, m.UTF8)
	if jm.To == "" {
		jm.To = message.MsgToUserAll
	}
	if m.DateTime.IsZero() {
		jm.DateTime = time.Now()
	} else {
		jm.DateTime = m.DateTime
	}
	jm.MsgID = m.MessageID
	jm.ReplyID = m.ReplyID
	if replyTo > 0 {
		jm.ReplyTo = uint32(replyTo)
	}
	route := n.hubID
	if m.Via != "" {
		route += "/" + m.Via
	}
	jm.Kludges = append(jm.Kludges, "QWKVIA: "+route)
	if m.Private {
		jm.Header = &jam.MessageHeader{Attribute: jam.MsgPrivate}
	}

	num, err := base.WriteMessageExt(jm, jam.MsgTypeEchomailMsg, "", "")
	if err != nil {
		return err
	}
	if hdr, herr := base.ReadMessageHeader(num); herr == nil {
		hdr.DateProcessed = uint32(time.Now().Unix())
		if uerr := base.UpdateMessageHeader(num, hdr); uerr != nil {
			slog.Warn("failed to mark imported message processed", "area", area.Tag, "msg", num, "error", uerr)
		}
	}
	slog.Info("qwknet tossed message", "network", n.Key, "area", area.Tag, "from", m.From, "subject", m.Subject, "msgid", m.MessageID)
	return nil
}

// toCP437 converts a UTF-8 packet string to the CP437 the message bases
// store. Bytes from a CP437 packet pass through unchanged.
func toCP437(s string, utf8 bool) string {
	if !utf8 {
		return s
	}
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r < 0x80:
			out = append(out, byte(r))
		default:
			if b, ok := ansi.UnicodeToCP437[r]; ok {
				out = append(out, b)
			} else {
				out = append(out, '?')
			}
		}
	}
	return string(out)
}
