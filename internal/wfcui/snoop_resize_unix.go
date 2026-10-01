//go:build unix

package wfcui

import (
	"os"
	"os/signal"
	"syscall"
)

// watchResize reports terminal size changes on the returned channel until
// stop is called.
func watchResize() (<-chan struct{}, func()) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	out := make(chan struct{}, 1)
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case <-sig:
				select {
				case out <- struct{}{}:
				default:
				}
			case <-quit:
				return
			}
		}
	}()
	return out, func() {
		signal.Stop(sig)
		close(quit)
	}
}
