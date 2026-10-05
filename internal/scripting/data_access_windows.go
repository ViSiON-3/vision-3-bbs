package scripting

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// checkDataReplacementAccess fails closed if a directory-inherited DACL would
// replace a different, file-specific DACL. No store data has been written yet.
func checkDataReplacementAccess(existing, probe *os.File) error {
	prior, err := windows.GetSecurityInfo(windows.Handle(existing.Fd()), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read existing store DACL: %w", err)
	}
	inherited, err := windows.GetSecurityInfo(windows.Handle(probe.Fd()), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read replacement DACL: %w", err)
	}
	if prior.String() != inherited.String() {
		return fmt.Errorf("cannot safely replace script data with a file-specific Windows DACL")
	}
	return nil
}
