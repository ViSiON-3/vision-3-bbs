//go:build !windows

package mailer

import (
	"fmt"
	"os"
)

// checkExecutable reports whether the binkd binary can actually be run.
//
// On unix the execute bits say so directly.
func checkExecutable(path string, info os.FileInfo) error {
	if info.Mode()&0111 == 0 {
		return fmt.Errorf("binkd binary %s is not executable", path)
	}
	return nil
}
