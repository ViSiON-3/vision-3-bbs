// Package qwknet makes ViSiON/3 a node on a QWK-based message network.
//
// A QWK network moves conference mail in the same packets offline readers
// use. The node scans its networked areas into a REP packet, uploads it to
// the hub by FTP, downloads the hub's QWK packet and tosses it into the
// local message bases. Routing is by conference number: the hub numbers its
// conferences and each local area records the number it mirrors.
//
// The design follows Synchronet's, which defined the conventions most hubs
// speak: HEADERS.DAT for full-length fields and Message-IDs, the @VIA/
// @MSGID/@REPLY/@TZ body kludges, <HUBID>.REP up and <HUBID>.QWK down.
package qwknet

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/tosser"
)

// ScannerUser is the synthetic JAM lastread user under which each area's
// export high-water mark is kept. It is separate from the FTN scanner's so
// the two never read each other's pointer.
const ScannerUser = "qwknet"

// dupeRetention is how long imported Message-IDs stay in the dupe database.
const dupeRetention = 90 * 24 * time.Hour

// Node is one configured QWK network and this system's membership in it.
type Node struct {
	Key    string
	cfg    config.QWKNetworkConfig
	paths  config.QWKNetConfig
	msgMgr *message.MessageManager
	dupes  *tosser.DupeDB
	nodeID string
	hubID  string
}

// New prepares a node for the network key. paths must already be resolved
// to absolute directories (config.QWKNetConfig.ResolvePaths). systemQWKID
// is the system-wide QWK ID used when the network sets no ownId.
func New(key string, cfg config.QWKNetworkConfig, paths config.QWKNetConfig, systemQWKID string,
	msgMgr *message.MessageManager, dupes *tosser.DupeDB) (*Node, error) {
	if err := config.ValidateQWKNetwork(key, cfg, systemQWKID); err != nil {
		return nil, err
	}
	for _, dir := range []string{paths.InboundPath, paths.OutboundPath, paths.TempPath} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	return &Node{
		Key:    key,
		cfg:    cfg,
		paths:  paths,
		msgMgr: msgMgr,
		dupes:  dupes,
		nodeID: cfg.NodeID(systemQWKID),
		hubID:  config.NormalizeQWKID(cfg.HubID),
	}, nil
}

// OpenDupeDB opens (or creates) the shared dupe database at path.
func OpenDupeDB(path string) (*tosser.DupeDB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return tosser.NewDupeDB(path, dupeRetention)
}

// NodeID is this system's QWK ID on the network.
func (n *Node) NodeID() string { return n.nodeID }

// HubID is the hub's QWK ID.
func (n *Node) HubID() string { return n.hubID }

// repPath is where the packed, not-yet-uploaded REP waits.
func (n *Node) repPath() string {
	return filepath.Join(n.paths.OutboundPath, n.hubID+".REP")
}

// areasByConference maps the hub's conference numbers to the local areas
// on this network. An area with no conference number is skipped with a
// warning, since nothing can be routed to or from it.
func (n *Node) areasByConference() map[int]*message.MessageArea {
	out := make(map[int]*message.MessageArea)
	for _, a := range n.msgMgr.ListAreas() {
		if !a.IsQWKNet() || !strings.EqualFold(a.Network, n.Key) {
			continue
		}
		if a.QWKConference <= 0 {
			slog.Warn("qwknet area has no conference number and cannot be routed", "network", n.Key, "area", a.Tag)
			continue
		}
		if prev, dup := out[a.QWKConference]; dup {
			slog.Warn("two areas claim the same QWK conference; the first wins",
				"network", n.Key, "conference", a.QWKConference, "kept", prev.Tag, "ignored", a.Tag)
			continue
		}
		out[a.QWKConference] = a
	}
	return out
}

// inboundPackets lists the hub's packets waiting in the inbound directory,
// oldest first by name. A download is stored as <HUBID>.QWK, or with a
// timestamp suffix when one is already waiting.
func (n *Node) inboundPackets() []string {
	entries, err := os.ReadDir(n.paths.InboundPath)
	if err != nil {
		return nil
	}
	var out []string
	prefix := strings.ToUpper(n.hubID)
	for _, e := range entries {
		name := strings.ToUpper(e.Name())
		if e.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".QWK") {
			continue
		}
		out = append(out, filepath.Join(n.paths.InboundPath, e.Name()))
	}
	sort.Strings(out)
	return out
}

// newInboundName picks a free file name for a downloaded packet.
func (n *Node) newInboundName() string {
	base := filepath.Join(n.paths.InboundPath, n.hubID+".QWK")
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return base
	}
	return filepath.Join(n.paths.InboundPath, fmt.Sprintf("%s-%d.QWK", n.hubID, time.Now().UnixNano()))
}

// routeContains reports whether a @VIA route (IDs separated by '/') names id.
func routeContains(route, id string) bool {
	for _, hop := range strings.Split(route, "/") {
		if strings.EqualFold(strings.TrimSpace(hop), id) {
			return true
		}
	}
	return false
}
