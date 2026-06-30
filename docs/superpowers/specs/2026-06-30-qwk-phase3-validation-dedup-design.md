# QWK Phase 3 — REP First-Block Validation and Import Deduplication

**Date:** 2026-06-30
**Status:** Approved design
**Branch:** `qwk-phase3-validation-dedup`
**Parent plan:** `docs-internal/plans/2026-06-29-qwk-rep-sync-mobile-design.md` (Phase 3)

---

## Problem

REP import (`internal/qwkservice`) is now conference-map aware (Phase 2) but
still has two safety gaps the parent plan calls out for Phase 3:

1. **No destination check.** `internal/qwk/reader.go` skips the REP `.MSG` first
   block entirely (`pos := BlockSize`). A `.REP` produced by an offline reader
   conventionally carries the destination BBS ID in that first block, so a
   packet meant for another BBS is currently imported anyway.

2. **No retry safety.** A mobile client (the eventual Phase 7/8 use case) that
   retries an upload — or a user who re-sends the same `.REP` — double-posts
   every reply. There is no record of what has already been imported.

## Goals

- Validate the REP first-block BBS ID and reject packets addressed to another
  BBS, with a clear error path.
- Deduplicate uploads so a retried/identical `.REP` does not double-post,
  safely under concurrent uploads from different sessions.
- Return a structured result that distinguishes posted / skipped / duplicate.

## Non-goals (later phases)

- `HEADERS.DAT` / Synchronet legacy tags (Phase 5).
- Reply/thread metadata, authored-date import (Phase 4).
- Any packet transport API or mobile client (Phases 7–8).
- Per-message dedup (this phase dedups whole packets).

## Decisions (confirmed)

| Decision | Choice |
| --- | --- |
| Dedup store backend | **SQLite** at `data/qwk_dedup.db` (`modernc.org/sqlite`, already a direct, cgo-free dependency used by `internal/v3net/dedup` and `internal/chat`). No new dependency for users. |
| BBS-ID validation strictness | **Lenient** — reject only when the first block contains an ID that does not match; accept an empty/absent ID. |
| Invalid-packet representation | Wrong-destination and structural problems are **error returns**, not an `ImportResult` field. The new result field is `Duplicate`. |
| Dedup timing | **Claim-before-post** — the fingerprint is atomically recorded before posting, so the dedup is authoritative under concurrency even if a packet ends up posting nothing. |
| Fingerprint input | SHA-256 of the `.MSG` payload bytes (stable across zip-container variation), not the whole `.REP` zip. |

---

## Design

### 1. First-block extraction — `internal/qwk/reader.go`

Add a packet-level reader that exposes the first-block BBS ID and the raw
payload alongside the messages:

```go
// REPPacket is a parsed REP upload.
type REPPacket struct {
    BBSID    string       // BBS ID from the first block ("" if blank/absent)
    Messages []REPMessage
    Payload  []byte       // raw .MSG bytes, for fingerprinting
}

func ReadREPPacket(r io.ReaderAt, size int64, bbsID string) (*REPPacket, error)
```

`ReadREPPacket` opens the zip and locates `<bbsID>.MSG` exactly as `ReadREP`
does today, then:

- `Payload` = the raw `.MSG` bytes.
- `BBSID` = `firstBlockID(Payload[:BlockSize])` — the leading
  whitespace-delimited token of the first 128-byte block, trimmed and
  uppercased (`""` if the block is all spaces / shorter than a block).
- `Messages` = the existing `parseREPMessages(Payload)` output.

`ReadREP` is retained as a thin wrapper (`return p.Messages, err`) so existing
callers and tests are unchanged.

`firstBlockID` extracts the first run of non-space bytes (capped at 8 chars, the
QWK BBS-ID length) and upper-cases it. It deliberately does not interpret the
rest of the block.

### 2. Writer parity — `internal/qwk/rep_writer.go`

`WriteREP` currently writes `"Produced by ViSiON/3 BBS"` into the spacer block.
Change it to write the upper-cased `bbsID` as the first token (space-padded to
the block) so ViSiON/3-generated REPs and test fixtures carry a valid
first-block ID and round-trip through validation. (Real offline readers populate
this field; this keeps our own packets realistic.)

### 3. Dedup store — `internal/qwkservice/dedup.go`

Mirrors `internal/v3net/dedup/dedup.go`.

```go
type repDedup struct{ db *sql.DB }

const repDedupSchema = `
CREATE TABLE IF NOT EXISTS rep_uploads (
    handle   TEXT NOT NULL,
    rep_hash TEXT NOT NULL,
    seen_at  DATETIME DEFAULT (datetime('now')),
    PRIMARY KEY (handle, rep_hash)
);`

func openREPDedup(path string) (*repDedup, error) // sql.Open, SetMaxOpenConns(1),
                                                  // WAL+busy_timeout pragmas, schema, prune
func (d *repDedup) Close() error
// RecordIfNew atomically records (handle, hash). Returns true if it was newly
// inserted, false if it was already present (a duplicate upload).
func (d *repDedup) RecordIfNew(handle, hash string) (bool, error)
```

