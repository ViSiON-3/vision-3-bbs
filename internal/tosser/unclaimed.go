package tosser

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// Reporting on inbound mail that no configured network would take.
//
// A tosser leaves a bundle in place when every packet inside it comes from an
// address that matches none of its links, on the assumption that some other
// network's tosser will claim it. Nothing checked that assumption. When it did
// not hold — a link address that did not match the uplink actually sending the
// mail — the bundles sat in the inbound directory indefinitely, every tosser
// re-extracted and re-parsed all of them on every cycle, and the run still
// reported "0 packets, 0 imported" and exited 0. A live system accumulated
// 23 bundles and 5,496 packets over eight weeks that way, with nothing in the
// logs above Debug to say so (#276).

// quarantineAfter is how long a bundle must have gone unclaimed before it is
// moved aside. Mail is only ever a few minutes old when a network is briefly
// misconfigured or disabled, so waiting a day leaves room to fix the config
// and have the mail tossed normally, while still bounding how large the
// inbound directory and the per-cycle rescan can grow.
const quarantineAfter = 24 * time.Hour

// UnclaimedDirName is the subdirectory of the temp path that unclaimed bundles
// are moved to. They are moved, never deleted: the usual cause is a
// correctable config mistake, and moving them back is how a sysop recovers.
const UnclaimedDirName = "unclaimed"

// UnclaimedReport describes inbound mail left behind after every enabled
// network has had its turn.
type UnclaimedReport struct {
	Files   []string       // paths still in the inbound directories
	Origins map[string]int // origin address -> packets declined, across all networks
	Oldest  time.Time      // modification time of the oldest file

	// Held is inbound mail belonging to a network whose tosser is switched
	// off. It is waiting for that network to be enabled rather than claimed
	// by nobody, so it is never quarantined and is reported in its own
	// words. See heldOrigins.
	Held []string

	Quarantined []string // files moved aside this run
}

// Empty reports whether anything was left unclaimed. Mail held for a disabled
// network does not count: it is waiting by the sysop's own instruction.
func (r UnclaimedReport) Empty() bool { return len(r.Files) == 0 }

// OriginList renders the declined origin addresses in a stable order, most
// packets first, for a log line or a console summary.
func (r UnclaimedReport) OriginList() string {
	type kv struct {
		addr string
		n    int
	}
	pairs := make([]kv, 0, len(r.Origins))
	for a, n := range r.Origins {
		pairs = append(pairs, kv{a, n})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].n != pairs[j].n {
			return pairs[i].n > pairs[j].n
		}
		return pairs[i].addr < pairs[j].addr
	})
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, fmt.Sprintf("%s (%d packets)", p.addr, p.n))
	}
	return strings.Join(parts, ", ")
}

// FindUnclaimed reports the inbound mail that survived a full toss pass.
//
// It must be called only after every enabled network has run, because a
// bundle is removed by whichever tosser processes it: anything still present
// is therefore mail that no configured network would take. skipped merges the
// SkippedByFile maps of every network's result, which is what turns "nobody
// wanted this" into a message naming the address whose mail is piling up.
// Counts are taken per file rather than summed, since each enabled network
// passes over the same packets and would otherwise multiply the total.
func FindUnclaimed(ftnCfg config.FTNConfig, skippedByFile map[string]map[string]int) UnclaimedReport {
	report := UnclaimedReport{Origins: map[string]int{}}

	// Mail for a network the sysop has switched off is held, not unclaimed:
	// quarantining it would age out exactly the mail someone is deliberately
	// waiting to toss once the areas are set up.
	held, anyDisabled, allDisabled := heldOrigins(ftnCfg)

	seen := make(map[string]bool)
	for _, dir := range []string{ftnCfg.SecureInboundPath, ftnCfg.InboundPath} {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true

		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // a missing inbound directory is not this function's problem
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			nameLower := strings.ToLower(entry.Name())
			if !ftn.BundleExtension(nameLower) && !strings.HasSuffix(nameLower, ".pkt") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			if isHeldForDisabledNetwork(skippedByFile[path], held, anyDisabled, allDisabled) {
				report.Held = append(report.Held, path)
				continue
			}
			report.Files = append(report.Files, path)
			// Only the origins of files still present are reported. A file a
			// later network claimed has been removed by it, so the addresses
			// an earlier network declined it under are not news.
			for origin, n := range skippedByFile[path] {
				if n > report.Origins[origin] {
					// Every network that declined this file saw the same
					// packets in it, so take the count rather than summing
					// one pass per enabled network.
					report.Origins[origin] = n
				}
			}
			if info, err := entry.Info(); err == nil {
				if report.Oldest.IsZero() || info.ModTime().Before(report.Oldest) {
					report.Oldest = info.ModTime()
				}
			}
		}
	}
	sort.Strings(report.Files)
	sort.Strings(report.Held)
	return report
}

