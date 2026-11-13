package user

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/filelock"
)

// A purged account's handle is retired: nobody can register it, or rename
// an account to it, again (#1048). Private mail is matched to its sender and
// recipient by handle, and a purge leaves the mail in place, so whoever took
// the handle next would read the old account's mail, sent and received.
// Retiring the handle keeps that mail with nobody and deletes nothing, so
// the people the purged account wrote to keep their letters.
//
// The list lives beside users.json, one handle per line, in lower case. A
// sysop releases a handle by deleting its line, once they're sure nothing
// still addressed to it should stay private.

// RetiredHandlesFile is the retired-handle list's name, beside users.json.
const RetiredHandlesFile = "retired_handles"

// ErrHandleRetired is returned for a handle on the retired list.
var ErrHandleRetired = errors.New("that handle is retired (a purged or renamed account had it) and can't be used again")

func retiredPath(usersPath string) string {
	return filepath.Join(filepath.Dir(usersPath), RetiredHandlesFile)
}

// readRetired is the retired list as a set of lower-case handles. A list
// that has never been written is empty; one that can't be read is an error,
// and callers then refuse rather than allow (a retired handle must never be
// reissued because its list was unreadable).
func readRetired(usersPath string) (map[string]bool, error) {
	out := map[string]bool{}
	if usersPath == "" {
		return out, nil
	}
	data, err := os.ReadFile(retiredPath(usersPath))
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", retiredPath(usersPath), err)
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if h := strings.ToLower(strings.TrimSpace(sc.Text())); h != "" && !strings.HasPrefix(h, "#") {
			out[h] = true
		}
	}
	return out, sc.Err()
}

// HandleRetired reports whether handle is on the retired list beside
// usersPath. An error means the list couldn't be read: treat the handle as
// unavailable.
func HandleRetired(usersPath, handle string) (bool, error) {
	set, err := readRetired(usersPath)
	if err != nil {
		return false, err
	}
	return set[strings.ToLower(strings.TrimSpace(handle))], nil
}

// RetireHandles adds handles to the retired list beside usersPath, under
// the list's own cross-process lock. Purges call it before removing any
// account, and purge nothing if it fails.
func RetireHandles(usersPath string, handles ...string) error {
	if usersPath == "" || len(handles) == 0 {
		return nil
	}
	path := retiredPath(usersPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	lock, err := filelock.Acquire(path, filelock.DefaultTimeout)
	if err != nil {
		return fmt.Errorf("locking %s: %w", path, err)
	}
	defer lock.Release()
	set, err := readRetired(usersPath)
	if err != nil {
		return err
	}
	changed := false
	for _, h := range handles {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" && !set[h] {
			set[h] = true
			changed = true
		}
	}
	if !changed {
		return nil
	}
	list := make([]string, 0, len(set))
	for h := range set {
		list = append(list, h)
	}
	sort.Strings(list)
	body := "# Handles of purged accounts, which can't be used again (see docs/sysop/users/user-management.md).\n# Delete a line to release that handle.\n" + strings.Join(list, "\n") + "\n"
	return writeFileAtomic(path, []byte(body), 0o600)
}

// handleRetired is HandleRetired for this manager's users.json, as an error:
// ErrHandleRetired, or the error reading the list.
func (um *UserMgr) handleRetired(handle string) error {
	retired, err := HandleRetired(um.path, handle)
	if err != nil {
		return fmt.Errorf("checking retired handles: %w", err)
	}
	if retired {
		return ErrHandleRetired
	}
	return nil
}

// HandleRetired reports whether handle belonged to a purged account and
// can't be registered. A list that can't be read counts as retired.
func (um *UserMgr) HandleRetired(handle string) bool {
	return um != nil && um.handleRetired(handle) != nil
}
