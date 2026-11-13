package user

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/filelock"
)

// User IDs are never handed out twice (#1020). Sponsorship and upload
// ownership name an account by ID, and the admin audit log records IDs, so
// an ID that came back after its account was purged would hand the old
// account's rights and history to a stranger. Taking the next ID from the
// highest one in users.json alone reuses it as soon as the newest account is
// purged, so the next ID is also kept in its own file beside users.json,
// which only ever goes up.

// NextUserIDFile is the high-water mark's file name, beside users.json.
const NextUserIDFile = "next_user_id"

func nextUserIDPath(usersPath string) string {
	return filepath.Join(filepath.Dir(usersPath), NextUserIDFile)
}

// readNextUserID is the stored mark, or 0 when there is none or it can't be
// read; the caller then has only users.json to go on.
func readNextUserID(usersPath string) int {
	if usersPath == "" {
		return 0
	}
	data, err := os.ReadFile(nextUserIDPath(usersPath))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("can't read the next user ID", "path", nextUserIDPath(usersPath), "error", err)
		}
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || n < 1 {
		slog.Warn("ignoring a bad next user ID", "path", nextUserIDPath(usersPath), "value", strings.TrimSpace(string(data)))
		return 0
	}
	return n
}

// raiseNextUserID stores next as the mark unless the stored one is already
// at or past it. It never lowers the mark. The read and the write are one
// step under the mark's own cross-process lock: the BBS and ./ue both raise
// it, and without the lock one could write a lower mark over the other's
// higher one. The lock is the mark's, not users.json's, so a caller already
// holding that one can't deadlock here.
func raiseNextUserID(usersPath string, next int) error {
	if usersPath == "" {
		return nil
	}
	path := nextUserIDPath(usersPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	lock, err := filelock.Acquire(path, filelock.DefaultTimeout)
	if err != nil {
		return fmt.Errorf("locking %s: %w", path, err)
	}
	defer lock.Release()
	if next <= readNextUserID(usersPath) {
		return nil
	}
	return writeFileAtomic(path, []byte(strconv.Itoa(next)+"\n"), 0o600)
}

// RecordUserIDs raises the mark past every ID in users, for a tool that
// edits users.json without a UserMgr (./ue), so an account it purges can't
// have its ID given out again.
func RecordUserIDs(usersPath string, users []*User) error {
	maxID := 0
	for _, u := range users {
		if u != nil && u.ID > maxID {
			maxID = u.ID
		}
	}
	return raiseNextUserID(usersPath, maxID+1)
}

// noteNextUserID stores um.nextUserID as the mark, logging a failure: the
// account is already saved, and the mark is put right on the next start
// unless the newest account is purged first. Called with um.mu held.
func (um *UserMgr) noteNextUserID() {
	if err := raiseNextUserID(um.path, um.nextUserID); err != nil {
		slog.Error("can't store the next user ID; a purged account's ID could be reused", "path", nextUserIDPath(um.path), "error", err)
	}
}
