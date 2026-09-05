package user

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"strings"
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
// The set is the editable field list from internal/usereditor/fields.go, minus
// the running totals. Display-only entries there (last login, uploads, posts,
// created/updated stamps, deletion state) are the BBS's to maintain and are
// left alone.
//
// TimesCalled and FilePoints are editable in ./ue but deliberately excluded:
// the BBS advances them continuously — every login, every transfer — so taking
// them from disk would silently discard a session's activity on any external
// edit, however unrelated. A sysop setting them by hand while that user is
// mid-session is the far rarer event, and it is visible to the sysop when it
// does not stick.
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
		// loadUsers promotes LegacyUsername into Handle; do the same here.
		// Without it an adopted legacy record is written back with neither
		// field set, and the next load skips it as having no handle.
		u.Handle = handle
		key := strings.ToLower(handle)
		if _, dup := out[key]; dup {
			continue // match loadUsers: first entry wins
		}
		out[key] = u
	}
	return out, nil
}

// fileFingerprint identifies the exact contents of users.json.
//
// Modification time alone is not enough. Filesystems vary in mtime resolution,
// and an editor writing within the same tick as our own write would report an
// unchanged file, so the next save would skip the merge and clobber the edit.
// Hashing the bytes is exact regardless of clock granularity, and users.json is
// small enough that reading it once per save costs nothing next to the write
// that follows.
//
// The zero value means "unknown": it never compares equal to a real
// fingerprint, so an unreadable or missing file degrades to "assume changed",
// which costs a re-read rather than risking a clobber.
type fileFingerprint struct {
	size int64
	sum  [sha256.Size]byte
}

func fingerprintOf(path string) fileFingerprint {
	data, err := os.ReadFile(path)
	if err != nil {
		return fileFingerprint{}
	}
	return fileFingerprint{size: int64(len(data)), sum: sha256.Sum256(data)}
}

// externallyModified reports whether users.json differs from what the manager
// last read or wrote.
func (um *UserMgr) externallyModified() bool {
	return fingerprintOf(um.path) != um.fileState
}

// mergeExternalEdits folds any sysop edits made on disk back into the in-memory
// map before it is written out, so a save cannot revert them.
//
// Users present on disk but not in memory (added by the editor while the BBS
// was running) are adopted. Users in memory but not on disk are dropped if we
// had previously persisted them (an external delete) and kept if we had not
// (a registration still in flight).
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
		diskUser.gen = 1 // ahead of any zero-valued copy in flight
		diskUser.persisted = true
		merged[key] = diskUser // added externally while we were running
	}

	// A user in memory but absent from disk is ambiguous: either the editor
	// deleted it, or we created it and have not written it out yet. The
	// persisted flag tells them apart. Rebuilding purely from disk would drop
	// a registration that is mid-save.
	for key, memUser := range um.users {
		if _, stillThere := merged[key]; stillThere {
			continue
		}
		if !memUser.persisted {
			merged[key] = memUser // created here, not yet written
		}
		// else: it was on disk before and is not now — deleted externally.
	}

	um.users = merged

	// AddUser hands out nextUserID directly, so adopting records with higher
	// IDs must push the counter past them or the next registration collides.
	for _, u := range um.users {
		if u.ID >= um.nextUserID {
			um.nextUserID = u.ID + 1
		}
	}
}
