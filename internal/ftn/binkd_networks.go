package ftn

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// Keeping binkd.conf's per-network declarations in step with ftn.json.
//
// A network needs three things in binkd.conf: a "domain" line naming its
// outbound and default zone, an "address" line for our AKA in it, and a "node"
// line for the uplink. SyncBinkdConf upserts the node line and
// SyncBinkdSettings repoints an existing domain line's path, but nothing
// created a missing domain or address line: those were written only by the
// generator, which runs from the FTN Setup Wizard or when binkd.conf is absent.
//
// So a network added any other way — by hand in the TUI, or by
// "helper ftnsetup" — got a node line and nothing else. binkd then refuses the
// poll with "<network>: unknown domain", and because our AKA is undeclared it
// also refuses the uplink's inbound session, with the only clue in the mailer's
// captured stderr.

// SyncBinkdNetworks appends a "domain" and "address" line for every configured
// network that has neither, so a network created outside the wizard becomes
// usable without hand-editing binkd.conf. Existing lines are left exactly as
// they are — repointing a domain's outbound is SyncBinkdSettings' job, and the
// sysop's own zone and formatting choices are not second-guessed. A missing
// binkd.conf is a no-op (EnsureBinkdConf regenerates it), and the file is only
// rewritten when something was actually added.
//
// Networks whose own_address does not parse are skipped with a warning: the
// zone for the domain line comes from it, and guessing would write a
// declaration that silently routes mail into the wrong zone's outbound.
func SyncBinkdNetworks(confPath, bbsRoot string, ftnCfg config.FTNConfig) error {
	existing, err := os.ReadFile(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no binkd.conf to sync
		}
		return fmt.Errorf("reading binkd.conf: %w", err)
	}

	content := string(existing)
	have := declaredDirectives(content)
	outbound := BinkdOutboundFor(bbsRoot, ftnCfg)

	// Sorted so repeated syncs produce identical files.
	names := make([]string, 0, len(ftnCfg.Networks))
	for name := range ftnCfg.Networks {
		names = append(names, name)
	}
	sort.Strings(names)

	var out strings.Builder
	for _, name := range names {
		netCfg := ftnCfg.Networks[name]
		domain := strings.ToLower(name)

		addr, err := jam.ParseAddress(netCfg.OwnAddress)
		if err != nil {
			// Only a problem when something is actually missing; a network
			// already declared needs nothing from us.
			if !have["domain "+domain] || !ourAddressDeclared(have, domain) {
				slog.Warn("ftn network has no usable own_address, so its binkd.conf domain and address "+
					"lines cannot be written — binkd will refuse its sessions with \"unknown domain\"",
					"network", name, "own_address", netCfg.OwnAddress, "error", err)
			}
			continue
		}

		var wrote bool
		if !have["domain "+domain] {
			fmt.Fprintf(&out, "\n#\n# %s (added to match ftn.json)\n#\ndomain %s %s %d\n",
				name, domain, outbound.For(domain), addr.Zone)
			have["domain "+domain] = true
			wrote = true
		}
		akaLine := fmt.Sprintf("%s@%s", netCfg.OwnAddress, domain)
		if !have["address "+akaLine] && !ourAddressDeclared(have, domain) {
			if !wrote {
				fmt.Fprintf(&out, "\n#\n# %s (added to match ftn.json)\n#\n", name)
			}
			fmt.Fprintf(&out, "address %s\n", akaLine)
			have["address "+akaLine] = true
			wrote = true
		}
		if wrote {
			slog.Info("declared ftn network in binkd.conf", "network", name,
				"outbound", outbound.For(domain), "zone", addr.Zone)
		}
	}

	if out.Len() == 0 {
		return nil
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return writeFileAtomic(confPath, content+out.String(), 0600)
}

// declaredDirectives returns the set of "domain <name>" and "address <addr>"
// directives present in content. Unlike keptDirectives it does not filter
// placeholders: this is used to decide whether to append, and a placeholder
// line still occupies the directive as far as binkd's parser is concerned.
func declaredDirectives(content string) map[string]bool {
	have := make(map[string]bool)
	for _, line := range confLines(content) {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && (fields[0] == "domain" || fields[0] == "address") {
			have[fields[0]+" "+fields[1]] = true
		}
	}
	return have
}

// ourAddressDeclared reports whether any "address" line already claims an AKA
// in this domain. Matching the domain rather than the exact address means a
// sysop who declared a different point or AKA of their own (4:900/100.2 where
// ftn.json says .1) does not get a second, conflicting address line — binkd
// treats every address line as one of ours, so an extra one changes which AKA
// it presents.
func ourAddressDeclared(have map[string]bool, domain string) bool {
	suffix := "@" + domain
	for k := range have {
		if rest, ok := strings.CutPrefix(k, "address "); ok && strings.HasSuffix(rest, suffix) {
			return true
		}
	}
	return false
}
