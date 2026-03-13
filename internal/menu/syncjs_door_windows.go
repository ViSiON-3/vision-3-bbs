//go:build windows

package menu

import "fmt"

// executeSyncJSDoor is a stub on Windows; Synchronet JS doors are not yet supported.
func executeSyncJSDoor(ctx *DoorCtx) error {
	return fmt.Errorf("synchronet_js doors are not yet supported on Windows")
}
