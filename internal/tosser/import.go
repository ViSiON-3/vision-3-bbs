package tosser

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// inboundDirs returns all inbound directories to scan, deduplicating empty paths.
func (t *Tosser) inboundDirs() []string {
	seen := make(map[string]bool)
	var dirs []string
	for _, d := range []string{t.paths.InboundPath, t.paths.SecureInboundPath} {
		if d != "" && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// TossResult holds the results of a toss/scan cycle.
type TossResult struct {
	PacketsProcessed int
	MessagesImported int
	MessagesExported int
	DupesSkipped     int
	Errors           []string
}

// ScannerUser is the synthetic username stored in each JAM base's .jlr file
// to track the export high-water mark. This follows the traditional FTN convention
// of keeping per-base scanner positions in the lastread file.
const ScannerUser = "v3mail"

// Tosser handles importing and exporting FTN echomail packets for a single network.
type Tosser struct {
	networkName    string
	config         networkConfig
	paths          pathConfig
	netmailAreaTag string // tag of the netmail area for this network, derived from message areas
	msgMgr         *message.MessageManager
	dupeDB         *DupeDB
	ownAddr        *jam.FidoAddress
}

// New creates a new Tosser instance for a single FTN network.
func New(networkName string, cfg networkConfig, globalCfg config.FTNConfig, dupeDB *DupeDB, msgMgr *message.MessageManager) (*Tosser, error) {
	addr, err := jam.ParseAddress(cfg.OwnAddress)
	if err != nil {
		return nil, fmt.Errorf("tosser[%s]: invalid own_address %q: %w", networkName, cfg.OwnAddress, err)
	}

	// Derive the netmail area tag from message areas (AreaType == "netmail" for this network).
	netmailAreaTag := ""
	for _, area := range msgMgr.ListAreas() {
		if strings.EqualFold(area.Network, networkName) && strings.EqualFold(area.AreaType, "netmail") {
			netmailAreaTag = area.Tag
			break
		}
	}

	return &Tosser{
		networkName:    networkName,
		config:         cfg,
		netmailAreaTag: netmailAreaTag,
		paths: pathConfig{
			InboundPath:       globalCfg.InboundPath,
			SecureInboundPath: globalCfg.SecureInboundPath,
			OutboundPath:      globalCfg.OutboundPath,
			BinkdOutboundPath: globalCfg.BinkdOutboundPath,
			TempPath:          globalCfg.TempPath,
			BadAreaTag:        globalCfg.BadAreaTag,
			DupeAreaTag:       globalCfg.DupeAreaTag,
		},
		msgMgr:  msgMgr,
		dupeDB:  dupeDB,
		ownAddr: addr,
	}, nil
}

// NewDupeDBFromPath creates a shared DupeDB for use across multiple tossers.
func NewDupeDBFromPath(dupeDBPath string) (*DupeDB, error) {
	maxAge := 30 * 24 * time.Hour // 30 day dupe history
	return NewDupeDB(dupeDBPath, maxAge)
}

// ProcessInbound scans all configured inbound directories for .PKT files and
// ZIP bundles, unpacking bundles as needed, then tosses each packet.
func (t *Tosser) ProcessInbound() TossResult {
	result := TossResult{}

	for _, inboundDir := range t.inboundDirs() {
		t.processInboundDir(inboundDir, &result)
	}

	// Save dupe DB after processing all directories
	if err := t.dupeDB.Save(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("save dupe DB: %v", err))
	}

	return result
}

// processInboundDir scans a single directory for bundles and .PKT files.
func (t *Tosser) processInboundDir(dir string, result *TossResult) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		result.Errors = append(result.Errors, fmt.Sprintf("read inbound dir %s: %v", dir, err))
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		nameLower := strings.ToLower(name)
		path := filepath.Join(dir, name)

		if strings.HasSuffix(nameLower, ".pkt") {
			// Direct .PKT file
			t.tossPktFile(path, name, result)
			continue
		}

		if ftn.BundleExtension(nameLower) {
			// Potential ZIP bundle — verify magic bytes first
			isZIP, err := ftn.IsZIPBundle(path)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("check bundle %s: %v", name, err))
				continue
			}
			if !isZIP {
				continue // .flo or other non-ZIP file, skip
			}
			t.processBundle(path, name, result)
		}
	}
}

