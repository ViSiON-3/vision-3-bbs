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
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"github.com/ViSiON-3/vision-3-bbs/internal/filelock"
)

// A purged account's handle is retired: nobody can register it, or rename
// an account to it, again (#1048). Private mail is matched to its sender and
// recipient by handle, and a purge leaves the mail in place, so whoever took
// the handle next would read the old account's mail, sent and received.
// Retiring the handle keeps that mail with nobody and deletes nothing, so
// the people the purged account wrote to keep their letters.
//
// The list lives beside users.json, one handle per line. A line starting
// with '#' is a comment; a handle that itself starts with '#' or '\' is
// written with a '\' in front, so it can't be read as one. Handles are
// compared the way private mail compares them (strings.EqualFold, Unicode
// case folding), not by lower-casing: "ſam" folds to "Sam", so it must
// count as the same retired handle. A sysop releases a handle by deleting
// its line, once they're sure nothing still addressed to it should stay
// private.

// syncDir is atomicfile.SyncDir; a test replaces it to make a sync fail.
var syncDir = atomicfile.SyncDir

// RetiredHandlesFile is the retired-handle list's name, beside users.json.
const RetiredHandlesFile = "retired_handles"

// ErrHandleRetired is returned for a handle on the retired list.
var ErrHandleRetired = errors.New("that handle is retired (a purged or renamed account had it) and can't be used again")

func retiredPath(usersPath string) string {
	return filepath.Join(filepath.Dir(usersPath), RetiredHandlesFile)
}

// readRetired is the retired list's handles. A list that has never been
// written is empty; one that can't be read is an error, and callers then
// refuse rather than allow (a retired handle must never be reissued because
// its list was unreadable).
func readRetired(usersPath string) ([]string, error) {
	if usersPath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(retiredPath(usersPath))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", retiredPath(usersPath), err)
	}
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, `\`):
			out = append(out, line[1:])
		case strings.HasPrefix(line, `"`):
			// A handle with a control character, from before those were
			// refused, written Go-quoted (see retiredEntry).
			if h, err := strconv.Unquote(line); err == nil {
				out = append(out, h)
			} else {
				out = append(out, line)
			}
		default:
			out = append(out, line)
		}
	}
	return out, sc.Err()
}

// retiredEntry is handle as a line of the list. A handle with a control
// character (an account from before those were refused, which might hold a
// line break) is written Go-quoted, so it stays one line and one entry. A
// leading '#', '\' or '"' is escaped with '\'.
func retiredEntry(handle string) string {
	if hasControl(handle) {
		return strconv.Quote(handle)
	}
	if strings.HasPrefix(handle, "#") || strings.HasPrefix(handle, `\`) || strings.HasPrefix(handle, `"`) {
		return `\` + handle
	}
	return handle
}

// containsFold reports whether list has handle, compared as private mail
// compares handles.
func containsFold(list []string, handle string) bool {
	for _, h := range list {
		if strings.EqualFold(h, handle) {
			return true
		}
	}
	return false
}

// HandleRetired reports whether handle is on the retired list beside
// usersPath, compared as private mail compares handles. An error means the
// list couldn't be read: treat the handle as unavailable.
func HandleRetired(usersPath, handle string) (bool, error) {
	list, err := readRetired(usersPath)
	if err != nil {
		return false, err
	}
	return containsFold(list, strings.TrimSpace(handle)), nil
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
	list, err := readRetired(usersPath)
	if err != nil {
		return err
	}
	changed := false
	for _, h := range handles {
		if h = strings.TrimSpace(h); h != "" && !containsFold(list, h) {
			list = append(list, h)
			changed = true
		}
	}
	if !changed {
		// Already listed, perhaps by an earlier attempt whose directory
		// sync failed: sync now, so the purge that follows never relies
		// on a rename that isn't on disk.
		return syncDir(filepath.Dir(path))
	}
	sort.Strings(list)
	lines := make([]string, len(list))
	for i, h := range list {
		lines[i] = retiredEntry(h)
	}
	body := "# Handles of purged and renamed accounts, which can't be used again\n# (see docs/sysop/users/user-management.md). Delete a line to release one.\n" + strings.Join(lines, "\n") + "\n"
	// Durable: a purge removes the accounts once this returns.
	return atomicfile.WriteFileDurable(path, []byte(body), 0o600)
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
