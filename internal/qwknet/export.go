package qwknet

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)

// ScanResult reports one export pass.
type ScanResult struct {
	Exported int    // messages newly added to the REP
	Pending  int    // messages already waiting from earlier passes
	REPPath  string // the REP file, when one exists after the pass
	Errors   []string
}

// pendingExport is a message chosen for export, with what is needed to
// mark it processed once the packet is safely on disk.
type pendingExport struct {
	base   *jam.Base
	hdr    *jam.MessageHeader
	msgNum int
	area   *message.MessageArea
}

// Scan collects every unexported message from this network's areas into
// <HUBID>.REP in the outbound directory. A REP already waiting there (an
// upload that has not happened yet) keeps its messages: the new ones are
// appended and the packet rewritten. Pointers advance only after the packet
// is written, so a failure leaves the messages to be picked up next time.
func (n *Node) Scan() ScanResult {
	var res ScanResult
	repPath := n.repPath()

	existing, err := n.pendingREPMessages(repPath)
	if err != nil {
		bad := freeBadName(repPath)
		slog.Warn("unreadable REP set aside", "network", n.Key, "path", repPath, "moved_to", bad, "error", err)
		if rerr := os.Rename(repPath, bad); rerr != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("set aside unreadable REP: %v", rerr))
			return res
		}
		existing = nil
	}
	res.Pending = len(existing)

	var outgoing []qwk.NetMessage
	var pending []pendingExport
	var openBases []*jam.Base
	defer func() {
		for _, b := range openBases {
			_ = b.Close() // best-effort close of read-side bases
		}
	}()

	for conf, area := range n.areasByConference() {
		base, err := n.msgMgr.GetBase(area.ID)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("area %s: %v", area.Tag, err))
			continue
		}
		openBases = append(openBases, base)
		count, err := base.GetMessageCount()
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("area %s: count: %v", area.Tag, err))
			continue
		}
		hwm := highWater(base)
		// advance moves the pointer past a message that needs no export,
		// but only while the run from the pointer is unbroken: a pending
		// message further back must stay in front of it.
		advance := func(num int) {
			if num != hwm+1 {
				return
			}
			if err := base.SetLastRead(ScannerUser, uint32(num), uint32(num)); err != nil {
				slog.Warn("failed to advance export pointer", "network", n.Key, "area", area.Tag, "error", err)
				return
			}
			hwm = num
		}
		for num := hwm + 1; num <= count; num++ {
			hdr, err := base.ReadMessageHeader(num)
			if err != nil {
				// Left in place: the pointer stops here and the message is
				// looked at again next time.
				slog.Warn("cannot read message header; will retry next scan", "network", n.Key, "area", area.Tag, "msg", num, "error", err)
				res.Errors = append(res.Errors, fmt.Sprintf("area %s msg %d: %v", area.Tag, num, err))
				continue
			}
			if hdr.DateProcessed != 0 || hdr.Attribute&jam.MsgDeleted != 0 {
				advance(num)
				continue
			}
			msg, err := base.ReadMessage(num)
			if err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("area %s msg %d: %v", area.Tag, num, err))
				continue
			}
			if hasQWKVia(msg) {
				// Imported from the hub but never marked processed (the
				// mark failed); it must not go back out.
				advance(num)
				continue
			}
			outgoing = append(outgoing, n.netMessageFor(base, area, conf, num, msg))
			pending = append(pending, pendingExport{base: base, hdr: hdr, msgNum: num, area: area})
		}
	}

	if len(outgoing) == 0 {
		if len(existing) > 0 {
			res.REPPath = repPath
		}
		return res
	}

	all := append(existing, outgoing...)
	if err := n.writeREP(repPath, all); err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("write REP: %v", err))
		return res
	}
	res.REPPath = repPath
	res.Exported = len(outgoing)

	now := uint32(time.Now().Unix())
	for _, p := range pending {
		p.hdr.DateProcessed = now
		if err := p.base.UpdateMessageHeader(p.msgNum, p.hdr); err != nil {
			slog.Warn("failed to mark message exported", "network", n.Key, "area", p.area.Tag, "msg", p.msgNum, "error", err)
		}
		// The pointer only moves over an unbroken run. A message that could
		// not be read sits between the pointer and this one, and jumping
		// past it would drop it for good; DateProcessed still keeps this
		// message from being packed twice.
		if p.msgNum == highWater(p.base)+1 {
			if err := p.base.SetLastRead(ScannerUser, uint32(p.msgNum), uint32(p.msgNum)); err != nil {
				slog.Warn("failed to advance export pointer", "network", n.Key, "area", p.area.Tag, "error", err)
			}
		}
	}
	slog.Info("qwknet REP packed", "network", n.Key, "hub", n.hubID, "new", res.Exported, "pending", res.Pending, "path", repPath)
	return res
}

