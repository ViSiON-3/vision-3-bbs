package ftn

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// binkd.conf generation: the config types and the entry point that creates or
// updates the file for a network. The writers, readers and per-link sync live
// in the binkd_* files beside this one.

// BinkdNode holds the parameters for a binkd node line.
type BinkdNode struct {
	Address     string // FTN address (e.g. "21:1/100@fsxnet")
	Hostname    string // host:port
	SessionPwd  string // session password ("-" if none)
	NetworkName string // used for section comment markers
}

// BinkdConfig holds all data needed to generate or update binkd.conf.
type BinkdConfig struct {
	BBSRoot   string // absolute path to BBS root directory
	BoardName string // BBS name for sysname
	SysopName string // sysop name (falls back to "SysOp")
	Location  string // BBS location (falls back to "Earth")

	// Domains maps network name to zone (e.g. "fsxnet" -> 21).
	Domains map[string]int

	// Addresses lists all "address" lines (e.g. "21:4/158@fsxnet").
	Addresses []string

	// Node is the new hub node to add.
	Node BinkdNode

	// OutboundPath is binkd's default BSO outbound directory, written into
	// the "domain" line of every network without its own. Empty falls back
	// to <BBSRoot>/data/ftn/out.
	OutboundPath string

	// NetworkOutbound holds per-network outbound overrides, keyed by
	// lower-cased network name. A network listed here gets its own directory
	// on its "domain" line instead of OutboundPath.
	NetworkOutbound map[string]string
}

// outboundPath returns the BSO outbound directory for the domain lines,
// resolving a relative configured path against the BBS root.
//
// This must be the same directory the tosser packs bundles into (ftn.json's
// binkd_outbound_path). binkd only sends what it finds in its own outbound,
// and a mismatch is silent: the tosser reports bundles created, binkd reports
// an empty queue, and echomail sits unsent with no error on either side.
func (c BinkdConfig) outboundPath() string {
	return BinkdOutboundDir(c.BBSRoot, c.OutboundPath)
}

// outbound resolves the default and every per-network outbound directory for
// the domain lines.
func (c BinkdConfig) outbound() BinkdOutbound {
	o := BinkdOutbound{
		Default:  c.outboundPath(),
		ByDomain: make(map[string]string, len(c.NetworkOutbound)),
	}
	for name, p := range c.NetworkOutbound {
		if p == "" {
			continue
		}
		o.ByDomain[strings.ToLower(name)] = BinkdOutboundDir(c.BBSRoot, p)
	}
	return o
}

// BinkdOutboundDir resolves ftn.json's binkd_outbound_path to the absolute
// directory binkd uses as its BSO outbound, falling back to the historical
// <bbsRoot>/data/ftn/out when unset. Callers hold the path in either form:
// config.FTNConfig.ResolvePaths has already made it absolute for the running
// BBS, while the config editor keeps the raw relative value.
func BinkdOutboundDir(bbsRoot, configured string) string {
	if configured == "" {
		return filepath.Join(bbsRoot, "data", "ftn", "out")
	}
	if filepath.IsAbs(configured) {
		return configured
	}
	return filepath.Join(bbsRoot, configured)
}

// BinkdOutbound holds the absolute BSO outbound directory for each binkd
// domain, so one binkd.conf can give two networks separate queues. ByDomain is
// keyed by lower-cased network name and carries only the networks that
// override the global path; every other domain resolves to Default.
type BinkdOutbound struct {
	Default  string
	ByDomain map[string]string
}

// For returns the outbound directory a binkd domain's queue lives in.
func (o BinkdOutbound) For(domain string) string {
	if p, ok := o.ByDomain[strings.ToLower(domain)]; ok && p != "" {
		return p
	}
	return o.Default
}

// Dirs returns every distinct outbound directory, sorted, for callers that
// have to create them: binkd will not create a missing outbound itself, and
// the tosser writing a bundle into a directory that does not exist fails.
func (o BinkdOutbound) Dirs() []string {
	seen := make(map[string]bool, len(o.ByDomain)+1)
	dirs := make([]string, 0, len(o.ByDomain)+1)
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		dirs = append(dirs, p)
	}
	add(o.Default)
	domains := make([]string, 0, len(o.ByDomain))
	for d := range o.ByDomain {
		domains = append(domains, d)
	}
	sort.Strings(domains)
	for _, d := range domains {
		add(o.ByDomain[d])
	}
	return dirs
}

