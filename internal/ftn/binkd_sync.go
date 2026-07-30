package ftn

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Syncing binkd.conf against the configured links: identity fields and per-node
// host/password updates, without disturbing the rest of the file.

// BinkdIdentity holds BBS identity fields synced to binkd.conf.
type BinkdIdentity struct {
	BoardName string // sysname
	SysopName string // sysop
	Location  string // location
}

// BinkdLinkSync carries the per-link values synced into a binkd.conf node
// line. HostPort is "hostname:port"; when empty only the password of an
// existing line is synced and no new line is created (host unknown).
type BinkdLinkSync struct {
	SessionPwd string
	HostPort   string
}

// SyncBinkdConf updates binkd.conf to reflect the current FTN links and BBS
// identity fields (sysname, sysop, location). Node lines are upserted from
// links: an existing line (matched by address) gets its hostname and password
// refreshed, and a configured link with a hostname but no node line has one
// appended — so a hub change in the TUI fully propagates. Only lines that
// differ are rewritten; if nothing changed the file is not touched. Called
// from saveAll so TUI edits are reflected automatically.
//
// links maps "address@network" (e.g. "21:1/100@fsxnet") to its sync values.
func SyncBinkdConf(confPath string, identity BinkdIdentity, links map[string]BinkdLinkSync) error {
	existing, err := os.ReadFile(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No binkd.conf to sync.
		}
		return fmt.Errorf("reading binkd.conf: %w", err)
	}

	var out strings.Builder
	changed := false
	seenNodes := make(map[string]bool)

	// confLines, not bufio.Scanner: an over-64KB line would stop a scanner
	// early and the append path below would then persist a truncated file.
	for _, line := range confLines(string(existing)) {
		trimmed := strings.TrimSpace(line)

		// Sync sysname.
		if strings.HasPrefix(trimmed, "sysname ") && identity.BoardName != "" {
			newLine := fmt.Sprintf("sysname \"%s\"", identity.BoardName)
			if trimmed != newLine {
				out.WriteString(newLine)
				out.WriteByte('\n')
				changed = true
				continue
			}
		}

		// Sync sysop.
		if strings.HasPrefix(trimmed, "sysop ") && identity.SysopName != "" {
			newLine := fmt.Sprintf("sysop \"%s\"", identity.SysopName)
			if trimmed != newLine {
				out.WriteString(newLine)
				out.WriteByte('\n')
				changed = true
				continue
			}
		}

		// Sync location.
		if strings.HasPrefix(trimmed, "location ") && identity.Location != "" {
			newLine := fmt.Sprintf("location \"%s\"", identity.Location)
			if trimmed != newLine {
				out.WriteString(newLine)
				out.WriteByte('\n')
				changed = true
				continue
			}
		}

		// Sync node hostname and session password.
		if strings.HasPrefix(trimmed, "node ") {
			fields := strings.Fields(trimmed)
			if len(fields) >= 4 {
				addr := fields[1] // e.g. "21:1/100@fsxnet"
				if link, ok := links[addr]; ok {
					seenNodes[addr] = true
					newPwd := link.SessionPwd
					if newPwd == "" {
						newPwd = "-"
					}
					lineChanged := false
					if link.HostPort != "" && fields[2] != link.HostPort {
						fields[2] = link.HostPort
						lineChanged = true
					}
					if fields[3] != newPwd {
						fields[3] = newPwd
						lineChanged = true
					}
					if lineChanged {
						out.WriteString(strings.Join(fields, " "))
						out.WriteByte('\n')
						changed = true
						continue
					}
				}
			}
		}

		out.WriteString(line)
		out.WriteByte('\n')
	}

	// Append node lines for configured links that have a hostname but no
	// line yet (new link, or the link's address was changed in the TUI).
	// Sorted so repeated syncs produce identical files.
	var missing []string
	for addr, link := range links {
		if !seenNodes[addr] && link.HostPort != "" {
			missing = append(missing, addr)
		}
	}
	sort.Strings(missing)
	for _, addr := range missing {
		link := links[addr]
		pwd := link.SessionPwd
		if pwd == "" {
			pwd = "-"
		}
		fmt.Fprintf(&out, "node %s %s %s\n", addr, link.HostPort, pwd)
		changed = true
	}

	if !changed {
		return nil
	}
	return writeFileAtomic(confPath, out.String(), 0600)
}
