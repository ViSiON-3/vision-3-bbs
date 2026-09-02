package user

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	callHistoryFile  = "callhistory.json"    // Filename for call history
	callNumberFile   = "callnumber.json"     // Filename for the next call number
	adminLogFile     = "admin_activity.json" // Filename for admin activity log
	callHistoryLimit = 20                    // Max number of call records to keep
	adminLogLimit    = 1000                  // Max number of admin log entries to keep
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
	path           string         // Path to users.json
	dataPath       string         // Path to the data directory (for callhistory.json etc)
	newUserLevel   int            // Access level assigned to new signups (from config)
	nextUserID     int            // Added to track the next available user ID
	callHistory    []CallRecord   // Added slice for call history
	nextCallNumber uint64         // Added counter for overall calls
	activeUserIDs  map[int32]bool // Track which user IDs are currently online
}

// NewUserManager creates and initializes a new user manager
func NewUserManager(dataPath string) (*UserMgr, error) { // Return renamed type
	um := &UserMgr{ // Use renamed type
		users:        make(map[string]*User),
		path:         filepath.Join(dataPath, userFile), // userFile path uses dataPath now
		newUserLevel: 1,                                 // Default to 1, will be overridden by SetNewUserLevel
		dataPath:     dataPath,                          // Store the data path
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
		slog.Warn("failed to load call history", "error", err)
	}

	// Load the next call number
	if err := um.loadNextCallNumber(); err != nil {
		// Log warning but continue, using the default start value of 1
		slog.Warn("failed to load next call number", "error", err)
	}

	if err := um.loadUsers(); err != nil {
		// If loading fails (e.g., file not found), create default felonius user
		if os.IsNotExist(err) {
			slog.Info("users.json not found; creating default felonius user")
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
				RealName:      "Joe Sysop",
				GroupLocation: "ViSiON/3",
				PrivateNote:   "SysOp",
				PasswordHash:  string(hashedPw),
				// Sysop level. This account is the only one a fresh install
				// has, and every sysop function (the ADMIN menu, user
				// validation) gates on S255, so anything less locks the
				// operator out of their own board with no in-BBS way back --
				// raising a level needs the admin menu, which needs the level.
				// setup.sh and dev-setup.sh already write 255 here; this path
				// runs when neither did, such as a Docker first start.
				AccessLevel: 255,
				Validated:   true,
				TimeLimit:   60,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			um.mu.Lock()
			um.users[strings.ToLower(defaultUser.Handle)] = defaultUser
			um.nextUserID = 2
			saveErr := um.saveUsersLocked()
			um.mu.Unlock()
			if saveErr != nil {
				return nil, fmt.Errorf("failed to save default felonius user: %w", saveErr)
			}
			slog.Info("default felonius user created (felonius/password)")
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
			slog.Info("migrated legacy username to handle", "username", user.LegacyUsername, "id", user.ID)
		}
		if strings.TrimSpace(user.Handle) == "" {
			slog.Warn("skipping user with no handle", "id", user.ID)
			continue
		}
		lowerHandle := strings.ToLower(user.Handle)
		if _, exists := um.users[lowerHandle]; exists {
			slog.Warn("duplicate handle; skipping subsequent entry", "handle", user.Handle)
			continue
		}
		um.users[lowerHandle] = user
		slog.Debug("loaded user", "handle", user.Handle, "group", user.GroupLocation)
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
	slog.Debug("determined next user ID", "id", um.nextUserID)
	um.mu.Unlock()
}

// NewUserMgrForTest builds a UserMgr seeded with the given users, keyed by
// lowercased handle to match the JSON load path and the lookup methods.
// Exported so tests in other packages (e.g. cmd/vision3) can seed a manager
// without touching the JSON load path.
func NewUserMgrForTest(users ...*User) *UserMgr {
	m := &UserMgr{users: make(map[string]*User, len(users))}
	for _, u := range users {
		m.users[strings.ToLower(u.Handle)] = u
	}
	return m
}
