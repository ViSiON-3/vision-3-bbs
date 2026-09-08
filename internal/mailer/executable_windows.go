//go:build windows

package mailer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// checkExecutable reports whether the binkd binary can actually be run.
//
// Windows has no execute bit. Go's os.Stat reports 0666 for every ordinary
// file there (0444 when read-only), so the unix test of Mode()&0111 is not
// merely unhelpful, it is always false -- which made mailer.New fail on every
// Windows installation and took FTN mail down with it.
//
// What decides executability on Windows is the extension, so check that
// instead. PATHEXT is the authority and is honoured here rather than a fixed
// list, since a site can extend it.
func checkExecutable(path string, info os.FileInfo) error {
	if info.IsDir() {
		return fmt.Errorf("binkd binary %s is a directory", path)
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return fmt.Errorf("binkd binary %s has no executable extension; "+
			"Windows needs one of %s", path, strings.Join(executableExts(), " "))
	}
	for _, allowed := range executableExts() {
		if ext == allowed {
			return nil
		}
	}
	return fmt.Errorf("binkd binary %s does not have an executable extension; "+
		"Windows needs one of %s", path, strings.Join(executableExts(), " "))
}

// executableExts returns the extensions Windows treats as runnable, from
// PATHEXT where it is set and a conventional list otherwise. Everything is
// lower-cased so comparisons are case-insensitive, as the filesystem is.
func executableExts() []string {
	raw := os.Getenv("PATHEXT")
	if raw == "" {
		return []string{".com", ".exe", ".bat", ".cmd"}
	}
	var exts []string
	for _, e := range strings.Split(raw, ";") {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" {
			exts = append(exts, e)
		}
	}
	if len(exts) == 0 {
		return []string{".com", ".exe", ".bat", ".cmd"}
	}
	return exts
}
