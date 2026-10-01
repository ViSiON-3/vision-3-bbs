package ftn

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// The address family binkd calls a link over. A hub whose hostname has both A
// and AAAA records is tried over IPv6 first, and on a host with no working
// IPv6 route every poll fails. binkd's "-4" and "-6" node options pin the
// family; a link's ip_family setting is written as one of them.

// binkd node options choosing the address family (readcfg.c). "-46" and "-64"
// try one family and fall back to the other, and exist only in binkd builds
// configured --with-af-force, so they are never written here; a sysop who
// added one by hand keeps it unless the link is pinned to a single family.
const (
	binkdFlagIPv4 = "-4"
	binkdFlagIPv6 = "-6"
)

// binkdFamilyFlag returns the node option for a configured family, or "" for
// auto.
func binkdFamilyFlag(fam string) string {
	switch fam {
	case config.IPFamilyIPv4:
		return binkdFlagIPv4
	case config.IPFamilyIPv6:
		return binkdFlagIPv6
	}
	return ""
}

// isPinFamilyFlag reports whether f pins a node to one family.
func isPinFamilyFlag(f string) bool {
	return f == binkdFlagIPv4 || f == binkdFlagIPv6
}

// isFallbackFamilyFlag reports whether f is one of the try-then-fall-back
// options.
func isFallbackFamilyFlag(f string) bool {
	return f == "-46" || f == "-64"
}

// formatNodeLine renders a new binkd "node" directive. The family option goes
// right after the address, where binkd's own sample config puts node options.
func formatNodeLine(address, hostPort, pwd, fam string) string {
	if flag := binkdFamilyFlag(fam); flag != "" {
		return fmt.Sprintf("node %s %s %s %s", address, flag, hostPort, nodePassword(pwd))
	}
	return fmt.Sprintf("node %s %s %s", address, hostPort, nodePassword(pwd))
}

// applyIPFamily sets the address-family option on a node directive's fields.
//
// A pinned family replaces whatever family options the line carries with its
// own. Auto removes "-4" and "-6" only when authoritative is set: the config
// editor passes that, because there auto is a choice the sysop made on screen
// (the editor has already read any hand-added flag into the setting, see
// ReadBinkdIPFamilies). Elsewhere auto just means the setting was never made,
// and a flag added to binkd.conf by hand — the only workaround before this
// setting existed — is left where it is. The fallback options are a sysop's
// own and are kept under auto either way.
func applyIPFamily(fields []string, fam string, authoritative bool) []string {
	flag := binkdFamilyFlag(fam)
	if flag == "" && !authoritative {
		return fields
	}

	out := make([]string, 0, len(fields)+1)
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if i > 0 && nodeOptionTakesValue(f) && i+1 < len(fields) {
			out = append(out, f, fields[i+1]) // an option's argument is never a family flag
			i++
			continue
		}
		if i > 0 && (isPinFamilyFlag(f) || (flag != "" && isFallbackFamilyFlag(f))) {
			continue
		}
		out = append(out, f)
	}
	if flag == "" {
		return out
	}

	// After the address, or straight after "node" when the line has none.
	at := 1
	if idx := nodePositionalIdx(out, 1); len(idx) > 0 {
		at = idx[0] + 1
	}
	return slices.Insert(out, at, flag)
}

// nodeLineIPFamily returns the family a node directive pins with "-4" or
// "-6", or auto when it has neither. binkd applies options in order, so the
// last one wins.
func nodeLineIPFamily(fields []string) string {
	fam := config.IPFamilyAuto
	for i := 1; i < len(fields); i++ {
		switch fields[i] {
		case binkdFlagIPv4:
			fam = config.IPFamilyIPv4
		case binkdFlagIPv6:
			fam = config.IPFamilyIPv6
		default:
			if nodeOptionTakesValue(fields[i]) {
				i++ // an option's argument is never a family flag
			}
		}
	}
	return fam
}

// ReadBinkdIPFamilies returns the family each node line in binkd.conf pins,
// keyed by the line's address lower-cased ("21:1/100@fsxnet"), since binkd
// matches addresses case-insensitively. Lines with no "-4" or "-6" are left
// out. A missing file yields an empty map.
//
// The config editor reads this on load so a link whose binkd.conf line was
// given "-4" by hand shows IPv4 rather than auto, and saving it keeps the flag
// instead of stripping it.
func ReadBinkdIPFamilies(confPath string) (map[string]string, error) {
	data, err := os.ReadFile(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("reading binkd.conf: %w", err)
	}
	out := make(map[string]string)
	for _, l := range confLines(string(data)) {
		fields, _, ok := nodeDirective(strings.TrimSpace(l))
		if !ok {
			continue
		}
		idx := nodePositionalIdx(fields, 1)
		if len(idx) == 0 {
			continue
		}
		if fam := nodeLineIPFamily(fields); fam != config.IPFamilyAuto {
			out[strings.ToLower(fields[idx[0]])] = fam
		}
	}
	return out, nil
}
