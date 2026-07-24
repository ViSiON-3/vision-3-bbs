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
