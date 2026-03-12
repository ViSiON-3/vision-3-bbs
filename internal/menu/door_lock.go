package menu

import "sync"

// doorLocks tracks which doors are currently in use and by which node.
// Used to enforce SingleInstance door configuration.
var (
	doorLocksMu sync.Mutex
	doorLocks   = make(map[string]int) // door name (uppercase) -> node number
)

// acquireDoorLock attempts to acquire a lock for the given door.
// Returns true if the lock was acquired, false if another node holds it.
func acquireDoorLock(doorName string, nodeNumber int) bool {
	doorLocksMu.Lock()
	defer doorLocksMu.Unlock()

	if holder, exists := doorLocks[doorName]; exists && holder != nodeNumber {
		return false
	}
	doorLocks[doorName] = nodeNumber
	return true
}

// releaseDoorLock releases the lock for the given door if held by the specified node.
func releaseDoorLock(doorName string, nodeNumber int) {
	doorLocksMu.Lock()
	defer doorLocksMu.Unlock()

	if holder, exists := doorLocks[doorName]; exists && holder == nodeNumber {
		delete(doorLocks, doorName)
	}
}