// processBundle unpacks a ZIP bundle, tosses its .PKT contents, then removes it.
func (t *Tosser) processBundle(path, name string, result *TossResult) {
	tempDir := filepath.Join(t.paths.TempPath, "unpack")
	pktPaths, err := ftn.ExtractBundle(path, tempDir)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("extract bundle %s: %v", name, err))
		// Move bad bundle to temp for inspection
		badPath := filepath.Join(t.paths.TempPath, name)
		if renErr := os.Rename(path, badPath); renErr != nil {
			slog.Warn("failed to move bad bundle", "path", path, "error", renErr)
		}
		return
	}

	slog.Info("unpacked bundle", "bundle", name, "count", len(pktPaths))

	// tossPktFile handles cleanup of each extracted .PKT (removes on success, moves to temp on error).
	allSkipped := true
	var skippedPkts []string
	for _, pktPath := range pktPaths {
		skipped := t.tossPktFile(pktPath, filepath.Base(pktPath), result)
		if !skipped {
			allSkipped = false
		} else {
			skippedPkts = append(skippedPkts, pktPath)
		}
	}

	// If all packets in the bundle belong to a different network, leave the
	// bundle in place for the correct tosser and clean up extracted temp files.
	if allSkipped && len(pktPaths) > 0 {
		for _, pktPath := range pktPaths {
			_ = os.Remove(pktPath) // best-effort cleanup of skipped packets
		}
		slog.Debug("skipping foreign bundle", "network", t.networkName, "bundle", name)
		return
	}

	// In the mixed-bundle case (some packets processed, some skipped), the bundle
	// is removed below but skipped packets would otherwise strand in unpack/ forever
	// since no tosser rescans that directory. Move them back to the inbound dir so
	// the appropriate tosser picks them up on this or a future toss cycle.
	for _, pktPath := range skippedPkts {
		destPath := filepath.Join(t.paths.InboundPath, filepath.Base(pktPath))
		if err := os.Rename(pktPath, destPath); err != nil {
			// Do not delete on rename failure — a stranded packet in unpack/ is
			// recoverable; a deleted packet is not.
			slog.Warn("failed to re-queue skipped packet, left in unpack dir for manual recovery", "network", t.networkName, "packet", filepath.Base(pktPath), "error", err)
		} else {
			slog.Debug("re-queued skipped packet to inbound", "network", t.networkName, "packet", filepath.Base(pktPath))
		}
	}

	// Remove the bundle itself after processing all packets.
	if err := os.Remove(path); err != nil {
		slog.Warn("failed to remove processed bundle", "path", path, "error", err)
	}
}

// tossPktFile processes a single .PKT file at path and updates result.
// Returns true if the packet was skipped (foreign network).
func (t *Tosser) tossPktFile(path, displayName string, result *TossResult) bool {
	imported, dupes, errs, skipped := t.tossPacket(path)
	if skipped {
		return true // packet doesn't belong to this network; leave for correct tosser
	}

	result.PacketsProcessed++
	result.MessagesImported += imported
	result.DupesSkipped += dupes
	result.Errors = append(result.Errors, errs...)

	if len(errs) == 0 {
		if err := os.Remove(path); err != nil {
			slog.Warn("failed to remove processed packet", "path", path, "error", err)
		}
	} else {
		badPath := filepath.Join(t.paths.TempPath, displayName)
		if err := os.Rename(path, badPath); err != nil {
			slog.Warn("failed to move bad packet", "path", path, "dest", badPath, "error", err)
		}
	}
	return false
}

// tossPacket processes a single .PKT file, returning counts and errors.
// When multiple networks share the same inbound directory, the packet header's
// source address is checked against the network's known links. If the packet
// doesn't originate from a known link, it is skipped (returned with skipped=true)
// so the correct network's tosser can process it later.
func (t *Tosser) tossPacket(path string) (imported, dupes int, errs []string, skipped bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, []string{fmt.Sprintf("open %s: %v", path, err)}, false
	}
	defer func() { _ = f.Close() }() // read-only

	pktHdr, msgs, err := ftn.ReadPacket(f)
	if err != nil {
		if len(msgs) == 0 {
			return 0, 0, []string{fmt.Sprintf("parse %s: %v", path, err)}, false
		}
		// Partial parse: packet is truncated but some messages were read before the
		// bad offset. Process what we have and treat the truncation as a warning.
		slog.Warn("truncated packet, processing available messages", "network", t.networkName, "packet", filepath.Base(path), "error", err, "count", len(msgs))
	}

	// Check if this packet belongs to our network by matching the source address
	// against our known links. This prevents cross-contamination when multiple
	// networks share the same inbound directory.
	if !t.isPacketFromKnownLink(pktHdr) {
		pktZone := pktHdr.OrigZone
		if pktZone == 0 {
			pktZone = pktHdr.QOrigZone
		}
		slog.Debug("skipping packet from unknown link", "network", t.networkName, "packet", filepath.Base(path), "zone", pktZone, "net", pktHdr.OrigNet, "node", pktHdr.OrigNode)
		return 0, 0, nil, true
	}

	for i, msg := range msgs {
		if err := t.tossMessage(msg, pktHdr); err != nil {
			if err == errDupe {
				dupes++
				continue
			}
			errs = append(errs, fmt.Sprintf("msg %d in %s: %v", i, filepath.Base(path), err))
			continue
		}
		imported++
	}

	return imported, dupes, errs, false
}