// BinkdOutboundFor builds the per-domain outbound map for a loaded FTN config,
// resolving every path against the BBS root.
func BinkdOutboundFor(bbsRoot string, ftnCfg config.FTNConfig) BinkdOutbound {
	o := BinkdOutbound{
		Default:  BinkdOutboundDir(bbsRoot, ftnCfg.BinkdOutboundPath),
		ByDomain: make(map[string]string, len(ftnCfg.Networks)),
	}
	for name, netCfg := range ftnCfg.Networks {
		if netCfg.BinkdOutboundPath == "" {
			continue
		}
		o.ByDomain[strings.ToLower(name)] = BinkdOutboundDir(bbsRoot, netCfg.BinkdOutboundPath)
	}
	return o
}

// identityOrDefaults fills fallback values for blank BBS identity fields.
// The sysop fallback is deliberately not "SysOp": that exact quoted string
// is a placeholder token in the shipped template, and HasPlaceholders would
// reject a conf carrying it (see binkd_placeholder.go).
func identityOrDefaults(cfg BinkdConfig) (boardName, sysop, location string) {
	boardName = cfg.BoardName
	if boardName == "" {
		boardName = "Vision3 BBS"
	}
	sysop = cfg.SysopName
	if sysop == "" {
		sysop = "Sysop"
	}
	location = cfg.Location
	if location == "" {
		location = "Earth"
	}
	return boardName, sysop, location
}

// sectionMarker returns the comment line used to identify a wizard-managed block.
func sectionMarker(networkName string) string {
	return fmt.Sprintf("# --- %s (added by FTN Setup Wizard) ---", networkName)
}

// UpdateBinkdConf reads an existing binkd.conf, strips placeholder lines,
// injects real domain/address/node/sysname values from cfg, and writes
// the result back. Existing wizard-managed node blocks (from prior runs)
// are preserved. If the file doesn't exist, a fresh one is generated.
func UpdateBinkdConf(confPath string, cfg BinkdConfig) error {
	existing, err := os.ReadFile(confPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading binkd.conf: %w", err)
	}

	// This node is already defined: rewrite its line in place rather than
	// skipping. The wizard can be re-run to change a hub's hostname, port or
	// session password, and skipping would leave binkd talking to the old
	// details while ftn.json showed the new ones. Appending instead would give
	// binkd two lines for one address.
	if len(existing) > 0 && nodeExists(string(existing), cfg.Node.Address) {
		updated, changed := replaceNodeLine(string(existing), cfg.Node.Address, cfg.Node.Hostname, cfg.Node.SessionPwd)
		if !changed {
			return nil
		}
		return writeFileAtomic(confPath, updated, 0600)
	}

	outbound := cfg.outbound()
	logPath := filepath.Join(cfg.BBSRoot, "data", "logs", "binkd.log")
	secureIn := filepath.Join(cfg.BBSRoot, "data", "ftn", "secure_in")
	insecureIn := filepath.Join(cfg.BBSRoot, "data", "ftn", "in")
	v3mailPath := filepath.Join(cfg.BBSRoot, "v3mail")

	boardName, sysop, location := identityOrDefaults(cfg)

	var out strings.Builder

	if len(existing) == 0 {
		// Generate fresh binkd.conf.
		writeFreshBinkdConf(&out, cfg, outbound, logPath, secureIn, insecureIn, v3mailPath, boardName, sysop, location)
	} else {
		// Rewrite existing file: strip placeholders, inject real values.
		rewriteBinkdConf(&out, string(existing), cfg, outbound, logPath, secureIn, insecureIn, v3mailPath, boardName, sysop, location)
	}

	// Append the new node block.
	fmt.Fprintf(&out, "\n%s\n%s\n", sectionMarker(cfg.Node.NetworkName), buildNodeLine(cfg))

	return writeFileAtomic(confPath, out.String(), 0600)
}

