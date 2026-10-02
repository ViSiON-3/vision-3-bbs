package ftn

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
)

// A compiled nodelist is a network's nodelist reduced to what the BBS looks
// up — each listed system's name, location, sysop, status and flags — and
// saved as <dir>/<network>.json. It is written by v3mail toss when the
// network's nodelist arrives by file echo, or by helper nodelist import, and
// read through a NodelistIndex.

// NodelistDir is where compiled nodelists are kept under the data directory.
func NodelistDir(dataDir string) string {
	return filepath.Join(dataDir, "ftn", "nodelist")
}

// CompiledNode is one system in a compiled nodelist.
type CompiledNode struct {
	Address  string   `json:"address"`          // zone:net/node
	Status   string   `json:"status,omitempty"` // nodelist keyword: Zone, Region, Host, Hub, Pvt, Down, Hold, or "" for a plain node
	Name     string   `json:"name"`
	Location string   `json:"location,omitempty"`
	Sysop    string   `json:"sysop,omitempty"`
	Flags    []string `json:"flags,omitempty"`
}

// CompiledNodelist is a network's nodelist as saved by SaveCompiledNodelist.
type CompiledNodelist struct {
	Network    string         `json:"network"`
	Source     string         `json:"source"`               // file name or URL it was compiled from
	Date       time.Time      `json:"date,omitzero"`        // publication date from the header; zero if it had none
	DayNumber  int            `json:"day_number,omitempty"` // day number from the header
	CompiledAt time.Time      `json:"compiled_at"`
	Nodes      []CompiledNode `json:"nodes"`
}

// CompileNodelist reduces a parsed nodelist to a CompiledNodelist.
func CompileNodelist(nl *Nodelist, network, source string) *CompiledNodelist {
	c := &CompiledNodelist{
		Network:    network,
		Source:     source,
		Date:       nl.Date,
		DayNumber:  nl.DayNumber,
		CompiledAt: time.Now().UTC(),
		Nodes:      make([]CompiledNode, 0, len(nl.Entries)),
	}
	for _, e := range nl.Entries {
		flags := make([]string, 0, len(e.Flags))
		for _, f := range e.Flags {
			if f = stripControl(strings.TrimSpace(f)); f != "" {
				flags = append(flags, f)
			}
		}
		c.Nodes = append(c.Nodes, CompiledNode{
			Address:  e.Address.String(),
			Status:   e.Keyword,
			Name:     stripControl(e.Name),
			Location: stripControl(e.Location),
			Sysop:    stripControl(e.Sysop),
			Flags:    flags,
		})
	}
	return c
}

// stripControl removes control characters. A nodelist comes from another
// system, and its text is shown to callers: an escape sequence in a system's
// name must not reach their terminal.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

// HasZone reports whether the list has a Zone entry for zone: a check that a
// file carried by a network's nodelist echo is that network's nodelist, and
// not another network's sent through the same echo.
func (c *CompiledNodelist) HasZone(zone int) bool {
	for _, n := range c.Nodes {
		if n.Status == "Zone" {
			if a, err := ParseAddress(n.Address); err == nil && a.Zone == zone {
				return true
			}
		}
	}
	return false
}

// ReadNodelistFile reads and parses a distribution nodelist: plain text, or a
// ZIP archive holding it (the .Zxx convention, e.g. FSXNET.Z75 holding
// FSXNET.275). A nodediff is refused rather than parsed: its added lines would
// read as a nodelist missing everything that did not change.
func ReadNodelistFile(path string) (*Nodelist, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxNodelistBytes {
		return nil, fmt.Errorf("%s is larger than the %d-byte nodelist limit", filepath.Base(path), maxNodelistBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseNodelistPayload(data, filepath.Base(path))
}

// parseNodelistPayload parses a nodelist as read from a file or downloaded:
// plain text, or a ZIP holding it. A nodediff is refused; name says what was
// read, for the error.
func parseNodelistPayload(data []byte, name string) (*Nodelist, error) {
	if isZipData(data) {
		var err error
		if data, err = extractNodelistMember(data); err != nil {
			return nil, err
		}
	}
	if isNodediff(data) {
		return nil, fmt.Errorf("%s is a nodediff, not a full nodelist", name)
	}
	return ParseNodelist(bytes.NewReader(data))
}

// nodediffCommandRe matches a nodediff edit command: Add, Copy or Delete n lines.
var nodediffCommandRe = regexp.MustCompile(`^[ACD]\d+$`)

// isNodediff reports whether data is a nodediff, which carries edit commands
// on lines of their own. A nodelist line always has commas.
func isNodediff(data []byte) bool {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if nodediffCommandRe.MatchString(strings.TrimSpace(scanner.Text())) {
			return true
		}
	}
	return false
}

// CompiledNodelistPath is where a network's compiled nodelist lives in dir.
func CompiledNodelistPath(dir, network string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(network))
	if name == "" || strings.Trim(name, ".") == "" || strings.ContainsAny(name, `/\:`) {
		return "", fmt.Errorf("network name %q cannot be used as a file name", network)
	}
	return filepath.Join(dir, name+".json"), nil
}

