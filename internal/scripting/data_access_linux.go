package scripting

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// checkDataReplacementAccess rejects metadata a fresh inode cannot retain.
// Matching inherited POSIX ACLs can be retained by the existing chmod path.
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
	sourceACL, err := dataAccessACL(existing)
	if err != nil {
		return err
	}
	probeACL, err := dataAccessACL(probe)
	if err != nil {
		return err
	}
	if !bytes.Equal(sourceACL, probeACL) {
		return fmt.Errorf("cannot safely replace script data with a file-specific POSIX ACL")
	}
	return nil
}

func dataAccessACL(f *os.File) ([]byte, error) {
	const name = "system.posix_acl_access"
	size, err := unix.Fgetxattr(int(f.Fd()), name, nil)
	if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read script data ACL: %w", err)
	}
	acl := make([]byte, size)
	size, err = unix.Fgetxattr(int(f.Fd()), name, acl)
	if err != nil {
		return nil, fmt.Errorf("read script data ACL: %w", err)
	}
	return acl[:size], nil
}
