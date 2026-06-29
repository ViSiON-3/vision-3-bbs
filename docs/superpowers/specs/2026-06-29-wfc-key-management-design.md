# WFC Key Management (ue + helper) — Design

**Date:** 2026-06-29
**Branch:** `feature/wfc`
**Status:** Approved design, pending implementation plan
**Builds on:** `docs/superpowers/specs/2026-06-29-wfc-sysop-console-design.md`

## 1. Purpose

WFC access requires a user to (a) have access level ≥ CoSysOp and (b) have an
SSH public key registered in their `User.PublicKeys`. Today the only way to
register a key is hand-editing `data/users/users.json`. This makes onboarding —
the sysop's own first key, and adding a co-sysop — error-prone and unfriendly.

This design adds **server-side key management** so a sysop can add/list/remove a
user's WFC public keys without editing JSON: a key-manager dialog in the `ue`
TUI, and a `helper users` CLI command group. Both share one validation/dedup
core so a key is handled identically everywhere.

Out of scope (deliberately, YAGNI): self-service in-BBS enrollment, setup-time
key generation, and a client-side `wfc keygen` helper. All three could later be
built on the same shared core without changing it.

## 2. Background (verified facts)

- `User.PublicKeys []string` (`json:"publicKeys,omitempty"`) holds OpenSSH
  `authorized_keys` lines (`internal/user/user.go`).
- `(*UserMgr) FindByAuthorizedKey(marshaled []byte) (*User, bool)` matches a
  presented key by `ssh.PublicKey.Marshal()` bytes, parsing each stored line
  with `ssh.ParseAuthorizedKey` (`internal/user/manager.go`). The new code uses
  the **same** parsing library so stored keys are guaranteed loadable by auth.
- `ue` does **not** use `UserMgr`; it loads/saves the user slice itself via
  `internal/usereditor` (`SaveUsers` does an atomic temp-file + rename). Its
  edit fields are typed: `ftString`, `ftInteger`, `ftYesNo`, `ftAction`. The
  **Password** field is an `ftAction` that opens a dialog — the model this
  feature follows.
- `helper` has a `users` command group (`helper users purge|list`) operating on
  the user store with a `--data DIR` flag (`cmd/helper/main.go`).
- **No hot-reload of users.json:** the daemon loads users at startup;
  `config_watcher` watches `configs/`, not `data/users/`. There is no
  `UserMgr.Reload`. So edits made by the external `ue`/`helper` processes take
  effect on the **next daemon start** — identical to how `ue` edits to any other
  field (e.g. access level) behave today.

## 3. Architecture

Three layers, each with one responsibility:

```
internal/user/pubkey ops  (shared core: validate, fingerprint, add/remove/list on a *User)
        ▲                         ▲
        │                         │
internal/usereditor          cmd/helper (users group)
  (WFC Keys dialog)            (addkey/listkeys/delkey)
```

### 3.1 Shared core — `internal/user`

Pure, persistence-free primitives so both consumers behave identically. They
operate on an in-memory `*User`; **saving is the caller's job** (ue saves via
`usereditor.SaveUsers`; helper saves via `UserMgr`). This keeps the core free of
file I/O and trivially testable.

```go
// PublicKeyInfo is a display-friendly summary of one registered key.
type PublicKeyInfo struct {
    Type        string // e.g. "ssh-ed25519"
    Comment     string // trailing comment on the authorized_keys line, if any
    Fingerprint string // ssh.FingerprintSHA256(pub), e.g. "SHA256:abc…"
}

// NormalizeAuthorizedKey parses one authorized_keys line with
// ssh.ParseAuthorizedKey, returning the canonical re-marshaled line, its
// SHA256 fingerprint, and the parsed PublicKeyInfo. Rejects malformed input.
func NormalizeAuthorizedKey(line string) (normalized string, info PublicKeyInfo, err error)

// AddPublicKey validates `line`, dedupes against u.PublicKeys (by marshaled key
// bytes, ignoring comment differences), and appends it. Returns the new key's
// fingerprint. err is non-nil on malformed input or an exact duplicate.
func (u *User) AddPublicKey(line string) (PublicKeyInfo, error)

// RemovePublicKey removes the key identified by `ref`, which may be a full or
// partial SHA256 fingerprint or a 1-based index as shown by ListPublicKeys.
// Returns the removed key's info; err if no match / ambiguous match.
func (u *User) RemovePublicKey(ref string) (PublicKeyInfo, error)

// ListPublicKeys returns display summaries for u.PublicKeys, skipping (and
// reporting count of) any unparseable stored lines so a corrupt entry can be
// surfaced rather than hidden.
func (u *User) ListPublicKeys() (keys []PublicKeyInfo, unparseable int)
```

