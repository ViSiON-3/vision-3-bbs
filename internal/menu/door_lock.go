package menu

import (
	"errors"
	"strings"
	"sync"
)

// ErrDoorBusy is returned when a single-instance door is already in use.
var ErrDoorBusy = errors.New("door is currently in use by another node")

// doorLocks tracks which doors are currently in use and by which node.
// Used to enforce SingleInstance door configuration.
var (
	doorLocksMu sync.Mutex
	doorLocks   = make(map[string]int) // door name (uppercase) -> node number
)

// acquireDoorLock attempts to acquire a lock for the given door.
// Returns true if the lock was acquired, false if another node holds it.
// Door names are normalized to uppercase for consistent locking.
func acquireDoorLock(doorName string, nodeNumber int) bool {
	doorLocksMu.Lock()
	defer doorLocksMu.Unlock()

	key := strings.ToUpper(doorName)
	if holder, exists := doorLocks[key]; exists && holder != nodeNumber {
		return false
	}
	doorLocks[key] = nodeNumber
	return true
}

// releaseDoorLock releases the lock for the given door if held by the specified node.
// Door names are normalized to uppercase for consistent locking.
func releaseDoorLock(doorName string, nodeNumber int) {
	doorLocksMu.Lock()
	defer doorLocksMu.Unlock()

	key := strings.ToUpper(doorName)
	if holder, exists := doorLocks[key]; exists && holder == nodeNumber {
		delete(doorLocks, key)
	}
}
