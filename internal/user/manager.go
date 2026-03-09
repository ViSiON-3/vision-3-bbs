package user

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt" // Import bcrypt
)

// Predefined errors for user management
var (
	ErrUserNotFound = errors.New("user not found")
	ErrHandleExists = errors.New("handle already exists")
)

const (
	userFile         = "users.json"
	callHistoryFile  = "callhistory.json"  // Filename for call history
	callNumberFile   = "callnumber.json"   // Filename for the next call number
	adminLogFile     = "admin_activity.json" // Filename for admin activity log
	callHistoryLimit = 20                  // Max number of call records to keep
	adminLogLimit    = 1000                // Max number of admin log entries to keep
)

// StripUTF8BOM returns data with UTF-8 BOM (EF BB BF) removed if present.
// PowerShell and some editors write JSON with BOM; Go's json package does not accept it.
func StripUTF8BOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

// UserMgr manages user data (Renamed from UserManager)
type UserMgr struct {
	users          map[string]*User
	mu             sync.RWMutex
	path           string // Path to users.json
	dataPath       string // Path to the data directory (for callhistory.json etc)
	newUserLevel   int    // Access level assigned to new signups (from config)
	nextUserID     int    // Added to track the next available user ID
	callHistory    []CallRecord    // Added slice for call history
	nextCallNumber uint64          // Added counter for overall calls
	activeUserIDs  map[int32]bool  // Track which user IDs are currently online
}

// NewUserManager creates and initializes a new user manager
func NewUserManager(dataPath string) (*UserMgr, error) { // Return renamed type
	um := &UserMgr{ // Use renamed type
		users:        make(map[string]*User),
		path:         filepath.Join(dataPath, userFile), // userFile path uses dataPath now
		newUserLevel: 1,                                 // Default to 1, will be overridden by SetNewUserLevel
		dataPath: dataPath,                          // Store the data path
		// LastLogins:  make([]LoginEvent, 0, MaxLastLogins), // Removed LastLogins initialization
		callHistory:    make([]CallRecord, 0, callHistoryLimit), // Initialize call history
		nextUserID:     1,                                       // Start user IDs from 1
		nextCallNumber: 1,                                       // Start call numbers from 1
		activeUserIDs:  make(map[int32]bool),                    // Initialize online user tracking
	}

	// Removed call to loadLastLogins

	// Load call history using the stored dataPath
	if err := um.loadCallHistory(); err != nil {
		// Log warning but continue
		log.Printf("WARN: Failed to load call history: %v", err)
	}

	// Load the next call number
	if err := um.loadNextCallNumber(); err != nil {
		// Log warning but continue, using the default start value of 1
		log.Printf("WARN: Failed to load next call number: %v", err)
	}

	if err := um.loadUsers(); err != nil {
		// If loading fails (e.g., file not found), create default felonius user
		if os.IsNotExist(err) {
			log.Println("INFO: users.json not found, creating default felonius user.")
			// Build the fully-initialized bootstrap user and write exactly once,
			// avoiding a partially-initialized entry on disk if a second save fails.
			hashedPw, hashErr := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.DefaultCost)
			if hashErr != nil {
				return nil, fmt.Errorf("failed to hash default felonius password: %w", hashErr)
			}
			now := time.Now()
			defaultUser := &User{
				ID:            1,
				Handle:        "Felonius",
				RealName:      "Felonius",
				GroupLocation: "FAiRLiGHT/PC",
				PasswordHash:  string(hashedPw),
				AccessLevel:   10,
				Validated:     true,
				TimeLimit:     60,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			um.mu.Lock()
			um.users[strings.ToLower(defaultUser.Handle)] = defaultUser
			um.nextUserID = 2
			saveErr := um.saveUsersLocked()
			um.mu.Unlock()
			if saveErr != nil {
				return nil, fmt.Errorf("failed to save default felonius user: %w", saveErr)
			}
			log.Println("INFO: Default felonius user created (felonius/password).")
			// Determine next user ID after creating the default user
			um.determineNextUserID()
			return um, nil // Return successfully after creating default
		} else {
			// Other load error
			return nil, fmt.Errorf("failed to load users: %w", err)
		}
	}
	// If load was successful, determine nextUserID
	um.determineNextUserID()
	return um, nil
}

