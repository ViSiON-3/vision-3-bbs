package user

import (
	"encoding/json"
	"os"
	"strings"
	"time"
)

// The BBS keeps every user in memory for the life of the process and rewrites
// the whole of users.json on each save. ./ue edits the same file directly from
// a separate process. Without the merge in this file, a sysop who changes a
// user's level or validates them while the BBS is running has that edit
// silently reverted by the next login, because the BBS writes back a copy it
// read at startup.
//
// ./ue already guards the other direction: it records the file's mtime at load
// and calls CheckFileChanged before saving, so it will not clobber the BBS.
// This gives the BBS the same discipline.

// sysopOwnedFields copies the fields that ./ue exposes for editing from src
// onto dst. These are the sysop's to set, so when the file has been edited
// underneath a running BBS, the on-disk values win over whatever the session
// happened to be carrying.
//
// The set is deliberately the editable field list from
// internal/usereditor/fields.go — display-only entries there (last login, call
// counts, created/updated stamps, deletion state) are the BBS's to maintain
// and are therefore left alone, taken from the in-memory record instead.
//
// Anything not listed here stays session-owned. When adding a field to the
// user editor, add it here too, or an external edit to it will be lost the
// same way this fixes.
func sysopOwnedFields(dst, src *User) {
	// Identity and access
	dst.RealName = src.RealName
	dst.AccessLevel = src.AccessLevel
	dst.Flags = src.Flags
	dst.Validated = src.Validated
	dst.TimeLimit = src.TimeLimit
	dst.FilePoints = src.FilePoints
	dst.TimesCalled = src.TimesCalled
	dst.GroupLocation = src.GroupLocation
	dst.PrivateNote = src.PrivateNote

	// Credentials
	dst.PasswordHash = src.PasswordHash
	dst.PublicKeys = src.PublicKeys

	// Terminal and display preferences
	dst.ScreenWidth = src.ScreenWidth
	dst.ScreenHeight = src.ScreenHeight
	dst.PreferredEncoding = src.PreferredEncoding
	dst.OutputMode = src.OutputMode
	dst.MsgHdr = src.MsgHdr
	dst.HotKeys = src.HotKeys
	dst.MorePrompts = src.MorePrompts
	dst.CustomPrompt = src.CustomPrompt
}

// readUsersFromDisk parses users.json into a handle-keyed map without touching
// the manager's own state. Used to see what an external editor wrote.
func readUsersFromDisk(path string) (map[string]*User, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var list []*User
	if err := json.Unmarshal(StripUTF8BOM(data), &list); err != nil {
		return nil, err
	}
	out := make(map[string]*User, len(list))
	for _, u := range list {
		if u == nil {
			continue
		}
		// Same keying as loadUsers: lowercased handle, with the legacy
		// "username" field standing in when Handle is absent.
		handle := strings.TrimSpace(u.Handle)
		if handle == "" {
			handle = strings.TrimSpace(u.LegacyUsername)
		}
		if handle == "" {
			continue
		}
		key := strings.ToLower(handle)
		if _, dup := out[key]; dup {
			continue // match loadUsers: first entry wins
		}
		out[key] = u
	}
	return out, nil
}

// fileMtimeOf returns the file's modification time, or the zero time if it
// cannot be stated (missing file, permissions). A zero time never compares
// equal to a real one, so an unreadable stat degrades to "assume changed",
// which costs a re-read rather than risking a clobber.
func fileMtimeOf(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// externallyModified reports whether users.json has changed since the manager
// last read or wrote it.
func (um *UserMgr) externallyModified() bool {
	return !fileMtimeOf(um.path).Equal(um.fileMtime)
}

// mergeExternalEdits folds any sysop edits made on disk back into the in-memory
// map before it is written out, so a save cannot revert them.
//
// Users present on disk but not in memory (added by the editor while the BBS
// was running) are adopted. Users in memory but not on disk were deleted
// externally and are dropped rather than resurrected.
//
// Called with um.mu already held.
func (um *UserMgr) mergeExternalEdits() {
	onDisk, err := readUsersFromDisk(um.path)
	if err != nil {
		// Unreadable or malformed: keep what we have rather than lose the
		// session's state. The write that follows restores a valid file.
		return
	}

	merged := make(map[string]*User, len(onDisk))
	for key, diskUser := range onDisk {
		if memUser, ok := um.users[key]; ok {
			combined := *memUser
			sysopOwnedFields(&combined, diskUser)
			// Mark the record as having moved on, so any copy a session is
			// already holding is recognised as predating this edit.
			combined.gen = memUser.gen + 1
			merged[key] = &combined
			continue
		}
		diskUser.gen = 1       // ahead of any zero-valued copy in flight
		merged[key] = diskUser // added externally while we were running
	}
	um.users = merged
}