Dedup rule: two lines are "the same key" when their parsed `Marshal()` bytes are
equal, regardless of comment — this matches how `FindByAuthorizedKey` decides
auth, so the UI's "duplicate" notion can never disagree with the auth layer.

### 3.2 `ue` — WFC Keys field + dialog

- Add a field **`WFC Keys`** of type `ftAction` to the user edit screen
  (`internal/usereditor/fields.go`), label showing the count, e.g. `WFC Keys (2)`.
- Activating it opens a new dialog in `internal/usereditor/dialogs.go` that:
  - Lists each key as `<Type>  <Fingerprint>  <Comment>` (never the full blob).
  - `[A]dd` → prompts for a public-key line; on Enter, calls
    `u.AddPublicKey(line)`. On error (malformed/dupe) shows the message and stays
    open. On success the list updates.
  - `[D]elete` → removes the highlighted key via `u.RemovePublicKey` (by the
    index/fingerprint it already holds).
  - `[Esc]` → close; the edited `*User` is already mutated in memory, persisted
    by `ue`'s normal save path (`SaveUsers`, atomic).
  - If `ListPublicKeys` reports `unparseable > 0`, show a one-line warning so a
    hand-corrupted entry is visible.
- Works on whatever user record is being edited → covers self and co-sysop.

### 3.3 `helper users` — CLI

Extend the existing group (`cmd/helper`). All resolve the user via the user
store (honoring `--data`), mutate the `*User`, and save:

- `helper users addkey <handle> <keyfile|->`
  Reads an OpenSSH public key from a file path or stdin (`-`), calls
  `AddPublicKey`, saves, prints the added fingerprint. Non-zero exit + clear
  message on parse error, duplicate, or unknown handle.
- `helper users listkeys <handle>`
  Prints `index  Type  Fingerprint  Comment` per key (and any unparseable count).
- `helper users delkey <handle> <fingerprint|index>`
  Calls `RemovePublicKey`, saves, prints the removed fingerprint.

Update `helper`'s help text (the `users` usage block) to list the three.

### 3.4 Persistence & concurrency

Both consumers reuse existing atomic-write paths (`usereditor.SaveUsers` /
`UserMgr` save = temp file + `os.Rename`), so a concurrent daemon never observes
a half-written file. No new locking is introduced.

## 4. Operational behavior (documented caveat)

A key added via `ue`/`helper` while the daemon is **running** is not visible to
auth until the **daemon restarts**, because the daemon does not hot-reload
`users.json`. This is consistent with all other external `ue` edits today. The
WFC how-to guide (`docs/sysop/how-to-guides/wfc-console.md`) gains a short note:
"After adding a key with `ue`/`helper` while the BBS is running, restart the BBS
for it to take effect."

## 5. Error handling

- Malformed key line → returned error names the problem (e.g. "not a valid
  OpenSSH public key"); UI/CLI shows it; nothing is saved.
- Duplicate key → explicit "key already registered" (no silent no-op).
- Unknown handle (helper) → non-zero exit, clear message.
- `delkey` ambiguous/no-match `ref` → error, nothing removed.
- Unparseable pre-existing stored line → surfaced, never silently dropped during
  add/remove of other keys.

## 6. Testing

- **Core (`internal/user`):** table-driven `NormalizeAuthorizedKey` (valid
  ed25519 + rsa, malformed, empty, comment handling); `AddPublicKey` dedup
  (same key, different comment → duplicate); `RemovePublicKey` by fingerprint,
  partial fingerprint, and index, plus no-match; `ListPublicKeys` with a
  deliberately corrupt entry → `unparseable` count.
- **helper:** `addkey`/`listkeys`/`delkey` against a temp `users.json`
  (file + stdin input); unknown handle; bad key.
- **ue:** dialog add/delete logic test driven by synthetic key lines (no real
  terminal needed), asserting the `*User.PublicKeys` mutation and error display.
- Run `go test ./...`, `gofmt`, `go vet` on changed packages.

## 7. Acceptance criteria

- A sysop can add, list, and remove a user's WFC public keys entirely through
  `ue` (dialog) and through `helper users addkey|listkeys|delkey` — no manual
  JSON editing.
- Keys are validated with the same library used by auth; an added key is
  guaranteed parseable by `FindByAuthorizedKey`.
- Duplicates are rejected; malformed input is rejected with a clear message;
  listings show fingerprints + comments, never full key blobs.
- The whole daemon still builds; `go test ./...` passes.
- The WFC guide documents the next-restart caveat.

## 8. Decisions recorded

1. Listings show **SHA256 fingerprint + comment**, not full keys (approved).
2. `helper` verbs: **`addkey` / `listkeys` / `delkey`** under `helper users`
   (approved).
3. Next-restart visibility for external edits is **accepted and documented**,
   not engineered around (approved). Live pickup would require in-BBS
   self-enrollment, which is out of scope here.