// LoadCompiledNodelist reads a network's compiled nodelist from dir. The
// error wraps os.ErrNotExist when none has been compiled.
func LoadCompiledNodelist(dir, network string) (*CompiledNodelist, error) {
	path, err := CompiledNodelistPath(dir, network)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c CompiledNodelist
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &c, nil
}

// SaveCompiledNodelist writes c as the network's compiled nodelist in dir,
// replacing the one there in a single step. Unless force is set, a list older
// than the one already saved (by header date) is not written, and saved is
// false: file echoes can deliver weeks out of order. current is the list in
// force afterwards.
func SaveCompiledNodelist(dir, network string, c *CompiledNodelist, force bool) (saved bool, current *CompiledNodelist, err error) {
	path, err := CompiledNodelistPath(dir, network)
	if err != nil {
		return false, nil, err
	}
	if !force {
		// A missing or unreadable list is simply replaced.
		existing, err := LoadCompiledNodelist(dir, network)
		if err == nil && !existing.Date.IsZero() && !c.Date.IsZero() && c.Date.Before(existing.Date) {
			return false, existing, nil
		}
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return false, nil, err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false, nil, err
	}
	if err := atomicfile.WriteFile(path, data, 0644); err != nil {
		return false, nil, err
	}
	return true, c, nil
}

// NodelistIndex looks systems up in the compiled nodelists in a directory. A
// network's list is loaded on first use and reloaded when its file changes,
// so a long-running process picks up each week's list. It is safe for
// concurrent use.
type NodelistIndex struct {
	dir string

	mu   sync.Mutex
	nets map[string]*indexedNodelist
}

type indexedNodelist struct {
	modTime time.Time
	size    int64
	list    *CompiledNodelist
	byAddr  map[Address]*CompiledNode
}

// NewNodelistIndex returns an index over the compiled nodelists in dir.
func NewNodelistIndex(dir string) *NodelistIndex {
	return &NodelistIndex{dir: dir, nets: make(map[string]*indexedNodelist)}
}

// Lookup finds addr in the network's compiled nodelist. Points are not
// listed, so a point address finds its boss node. ok is false when the
// network has no compiled nodelist or the address is not in it.
func (x *NodelistIndex) Lookup(network string, addr Address) (node CompiledNode, ok bool) {
	ix := x.load(network)
	if ix == nil {
		return CompiledNode{}, false
	}
	addr.Point = 0
	n, ok := ix.byAddr[addr]
	if !ok {
		return CompiledNode{}, false
	}
	return *n, true
}

// List returns the network's compiled nodelist, or nil when it has none.
func (x *NodelistIndex) List(network string) *CompiledNodelist {
	if ix := x.load(network); ix != nil {
		return ix.list
	}
	return nil
}

func (x *NodelistIndex) load(network string) *indexedNodelist {
	key := strings.ToLower(network)
	path, err := CompiledNodelistPath(x.dir, key)
	if err != nil {
		return nil
	}

	x.mu.Lock()
	defer x.mu.Unlock()
	info, err := os.Stat(path)
	if err != nil {
		delete(x.nets, key)
		return nil
	}
	if ix, ok := x.nets[key]; ok && ix.modTime.Equal(info.ModTime()) && ix.size == info.Size() {
		return ix
	}
	list, err := LoadCompiledNodelist(x.dir, key)
	if err != nil {
		// Keep serving the last good list rather than none.
		return x.nets[key]
	}
	ix := &indexedNodelist{modTime: info.ModTime(), size: info.Size(), list: list, byAddr: make(map[Address]*CompiledNode, len(list.Nodes))}
	for i := range list.Nodes {
		if a, err := ParseAddress(list.Nodes[i].Address); err == nil {
			if _, dup := ix.byAddr[a]; !dup {
				ix.byAddr[a] = &list.Nodes[i]
			}
		}
	}
	x.nets[key] = ix
	return ix
}
