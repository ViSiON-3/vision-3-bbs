//go:build windows

package menu

import (
	"os"
	"os/exec"
	"time"
)

// setDoorProcessGroup does nothing on Windows, where hanging up kills the
// door process itself.
func setDoorProcessGroup(*exec.Cmd) {}

// doorGroupOf returns 0: Windows doors run no process group here.
func doorGroupOf(*os.Process) int { return 0 }

// killProcessGroupOf kills p. Windows has no process groups here.
func killProcessGroupOf(_ int, p *os.Process) {
	_ = p.Kill() // best effort: it may have exited
}

// hangUpDoorProcess ends a door whose caller has gone idle. Windows has no
// hang-up signal, so the process is killed.
func hangUpDoorProcess(p *os.Process, _ int, _ <-chan struct{}, _ time.Duration) {
	_ = p.Kill() // best effort: it may have exited
}
