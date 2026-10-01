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

// hangUpDoorProcess ends a door whose caller has gone idle. Windows has no
// hang-up signal, so the process is killed.
func hangUpDoorProcess(p *os.Process, _ <-chan struct{}, _ time.Duration) {
	_ = p.Kill() // best effort: it may have exited
}
