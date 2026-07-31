package ftn

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	// Check if this node address already exists — skip if so.
	if len(existing) > 0 && nodeExists(string(existing), cfg.Node.Address) {
		return nil
	}

	outPath := filepath.Join(cfg.BBSRoot, "data", "ftn", "out")
	logPath := filepath.Join(cfg.BBSRoot, "data", "logs", "binkd.log")
	secureIn := filepath.Join(cfg.BBSRoot, "data", "ftn", "secure_in")
	insecureIn := filepath.Join(cfg.BBSRoot, "data", "ftn", "in")
	v3mailPath := filepath.Join(cfg.BBSRoot, "v3mail")

	boardName, sysop, location := identityOrDefaults(cfg)

	var out strings.Builder

	if len(existing) == 0 {
		// Generate fresh binkd.conf.
		writeFreshBinkdConf(&out, cfg, outPath, logPath, secureIn, insecureIn, v3mailPath, boardName, sysop, location)
	} else {
		// Rewrite existing file: strip placeholders, inject real values.
		rewriteBinkdConf(&out, string(existing), cfg, outPath, logPath, secureIn, insecureIn, v3mailPath, boardName, sysop, location)
	}

	// Append the new node block.
	pwd := cfg.Node.SessionPwd
	if pwd == "" {
		pwd = "-"
	}
	fmt.Fprintf(&out, "\n%s\nnode %s %s %s\n",
		sectionMarker(cfg.Node.NetworkName),
		cfg.Node.Address,
		cfg.Node.Hostname,
		pwd)

	return writeFileAtomic(confPath, out.String(), 0600)
}

// nodeExists checks whether a node address is already defined in the config.
func nodeExists(content, address string) bool {
	for _, l := range confLines(content) {
		line := strings.TrimSpace(l)
		if strings.HasPrefix(line, "node ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == address {
				return true
			}
		}
	}
	return false
}
