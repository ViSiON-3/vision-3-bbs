//go:build !windows && !linux && !darwin

package scripting

import (
	"fmt"
	"os"
)

func checkDataReplacementAccess(_, _ *os.File) error {
	return fmt.Errorf("safe script data replacement is unsupported on this platform")
}
