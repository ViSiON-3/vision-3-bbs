package ftn

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// NodelistEntry is one parsed line of an FTS-5000 nodelist.
type NodelistEntry struct {
	Keyword  string  // "Zone", "Region", "Host", "Hub", "Pvt", "Down", "Hold", or "" for a plain node
	Address  Address // fully resolved zone:net/node
	Name     string  // system name, underscores translated to spaces
	Location string
	Sysop    string
	Flags    []string // raw flag fields after the baud field (e.g. "CM", "INA:host", "IBN:24556")
}

// Nodelist is a parsed nodelist. Entry order is preserved because FTS-5000
// expresses segment structure (which hub a node belongs to) purely by order.
type Nodelist struct {
	Entries []NodelistEntry
}

// ParseNodelist parses an FTS-5000 distribution nodelist. Comment lines
// (';'), blank lines, malformed lines, and lines before the first Zone
// line are skipped rather than failing the whole list.
func ParseNodelist(r io.Reader) (*Nodelist, error) {
	nl := &Nodelist{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	zone, net := 0, 0
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r\x1a")
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 2 {
			continue
		}
		keyword := strings.TrimSpace(fields[0])
		num, err := strconv.Atoi(strings.TrimSpace(fields[1]))
		if err != nil || num < 0 {
			continue
		}

		e := NodelistEntry{Keyword: keyword}
		switch keyword {
		case "Zone":
			zone, net = num, num
			e.Address = Address{Zone: zone, Net: net}
		case "Region", "Host":
			net = num
			e.Address = Address{Zone: zone, Net: net}
		case "Hub", "Pvt", "Down", "Hold", "":
			e.Address = Address{Zone: zone, Net: net, Node: num}
		default:
			continue
		}
		if zone == 0 {
			continue // no zone context yet
		}

		if len(fields) > 2 {
			e.Name = deunderscore(fields[2])
		}
		if len(fields) > 3 {
			e.Location = deunderscore(fields[3])
		}
		if len(fields) > 4 {
			e.Sysop = deunderscore(fields[4])
		}
		// fields[5] = phone, fields[6] = baud, rest are flags.
		if len(fields) > 7 {
			e.Flags = fields[7:]
		}
		nl.Entries = append(nl.Entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading nodelist: %w", err)
	}
	if len(nl.Entries) == 0 {
		return nil, fmt.Errorf("no nodelist entries found")
	}
	return nl, nil
}

// deunderscore converts nodelist underscore-encoding back to spaces.
func deunderscore(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "_", " ")
}

// DefaultBinkpPort is the standard BinkP TCP port.
const DefaultBinkpPort = 24554

// NodeLookup is the result of resolving a node and its uplink hub.
type NodeLookup struct {
	Self     *NodelistEntry // nil when the address is not in the nodelist
	Uplink   *NodelistEntry // hub/host/zone entry chosen as the uplink
	Hostname string         // resolved BinkP hostname for Uplink
	Port     int            // resolved BinkP port for Uplink
	Inferred bool           // true when Self is nil (uplink inferred from the net segment)
}

// Lookup finds addr's entry and its uplink hub. The uplink is the nearest
// enclosing Hub segment, else the net's Host, else the Zone entry —
// skipping entries that are Down/Hold or have no resolvable hostname.
// dnsSuffix (e.g. "binkp.net"), when non-empty, derives a hostname for
// uplinks that carry no INA/IBN hostname flag.
func (nl *Nodelist) Lookup(addr Address, dnsSuffix string) (*NodeLookup, error) {
	selfIdx, hubIdx, hostIdx, zoneIdx := -1, -1, -1, -1
	curHub := -1
	netSeen := false

	for i := range nl.Entries {
		e := &nl.Entries[i]
		if e.Address.Zone != addr.Zone {
			continue
		}
		if e.Keyword == "Zone" && zoneIdx == -1 {
			zoneIdx = i
		}
		if e.Address.Net != addr.Net {
			continue
		}
		switch e.Keyword {
		case "Region", "Host":
			netSeen = true
			if hostIdx == -1 {
				hostIdx = i
			}
			curHub = -1
		case "Hub":
			netSeen = true
			curHub = i
		default:
			netSeen = true
		}
		if selfIdx == -1 && e.Address.Node == addr.Node {
			selfIdx = i
			if curHub != i {
				hubIdx = curHub
			}
		}
	}

	if !netSeen {
		return nil, fmt.Errorf("net %d:%d not found in nodelist", addr.Zone, addr.Net)
	}

	res := &NodeLookup{Inferred: selfIdx == -1}
	if selfIdx != -1 {
		res.Self = &nl.Entries[selfIdx]
	}

	// Candidate uplinks in preference order. For an unlisted node the
	// governing hub is unknowable (position matters), so start at the Host.
	candidates := []int{hostIdx, zoneIdx}
	if selfIdx != -1 {
		candidates = append([]int{hubIdx}, candidates...)
	}
	for _, ci := range candidates {
		if ci == -1 || ci == selfIdx {
			continue
		}
		cand := &nl.Entries[ci]
		// Defense-in-depth: candidates are structurally Hub/Host/Region/Zone
		// entries (see hubIdx/hostIdx/zoneIdx above), and FTS-5000 lists a
		// downed hub under the single-valued "Down" keyword instead, so the
		// scan never records one as a candidate in the first place. This
		// check enforces the skip rule by construction should that ever
		// change.
		if cand.Keyword == "Down" || cand.Keyword == "Hold" {
			continue
		}
		host, port := cand.binkpHostPort()
		if host == "" && dnsSuffix != "" {
			host = fmt.Sprintf("f%d.n%d.z%d.%s",
				cand.Address.Node, cand.Address.Net, cand.Address.Zone, dnsSuffix)
		}
		if host == "" {
			continue
		}
		res.Uplink = cand
		res.Hostname = host
		res.Port = port
		return res, nil
	}
	return nil, fmt.Errorf("no usable uplink found for %s in nodelist", addr)
}

// binkpHostPort resolves the entry's BinkP hostname and port from its
// INA/IBN flags. Hostname preference: INA, then IBN's host part. Port
// comes from IBN ("IBN:port" or "IBN:host:port"), else DefaultBinkpPort.
func (e *NodelistEntry) binkpHostPort() (string, int) {
	inaHost, ibnHost := "", ""
	port := DefaultBinkpPort
	for _, f := range e.Flags {
		parts := strings.Split(strings.TrimSpace(f), ":")
		switch strings.ToUpper(parts[0]) {
		case "INA":
			if len(parts) > 1 && parts[1] != "" && inaHost == "" {
				inaHost = parts[1]
			}
		case "IBN":
			switch len(parts) {
			case 2:
				if p, err := strconv.Atoi(parts[1]); err == nil {
					port = p
				} else if ibnHost == "" {
					ibnHost = parts[1]
				}
			case 3:
				if ibnHost == "" {
					ibnHost = parts[1]
				}
				if p, err := strconv.Atoi(parts[2]); err == nil {
					port = p
				}
			}
		}
	}
	if inaHost != "" {
		return inaHost, port
	}
	return ibnHost, port
}
