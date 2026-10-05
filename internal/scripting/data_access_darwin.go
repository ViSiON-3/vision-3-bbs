package scripting

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

// checkDataReplacementAccess rejects macOS extended ACLs rather than dropping
// them. The system ls exposes ACL entries; -q keeps names on one output line.
func checkDataReplacementAccess(existing, probe *os.File) error {
	var prior, replacement unix.Stat_t
	if err := unix.Fstat(int(existing.Fd()), &prior); err != nil {
		return err
	}
	if err := unix.Fstat(int(probe.Fd()), &replacement); err != nil {
		return err
	}
	if prior.Uid != replacement.Uid || prior.Gid != replacement.Gid {
		return fmt.Errorf("cannot safely replace script data with different ownership")
	}
	for _, f := range []*os.File{existing, probe} {
		output, err := exec.Command("/bin/ls", "-ldeq", f.Name()).Output()
		if err != nil {
			return fmt.Errorf("inspect script data ACL: %w", err)
		}
		if bytes.Count(bytes.TrimSpace(output), []byte{'\n'}) != 0 {
			return fmt.Errorf("cannot safely replace script data with a macOS ACL")
		}
	}
	return nil
}
