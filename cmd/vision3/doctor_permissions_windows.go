//go:build windows

package main

import "os"

// Windows ACLs are not represented by FileMode; this is only a conservative
// mode-bit fallback and should not be treated as an ACL verification.
func doctorPathWritable(info os.FileInfo) bool {
	return info.Mode().Perm()&0o222 != 0
}
