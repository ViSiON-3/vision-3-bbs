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

// v3netRecentAreaWindow is how recently an area must have been added to its
// network to be offered from the first NAL a node sees for that network.
const v3netRecentAreaWindow = 30 * 24 * time.Hour

// recordV3NetAreas stores the NAL's area tags as the seen set for network and
// returns the areas that were not in the previous set. The first NAL seen for
// a network mostly just records its tags: those areas were there before this
// node was watching, and offering every one of them would bury the sysop. The
// exception is an area the hub stamped as added within v3netRecentAreaWindow,
// so a node that joins or upgrades just after an area appears still hears of
// it. An area with no Added time (an older hub, or an area that predates the
// field) counts as old.
//
// When there are new areas and offer is non-nil, offer is called with them
// before the seen set is written, and an error from it leaves the set
// unchanged. The areas are then still new at the next NAL and are offered
// again, rather than being marked seen without anyone having been asked.
func recordV3NetAreas(path, network string, n *protocol.NAL, offer func([]protocol.Area) error) ([]protocol.Area, error) {
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
	cutoff := time.Now().Add(-v3netRecentAreaWindow)
	for _, a := range n.Areas {
		tags = append(tags, a.Tag)
		if known && !prevSet[a.Tag] {
			fresh = append(fresh, a)
		} else if !known && v3netAddedSince(a, cutoff) {
			fresh = append(fresh, a)
		}
	}
	if known && len(fresh) == 0 && len(tags) == len(prev) {
		return nil, nil // unchanged; skip the write
	}
	if len(fresh) > 0 && offer != nil {
		if err := offer(fresh); err != nil {
			return nil, err
		}
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

// v3netAddedSince reports whether the hub stamped a as added after cutoff.
// A missing or unparseable time is treated as old.
func v3netAddedSince(a protocol.Area, cutoff time.Time) bool {
	if a.Added == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, a.Added)
	return err == nil && t.After(cutoff)
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
//
// If the notices cannot all be queued, the areas are not recorded as seen, so
// the next NAL offers them again; a sysop who already has a notice for an area
// is not given a second one.
func (e *MenuExecutor) NoteV3NetNAL(userManager *user.UserMgr, network string, n *protocol.NAL, nodeID string) int {
	if n == nil {
		return 0
	}
	dataDir := e.GetServerConfig().DataDir
	queued := 0
	offer := func(fresh []protocol.Area) error {
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
		if len(offers) == 0 {
			return nil
		}

		// LoadStrings fills a blank v3netNewAreaNotice from StringFallbacks,
		// so this is only empty for an executor built without loaded strings.
		format := e.Strings().V3NetNewAreaNotice
		if format == "" {
			return fmt.Errorf("v3netNewAreaNotice has no text")
		}
		if userManager == nil {
			return fmt.Errorf("no user manager")
		}

		path := sysopNoticesPath(dataDir)
		label := v3netNetworkLabel(network)
		v3netDeclinedMu.Lock()
		declined, err := loadV3NetDeclined(v3netDeclinedAreasPath(dataDir))
		v3netDeclinedMu.Unlock()
		if err != nil {
			return err
		}
		var failed error
		for _, u := range userManager.GetAllUsers() {
			if u == nil || u.DeletedUser || !e.isSysOpOrAbove(u) {
				continue
			}
			pending, err := peekSysopNotices(path, u.ID)
			if err != nil {
				failed = err
				continue
			}
			for _, a := range offers {
				if hasV3NetOffer(pending, network, a.Tag) {
					continue // queued by an earlier attempt
				}
				if declined.has(u.ID, network, a.Tag) {
					continue // answered No to an earlier attempt
				}
				notice := sysopNotice{
					Text:         fmt.Sprintf(format, label, a.Name),
					V3NetNetwork: network,
					V3NetTag:     a.Tag,
					V3NetName:    a.Name,
					CreatedAt:    time.Now(),
				}
				if err := enqueueSysopNotice(path, u.ID, notice); err != nil {
					slog.Warn("v3net: could not queue a new-area notice", "network", network, "tag", a.Tag, "recipient", u.Handle, "error", err)
					failed = err
					continue
				}
				queued++
			}
		}
		if failed != nil {
			return fmt.Errorf("queue notices: %w", failed)
		}
		slog.Info("v3net: new areas offered to sysops", "network", network, "areas", len(offers), "notices", queued)
		return nil
	}

	if _, err := recordV3NetAreas(v3netSeenAreasPath(dataDir), network, n, offer); err != nil {
		slog.Warn("v3net: new areas not offered; will retry at the next NAL", "network", network, "error", err)
	}
	return queued
}

// v3netDeclinedMu guards the declined-areas file.
var v3netDeclinedMu sync.Mutex

// v3netDeclinedAreasPath returns the declined-areas file path for the given
// data dir, falling back to the conventional "data" dir when none is set.
func v3netDeclinedAreasPath(dataDir string) string {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "data"
	}
	return filepath.Join(dataDir, "v3net_declined_areas.json")
}

// v3netDeclined maps user ID to network to the area tags that sysop said No to.
type v3netDeclined map[int]map[string][]string

func loadV3NetDeclined(path string) (v3netDeclined, error) {
	d := v3netDeclined{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return d, nil
		}
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return d, nil
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if d == nil {
		d = v3netDeclined{}
	}
	return d, nil
}

func (d v3netDeclined) has(userID int, network, tag string) bool {
	for _, t := range d[userID][network] {
		if t == tag {
			return true
		}
	}
	return false
}

// recordV3NetDecline remembers that userID said No to tag on network, so a
// later retry of the same offer does not ask them again.
func recordV3NetDecline(path string, userID int, network, tag string) error {
	v3netDeclinedMu.Lock()
	defer v3netDeclinedMu.Unlock()

	d, err := loadV3NetDeclined(path)
	if err != nil {
		return err
	}
	if d.has(userID, network, tag) {
		return nil
	}
	if d[userID] == nil {
		d[userID] = map[string][]string{}
	}
	d[userID][network] = append(d[userID][network], tag)
	out, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.WriteFile(path, out, 0o644)
}

// v3netOffersAsking holds the network/tag offers a session is asking about
// right now, so two sysops logged in together are not both asked.
var v3netOffersAsking sync.Map

// claimV3NetOffer reserves an offer for this session. It returns false when
// another session is already asking about it; release must be called once
// the question is settled.
func claimV3NetOffer(network, tag string) (release func(), ok bool) {
	key := network + "\x00" + tag
	if _, taken := v3netOffersAsking.LoadOrStore(key, struct{}{}); taken {
		return nil, false
	}
	return func() { v3netOffersAsking.Delete(key) }, true
}

// hasV3NetOffer reports whether queue already holds an offer for tag on network.
func hasV3NetOffer(queue []sysopNotice, network, tag string) bool {
	for _, n := range queue {
		if n.V3NetNetwork == network && n.V3NetTag == tag {
			return true
		}
	}
	return false
}