var errDupe = fmt.Errorf("duplicate message")

// isPacketFromKnownLink checks whether a packet header's source address matches
// any configured link for this network. Compares zone, net, and node; point is
// ignored since hub packets typically originate from the main node address.
func (t *Tosser) isPacketFromKnownLink(hdr *ftn.PacketHeader) bool {
	pktZone := hdr.OrigZone
	if pktZone == 0 {
		pktZone = hdr.QOrigZone
	}

	hasValidLink := false
	for _, link := range t.config.Links {
		addr, err := jam.ParseAddress(link.Address)
		if err != nil {
			slog.Warn("invalid link address", "network", t.networkName, "link", link.Address, "error", err)
			continue
		}
		hasValidLink = true
		if uint16(addr.Zone) == pktZone &&
			uint16(addr.Net) == hdr.OrigNet &&
			uint16(addr.Node) == hdr.OrigNode {
			return true
		}
	}
	// If no configured link addresses could be parsed, accept the packet
	// to avoid silently stalling all inbound processing.
	if !hasValidLink {
		slog.Warn("no valid link addresses configured, accepting packet", "network", t.networkName, "zone", pktZone, "net", hdr.OrigNet, "node", hdr.OrigNode)
		return true
	}
	return false
}

// tossMessage processes a single message from a packet.
func (t *Tosser) tossMessage(msg *ftn.PackedMessage, pktHdr *ftn.PacketHeader) (retErr error) {
	parsed := ftn.ParsePackedMessageBody(msg.Body)

	// Extract MSGID and CHRS from kludges
	msgID := ""
	chrs := ""
	for _, k := range parsed.Kludges {
		if strings.HasPrefix(k, "MSGID: ") {
			msgID = strings.TrimPrefix(k, "MSGID: ")
		} else if strings.HasPrefix(k, "CHRS: ") {
			chrs = strings.TrimPrefix(k, "CHRS: ")
		}
	}

	// Decode header strings from their source encoding (CP437 unless CHRS says UTF-8)
	msg.From = ftn.DecodeFTNString(msg.From, chrs)
	msg.To = ftn.DecodeFTNString(msg.To, chrs)
	msg.Subject = ftn.DecodeFTNString(msg.Subject, chrs)

	// Netmail: messages without AREA: kludge are private point-to-point messages.
	if parsed.Area == "" {
		if t.netmailAreaTag != "" {
			if err := t.writeMsgToArea(t.netmailAreaTag, msg, pktHdr, parsed, msgID); err != nil {
				return fmt.Errorf("netmail to area %q: %w", t.netmailAreaTag, err)
			}
			slog.Info("tossed netmail", "from", msg.From, "to", msg.To, "msgid", msgID)
			return nil
		}
		slog.Debug("skipping netmail, no netmail area configured", "from", msg.From, "to", msg.To, "network", t.networkName)
		return nil
	}

	// Dupe check (only meaningful if message has a MSGID)
	if msgID != "" && t.dupeDB.Add(msgID) {
		slog.Debug("dupe message", "msgid", msgID, "area", parsed.Area)
		if t.paths.DupeAreaTag != "" {
			if err := t.writeMsgToArea(t.paths.DupeAreaTag, msg, pktHdr, parsed, msgID); err != nil {
				slog.Warn("dupe area write failed", "error", err)
			}
		}
		return errDupe
	}

	// Find the target area by echo tag.
	// Fall back to EchoTag index for areas using a local tag-prefix
	// (e.g. Tag="FD_LINUX", EchoTag="LINUX" when --tag-prefix was used during setup).
	// Constrain the fallback to the current network to avoid cross-network misrouting
	// when two networks share the same echo tag.
	area, found := t.msgMgr.GetAreaByTag(parsed.Area)
	if !found {
		if a, ok := t.msgMgr.GetAreaByEchoTag(parsed.Area); ok && strings.EqualFold(a.Network, t.networkName) {
			area, found = a, true
		}
	}
	if !found {
		slog.Warn("unknown echo area", "area", parsed.Area, "from", msg.From)
		if t.paths.BadAreaTag != "" {
			if err := t.writeMsgToArea(t.paths.BadAreaTag, msg, pktHdr, parsed, msgID); err != nil {
				slog.Warn("bad area write failed", "area", parsed.Area, "error", err)
				return fmt.Errorf("unknown area %q", parsed.Area)
			}
			slog.Info("routed unknown area message to bad area", "area", parsed.Area, "from", msg.From)
			return nil // counted as imported
		}
		return fmt.Errorf("unknown area %q", parsed.Area)
	}

	base, err := t.msgMgr.GetBase(area.ID)
	if err != nil {
		return fmt.Errorf("get base for area %d: %w", area.ID, err)
	}
	// A failed close means the JAM write may not be fully flushed, so treat
	// it as a write failure — otherwise the packet would be acknowledged and
	// removed while the message is potentially lost.
	defer func() {
		if cerr := base.Close(); cerr != nil {
			if retErr == nil {
				retErr = fmt.Errorf("closing JAM base: %w", cerr)
			} else {
				slog.Warn("closing JAM base", "error", cerr)
			}
		}
	}()

	// Update SEEN-BY and PATH with our address
	own2D := t.ownAddr.String2D()
	parsed.SeenBy = MergeSeenBy(parsed.SeenBy, own2D)
	parsed.Path = AppendPath(parsed.Path, own2D)

	// Build JAM message
	jamMsg := jam.NewMessage()
	jamMsg.From = msg.From
	jamMsg.To = msg.To
	jamMsg.Subject = msg.Subject
	jamMsg.Text = parsed.Text // Store only the message text, not kludges/SEEN-BY/PATH

	// Store SEEN-BY and PATH as JAM subfields for proper roundtrip with export
	if len(parsed.SeenBy) > 0 {
		jamMsg.SeenBy = strings.Join(parsed.SeenBy, " ")
	}
	if len(parsed.Path) > 0 {
		jamMsg.Path = strings.Join(parsed.Path, " ")
	}

	// Parse datetime
	dt, err := ftn.ParseFTNDateTime(msg.DateTime)
	if err != nil {
		dt = time.Now()
	}
	jamMsg.DateTime = dt

	// Set origin address from the packet header and message
	origZone := pktHdr.OrigZone
	if origZone == 0 {
		origZone = pktHdr.QOrigZone // Fallback to QMail zone field
	}
	if origZone == 0 {
		origZone = uint16(t.ownAddr.Zone) // Last resort: assume same zone
	}
	origAddr := fmt.Sprintf("%d:%d/%d", origZone, msg.OrigNet, msg.OrigNode)
	jamMsg.OrigAddr = origAddr

	// Set MSGID if we have one
	if msgID != "" {
		jamMsg.MsgID = msgID
	}

	// Preserve kludges
	for _, k := range parsed.Kludges {
		if strings.HasPrefix(k, "MSGID: ") || strings.HasPrefix(k, "REPLY: ") {
			continue // Handled separately
		}
		jamMsg.Kludges = append(jamMsg.Kludges, k)
	}

	// Extract REPLY kludge. Keep the full MSGID value (including the unique
	// hash portion for @-style addresses like "a1b2c3d4@1:2/3") so that
	// reply linking can match against the MSGID index.
	for _, k := range parsed.Kludges {
		if strings.HasPrefix(k, "REPLY: ") {
			replyValue := strings.TrimPrefix(k, "REPLY: ")
			// FTN MSGID is "address unique" — take the first complete pair
			// (addr + unique) when multiple MSGIDs are present in the kludge.
			if parts := strings.Fields(replyValue); len(parts) >= 2 {
				jamMsg.ReplyID = parts[0] + " " + parts[1]
			} else if len(parts) == 1 {
				jamMsg.ReplyID = parts[0]
			}
			break
		}
	}

	// Write to JAM base with echomail handling
	msgType := jam.DetermineMessageType(area.AreaType, area.EchoTag)
	msgNum, err := base.WriteMessageExt(jamMsg, msgType, area.EchoTag, "", "")
	if err != nil {
		return fmt.Errorf("write to JAM: %w", err)
	}

	// WriteMessageExt sets DateProcessed=0 for echomail to signal "needs export".
	// Inbound messages have already been processed by the network, so mark them
	// as processed now to prevent v3mail scan from re-exporting them to the uplink.
	if hdr, herr := base.ReadMessageHeader(msgNum); herr == nil {
		hdr.DateProcessed = uint32(time.Now().Unix())
		if uerr := base.UpdateMessageHeader(msgNum, hdr); uerr != nil {
			slog.Warn("failed to mark msg as processed", "msg", msgNum, "area", area.Tag, "error", uerr)
		}
	} else {
		slog.Warn("failed to read header to mark msg as processed", "msg", msgNum, "area", area.Tag, "error", herr)
	}

	slog.Info("tossed message", "from", msg.From, "to", msg.To, "area", parsed.Area, "msgid", msgID)
	return nil
}

