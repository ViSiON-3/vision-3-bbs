# QWK Explicit BBS ID

**Date:** 2026-06-30
**Status:** Approved design
**Branch:** `qwk-explicit-bbs-id`

---

## Problem

The QWK BBS ID is derived at runtime from the board name:
`qwkBBSID(e.ServerCfg.BoardName)` in `internal/menu/qwk_handler.go` (alphanumeric,
≤8 chars, uppercase, `"BBS"` fallback). After QWK Phases 2–3 this ID is no longer
cosmetic — it is a **stable identity**:

- the `.QWK` / `.REP` filename (`VISION3.QWK`, `VISION3.MSG`),
- the destination ID written into REP packets and **validated on import**
  (`ErrWrongBBS`),
- the BBS identity handed to `qwkservice.New(...)`.

Deriving that from the mutable `BoardName` is the same anti-pattern Phase 2
eliminated for conference numbers: a stable exported contract must not depend on
an incidental, mutable value. Renaming the board silently changes the QWK ID,
breaking offline readers' saved packets and changing the packet filename match.

## Goal

Let the sysop set an explicit, stable QWK ID (config + config TUI), independent
of the board name, while keeping the derived behavior as a backward-compatible
default.

## Non-goals

- No change to how packets are built/validated beyond *which* ID string is used.
- No migration of existing `config.json` files (a new optional field; absent ⇒
  current behavior).
- No multi-ID / per-conference ID support.

## Decisions (confirmed)

| Decision | Choice |
| --- | --- |
| Config field | `ServerConfig.QWKID string` (`json:"qwkID,omitempty"`), default empty |
| Empty semantics | Empty ⇒ derive from `BoardName` (current behavior, back-compat) |
| Normalizer location | `config.NormalizeQWKID` (both menu and configeditor consume it; configeditor already imports config — no cycle) |
| Config TUI storage | Normalize-on-Set (canonical storage; WYSIWYG) |

---

## Design

### 1. Config field — `internal/config/config.go`

Add to `ServerConfig` (near `BoardName`, line ~840):

```go
QWKID string `json:"qwkID,omitempty"`
```

Default is the zero value `""` (no entry needed in `defaultConfig`). `Load`/`Save`
already round-trip arbitrary `ServerConfig` fields, so no loader change is needed.

### 2. Normalizer — `internal/config` (new function)

```go
// NormalizeQWKID reduces a string to a valid QWK BBS ID: ASCII letters and
// digits only, upper-cased, at most 8 characters. Returns "" if nothing valid
// remains (the caller decides the fallback).
func NormalizeQWKID(s string) string
```

Implementation mirrors the existing `qwkBBSID` body (keep only `A–Z`, `a–z`,
`0–9`; stop at 8; upper-case) **without** the `"BBS"` fallback — that belongs to
the resolver. Lives in a small file, e.g. `internal/config/qwkid.go`.

### 3. Resolver + `qwkBBSID` — `internal/menu/qwk_handler.go`

Add:

```go
// resolveQWKID returns the BBS's QWK packet ID: the explicitly configured ID if
// set, otherwise one derived from the board name, otherwise "BBS".
func resolveQWKID(cfg config.ServerConfig) string {
	if id := config.NormalizeQWKID(cfg.QWKID); id != "" {
		return id
	}
	if id := config.NormalizeQWKID(cfg.BoardName); id != "" {
		return id
	}
	return "BBS"
}
```

Reimplement the existing `qwkBBSID` on top of the shared normalizer so its
current contract and tests are preserved:

```go
func qwkBBSID(boardName string) string {
	if id := config.NormalizeQWKID(boardName); id != "" {
		return id
	}
	return "BBS"
}
```

Replace both `bbsID := qwkBBSID(e.ServerCfg.BoardName)` call sites
(`runQWKDownload` line ~59, `runQWKUpload` line ~183) with
`bbsID := resolveQWKID(e.ServerCfg)`.

`e.ServerCfg` is a `config.ServerConfig` value on `MenuExecutor`
(`internal/menu/executor.go:224`).

### 4. Config TUI field — `internal/configeditor/fields_system.go`

The editor uses `fieldDef` descriptors (`internal/configeditor/fields.go:26`)
with `Label/Help/Type/Col/Row/Width/Get/Set`. Add a row next to "Board Name" on
the registration screen (`sysFieldsRegistration`):

```go
{
	Label: "QWK ID", Help: "Stable QWK packet ID (max 8, A-Z/0-9); blank = derive from Board Name",
	Type: ftString, Col: 3, Row: <next>, Width: 8,
	Get: func() string { return cfg.QWKID },
	Set: func(val string) error { cfg.QWKID = config.NormalizeQWKID(val); return nil },
}
```

Normalize-on-Set means the stored and displayed value is always the canonical ID
that will actually be used — no divergence between what the sysop sees and what
ships in packets. `Width: 8` also bounds the input length (`CharLimit`,
`update_syscfg.go:187`). Place the row so it does not overlap an existing
`Row`/`Col`; renumber neighboring rows if needed.

### 5. Documentation — `docs/sysop/messages/qwk.md`

Add a short note: the QWK ID can be set explicitly in the config editor; it is a
**stable identity** (like the conference map — set once, early). Blank derives it
from the board name. Changing it later re-keys packet identity (filenames + REP
first-block validation), so offline readers will see it as a different BBS.

---

## Testing

**`internal/config`:**
- `NormalizeQWKID` table test: plain id; lowercase → upper; spaces/symbols
  stripped; >8 truncated; all-invalid → `""`; empty → `""`.
- Load/save round-trip preserves `QWKID`; absent `qwkID` in JSON → `""`.

**`internal/menu`:**
- `resolveQWKID`: explicit `QWKID` wins (and is normalized); blank `QWKID` falls
  back to board-name derivation; both blank/invalid → `"BBS"`.
- Existing `TestQwkBBSID` continues to pass unchanged.

**`internal/configeditor`:** if a focused test fits the existing patterns, assert
the QWK ID field's `Set` normalizes; otherwise the field is covered by build +
the resolver/normalizer unit tests (the editor wiring is declarative).

---

## Risks / notes

- **Identity change on edit:** setting a QWK ID different from the previously
  derived one changes packet filenames and the REP first-block ID; documented as
  a deliberate, set-once action.
- **Hand-edited config:** the resolver re-normalizes `cfg.QWKID` at consume time,
  so a malformed hand-edited value still yields a valid ID.
- **Back-compat:** absent/empty `qwkID` reproduces today's exact behavior.

## Acceptance criteria

- A sysop can set an explicit QWK ID in the config editor; it is used for QWK
  download/upload filenames and REP validation.
- With no QWK ID set, behavior is identical to today (derived from board name).
- Renaming the board no longer changes the QWK ID when an explicit ID is set.
- `config.NormalizeQWKID`, `resolveQWKID`, and config round-trip are tested;
  existing `qwkBBSID` tests still pass.