- `RecordIfNew` runs a single `INSERT OR IGNORE … VALUES(?,?)` and returns
  `RowsAffected() == 1`. The atomic insert is the concurrency guard: two
  simultaneous identical uploads cannot both observe "new".
- `openREPDedup` opportunistically prunes on open:
  `DELETE FROM rep_uploads WHERE seen_at < datetime('now','-90 days')`.
- Concurrency relies on SQLite (WAL + `busy_timeout=5000`); no process-level
  mutex is added.

Fingerprint helper (in the service): `sha256` hex of `packet.Payload` via
`crypto/sha256` + `encoding/hex`.

### 4. Service changes — `internal/qwkservice`

`Service` gains `dedupPath string`, set by `New` to
`filepath.Join(dataPath, "qwk_dedup.db")`. `New`'s signature is unchanged.

`ImportResult` gains `Duplicate int`.

A sentinel error:

```go
var ErrWrongBBS = errors.New("qwk: REP packet addressed to another BBS")
```

`ImportREP` (revised order):

```go
func (s *Service) ImportREP(data []byte, opts ImportOptions) (*ImportResult, error) {
    packet, err := qwk.ReadREPPacket(bytes.NewReader(data), int64(len(data)), s.bbsID)
    if err != nil {
        return nil, err // structural/parse error (existing behaviour)
    }
    // Lenient destination check.
    if packet.BBSID != "" && !strings.EqualFold(packet.BBSID, s.bbsID) {
        return nil, fmt.Errorf("%w: packet for %q, this is %q", ErrWrongBBS, packet.BBSID, s.bbsID)
    }
    // Atomic dedup claim (before posting).
    fp := fingerprint(packet.Payload)
    dedup, err := openREPDedup(s.dedupPath)
    if err != nil {
        return nil, err
    }
    defer dedup.Close()
    isNew, err := dedup.RecordIfNew(opts.Handle, fp)
    if err != nil {
        return nil, err
    }
    if !isNew {
        return &ImportResult{Duplicate: len(packet.Messages)}, nil
    }
    // ... existing conference-map resolution + posting loop, unchanged ...
}
```

Because per-message post failures are non-fatal (counted as `Skipped`, not
returned as errors), the only error paths after the claim are infrastructure
failures; the claim-before-post ordering is what guarantees concurrency safety,
and a claimed packet is considered handled.

### 5. Menu — `internal/menu/qwk_handler.go`

`runQWKUpload` adds two branches around the existing import call:

- `errors.Is(err, qwkservice.ErrWrongBBS)` → display
  `"This REP packet is addressed to another BBS."` (not the generic parse
  error).
- `importRes.Duplicate > 0` → display
  `"This packet was already uploaded — nothing posted."` and skip the
  user-stats / `TotalQWKAdded` path.

No new config; validation and dedup are always on.

---

## Testing

**Codec (`internal/qwk`):**
- `firstBlockID`: extracts a token, caps at 8 chars, empty block → `""`.
- `ReadREPPacket`: returns the first-block BBS ID, the messages, and a non-empty
  payload; `ReadREP` wrapper still returns just the messages.
- `WriteREP` round-trip: a packet written with bbsID "VISION3" reads back with
  `BBSID == "VISION3"`.

**Service (`internal/qwkservice`):**
- Wrong-BBS: a packet whose first block says "OTHERBBS" returns `ErrWrongBBS`,
  nothing posted.
- Matching / empty first block: imports normally.
- Duplicate: importing the same payload twice — first posts, second returns
  `Duplicate == len(messages)` with `Posted == 0` and no second post in the
  fake store.
- Per-handle isolation: the same payload under a different handle is not a
  duplicate.
- Persistence: dedup survives across `Service` instances pointed at the same
  `dataPath` (reopen the DB, re-import → duplicate).

**Menu:** existing tests remain; the two new display branches are thin and
covered indirectly.

---

## Risks / notes

- **Zip-vs-payload hashing:** hashing the `.MSG` payload (not the zip) means a
  client that re-zips identical replies still dedups correctly. A client that
  changes the reply content produces a new fingerprint, as intended.
- **Claimed-but-unposted:** if every message in a fresh packet is skipped (e.g.
  all ACS-denied), the packet is still claimed; a retry is reported as a
  duplicate. This is acceptable — re-uploading would hit the same denials.
- **Lenient validation:** readers that leave the first block blank are accepted;
  only a present, mismatching ID is rejected.
- **DB growth:** bounded by the 90-day prune-on-open; the table is tiny
  (one row per upload).

## Acceptance criteria (parent plan, Phase 3)

- Duplicate REP uploads do not double-post messages.
- Wrong-destination REP packets are rejected.
- Tests cover valid, invalid (wrong-BBS), and duplicate upload scenarios.
- Import result distinguishes posted / skipped / duplicate.
