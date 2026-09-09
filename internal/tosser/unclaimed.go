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
	Files       []string       // paths still in the inbound directories
	Origins     map[string]int // origin address -> packets declined, across all networks
	Oldest      time.Time      // modification time of the oldest file
	Quarantined []string       // files moved aside this run
}

// Empty reports whether anything was left unclaimed.
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
// SkippedOrigins of every network's result, which is what turns "nobody wanted
// this" into a message naming the address whose mail is piling up.
func FindUnclaimed(ftnCfg config.FTNConfig, skipped map[string]int) UnclaimedReport {
	report := UnclaimedReport{Origins: skipped}

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
			report.Files = append(report.Files, path)
			if info, err := entry.Info(); err == nil {
				if report.Oldest.IsZero() || info.ModTime().Before(report.Oldest) {
					report.Oldest = info.ModTime()
				}
			}
		}
	}
	sort.Strings(report.Files)
	return report
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