// heldOrigins returns the "zone:net/node" addresses whose mail belongs to a
// network with internal_tosser_enabled off, whether any network is disabled
// at all, and whether every configured network is.
//
// Addresses are rendered the way tossPacket reports a declined origin — zone,
// net and node, no point — so the two can be compared directly without
// re-reading the packets.
func heldOrigins(ftnCfg config.FTNConfig) (held map[string]bool, anyDisabled, allDisabled bool) {
	held = map[string]bool{}
	allDisabled = len(ftnCfg.Networks) > 0
	for name, netCfg := range ftnCfg.Networks {
		if netCfg.InternalTosserEnabled {
			allDisabled = false
			continue
		}
		anyDisabled = true
		for _, link := range netCfg.Links {
			addr, err := jam.ParseAddress(link.Address)
			if err != nil {
				slog.Warn("invalid link address on a disabled network; its mail cannot be recognised as held",
					"network", name, "link", link.Address, "error", err)
				continue
			}
			held[fmt.Sprintf("%d:%d/%d", addr.Zone, addr.Net, addr.Node)] = true
		}
	}
	return held, anyDisabled, allDisabled
}

// isHeldForDisabledNetwork reports whether an inbound file is waiting for a
// switched-off network rather than claimed by nobody.
//
// origins is what the enabled networks declined the file under. A file whose
// origin matches a disabled network's link is plainly that network's. A file
// with no recorded origins is unattributable, and is held only when every
// network is disabled: that is the one case where nothing parsed it, and mail
// should not be aged out on a guess. With any network enabled, an origin-less
// file is one the enabled tossers saw and could not attribute — unreadable, or
// a packet a mixed bundle re-queued — and holding it would let a stale, broken
// file sit forever behind an unrelated network being switched off. With every
// network enabled this is false throughout and the #276 behaviour is
// unchanged.
func isHeldForDisabledNetwork(origins map[string]int, held map[string]bool, anyDisabled, allDisabled bool) bool {
	if !anyDisabled {
		return false
	}
	if len(origins) == 0 {
		return allDisabled
	}
	for origin := range origins {
		if held[origin] {
			return true
		}
	}
	return false
}

// QuarantineStale moves unclaimed files older than quarantineAfter into the
// temp path's unclaimed directory, and records them on the report. Anything
// newer is left alone so a config fix made the same day still tosses normally.
func (r *UnclaimedReport) QuarantineStale(tempPath string) {
	if tempPath == "" || len(r.Files) == 0 {
		return
	}
	cutoff := time.Now().Add(-quarantineAfter)
	dest := filepath.Join(tempPath, UnclaimedDirName)

	var kept []string
	for _, path := range r.Files {
		info, err := os.Stat(path)
		if err != nil || !info.ModTime().Before(cutoff) {
			kept = append(kept, path)
			continue
		}
		if err := os.MkdirAll(dest, 0755); err != nil {
			slog.Warn("cannot create unclaimed directory, leaving mail in place", "dir", dest, "error", err)
			return
		}
		target := filepath.Join(dest, filepath.Base(path))
		if err := os.Rename(path, target); err != nil {
			// Leave it where it is: an unclaimed bundle in the inbound
			// directory is recoverable, and this runs again next cycle.
			slog.Warn("cannot quarantine unclaimed mail, leaving it in place", "path", path, "error", err)
			kept = append(kept, path)
			continue
		}
		r.Quarantined = append(r.Quarantined, target)
	}
	r.Files = kept
}

// Log writes the report to the log. It is the one place that turns routine,
// per-packet skipping into a single operator-visible line, so it says which
// address the mail came from — the piece needed to spot that a link address
// does not match the uplink actually sending.
func (r UnclaimedReport) Log() {
	// Held mail is expected, so it is reported at Info and says why it is
	// waiting — a Warn here would train the sysop to ignore the Warn below.
	if len(r.Held) > 0 {
		slog.Info("inbound mail waiting for a network whose tosser is switched off — "+
			"it is held, not quarantined; enable the network to toss it",
			"files", len(r.Held))
	}
	if r.Empty() && len(r.Quarantined) == 0 {
		return
	}
	args := []any{"files", len(r.Files) + len(r.Quarantined)}
	if origins := r.OriginList(); origins != "" {
		args = append(args, "origins", origins)
	}
	if !r.Oldest.IsZero() {
		args = append(args, "oldest", r.Oldest.Format(time.RFC3339))
	}
	if len(r.Quarantined) > 0 {
		args = append(args, "quarantined", len(r.Quarantined),
			"quarantine_dir", filepath.Dir(r.Quarantined[0]))
	}
	slog.Warn("inbound mail claimed by no configured network — check that each network's links "+
		"list the address its mail actually comes from", args...)
}
