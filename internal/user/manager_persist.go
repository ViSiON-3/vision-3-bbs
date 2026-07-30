package user

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// saveUsersLocked performs the actual saving without acquiring locks.
// Uses um.path (which should point to data/users.json)
func (um *UserMgr) saveUsersLocked() error { // Receiver uses renamed type
	// Convert map back to slice for saving as JSON array.
	// Clear LegacyUsername before marshaling so the old "username" key is not written back.
	usersList := make([]*User, 0, len(um.users))
	for _, user := range um.users {
		if user.LegacyUsername != "" {
			cp := *user
			cp.LegacyUsername = ""
			usersList = append(usersList, &cp)
		} else {
			usersList = append(usersList, user)
		}
	}

	data, err := json.MarshalIndent(usersList, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal users slice: %w", err)
	}

	// Ensure the directory exists before writing the file
	dir := filepath.Dir(um.path)
	if err := os.MkdirAll(dir, 0750); err != nil { // Use 0750 for permissions
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// WriteFile ensures atomic write (usually via temp file)
	if err = os.WriteFile(um.path, data, 0600); err != nil {
		return fmt.Errorf("failed to write users file %s: %w", um.path, err) // Include path in error
	}
	return nil
}

// SaveUsers saves the current user data to the JSON file (acquires lock).
func (um *UserMgr) SaveUsers() error { // Receiver uses renamed type
	um.mu.Lock()
	defer um.mu.Unlock()
	return um.saveUsersLocked()
}

// LogAdminActivity logs an administrative action to the activity log file
func (um *UserMgr) LogAdminActivity(logEntry AdminActivityLog) error {
	um.mu.Lock()
	defer um.mu.Unlock()

	// Load existing logs
	logPath := filepath.Join(filepath.Dir(um.path), adminLogFile)
	var logs []AdminActivityLog

	// Try to load existing logs
	if data, err := os.ReadFile(logPath); err == nil {
		data = StripUTF8BOM(data)
		_ = json.Unmarshal(data, &logs) // Ignore errors, start fresh if corrupt
	}

	// Add new entry
	logEntry.ID = len(logs) + 1
	logEntry.Timestamp = time.Now()
	logs = append(logs, logEntry)

	// Keep only recent entries (prevent file from growing indefinitely)
	if len(logs) > adminLogLimit {
		logs = logs[len(logs)-adminLogLimit:]
	}

	// Save logs
	data, err := json.MarshalIndent(logs, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal admin logs: %w", err)
	}

	if err := os.WriteFile(logPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write admin log: %w", err)
	}

	return nil
}
