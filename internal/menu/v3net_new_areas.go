package menu

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

// New V3Net areas are offered to sysops as login notices. Every NAL a leaf
// fetches is compared with the area tags already seen for that network; a tag
// not seen before is new, and each sysop gets a notice asking whether to add
// it. The seen tags are kept on disk so areas the hub added while the BBS was
// down are still noticed, and so a restart does not offer everything again.

// v3netSeenMu guards the seen-areas file against concurrent leaf callbacks.
var v3netSeenMu sync.Mutex

// v3netSeenAreasPath returns the seen-areas file path for the given data dir,
// falling back to the conventional "data" dir when none is configured.
func v3netSeenAreasPath(dataDir string) string {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "data"
	}
	return filepath.Join(dataDir, "v3net_seen_areas.json")
}

// recordV3NetAreas stores the NAL's area tags as the seen set for network and
// returns the areas that were not in the previous set. The first NAL seen for
// a network only records its tags: those areas were there before this node
// was watching, and offering every one of them would bury the sysop.
func recordV3NetAreas(path, network string, n *protocol.NAL) ([]protocol.Area, error) {
	v3netSeenMu.Lock()
	defer v3netSeenMu.Unlock()

	seen := map[string][]string{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if strings.TrimSpace(string(data)) != "" {
			if err := json.Unmarshal(data, &seen); err != nil {
				return nil, fmt.Errorf("parse %s: %w", path, err)
			}
		}
	case os.IsNotExist(err):
	default:
		return nil, err
	}
	if seen == nil { // the file held JSON null
		seen = map[string][]string{}
	}

	prev, known := seen[network]
	prevSet := make(map[string]bool, len(prev))
	for _, tag := range prev {
		prevSet[tag] = true
	}

	var fresh []protocol.Area
	tags := make([]string, 0, len(n.Areas))
	for _, a := range n.Areas {
		tags = append(tags, a.Tag)
		if known && !prevSet[a.Tag] {
			fresh = append(fresh, a)
		}
	}
	if known && len(fresh) == 0 && len(tags) == len(prev) {
		return nil, nil // unchanged; skip the write
	}

	seen[network] = tags
	out, err := json.MarshalIndent(seen, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := atomicfile.WriteFile(path, out, 0o644); err != nil {
		return nil, err
	}
	return fresh, nil
}

// v3netNetworkLabel is how a network name reads in a notice: "felonynet"
// becomes "Felonynet".
func v3netNetworkLabel(network string) string {
	r, size := utf8.DecodeRuneInString(network)
	if r == utf8.RuneError {
		return network
	}
	return string(unicode.ToUpper(r)) + network[size:]
}

// v3netSubscribedBoards returns the area tags this BBS already carries on a
// network, from v3net.json.
func v3netSubscribedBoards(configPath, network string) map[string]bool {
	boards := map[string]bool{}
	cfg, err := config.LoadV3NetConfig(configPath)
	if err != nil {
		return boards
	}
	for _, l := range cfg.Leaves {
		if l.Network == network {
			for _, b := range l.Boards {
				boards[b] = true
			}
		}
	}
	return boards
}

// NoteV3NetNAL is the V3Net service's NAL observer. It records the network's
// areas and queues an "Add?" notice for every sysop about each area that is
// new since the last NAL, unless the BBS already carries it or the area is
// closed to this node. nodeID is this node's V3Net ID. It returns the number
// of notices queued.
func (e *MenuExecutor) NoteV3NetNAL(userManager *user.UserMgr, network string, n *protocol.NAL, nodeID string) int {
	if n == nil {
		return 0
	}
	dataDir := e.GetServerConfig().DataDir
	fresh, err := recordV3NetAreas(v3netSeenAreasPath(dataDir), network, n)
	if err != nil {
		slog.Warn("v3net: could not record seen areas", "network", network, "error", err)
		return 0
	}
	if len(fresh) == 0 {
		return 0
	}

	// LoadStrings fills a blank v3netNewAreaNotice from StringFallbacks, so
	// this is only empty for an executor built without loaded strings.
	format := e.Strings().V3NetNewAreaNotice
	if format == "" {
		slog.Warn("v3net: new areas found but v3netNewAreaNotice has no text; not offered", "network", network, "count", len(fresh))
		return 0
	}

	subscribed := v3netSubscribedBoards(e.RootConfigPath, network)
	var offers []protocol.Area
	for _, a := range fresh {
		if subscribed[a.Tag] {
			continue
		}
		if a.Access.Mode == protocol.AccessModeClosed && !nal.NodeAllowed(&a, nodeID) {
			continue // this node could not join it anyway
		}
		offers = append(offers, a)
	}
	if len(offers) == 0 || userManager == nil {
		return 0
	}

	path := sysopNoticesPath(dataDir)
	label := v3netNetworkLabel(network)
	queued := 0
	for _, u := range userManager.GetAllUsers() {
		if u == nil || u.DeletedUser || !e.isSysOpOrAbove(u) {
			continue
		}
		for _, a := range offers {
			notice := sysopNotice{
				Text:         fmt.Sprintf(format, label, a.Name),
				V3NetNetwork: network,
				V3NetTag:     a.Tag,
				V3NetName:    a.Name,
				CreatedAt:    time.Now(),
			}
			if err := enqueueSysopNotice(path, u.ID, notice); err != nil {
				slog.Warn("v3net: could not queue a new-area notice", "network", network, "tag", a.Tag, "recipient", u.Handle, "error", err)
				continue
			}
			queued++
		}
	}
	slog.Info("v3net: new areas offered to sysops", "network", network, "areas", len(offers), "notices", queued)
	return queued
}
