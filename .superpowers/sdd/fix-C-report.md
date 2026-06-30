# Fix-C Report

Branch: feature/wfc  
Date: 2026-06-29

---

## C1 — Wrong key deleted by index (Major)

**Root cause:** `RemovePublicKey`'s index path used `u.PublicKeys[idx-1]` (raw slice
index) and bounds-checked against `len(u.PublicKeys)`, but `ListPublicKeys` skips
unparseable entries. Any corrupt stored line caused a mismatch between what the
user saw and what was actually removed.

**Fix (`internal/user/pubkey_ops.go`):** Replaced the raw-slice index path with an
iterator that counts only parseable (visible) keys. On the `idx`-th parseable key
it removes that raw slice element and returns its info. Error message uses the
visible count: `"key index N out of range (have <visibleCount>)"`.

**Test added (`internal/user/pubkey_ops_test.go` — `TestRemovePublicKeyVisibleIndex`):**
User with `[corrupt, goodA, goodB]`; asserts index 1 removes goodA (not the corrupt
entry), index 1 again removes goodB, index 1 with no visible keys errors correctly.

**Design spec check:** `docs/superpowers/specs/2026-06-29-wfc-key-management-design.md`
line 83 already says "1-based index as shown by ListPublicKeys" — no change needed.

---

## C2 — Keys past the 8th unreachable in dialog (Major)

**Root cause:** `overlayKeyListDialog` hard-capped the rendered slice at `[:8]` but
`keySelected` could move to any index in the full list, producing an off-screen
highlight and allowing delete to target an unseen entry.

**Fix (`internal/usereditor/pubkey_dialog.go` and `model.go`):**
- Added `keyScroll int` field to `Model` (reset to 0 in `openKeyDialog`).
- Replaced the hard `[:8]` truncation with a `maxVisible = 8` scroll window:
  `scroll` is derived from `m.keyScroll` clamped so `keySelected` is always within
  `[scroll, scroll+maxVisible)`. The rendered window is `keys[scroll:end]`.
  Row highlight compares `absIdx == m.keySelected` (absolute index, not window index).

**Test added (`internal/usereditor/pubkey_dialog_test.go` — `TestKeyListDialogScrollWindow`):**
Adds 10 distinct keys, sets `keySelected = 9`, renders the dialog, asserts the 10th
key's 47-char fingerprint prefix is present and the 1st key's is absent (scrolled off).

---

## C3 — UTF-8 truncation splits multi-byte runes (Minor)

**Root cause:** All truncation in `pubkey_dialog.go` used byte-slice syntax
(`s[:n]`) which can split a multi-byte UTF-8 rune in a key comment or error string,
producing garbled output.

**Fix (`internal/usereditor/pubkey_dialog.go`):**
- Added `truncateRunes(s string, n int) string` helper (converts to `[]rune`, slices,
  converts back) at the top of the file.
- Used `truncateRunes` at all three cut sites:
  - Comment truncation in the key list loop (previously `cmt[:avail]`).
  - Error text in `overlayKeyListDialog` (previously `errText[:innerW]`).
  - Error text in `overlayKeyAddDialog` (previously `errText[:innerW]`).
- Padding arithmetic uses `len([]rune(s))` (rune count) instead of `len(s)` (byte
  count) wherever the truncated string feeds into the space-fill calculation.

---

## Test & Build Summary

```
gofmt -w internal/user/ internal/usereditor/   → OK (no changes)
go vet ./internal/user/ ./internal/usereditor/  → No issues
go test ./internal/user/ ./internal/usereditor/ → 95 passed, 0 failed
go build ./cmd/ue/ ./cmd/helper/                → Success
```

New tests added: `TestRemovePublicKeyVisibleIndex` (user pkg), `TestKeyListDialogScrollWindow` (usereditor pkg).

---

## Concerns

None. The scroll offset is computed lazily at render time from `m.keyScroll`
(clamped to keep `keySelected` visible), so no additional keypress handler updates
are needed — the view always self-corrects. `padRight` still uses byte length for
fingerprint and type columns (those are ASCII-only SSH key metadata), so only the
comment and error-string paths required rune-aware handling.
