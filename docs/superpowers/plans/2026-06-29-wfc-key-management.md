# WFC Key Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a sysop add/list/remove a user's WFC SSH public keys through the `ue` TUI and a `helper users` CLI — no manual `users.json` editing.

**Architecture:** A pure, persistence-free core in `internal/user` (validate + fingerprint + add/remove/list on a `*User`) shared by two consumers: a key-manager dialog in the `ue` user editor, and `helper users addkey|listkeys|delkey`. Each consumer persists with its own existing atomic-save path.

**Tech Stack:** Go, `golang.org/x/crypto/ssh` (already used by auth), `github.com/charmbracelet/bubbletea` (the `ue` TUI).

## Global Constraints

- Module path: `github.com/ViSiON-3/vision-3-bbs`. Pure Go, no CGO. Go file names `snake_case`. Files under 300 lines.
- Key parsing MUST use `golang.org/x/crypto/ssh` (`ssh.ParseAuthorizedKey`, `ssh.MarshalAuthorizedKey`, `ssh.FingerprintSHA256`) — the same library `UserMgr.FindByAuthorizedKey` uses, so any stored key is guaranteed loadable by WFC auth.
- Listings show **SHA256 fingerprint + comment**, never the full key blob.
- Duplicate keys (same wire bytes, ignoring comment) are rejected; malformed input is rejected with a clear message; nothing is saved on error.
- Reuse existing atomic-save paths — no new file-writing or locking. `ue` persists via `usereditor.SaveUsers`; `helper` persists via `UserMgr.UpdateUser` (which re-inserts the user and saves atomically). Note `UserMgr.GetUser` returns a **copy**, so a mutated user must be written back with `UpdateUser`, not `SaveUsers`.
- Run `gofmt -w`, `go vet`, and the package's `go test` before each commit. The whole tree must still `go test ./...` and build.