// loadUsers loads user data from the JSON file.
func (um *UserMgr) loadUsers() error { // Receiver uses renamed type
	data, err := os.ReadFile(um.path)
	if err != nil {
		return err // Return error to NewUserManager to handle
	}
	data = StripUTF8BOM(data)

	// Temporary slice to hold users from JSON array
	// We load into a slice because the JSON is an array.
	var usersList []*User // Load into a slice of pointers to handle omitempty correctly
	if err := json.Unmarshal(data, &usersList); err != nil {
		return fmt.Errorf("failed to unmarshal users array: %w", err)
	}

	um.mu.Lock()
	defer um.mu.Unlock()
	// Ensure map is initialized
	if um.users == nil {
		um.users = make(map[string]*User)
	}

	// Populate the map from the slice
	for _, user := range usersList { // Iterate directly over the slice of pointers
		if user == nil { // Safety check for nil entries in JSON array
			continue
		}
		// Migration: legacy records stored handle in "username"; if Handle is absent use it.
		if strings.TrimSpace(user.Handle) == "" && user.LegacyUsername != "" {
			user.Handle = user.LegacyUsername
			log.Printf("INFO: Migrated legacy username %q to handle for user ID %d.", user.LegacyUsername, user.ID)
		}
		if strings.TrimSpace(user.Handle) == "" {
			log.Printf("WARN: Skipping user ID %d with no handle in users.json.", user.ID)
			continue
		}
		lowerHandle := strings.ToLower(user.Handle)
		if _, exists := um.users[lowerHandle]; exists {
			log.Printf("WARN: Duplicate handle found in users.json: %s. Skipping subsequent entry.", user.Handle)
			continue
		}
		um.users[lowerHandle] = user
		log.Printf("TRACE: Loaded user %s (Group: %s) from JSON.", user.Handle, user.GroupLocation)
	}

	// Note: determineNextUserID should be called *after* successful load
	// but *outside* the lock (or re-acquire read lock if needed) if called from NewUserManager.
	// It's called from NewUserManager after this returns.
	return nil
}

// determineNextUserID finds the max existing ID and sets nextUserID appropriately.
// Should be called after users are loaded.
func (um *UserMgr) determineNextUserID() { // Receiver uses renamed type
	um.mu.RLock() // Use read lock
	maxID := 0
	for _, u := range um.users {
		if u.ID > maxID {
			maxID = u.ID
		}
	}
	um.mu.RUnlock()

	um.mu.Lock() // Need write lock to set nextUserID
	um.nextUserID = maxID + 1
	log.Printf("DEBUG: Determined next User ID to be %d", um.nextUserID)
	um.mu.Unlock()
}

// loadCallHistory loads the call history events from JSON.
// Now uses um.dataPath internally.
func (um *UserMgr) loadCallHistory() error {
	filePath := filepath.Join(um.dataPath, callHistoryFile) // Use stored dataPath
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("INFO: %s not found, starting with empty call history list.", callHistoryFile)
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
	log.Printf("DEBUG: Loaded %d call history records from %s", len(um.callHistory), callHistoryFile)
	return nil
}

