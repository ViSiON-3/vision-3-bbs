package ftn

import "strings"

// Reading an existing binkd.conf: which directives to keep on rewrite, and the
// helpers that inspect what is already there.

// keptDirectives returns a set of "domain <name>" and "address <addr>" keys
// for non-placeholder lines in content — directives that a rewrite will keep.
func keptDirectives(content, bbsRoot string) map[string]bool {
	kept := make(map[string]bool)
	for _, line := range confLines(content) {
		if isPlaceholderLine(line, bbsRoot) {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && (fields[0] == "domain" || fields[0] == "address") {
			kept[fields[0]+" "+fields[1]] = true
		}
	}
	return kept
}

// confLines splits conf content into lines without bufio.Scanner's 64KB
// per-line limit; a trailing newline does not yield a spurious empty line.
func confLines(content string) []string {
	return strings.Split(strings.TrimSuffix(content, "\n"), "\n")
}

// domainNames returns the keys of a domain->zone map.
func domainNames(domains map[string]int) []string {
	names := make([]string, 0, len(domains))
	for name := range domains {
		names = append(names, name)
	}
	return names
}

// anyMissing reports whether any of names is absent from kept under prefix.
func anyMissing(kept map[string]bool, prefix string, names []string) bool {
	for _, n := range names {
		if !kept[prefix+n] {
			return true
		}
	}
	return false
}
