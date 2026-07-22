package ftn

import (
	"fmt"
	"path/filepath"
	"strings"
)

// RegenerateBinkdConf writes a complete binkd.conf from configuration alone:
// identity, domains, addresses, derived paths, and one node line per hub.
// Used when binkd.conf is missing (e.g. deleted for a reset) — the FTN Setup
// Wizard refuses to re-run for an existing network, so without this the file
// could not be recreated. The caller supplies real values; iport/loglevel are
// template defaults here and are corrected by SyncBinkdSettings afterwards.
func RegenerateBinkdConf(confPath string, cfg BinkdConfig, nodes []BinkdNode) error {
	outPath := filepath.Join(cfg.BBSRoot, "data", "ftn", "out")
	logPath := filepath.Join(cfg.BBSRoot, "data", "logs", "binkd.log")
	secureIn := filepath.Join(cfg.BBSRoot, "data", "ftn", "secure_in")
	insecureIn := filepath.Join(cfg.BBSRoot, "data", "ftn", "in")
	v3mailPath := filepath.Join(cfg.BBSRoot, "v3mail")

	boardName, sysop, location := identityOrDefaults(cfg)

	var out strings.Builder
	writeFreshBinkdConf(&out, cfg, outPath, logPath, secureIn, insecureIn, v3mailPath, boardName, sysop, location)
	for _, n := range nodes {
		pwd := n.SessionPwd
		if pwd == "" {
			pwd = "-"
		}
		fmt.Fprintf(&out, "\n%s\nnode %s %s %s\n", sectionMarker(n.NetworkName), n.Address, n.Hostname, pwd)
	}
	return writeFileAtomic(confPath, out.String(), 0600)
}