// loadNextCallNumber loads the next call number from its dedicated JSON file.
func (um *UserMgr) loadNextCallNumber() error {
	filePath := filepath.Join(um.dataPath, callNumberFile)
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("INFO: %s not found, starting call numbers from 1.", callNumberFile)
			// Keep the default um.nextCallNumber = 1
			return nil // Not an error if the file doesn't exist
		}
		return fmt.Errorf("failed to read %s: %w", callNumberFile, err)
	}

	data = StripUTF8BOM(data)
	if len(data) == 0 {
		log.Printf("WARN: %s is empty, starting call numbers from 1.", callNumberFile)
		return nil // Empty file, use default
	}

	um.mu.Lock() // Lock before modifying
	defer um.mu.Unlock()
	if err := json.Unmarshal(data, &um.nextCallNumber); err != nil {
		// If unmarshal fails, log and keep the default
		log.Printf("WARN: Failed to unmarshal %s: %v. Starting call numbers from 1.", callNumberFile, err)
		um.nextCallNumber = 1
		return nil // Don't return error, just use default
	}

	log.Printf("DEBUG: Loaded next call number %d from %s", um.nextCallNumber, callNumberFile)
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
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", callHistoryFile, err)
	}

	// Also save the next call number (atomically with history? separate file is simpler for now)
	if err := um.saveNextCallNumberLocked(); err != nil {
		// Log error but don't fail the history save if number save fails
		log.Printf("ERROR: Failed to save next call number: %v", err)
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
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", callNumberFile, err)
	}
	return nil
}

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

// UpdateUserByID updates a user looked up by their stable ID, safely re-keying
// the internal map when the handle has changed. Use this in any flow that may
// rename a user's handle (e.g., admin editor); for all other updates prefer
// UpdateUser.
func (um *UserMgr) UpdateUserByID(u *User) error {
	if u == nil {
		return fmt.Errorf("cannot update nil user")
	}
	u.Handle = strings.TrimSpace(u.Handle)
	if u.Handle == "" {
		return fmt.Errorf("handle cannot be blank")
	}
	um.mu.Lock()
	defer um.mu.Unlock()

	// Locate existing map entry by stable ID.
	var oldKey string
	for k, existing := range um.users {
		if existing.ID == u.ID {
			oldKey = k
			break
		}
	}
	if oldKey == "" {
		return ErrUserNotFound
	}

	newKey := strings.ToLower(u.Handle)
	var originalEntry *User
	rekeyed := false
	if newKey != oldKey {
		// Handle changed — ensure no collision with a different user.
		if existing, exists := um.users[newKey]; exists && existing.ID != u.ID {
			return ErrHandleExists
		}
		originalEntry = um.users[oldKey] // save for rollback
		delete(um.users, oldKey)
		rekeyed = true
	}

	userCopy := *u
	um.users[newKey] = &userCopy
	if err := um.saveUsersLocked(); err != nil {
		// Rollback in-memory map to match what is still on disk.
		delete(um.users, newKey)
		if rekeyed {
			um.users[oldKey] = originalEntry
		}
		return err
	}
	return nil
}

// UpdateUser copies the modified user back into the internal map and saves to disk.
// Use this instead of SaveUsers when you have modified a user copy obtained from
// GetUser or Authenticate and need those changes persisted.
func (um *UserMgr) UpdateUser(u *User) error {
	if u == nil {
		return fmt.Errorf("cannot update nil user")
	}
	um.mu.Lock()
	defer um.mu.Unlock()
	lowerHandle := strings.ToLower(u.Handle)
	if _, exists := um.users[lowerHandle]; !exists {
		return ErrUserNotFound
	}
	// Create a defensive copy to prevent external mutations from bypassing locks
	userCopy := *u
	um.users[lowerHandle] = &userCopy
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

// Authenticate checks handle and compares password hash.
// Handle lookup is case-insensitive.
// Returns: (user, success)
func (um *UserMgr) Authenticate(handle, password string) (*User, bool) {
	lowerHandle := strings.ToLower(handle)

	um.mu.RLock()
	user, exists := um.users[lowerHandle]
	if !exists {
		um.mu.RUnlock()
		return nil, false
	}
	// Deny login if user is deleted
	if user.DeletedUser {
		um.mu.RUnlock()
		return nil, false
	}
	// Copy the password hash while holding the read lock
	passwordHash := user.PasswordHash
	um.mu.RUnlock()

	// Compare hashed password outside any lock (bcrypt is CPU-intensive)
	err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password))
	if err != nil {
		return nil, false
	}

	// Authentication successful - update LastLogin and TimesCalled
	um.mu.Lock()
	user = um.users[lowerHandle] // Re-fetch under write lock
	if user == nil {
		um.mu.Unlock()
		return nil, false
	}
	user.LastLogin = time.Now()
	user.TimesCalled++
	um.mu.Unlock()

	// Save outside the write lock to avoid blocking other user operations
	if err := um.SaveUsers(); err != nil {
		log.Printf("ERROR: Failed to save user data after successful login for %s: %v", handle, err)
	}

	// Return a copy
	um.mu.RLock()
	userCopy := *um.users[lowerHandle]
	um.mu.RUnlock()
	return &userCopy, true
}