// buildNodeLine renders the binkd "node" directive for a link.
func buildNodeLine(cfg BinkdConfig) string {
	return fmt.Sprintf("node %s %s %s", cfg.Node.Address, cfg.Node.Hostname, nodePassword(cfg.Node.SessionPwd))
}

// nodePassword renders a session password, using binkd's "-" for none.
func nodePassword(pwd string) string {
	if pwd == "" {
		return "-"
	}
	return pwd
}

// nodeOptionTakesValue reports whether a binkd node option consumes the word
// after it as its argument.
func nodeOptionTakesValue(opt string) bool {
	switch strings.ToLower(opt) {
	case "-pipe", "-bw":
		return true
	}
	return false
}

// nodePositionalIdx returns the index in fields of each of the first want
// positional arguments of a binkd "node" directive, in order: address, then
// host, then password, then flavour and the fileboxes.
//
// The positions are not fixed offsets. binkd (readcfg.c) drops every word
// beginning with "-" from the positional stream wherever it appears, so
// "node 21:1/100@fsxnet -nomd hub.example.com:24554 secret" is a perfectly
// ordinary directive in which the host is fields[3] and the password
// fields[4]. Options may equally precede the address. A lone "-" is a
// positional placeholder meaning "unset", not an option.
//
// Fewer than want indices are returned when the line carries fewer
// positional arguments than that.
func nodePositionalIdx(fields []string, want int) []int {
	idx := make([]int, 0, want)
	for i := 1; i < len(fields) && len(idx) < want; i++ {
		f := fields[i]
		if len(f) > 1 && f[0] == '-' {
			if nodeOptionTakesValue(f) {
				i++ // skip the option's argument
			}
			continue
		}
		idx = append(idx, i)
	}
	return idx
}

// mergeNodeFields rewrites the host and password of an existing binkd node
// directive while keeping everything else on the line.
//
// binkd accepts options such as -md, -ip or filebox settings that the wizard
// knows nothing about. Rewriting the whole line would silently drop a sysop's
// hand-added flags, so only the two fields the wizard owns are replaced and
// everything else is carried across untouched — which means finding those two
// fields by their positional rank rather than their offset, since an option
// anywhere earlier on the line shifts them.
func mergeNodeFields(existing []string, address, hostname, pwd string) []string {
	merged := append([]string(nil), existing...)
	if len(merged) == 0 {
		merged = append(merged, "node")
	}
	merged[0] = "node"

	// A short directive is grown by appending placeholders. Options are
	// stripped from the positional stream wherever they sit, so a value
	// appended after them still lands in the slot it is meant for.
	idx := nodePositionalIdx(merged, 3)
	for len(idx) < 3 {
		merged = append(merged, "-")
		idx = append(idx, len(merged)-1)
	}

	merged[idx[0]] = address
	merged[idx[1]] = hostname
	merged[idx[2]] = nodePassword(pwd)
	return merged
}

// replaceNodeLine updates the "node <address> ..." directive for the given
// address in place, preserving the line's indentation, any binkd flags beyond
// the fields the wizard manages, and the rest of the file. It reports whether
// anything actually changed, so an unchanged config is left untouched on disk.
func replaceNodeLine(content, address, hostname, pwd string) (string, bool) {
	lines := confLines(content)
	changed := false
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if !strings.HasPrefix(trimmed, "node ") {
			continue
		}
		fields := strings.Fields(trimmed)
		idx := nodePositionalIdx(fields, 1)
		if len(idx) == 0 || fields[idx[0]] != address {
			continue
		}

		merged := strings.Join(mergeNodeFields(fields, address, hostname, pwd), " ")
		if trimmed == merged {
			continue // already correct
		}
		indent := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		lines[i] = indent + merged
		changed = true
	}
	if !changed {
		return content, false
	}
	out := strings.Join(lines, "\n")
	if strings.HasSuffix(content, "\n") {
		out += "\n"
	}
	return out, true
}

// nodeExists checks whether a node address is already defined in the config.
func nodeExists(content, address string) bool {
	for _, l := range confLines(content) {
		line := strings.TrimSpace(l)
		if strings.HasPrefix(line, "node ") {
			fields := strings.Fields(line)
			if idx := nodePositionalIdx(fields, 1); len(idx) > 0 && fields[idx[0]] == address {
				return true
			}
		}
	}
	return false
}
