//go:build unix

package archiver

import "syscall"

func mkfifo(path string) error { return syscall.Mkfifo(path, 0600) }
