//go:build !windows

package main

import (
	"os"
	"syscall"
)

func doctorPathWritable(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.Mode().Perm()&0o222 != 0
	}
	if os.Geteuid() == 0 {
		return true
	}
	mode := stat.Mode
	if uint32(os.Geteuid()) == stat.Uid {
		return mode&0o200 != 0
	}
	if uint32(os.Getegid()) == stat.Gid || supplementaryGroup(stat.Gid) {
		return mode&0o020 != 0
	}
	return mode&0o002 != 0
}

func supplementaryGroup(gid uint32) bool {
	groups, err := os.Getgroups()
	if err != nil {
		return false
	}
	for _, group := range groups {
		if uint32(group) == gid {
			return true
		}
	}
	return false
}
