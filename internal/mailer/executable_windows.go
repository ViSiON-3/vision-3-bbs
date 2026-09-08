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
// instead, along with the file being an ordinary file: a directory, device or
// named pipe cannot be run whatever it is called.
func checkExecutable(path string, info os.FileInfo) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("binkd binary %s is not a regular file", path)
	}
	exts := executableExts()
	ext := strings.ToLower(filepath.Ext(path))
	for _, allowed := range exts {
		if ext == allowed {
			return nil
		}
	}
	return fmt.Errorf("binkd binary %s does not have an executable extension; "+
		"Windows needs one of %s", path, strings.Join(exts, " "))
}

// defaultExecutableExts is what Windows treats as runnable when PATHEXT says
// nothing.
var defaultExecutableExts = []string{".com", ".exe", ".bat", ".cmd"}

// executableExts returns the extensions Windows treats as runnable, from
// PATHEXT where it is set. Everything is lower-cased so comparisons are
// case-insensitive, as the filesystem is.
func executableExts() []string {
	raw := os.Getenv("PATHEXT")
	if raw == "" {
		return defaultExecutableExts
	}
	var exts []string
	for _, e := range strings.Split(raw, ";") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			exts = append(exts, e)
		}
	}
	if len(exts) == 0 {
		return defaultExecutableExts
	}
	return exts
}