// hasQWKVia reports whether a stored message carries the QWKVIA kludge the
// tosser stamps on everything that arrived from a hub.
func hasQWKVia(m *jam.Message) bool {
	for _, k := range m.Kludges {
		if strings.HasPrefix(strings.TrimPrefix(k, "\x01"), "QWKVIA:") {
			return true
		}
	}
	return false
}

// highWater reads the area's export pointer; 0 when none is recorded yet.
func highWater(base *jam.Base) int {
	lr, err := base.GetLastRead(ScannerUser)
	if err != nil {
		if err != jam.ErrNotFound {
			slog.Warn("failed to read qwknet export pointer; area will be rescanned", "error", err)
		}
		return 0
	}
	return int(lr.LastReadMsg)
}

// netMessageFor converts a stored message to its packet form. A message
// without a MSGID gets one keyed on this node so replies can thread back.
func (n *Node) netMessageFor(base *jam.Base, area *message.MessageArea, conf, num int, msg *jam.Message) qwk.NetMessage {
	body := strings.ReplaceAll(msg.Text, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	m := qwk.NetMessage{
		Conference:    conf,
		Number:        num,
		From:          msg.From,
		To:            msg.To,
		Subject:       msg.Subject,
		DateTime:      msg.DateTime,
		Body:          body,
		Private:       msg.IsPrivate(),
		MessageID:     msg.MsgID,
		ReplyID:       msg.ReplyID,
		SenderNetAddr: n.nodeID,
	}
	if m.To == "" {
		m.To = message.MsgToUserAll
	}
	if m.MessageID == "" {
		m.MessageID = SynthMessageID(n.nodeID, area.Tag, num)
	}
	// A reply written locally records its parent by number; carry the
	// parent's MSGID so the hub can thread it.
	// A parent that has no MSGID of its own was (or will be) exported under
	// the synthesized one, so the same synthesis names it here.
	if m.ReplyID == "" && msg.Header != nil && msg.Header.ReplyTo > 0 {
		parentNum := int(msg.Header.ReplyTo)
		if parent, err := base.ReadMessage(parentNum); err == nil {
			m.ReplyID = parent.MsgID
			if m.ReplyID == "" {
				m.ReplyID = SynthMessageID(n.nodeID, area.Tag, parentNum)
			}
		}
	}
	return m
}

// SynthMessageID builds the Message-ID for a message that has none:
// <number.areatag@nodeid>, unique per base and readable on the far side.
func SynthMessageID(nodeID, areaTag string, num int) string {
	return fmt.Sprintf("<%d.%s@%s>", num, strings.ToLower(areaTag), strings.ToLower(nodeID))
}

// pendingREPMessages reads back the messages of a REP that is still
// waiting for upload, so a rescan can append to it rather than lose them.
func (n *Node) pendingREPMessages(repPath string) ([]qwk.NetMessage, error) {
	f, err := os.Open(repPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	// An empty REP is one already uploaded that could not be removed
	// (retireUploadedREP empties it as a last resort): nothing is waiting.
	if st.Size() == 0 {
		return nil, nil
	}
	p, err := qwk.ReadREPPacket(f, st.Size(), n.hubID)
	if err != nil {
		return nil, err
	}
	hdrs, _ := qwk.ReadArchiveHeaders(f, st.Size())
	_, msgs, perr := qwk.ParseMSGPayload(p.Payload, hdrs)
	if perr != nil {
		return nil, perr
	}
	return msgs, nil
}

// writeREP builds the packet beside the REP and moves it into place so a
// crash mid-write never leaves a truncated REP for the uploader. The temp
// file lives in the outbound directory, not TempPath, so the rename never
// crosses a filesystem when the two are configured on different volumes.
func (n *Node) writeREP(repPath string, msgs []qwk.NetMessage) error {
	var buf bytes.Buffer
	opts := qwk.NetREPOptions{Tagline: n.cfg.Tagline, NoHeaders: n.cfg.NoHeaders, NodeID: n.nodeID}
	if err := qwk.WriteNetREP(&buf, n.hubID, msgs, opts); err != nil {
		return err
	}
	tmp := repPath + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	// Replace rather than Rename: a REP waiting for upload is overwritten
	// with the appended one, and on Windows Rename refuses to replace.
	if err := atomicfile.Replace(tmp, repPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