// GetUser retrieves a user by handle (case-insensitive).
// Returns a copy to prevent callers from mutating internal state without the lock.
func (um *UserMgr) GetUser(handle string) (*User, bool) {
	um.mu.RLock()
	defer um.mu.RUnlock()

	user, exists := um.users[strings.ToLower(handle)]
	if !exists {
		return nil, false
	}
	userCopy := *user
	return &userCopy, true
}

// GetUserByID returns a user by their ID (for optimistic locking checks)
func (um *UserMgr) GetUserByID(id int) (*User, bool) {
	um.mu.RLock()
	defer um.mu.RUnlock()

	for _, user := range um.users {
		if user.ID == id {
			// Return a copy to prevent modification of the internal user data
			userCopy := *user
			return &userCopy, true
		}
	}
	return nil, false
}

// NextUserID returns the ID that will be assigned to the next new user.
func (um *UserMgr) NextUserID() int {
	um.mu.RLock()
	defer um.mu.RUnlock()
	return um.nextUserID
}

// AddUser creates a new user, hashes the password, assigns an ID, and saves.
func (um *UserMgr) AddUser(password, handle, realName, groupLocation string) (*User, error) {
	handle = strings.TrimSpace(handle)
	if handle == "" {
		return nil, fmt.Errorf("handle cannot be blank")
	}
	lowerHandle := strings.ToLower(handle)

	um.mu.Lock()
	defer um.mu.Unlock()

	// Check if handle already exists
	if _, exists := um.users[lowerHandle]; exists {
		return nil, ErrHandleExists
	}

	// Hash the password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Create new user
	newUser := &User{
		ID:            um.nextUserID,
		PasswordHash:  string(hashedPassword),
		Handle:        handle,
		RealName:      realName,
		GroupLocation: groupLocation,
		AccessLevel:   um.newUserLevel,
		TimeLimit:     60,
		Validated:     false,
		LastLogin:     time.Time{},
	}

	// Add to map and increment nextUserID
	um.users[lowerHandle] = newUser
	um.nextUserID++

	// Save the updated user list *while still holding the lock*
	if err := um.saveUsersLocked(); err != nil {
		log.Printf("ERROR: Failed to save users after adding %s: %v", handle, err)
		delete(um.users, lowerHandle)
		um.nextUserID--
		return nil, err
	}

	log.Printf("INFO: Added user %s (ID: %d)", newUser.Handle, newUser.ID)
	return newUser, nil
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
		log.Printf("ERROR: Failed to save call history after adding record for user %d: %v", record.UserID, err)
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

// GetAllUsers returns a slice containing copies of all user records.
// Returns copies to prevent callers from mutating internal state.
func (um *UserMgr) GetAllUsers() []*User {
	um.mu.RLock()
	defer um.mu.RUnlock()

	usersSlice := make([]*User, 0, len(um.users))
	for _, user := range um.users {
		userCopy := *user
		usersSlice = append(usersSlice, &userCopy)
	}
	return usersSlice
}

// GetUserCount returns the total number of registered users.
func (um *UserMgr) GetUserCount() int {
	um.mu.RLock()
	defer um.mu.RUnlock()
	return len(um.users)
}

// GetTotalCalls returns the total number of calls (logins) recorded.
func (um *UserMgr) GetTotalCalls() uint64 {
	um.mu.RLock()
	defer um.mu.RUnlock()
	if um.nextCallNumber <= 1 {
		return 0
	}
	return um.nextCallNumber - 1
}

// SetNewUserLevel sets the access level assigned to new user signups.
// This should be called after loading the server config.
// Level is clamped to the valid range of 0-255.
func (um *UserMgr) SetNewUserLevel(level int) {
	um.mu.Lock()
	defer um.mu.Unlock()

	// Validate and clamp to 0-255 range
	if level < 0 {
		log.Printf("WARN: invalid newUserLevel %d; clamping to 0", level)
		level = 0
	} else if level > 255 {
		log.Printf("WARN: invalid newUserLevel %d; clamping to 255", level)
		level = 255
	}

	um.newUserLevel = level
}

// MarkUserOnline marks a user as currently online/connected
func (um *UserMgr) MarkUserOnline(userID int) {
	um.mu.Lock()
	defer um.mu.Unlock()
	um.activeUserIDs[int32(userID)] = true
	log.Printf("DEBUG: User ID %d marked as ONLINE", userID)
}

// MarkUserOffline marks a user as offline/disconnected
func (um *UserMgr) MarkUserOffline(userID int) {
	um.mu.Lock()
	defer um.mu.Unlock()
	delete(um.activeUserIDs, int32(userID))
	log.Printf("DEBUG: User ID %d marked as OFFLINE", userID)
}

// IsUserOnline returns true if the user is currently connected
func (um *UserMgr) IsUserOnline(userID int) bool {
	um.mu.RLock()
	defer um.mu.RUnlock()
	return um.activeUserIDs[int32(userID)]
}

// PurgeResult holds information about a permanently purged user for reporting.
type PurgeResult struct {
	ID        int
	Handle    string
	DeletedAt time.Time
}

// PurgeDeletedUsers permanently removes soft-deleted users whose DeletedAt timestamp
// is older than retentionDays days. Pass retentionDays=-1 to skip (no-op).
// Returns a slice of PurgeResult describing the removed accounts.
// The caller is responsible for logging admin activity if desired.
func (um *UserMgr) PurgeDeletedUsers(retentionDays int) ([]PurgeResult, error) {
	if retentionDays < 0 {
		return nil, nil
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	um.mu.Lock()
	defer um.mu.Unlock()

	// Phase 1: identify eligible users without modifying the map yet.
	type candidate struct {
		key    string
		user   *User
		result PurgeResult
	}
	var candidates []candidate
	for key, u := range um.users {
		if !u.DeletedUser {
			continue
		}
		if u.DeletedAt == nil {
			// Deleted but no timestamp: treat as immediately eligible.
			candidates = append(candidates, candidate{
				key:  key,
				user: u,
				result: PurgeResult{
					ID:     u.ID,
					Handle: u.Handle,
				},
			})
		} else if u.DeletedAt.Before(cutoff) {
			candidates = append(candidates, candidate{
				key:  key,
				user: u,
				result: PurgeResult{
					ID:        u.ID,
					Handle:    u.Handle,
					DeletedAt: *u.DeletedAt,
				},
			})
		}
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Phase 2: remove from in-memory store, then persist.
	// Roll back in-memory changes if save fails so the store stays consistent.
	for _, c := range candidates {
		delete(um.users, c.key)
	}
	if err := um.saveUsersLocked(); err != nil {
		for _, c := range candidates {
			um.users[c.key] = c.user
		}
		return nil, fmt.Errorf("purge: failed to save users: %w", err)
	}

	purged := make([]PurgeResult, len(candidates))
	for i, c := range candidates {
		purged[i] = c.result
	}
	log.Printf("INFO: Purged %d soft-deleted user account(s)", len(purged))
	return purged, nil
}