**⚠️ WORKTREE DISCIPLINE (the Bash tool's default cwd is the MAIN repo, NOT the worktree):** All work happens in the worktree `/Users/whiting/Github/vision-3-bbs/.claude/worktrees/feature+wfc` (branch `feature/wfc`). Prefix every Bash command with `cd "<worktree>" && …`; use `git -C "<worktree>" …`; use absolute worktree paths for all file reads/writes. Before committing, confirm `git -C "<worktree>" rev-parse --abbrev-ref HEAD` prints `feature/wfc`.

Spec: `docs/superpowers/specs/2026-06-29-wfc-key-management-design.md`.
Verified facts: `User.PublicKeys []string` (`json:"publicKeys,omitempty"`) exists; `UserMgr` has `GetUser(handle) (*User, bool)`, `SaveUsers() error`, `NewUserManager(dataPath)`; `ue` fields are typed (`ftString/ftInteger/ftYesNo/ftAction`), the Password field is the lone `ftAction` and at `internal/usereditor/model.go:414` an `ftAction` sets `m.mode = modePasswordEntry`, rendered by `overlayPasswordDialog` in `internal/usereditor/view_edit.go`.

---

### Task 1: Shared key-management core in `internal/user`

**Files:**
- Create: `internal/user/pubkey_ops.go`
- Test: `internal/user/pubkey_ops_test.go`

**Interfaces:**
- Consumes: `User.PublicKeys []string`; `golang.org/x/crypto/ssh`.
- Produces:
  - `type PublicKeyInfo struct { Type, Comment, Fingerprint string }`
  - `func NormalizeAuthorizedKey(line string) (normalized string, info PublicKeyInfo, err error)`
  - `func (u *User) AddPublicKey(line string) (PublicKeyInfo, error)`
  - `func (u *User) RemovePublicKey(ref string) (PublicKeyInfo, error)` — `ref` = SHA256 fingerprint, unique fingerprint prefix, or 1-based index
  - `func (u *User) ListPublicKeys() (keys []PublicKeyInfo, unparseable int)`

- [ ] **Step 1: Write the failing test**

```go
package user

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// makeKey returns a valid OpenSSH ed25519 authorized_keys line and its SHA256
// fingerprint. comment is appended when non-empty.
func makeKey(t *testing.T, comment string) (line, fingerprint string) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh pub: %v", err)
	}
	line = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	if comment != "" {
		line += " " + comment
	}
	return line, ssh.FingerprintSHA256(sshPub)
}

func TestNormalizeAuthorizedKey(t *testing.T) {
	line, fp := makeKey(t, "sysop@laptop")
	norm, info, err := NormalizeAuthorizedKey(line)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if info.Type != "ssh-ed25519" || info.Comment != "sysop@laptop" || info.Fingerprint != fp {
		t.Fatalf("info wrong: %+v (want fp %s)", info, fp)
	}
	if !strings.HasPrefix(norm, "ssh-ed25519 ") || !strings.HasSuffix(norm, " sysop@laptop") {
		t.Fatalf("normalized wrong: %q", norm)
	}
	if _, _, err := NormalizeAuthorizedKey("not a key"); err == nil {
		t.Fatal("expected error for malformed key")
	}
	if _, _, err := NormalizeAuthorizedKey("   "); err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestAddPublicKeyDedup(t *testing.T) {
	line, _ := makeKey(t, "a@host")
	u := &User{}
	if _, err := u.AddPublicKey(line); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if len(u.PublicKeys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(u.PublicKeys))
	}
	// Same key bytes, different comment → duplicate.
	sameKeyDiffComment := strings.SplitN(line, " ", 3)
	dup := sameKeyDiffComment[0] + " " + sameKeyDiffComment[1] + " other@host"
	if _, err := u.AddPublicKey(dup); err == nil {
		t.Fatal("expected duplicate rejection")
	}
	if len(u.PublicKeys) != 1 {
		t.Fatalf("dup must not be appended, got %d", len(u.PublicKeys))
	}
	if _, err := u.AddPublicKey("garbage"); err == nil {
		t.Fatal("expected malformed rejection")
	}
}

func TestRemovePublicKey(t *testing.T) {
	l1, fp1 := makeKey(t, "one")
	l2, fp2 := makeKey(t, "two")
	u := &User{PublicKeys: []string{l1, l2}}

	// Remove by full fingerprint.
	if info, err := u.RemovePublicKey(fp1); err != nil || info.Fingerprint != fp1 {
		t.Fatalf("remove by fp: %v %+v", err, info)
	}
	if len(u.PublicKeys) != 1 {
		t.Fatalf("expected 1 left, got %d", len(u.PublicKeys))
	}
	// Remove remaining by index 1.
	if info, err := u.RemovePublicKey("1"); err != nil || info.Fingerprint != fp2 {
		t.Fatalf("remove by index: %v %+v", err, info)
	}
	if len(u.PublicKeys) != 0 {
		t.Fatalf("expected 0 left, got %d", len(u.PublicKeys))
	}
	// No match.
	if _, err := u.RemovePublicKey("SHA256:nope"); err == nil {
		t.Fatal("expected no-match error")
	}
}

func TestListPublicKeysReportsUnparseable(t *testing.T) {
	good, _ := makeKey(t, "ok")
	u := &User{PublicKeys: []string{good, "corrupt-entry"}}
	keys, bad := u.ListPublicKeys()
	if len(keys) != 1 || bad != 1 {
		t.Fatalf("want 1 good / 1 bad, got %d / %d", len(keys), bad)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "<worktree>" && go test ./internal/user/ -run 'TestNormalizeAuthorizedKey|TestAddPublicKeyDedup|TestRemovePublicKey|TestListPublicKeysReportsUnparseable' -v`
Expected: FAIL — undefined `NormalizeAuthorizedKey` / `AddPublicKey` etc.

- [ ] **Step 3: Write the implementation**

Create `internal/user/pubkey_ops.go`:

```go
package user

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

// PublicKeyInfo is a display-friendly summary of one registered WFC public key.
type PublicKeyInfo struct {
	Type        string // e.g. "ssh-ed25519"
	Comment     string // trailing comment on the authorized_keys line, if any
	Fingerprint string // ssh.FingerprintSHA256, e.g. "SHA256:abc…"
}

// NormalizeAuthorizedKey parses one OpenSSH authorized_keys line and returns
// its canonical stored form (key type + base64 key + optional comment), a
// display summary, and any error. Malformed or empty input is rejected.
func NormalizeAuthorizedKey(line string) (string, PublicKeyInfo, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", PublicKeyInfo{}, fmt.Errorf("empty public key")
	}
	pub, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return "", PublicKeyInfo{}, fmt.Errorf("not a valid OpenSSH public key: %w", err)
	}
	normalized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if comment != "" {
		normalized += " " + comment
	}
	return normalized, PublicKeyInfo{
		Type:        pub.Type(),
		Comment:     comment,
		Fingerprint: ssh.FingerprintSHA256(pub),
	}, nil
}

// keyMarshal returns the wire bytes of a stored line's key, for dedup/matching.
func keyMarshal(line string) ([]byte, bool) {
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return nil, false
	}
	return pub.Marshal(), true
}

// AddPublicKey validates line, dedupes against u.PublicKeys by wire bytes
// (ignoring comment, matching how auth compares keys), appends the normalized
// form, and returns the new key's info.
func (u *User) AddPublicKey(line string) (PublicKeyInfo, error) {
	normalized, info, err := NormalizeAuthorizedKey(line)
	if err != nil {
		return PublicKeyInfo{}, err
	}
	newBytes, _ := keyMarshal(normalized)
	for _, existing := range u.PublicKeys {
		if b, ok := keyMarshal(existing); ok && string(b) == string(newBytes) {
			return PublicKeyInfo{}, fmt.Errorf("key already registered (%s)", info.Fingerprint)
		}
	}
	u.PublicKeys = append(u.PublicKeys, normalized)
	return info, nil
}

// RemovePublicKey removes the key identified by ref: a full SHA256 fingerprint,
// a unique fingerprint prefix, or a 1-based index as shown by ListPublicKeys.
func (u *User) RemovePublicKey(ref string) (PublicKeyInfo, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return PublicKeyInfo{}, fmt.Errorf("no key reference given")
	}
	if idx, err := strconv.Atoi(ref); err == nil {
		if idx < 1 || idx > len(u.PublicKeys) {
			return PublicKeyInfo{}, fmt.Errorf("key index %d out of range (have %d)", idx, len(u.PublicKeys))
		}
		_, info, _ := NormalizeAuthorizedKey(u.PublicKeys[idx-1])
		u.PublicKeys = append(u.PublicKeys[:idx-1], u.PublicKeys[idx:]...)
		return info, nil
	}
	matchIdx := -1
	var matchInfo PublicKeyInfo
	for i, line := range u.PublicKeys {
		_, info, err := NormalizeAuthorizedKey(line)
		if err != nil {
			continue
		}
		if info.Fingerprint == ref || strings.HasPrefix(info.Fingerprint, ref) {
			if matchIdx != -1 {
				return PublicKeyInfo{}, fmt.Errorf("ambiguous key reference %q", ref)
			}
			matchIdx, matchInfo = i, info
		}
	}
	if matchIdx == -1 {
		return PublicKeyInfo{}, fmt.Errorf("no key matching %q", ref)
	}
	u.PublicKeys = append(u.PublicKeys[:matchIdx], u.PublicKeys[matchIdx+1:]...)
	return matchInfo, nil
}

// ListPublicKeys returns display summaries for u.PublicKeys and the count of
// stored lines that failed to parse (so a corrupt entry is surfaced).
func (u *User) ListPublicKeys() ([]PublicKeyInfo, int) {
	var out []PublicKeyInfo
	unparseable := 0
	for _, line := range u.PublicKeys {
		_, info, err := NormalizeAuthorizedKey(line)
		if err != nil {
			unparseable++
			continue
		}
		out = append(out, info)
	}
	return out, unparseable
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "<worktree>" && gofmt -w internal/user/ && go vet ./internal/user/ && go test ./internal/user/ -v`
Expected: PASS (new tests + existing user tests).

- [ ] **Step 5: Commit**

```bash
git -C "<worktree>" add internal/user/pubkey_ops.go internal/user/pubkey_ops_test.go
git -C "<worktree>" commit -m "feat(user): shared WFC public-key add/remove/list core"
```

---

### Task 2: `helper users addkey|listkeys|delkey`

**Files:**
- Modify: `cmd/helper/main.go` (add three `case`s to the `cmdUsers` switch ~line 165, and three lines to the `users` help block ~line 145)
- Create: `cmd/helper/users_keys.go`
- Test: `cmd/helper/users_keys_test.go`

**Interfaces:**
- Consumes: `user.NewUserManager(dataPath)`, `UserMgr.GetUser`, `UserMgr.UpdateUser`, and Task 1's `(*User).AddPublicKey/RemovePublicKey/ListPublicKeys`.

> **Critical (verified):** `UserMgr.GetUser(handle)` returns a *copy* of the user (`userCopy := *user`), NOT the pointer stored in the map. Mutating that copy and calling `SaveUsers()` would persist the **unchanged** map and silently drop the key. You MUST write the mutated copy back with `um.UpdateUser(u)` — `UpdateUser` re-inserts the user into the map and calls the atomic save itself. (`UpdateUser` returns `ErrUserNotFound` if the handle is missing; here the handle was just fetched, so it won't.)
- Produces: `func cmdUsersAddKey(args []string)`, `func cmdUsersListKeys(args []string)`, `func cmdUsersDelKey(args []string)`, plus a testable helper `func resolveUsersDataPath(args []string) string` mirroring the existing `--data` default (`data/users`).

Before writing, read `cmd/helper/main.go`'s `cmdUsersList`/`cmdUsersPurge` to copy the exact `--data` flag parsing and `user.NewUserManager` load pattern used there; reuse it rather than inventing a new one.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func writeUsersJSON(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const usersJSON = `[{"id":1,"handle":"Boss","accessLevel":255}]`
	if err := os.WriteFile(filepath.Join(dir, "users.json"), []byte(usersJSON), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeKeyLine(t *testing.T) string {
	t.Helper()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	sshPub, _ := ssh.NewPublicKey(pub)
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " co@laptop"
}

func TestUsersAddAndDelKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "users")
	writeUsersJSON(t, dir)
	keyFile := filepath.Join(t.TempDir(), "co.pub")
	line := makeKeyLine(t)
	if err := os.WriteFile(keyFile, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// addkey from a file.
	cmdUsersAddKey([]string{"--data", dir, "Boss", keyFile})

	um, err := user.NewUserManager(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	u, ok := um.GetUser("Boss")
	if !ok || len(u.PublicKeys) != 1 {
		t.Fatalf("addkey did not persist: %+v", u)
	}
	keys, _ := u.ListPublicKeys()
	fp := keys[0].Fingerprint

	// delkey by fingerprint.
	cmdUsersDelKey([]string{"--data", dir, "Boss", fp})
	um2, _ := user.NewUserManager(dir)
	u2, _ := um2.GetUser("Boss")
	if len(u2.PublicKeys) != 0 {
		t.Fatalf("delkey did not persist: %+v", u2)
	}
}
```

> These commands call `os.Exit` on error paths in normal CLI use. In the test we only exercise success paths (valid handle/key), so no exit fires. If you choose to `os.Exit(1)` on errors, keep the success path return-only so the test process survives.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "<worktree>" && go test ./cmd/helper/ -run TestUsersAddAndDelKey -v`
Expected: FAIL — undefined `cmdUsersAddKey` / `cmdUsersDelKey`.

- [ ] **Step 3: Write the implementation**

Create `cmd/helper/users_keys.go`:

```go
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// resolveUsersDataPath returns the value of a trailing/leading --data flag,
// defaulting to "data/users" (mirrors cmdUsersList/cmdUsersPurge).
func resolveUsersDataPath(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "--data" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return "data/users"
}

// stripFlags removes "--data <v>" pairs, leaving positional args.
func stripFlags(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--data" {
			i++ // skip value
			continue
		}
		out = append(out, args[i])
	}
	return out
}

func loadUserOrExit(dataPath, handle string) (*user.UserMgr, *user.User) {
	um, err := user.NewUserManager(dataPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to load users from %s: %v\n", dataPath, err)
		os.Exit(1)
	}
	u, ok := um.GetUser(handle)
	if !ok || u == nil {
		fmt.Fprintf(os.Stderr, "Error: no user with handle %q\n", handle)
		os.Exit(1)
	}
	return um, u
}

func cmdUsersAddKey(args []string) {
	dataPath := resolveUsersDataPath(args)
	pos := stripFlags(args)
	if len(pos) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: helper users addkey <handle> <keyfile|->")
		os.Exit(1)
	}
	handle, src := pos[0], pos[1]

	var raw []byte
	var err error
	if src == "-" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(src)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot read key from %s: %v\n", src, err)
		os.Exit(1)
	}

	um, u := loadUserOrExit(dataPath, handle)
	info, err := u.AddPublicKey(strings.TrimSpace(string(raw)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := um.UpdateUser(u); err != nil { // writes the mutated copy back + saves
		fmt.Fprintf(os.Stderr, "Error: failed to save: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Added key %s (%s) to %s\n", info.Fingerprint, info.Comment, u.Handle)
}

func cmdUsersListKeys(args []string) {
	dataPath := resolveUsersDataPath(args)
	pos := stripFlags(args)
	if len(pos) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: helper users listkeys <handle>")
		os.Exit(1)
	}
	_, u := loadUserOrExit(dataPath, pos[0])
	keys, unparseable := u.ListPublicKeys()
	if len(keys) == 0 {
		fmt.Printf("%s has no WFC public keys.\n", u.Handle)
	}
	for i, k := range keys {
		fmt.Printf("  %d. %-12s %s  %s\n", i+1, k.Type, k.Fingerprint, k.Comment)
	}
	if unparseable > 0 {
		fmt.Fprintf(os.Stderr, "Warning: %d unparseable key line(s) in this user's record.\n", unparseable)
	}
}

func cmdUsersDelKey(args []string) {
	dataPath := resolveUsersDataPath(args)
	pos := stripFlags(args)
	if len(pos) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: helper users delkey <handle> <fingerprint|index>")
		os.Exit(1)
	}
	um, u := loadUserOrExit(dataPath, pos[0])
	info, err := u.RemovePublicKey(pos[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := um.UpdateUser(u); err != nil { // writes the mutated copy back + saves
		fmt.Fprintf(os.Stderr, "Error: failed to save: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Removed key %s from %s\n", info.Fingerprint, u.Handle)
}
```

Then in `cmd/helper/main.go`, add to the `cmdUsers` switch (near `case "purge":` / `case "list":`):

```go
	case "addkey":
		cmdUsersAddKey(args[1:])
	case "listkeys":
		cmdUsersListKeys(args[1:])
	case "delkey":
		cmdUsersDelKey(args[1:])
```

> Confirm the exact slice passed to subcommands by reading how `cmdUsers` dispatches `purge`/`list` (it switches on `args[0]` then passes `args[1:]`). Match that convention exactly — if it passes `args[1:]`, use `args[1:]`.

And add to the `users` help block (near the `PURGE`/`LIST` help lines ~145):

```go
	fmt.Fprintln(w, helpcmd("ADDKEY <handle> <keyfile|->", "Register a WFC SSH public key for a user"))
	fmt.Fprintln(w, helpcmd("LISTKEYS <handle>", "List a user's WFC public keys (fingerprints)"))
	fmt.Fprintln(w, helpcmd("DELKEY <handle> <fingerprint|index>", "Remove a WFC public key from a user"))
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "<worktree>" && gofmt -w cmd/helper/ && go vet ./cmd/helper/ && go test ./cmd/helper/ -v && go build ./cmd/helper/`
Expected: PASS and clean build.

- [ ] **Step 5: Commit**

```bash
git -C "<worktree>" add cmd/helper/users_keys.go cmd/helper/users_keys_test.go cmd/helper/main.go
git -C "<worktree>" commit -m "feat(helper): users addkey/listkeys/delkey for WFC keys"
```

---

### Task 3: `ue` WFC Keys field + key-manager dialog

**Files:**
- Modify: `internal/usereditor/fields.go` (add a `WFC Keys` `ftAction` field)
- Modify: `internal/usereditor/model.go` (new modes + action dispatch + Update handling for the key dialog)
- Modify: `internal/usereditor/view_edit.go` or `internal/usereditor/dialogs.go` (render the key dialog overlay)
- Test: `internal/usereditor/pubkey_dialog_test.go`

**Interfaces:**
- Consumes: Task 1's `(*User).AddPublicKey/RemovePublicKey/ListPublicKeys`; the existing `ue` model (`m.mode editorMode`, the shared text input used by `modePasswordEntry`, `overlayPasswordDialog`).
- Produces: a `WFC Keys (N)` action field and an in-editor dialog to add/list/delete keys on the user currently being edited.

**Read first (this task clones an existing pattern):** `internal/usereditor/model.go` around line 414 (the `if f.Type == ftAction { m.mode = modePasswordEntry }` dispatch and the `modePasswordEntry` Update handling) and `internal/usereditor/view_edit.go`'s `overlayPasswordDialog`. The WFC Keys dialog mirrors that password-dialog flow (open a mode, capture text input, Esc to close), differing only in that it lists existing keys and supports add + delete.

Because the lone `ftAction` today is hardcoded to the password dialog, you must **discriminate which action field was activated**. Use the field's `Label`: in the `ftAction` branch, `if f.Label == "WFC Keys" { m.mode = modeKeyList } else { m.mode = modePasswordEntry }`.

- [ ] **Step 1: Write the failing test**

Test the dialog *logic* at the model level (no real terminal). It drives the same key-handling entry points the Bubble Tea `Update` uses. Implement small testable helpers on the model so this compiles:
- `func (m *model) openKeyDialog()` — sets mode to key-list for the current user.
- `func (m *model) keyDialogAdd(line string) error` — calls `AddPublicKey` on the edited user, returns the error to display.
- `func (m *model) keyDialogDelete(ref string) error` — calls `RemovePublicKey`.

```go
package usereditor

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func keyLine(t *testing.T) string {
	t.Helper()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	sp, _ := ssh.NewPublicKey(pub)
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))) + " me@host"
}

func TestKeyDialogAddDelete(t *testing.T) {
	u := &user.User{Handle: "Boss", AccessLevel: 255}
	m := newTestModelEditing(u) // see note below
	m.openKeyDialog()

	line := keyLine(t)
	if err := m.keyDialogAdd(line); err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(u.PublicKeys) != 1 {
		t.Fatalf("want 1 key, got %d", len(u.PublicKeys))
	}
	if err := m.keyDialogAdd("garbage"); err == nil {
		t.Fatal("expected malformed-key error")
	}
	keys, _ := u.ListPublicKeys()
	if err := m.keyDialogDelete(keys[0].Fingerprint); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(u.PublicKeys) != 0 {
		t.Fatalf("want 0 keys, got %d", len(u.PublicKeys))
	}
}
```

> `newTestModelEditing(u)` is a tiny test constructor you add: build a `model` whose "currently edited user" points at `u` (mirror however the model already holds the edit-target user — read `model.go` for the field name, e.g. `m.editUser` or an index into `m.users`). Keep it in the test file. If the model edits a copy that is committed on save, point the helper at that same in-progress copy so the assertions observe the mutation.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd "<worktree>" && go test ./internal/usereditor/ -run TestKeyDialogAddDelete -v`
Expected: FAIL — undefined `openKeyDialog`/`keyDialogAdd`/`keyDialogDelete`.

- [ ] **Step 3: Write the implementation**

3a. In `internal/usereditor/fields.go`, add a field next to the Password action (a free slot in the layout — pick an unused `Row`, e.g. right column):

```go
		{
			Label: "WFC Keys", Type: ftAction, Col: 50, Row: 12, Width: 22,
			Get: func(u *user.User) string {
				keys, _ := u.ListPublicKeys()
				return fmt.Sprintf("(%d)", len(keys))
			},
		},
```

> Ensure `fmt` is imported in fields.go (add if missing). Choose a `Col/Row` that doesn't overlap an existing field — read the current layout and pick a free cell.

3b. In `internal/usereditor/model.go`:
- Add modes to the `editorMode` const block: `modeKeyList` and `modeKeyAdd`.
- In the `ftAction` dispatch (~line 414), discriminate by label as shown above.
- Add the helper methods + Update handling. The helpers (testable, no terminal):

```go
// openKeyDialog enters the WFC key-list view for the user being edited.
func (m *model) openKeyDialog() {
	m.mode = modeKeyList
	m.keySelected = 0
}

// keyDialogAdd validates and adds a key to the edited user; returns a
// user-facing error to display in the dialog (nil on success).
func (m *model) keyDialogAdd(line string) error {
	u := m.editingUser() // returns the *user.User currently being edited
	_, err := u.AddPublicKey(line)
	return err
}

// keyDialogDelete removes a key (by fingerprint or 1-based index) from the
// edited user.
func (m *model) keyDialogDelete(ref string) error {
	u := m.editingUser()
	_, err := u.RemovePublicKey(ref)
	return err
}
```

> `m.editingUser()` and `m.keySelected int` are the two pieces of model state you wire to the real model. Read `model.go` to find how the edit target is stored and add `editingUser()` returning that `*user.User`, plus a `keySelected int` field and a `keyAddInput string` (or reuse the existing shared text input used by `modePasswordEntry`). Add `keyDialogErr string` to hold the last add/delete error for display.
- Wire `Update` so that, in `modeKeyList`: `↑/↓` adjust `keySelected`; `a`/`A` → `m.mode = modeKeyAdd` (clear input); `d`/`D`/Delete → build the index ref `strconv.Itoa(keySelected+1)` and call `keyDialogDelete`, storing any error in `keyDialogErr`; `Esc` → `m.mode = modeEdit`. In `modeKeyAdd`: capture text into the input; `Enter` → `keyDialogAdd(input)`, on success clear input + `m.mode = modeKeyList`, on error set `keyDialogErr`; `Esc` → back to `modeKeyList`. Mirror exactly how `modePasswordEntry` reads the shared text input and how other modes adjust a selection index.

3c. Render the dialog. Add an overlay renderer (in `view_edit.go` or `dialogs.go`) mirroring `overlayPasswordDialog`: for `modeKeyList`, draw a bordered box listing `i. <Type>  <Fingerprint>  <Comment>` with the selected row highlighted, a footer `[A]dd  [D]elete  [Esc] back`, and if `unparseable > 0` a warning line; for `modeKeyAdd`, draw a single-line input box titled "Paste WFC public key" with `keyDialogErr` shown beneath when non-empty. Hook these into the same place the main view chooses an overlay by `m.mode`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd "<worktree>" && gofmt -w internal/usereditor/ && go vet ./internal/usereditor/ && go test ./internal/usereditor/ -v && go build ./cmd/ue/`
Expected: PASS (new + existing usereditor tests) and clean `ue` build.

- [ ] **Step 5: Commit**

```bash
git -C "<worktree>" add internal/usereditor/
git -C "<worktree>" commit -m "feat(ue): WFC Keys field and add/list/delete dialog"
```

---

### Task 4: Docs caveat + full verification

**Files:**
- Modify: `docs/sysop/how-to-guides/wfc-console.md` (add the management + restart caveat)

**Interfaces:** none.

- [ ] **Step 1: Add the docs note**

In `docs/sysop/how-to-guides/wfc-console.md`, under the "Enabling access for a sysop" section, replace the manual-JSON guidance lead-in with a note that keys can now be managed via tools, and add the restart caveat. Add this paragraph after the existing `publicKeys` JSON example:

```markdown
You can also manage keys without editing JSON:

- **In `ue`** — open a user, activate the **WFC Keys** field, then `[A]dd` /
  `[D]elete` keys (shown by fingerprint).
- **From the CLI** — `helper users addkey <handle> <keyfile|->`,
  `helper users listkeys <handle>`, `helper users delkey <handle> <fingerprint|index>`.

> **Restart note:** `ue` and `helper` edit `users.json` directly; the running
> BBS loads users at startup and does not hot-reload that file. After adding or
> removing a key while the BBS is running, **restart the BBS** for it to take
> effect.
```

- [ ] **Step 2: Commit the docs**

```bash
git -C "<worktree>" add docs/sysop/how-to-guides/wfc-console.md
git -C "<worktree>" commit -m "docs(wfc): document ue/helper key management and restart caveat"
```

- [ ] **Step 3: Full verification**

Run:
```bash
cd "<worktree>"
gofmt -l internal/user/ cmd/helper/ internal/usereditor/   # expect no output
go vet ./internal/user/ ./cmd/helper/ ./internal/usereditor/
go test ./...
go build ./cmd/helper/ ./cmd/ue/ ./cmd/vision3/ ./cmd/wfc/
```
Expected: `gofmt -l` prints nothing for these dirs; `go vet` clean; `go test ./...` exits 0; all four binaries build.

- [ ] **Step 4: Smoke-test the CLI surface**

Run: `cd "<worktree>" && go run ./cmd/helper users help`
Expected: the `users` help lists `ADDKEY`, `LISTKEYS`, and `DELKEY`.

---

## Self-Review

**Spec coverage:**
- §3.1 shared core (Normalize + Add/Remove/List, dedup by wire bytes, fingerprints) → Task 1.
- §3.3 `helper users addkey/listkeys/delkey` (+ help text) → Task 2.
- §3.2 `ue` WFC Keys action field + dialog (fingerprint display, add/delete, label discriminator) → Task 3.
- §3.4 reuse atomic saves (`UserMgr.UpdateUser` for helper, `usereditor.SaveUsers` for ue) → Tasks 2 & 3.
- §4 next-restart caveat documented → Task 4.
- §5 error handling (malformed/dupe/unknown handle/ambiguous/unparseable) → Tasks 1–2 code + tests.
- §6 testing (core table tests, helper temp-file tests, ue dialog logic test) → each task; §7 acceptance (no manual JSON; same lib as auth; builds; `go test ./...`) → Task 4.

**Placeholder scan:** No `TODO`/`TBD` in code steps. The "read X and mirror it" notes in Tasks 2–3 name the exact existing functions to copy (`cmdUsersList` dispatch, `modePasswordEntry`/`overlayPasswordDialog`) with concrete code around them — they are integration anchors against real symbols, not deferred work. Task 3 explicitly flags the two model-state names (`editingUser()`, `keySelected`) the implementer binds to the real model, because the exact edit-target field name lives in unread model code.

**Type consistency:** `PublicKeyInfo{Type,Comment,Fingerprint}`, `NormalizeAuthorizedKey`, and `(*User).AddPublicKey/RemovePublicKey/ListPublicKeys` are defined in Task 1 and consumed with identical signatures in Tasks 2–3. `ref` semantics (fingerprint | prefix | 1-based index) are consistent between `RemovePublicKey`, `helper delkey`, and the `ue` delete handler.

## Execution Handoff

Two execution options:
1. **Subagent-Driven (recommended)** — fresh subagent per task, two-stage review between tasks.
2. **Inline Execution** — execute tasks here with checkpoints.

Which approach?