// writeMsgToArea writes a packet message to any JAM area by tag.
// Used for netmail, bad-area, and dupe-area routing.
func (t *Tosser) writeMsgToArea(areaTag string, msg *ftn.PackedMessage, pktHdr *ftn.PacketHeader, parsed *ftn.ParsedBody, msgID string) (retErr error) {
	area, found := t.msgMgr.GetAreaByTag(areaTag)
	if !found {
		return fmt.Errorf("area %q not configured", areaTag)
	}
	base, err := t.msgMgr.GetBase(area.ID)
	if err != nil {
		return fmt.Errorf("get base for area %q: %w", areaTag, err)
	}
	// Treat a failed close as a write failure so callers do not acknowledge
	// a packet whose JAM finalization failed.
	defer func() {
		if cerr := base.Close(); cerr != nil {
			if retErr == nil {
				retErr = fmt.Errorf("closing JAM base: %w", cerr)
			} else {
				slog.Warn("closing JAM base", "error", cerr)
			}
		}
	}()

	jamMsg := jam.NewMessage()
	jamMsg.From = msg.From
	jamMsg.To = msg.To
	jamMsg.Subject = msg.Subject
	jamMsg.Text = parsed.Text
	if msgID != "" {
		jamMsg.MsgID = msgID
	}

	dt, err := ftn.ParseFTNDateTime(msg.DateTime)
	if err != nil {
		dt = time.Now()
	}
	jamMsg.DateTime = dt

	origZone := pktHdr.OrigZone
	if origZone == 0 {
		origZone = pktHdr.QOrigZone
	}
	if origZone == 0 {
		origZone = uint16(t.ownAddr.Zone)
	}
	jamMsg.OrigAddr = fmt.Sprintf("%d:%d/%d", origZone, msg.OrigNet, msg.OrigNode)

	// Preserve kludges (excluding MSGID/REPLY which are handled separately)
	for _, k := range parsed.Kludges {
		if strings.HasPrefix(k, "MSGID: ") || strings.HasPrefix(k, "REPLY: ") {
			continue
		}
		jamMsg.Kludges = append(jamMsg.Kludges, k)
	}

	// Extract REPLY kludge
	for _, k := range parsed.Kludges {
		if strings.HasPrefix(k, "REPLY: ") {
			jamMsg.ReplyID = strings.TrimPrefix(k, "REPLY: ")
			break
		}
	}

	// Preserve SEEN-BY and PATH
	if len(parsed.SeenBy) > 0 {
		jamMsg.SeenBy = strings.Join(parsed.SeenBy, " ")
	}
	if len(parsed.Path) > 0 {
		jamMsg.Path = strings.Join(parsed.Path, " ")
	}

	msgType := jam.DetermineMessageType(area.AreaType, area.EchoTag)
	msgNum, err := base.WriteMessageExt(jamMsg, msgType, area.EchoTag, "", "")
	if err != nil {
		return err
	}

	// Same as tossMessage: mark as processed so scan doesn't re-export routed messages.
	if hdr, herr := base.ReadMessageHeader(msgNum); herr == nil {
		hdr.DateProcessed = uint32(time.Now().Unix())
		if uerr := base.UpdateMessageHeader(msgNum, hdr); uerr != nil {
			slog.Warn("failed to mark routed msg as processed", "msg", msgNum, "area", areaTag, "error", uerr)
		}
	} else {
		slog.Warn("failed to read header for routed msg to mark as processed", "msg", msgNum, "area", areaTag, "error", herr)
	}
	return nil
}
