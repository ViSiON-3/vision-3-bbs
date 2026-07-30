package user

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// loadCallHistory loads the call history events from JSON.
// Now uses um.dataPath internally.
func (um *UserMgr) loadCallHistory() error {
	filePath := filepath.Join(um.dataPath, callHistoryFile) // Use stored dataPath
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("call history file not found; starting empty", "file", callHistoryFile)
			return nil // Not an error if the file doesn't exist yet
		}
		return fmt.Errorf("failed to read %s: %w", callHistoryFile, err)
	}

	data = StripUTF8BOM(data)
	if len(data) == 0 {
		return nil // Empty file is okay
	}

	um.mu.Lock() // Lock before modifying internal slice
	defer um.mu.Unlock()
	// Ensure slice exists
	if um.callHistory == nil {
		um.callHistory = make([]CallRecord, 0, callHistoryLimit)
	}
	if err := json.Unmarshal(data, &um.callHistory); err != nil {
		return fmt.Errorf("failed to unmarshal %s: %w", callHistoryFile, err)
	}
	// Ensure capacity and length limits are respected after loading
	if len(um.callHistory) > callHistoryLimit {
		startIdx := len(um.callHistory) - callHistoryLimit
		um.callHistory = um.callHistory[startIdx:]
	}
	slog.Debug("loaded call history records", "count", len(um.callHistory), "file", callHistoryFile)
	return nil
}

// loadNextCallNumber loads the next call number from its dedicated JSON file.
func (um *UserMgr) loadNextCallNumber() error {
	filePath := filepath.Join(um.dataPath, callNumberFile)
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("call number file not found; starting from 1", "file", callNumberFile)
			// Keep the default um.nextCallNumber = 1
			return nil // Not an error if the file doesn't exist
		}
		return fmt.Errorf("failed to read %s: %w", callNumberFile, err)
	}

	data = StripUTF8BOM(data)
	if len(data) == 0 {
		slog.Warn("call number file is empty; starting from 1", "file", callNumberFile)
		return nil // Empty file, use default
	}

	um.mu.Lock() // Lock before modifying
	defer um.mu.Unlock()
	if err := json.Unmarshal(data, &um.nextCallNumber); err != nil {
		// If unmarshal fails, log and keep the default
		slog.Warn("failed to unmarshal call number file; starting from 1", "file", callNumberFile, "error", err)
		um.nextCallNumber = 1
		return nil // Don't return error, just use default
	}

	slog.Debug("loaded next call number", "number", um.nextCallNumber, "file", callNumberFile)
	return nil
}

// saveCallHistoryLocked saves the current callHistory slice to JSON (assumes lock is held).
// Now uses um.dataPath internally.
func (um *UserMgr) saveCallHistoryLocked() error {
	if um.callHistory == nil {
		// Avoid marshaling nil slice, treat as empty
		um.callHistory = make([]CallRecord, 0)
	}
	data, err := json.MarshalIndent(um.callHistory, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal call history: %w", err)
	}

	filePath := filepath.Join(um.dataPath, callHistoryFile) // Use stored dataPath
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", callHistoryFile, err)
	}

	// Also save the next call number (atomically with history? separate file is simpler for now)
	if err := um.saveNextCallNumberLocked(); err != nil {
		// Log error but don't fail the history save if number save fails
		slog.Error("failed to save next call number", "error", err)
	}

	return nil
}

// saveNextCallNumberLocked saves the current nextCallNumber to its JSON file (assumes lock is held).
func (um *UserMgr) saveNextCallNumberLocked() error {
	data, err := json.Marshal(um.nextCallNumber) // Simple marshal, no indent needed
	if err != nil {
		return fmt.Errorf("failed to marshal next call number: %w", err)
	}

	filePath := filepath.Join(um.dataPath, callNumberFile)
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", callNumberFile, err)
	}
	return nil
}

// AddCallRecord adds a call record to the history and saves.
func (um *UserMgr) AddCallRecord(record CallRecord) {
	um.mu.Lock()
	defer um.mu.Unlock()

	// Ensure slice exists
	if um.callHistory == nil {
		um.callHistory = make([]CallRecord, 0, callHistoryLimit)
	}

	// Assign the current call number and increment the counter
	record.CallNumber = um.nextCallNumber
	um.nextCallNumber++

	// Append the new record
	um.callHistory = append(um.callHistory, record)

	// Limit the size of the history
	if len(um.callHistory) > callHistoryLimit {
		// Remove the oldest entry
		um.callHistory = um.callHistory[1:]
	}

	// Save the updated history *while still holding the lock*
	if err := um.saveCallHistoryLocked(); err != nil {
		slog.Error("failed to save call history after adding record", "id", record.UserID, "error", err)
		// Maybe try to rollback the append? Less critical than user add.
	}
}

// GetLastCallers retrieves the recent call history (from memory).
func (um *UserMgr) GetLastCallers() []CallRecord {
	um.mu.RLock()
	defer um.mu.RUnlock()

	// Return a copy to prevent modification of the internal slice
	historyCopy := make([]CallRecord, len(um.callHistory))
	copy(historyCopy, um.callHistory)
	return historyCopy
}
