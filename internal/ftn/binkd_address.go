package ftn

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Following a changed own address into binkd.conf.
//
// Every other sync leaves a network's "address" line alone once one exists:
// SyncBinkdNetworks only declares what is missing, and cannot tell a stale
// address from an AKA the sysop chose on purpose. So an own address corrected
// in the editor reached ftn.json and the message areas while binkd went on
// presenting the old one. The uplink then holds mail for the node it was told
// about in ftn.json and hands binkd whatever is queued for the one it
// announced — netmail sits on the hub, with no error on either side.
//
// The editor knows both the old and the new address, which is what makes the
// stale line identifiable, so the change is applied from there.

// UpdateBinkdOwnAddress rewrites binkd.conf's declaration of oldAddr in the
// given network to newAddr. Only an "address" entry naming exactly
// oldAddr@network is touched: any other AKA in the domain is the sysop's own
// and stays. If newAddr is already declared the stale entry is dropped instead,
// since a second one would be a duplicate.
//
// The network's "domain" line follows when the change moves to another zone
// and the line still carries the old one. The tosser names the files in a
// network's outbound with no zone component, which binkd reads as the domain's
// default zone, so a stale zone there leaves outbound mail unsent.
//
// A missing binkd.conf is a no-op (EnsureBinkdConf regenerates it from the new
// address), as is a file that does not declare oldAddr. The file is only
// rewritten when something changed.
func UpdateBinkdOwnAddress(confPath, network, oldAddr, newAddr string) error {
	oldAddr, newAddr = strings.TrimSpace(oldAddr), strings.TrimSpace(newAddr)
	if oldAddr == "" || newAddr == "" || strings.EqualFold(oldAddr, newAddr) {
		return nil
	}

	existing, err := os.ReadFile(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no binkd.conf to update
		}
		return fmt.Errorf("reading binkd.conf: %w", err)
	}

	// binkd matches keywords and domain names case-insensitively, so the
	// comparisons here do too (see declaredDirectives).
	domain := strings.ToLower(network)
	oldAKA := strings.ToLower(oldAddr) + "@" + domain
	newAKA := newAddr + "@" + domain

	content := string(existing)
	lines := confLines(content)

	// binkd accepts several addresses on one line, so every entry is checked
	// rather than only the first.
	oldDeclared, newDeclared := false, false
	for _, l := range lines {
		fields := strings.Fields(l)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "address") {
			continue
		}
		for _, f := range fields[1:] {
			switch strings.ToLower(f) {
			case oldAKA:
				oldDeclared = true
			case strings.ToLower(newAKA):
				newDeclared = true
			}
		}
	}
	if !oldDeclared {
		return nil
	}

	// The zone only follows when both addresses parse; otherwise the domain
	// line is left as it is.
	oldZone, newZone := "", ""
	if o, err := ParseAddress(oldAddr); err == nil {
		if n, err := ParseAddress(newAddr); err == nil && o.Zone != n.Zone {
			oldZone, newZone = strconv.Itoa(o.Zone), strconv.Itoa(n.Zone)
		}
	}

	out := make([]string, 0, len(lines))
	for _, l := range lines {
		fields := strings.Fields(l)
		if len(fields) < 2 {
			out = append(out, l)
			continue
		}
		indent := l[:len(l)-len(strings.TrimLeft(l, " \t"))]

		switch strings.ToLower(fields[0]) {
		case "address":
			kept := fields[:1:1]
			touched := false
			for _, f := range fields[1:] {
				if strings.ToLower(f) != oldAKA {
					kept = append(kept, f)
					continue
				}
				touched = true
				if newDeclared {
					continue // already declared elsewhere: drop the stale entry
				}
				kept = append(kept, newAKA)
				newDeclared = true
			}
			if !touched {
				out = append(out, l)
				continue
			}
			if len(kept) > 1 {
				out = append(out, indent+strings.Join(kept, " "))
			}
		case "domain":
			// "domain <name> <outbound-path> <zone>"
			if newZone != "" && len(fields) == 4 && strings.ToLower(fields[1]) == domain && fields[3] == oldZone {
				fields[3] = newZone
				out = append(out, indent+strings.Join(fields, " "))
				continue
			}
			out = append(out, l)
		default:
			out = append(out, l)
		}
	}

	updated := strings.Join(out, "\n")
	if strings.HasSuffix(content, "\n") {
		updated += "\n"
	}
	return writeFileAtomic(confPath, updated, 0600)
}
