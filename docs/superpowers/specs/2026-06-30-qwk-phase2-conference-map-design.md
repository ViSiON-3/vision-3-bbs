# QWK Phase 2 — Stable Conference Mapping and Private-Mail Routing

**Date:** 2026-06-30
**Status:** Approved design
**Branch:** `qwk-phase2-conference-map`
**Parent plan:** `docs-internal/plans/2026-06-29-qwk-rep-sync-mobile-design.md` (Phase 2)

---

## Problem

Phase 1 extracted a reusable `internal/qwkservice` over the `internal/qwk` codec.
It still has two correctness gaps the parent plan calls out for Phase 2:

1. **Unstable conference numbering.** Export uses the raw, local `MessageArea.ID`
   as the QWK conference number (`internal/qwkservice/service.go`), and import
   looks an area up by that same number (`GetAreaByID(msg.Conference)`). Local
   IDs are an internal detail; using them as the offline-reader contract means a
   re-created or re-numbered area silently breaks saved packets and replies.

2. **Private mail is not first-class.**
   - **Import:** every REP reply is posted through the public `AddMessage(...)`
     path, even replies that belong in private mail.
   - **Export:** when the export falls back to "all areas" (or the user tags the
     private-mail area), the packet includes **every** message in the
     `PRIVMAIL` base — i.e. all users' private mail. Only `PERSONAL.NDX`
     filters by recipient, so the message bodies still ship. This is a privacy
     leak.

## Goals

- Give exported conferences a **stable, persisted number** that does not depend
  on mutable local area IDs.
- Reserve QWK conference **0** for the private-mail/email conference.
- Route imported private replies to `AddPrivateMessage(...)`; keep public
  replies on `AddMessage(...)`.
- Fix private-mail export so a packet contains only the requesting user's own
  private mail.

## Non-goals (stay in later phases)

- `HEADERS.DAT` read/write and Synchronet legacy tags (Phase 5).
- Reply/thread metadata preservation, authored-date import (Phase 4).
- REP first-block BBS-ID validation and dedup (Phase 3).
- Any packet transport API (Phase 7).

---

## Decisions (confirmed)

| Decision | Choice |
| --- | --- |
| Map file location | `data/qwk_conferences.json` (auto-generated, self-maintaining derived state) |
| Private-mail identification | The area whose `Tag == "PRIVMAIL"`, reusing the existing convention (`runSendPrivateMail`, `executor_messages.go`) |
| Private-mail export filtering | Yes — include only the requesting user's own private mail |

---

## Design

### 1. Conference map — `internal/qwkservice/conference_map.go`

```go
type ConferenceKind string

const (
    KindPublic      ConferenceKind = "public"
    KindPrivateMail ConferenceKind = "private_mail"
)

// PrivateMailTag is the message-area tag treated as the private-mail/email
// conference (mapped to QWK conference 0).
const PrivateMailTag = "PRIVMAIL"

type ConferenceMapEntry struct {
    QWKNumber int            `json:"qwk_number"`
    AreaTag   string         `json:"area_tag"`
    Kind      ConferenceKind `json:"kind"`
}

type ConferenceMap struct {
    entries []ConferenceMapEntry
    byTag   map[string]ConferenceMapEntry
    byNum   map[int]ConferenceMapEntry
}
```

Behaviour:

- `LoadConferenceMap(path string) (*ConferenceMap, error)` — a missing file
  yields an empty map (not an error); a malformed file is an error.
- `Save(path string) error` — atomic write (temp file + `os.Rename`), mirroring
  `MessageManager.SaveAreas`.
- `Sync(areas []*message.MessageArea) (changed bool)` — ensure every current
  area has an entry, **never renumbering an existing entry**:
  - The `PRIVMAIL` area → `QWKNumber 0`, `KindPrivateMail`.
  - Any other area already in the map → keep its number/kind.
  - A new public area → `QWKNumber = area.ID`, unless that value is `0` or
    already taken, in which case the next free positive integer.
  - Areas that have disappeared keep their entries (preserves the contract for
    already-distributed packets).
  - Returns whether anything changed, so the caller persists only on change.
- Lookups: `NumberForTag(tag) (int, bool)`, `EntryForNumber(num) (ConferenceMapEntry, bool)`.

The map type depends only on `internal/message` (for `MessageArea`) and stdlib;
it has no codec or terminal dependencies and is unit-tested in isolation.

### 2. Service wiring

`qwkservice.New` gains a `dataPath` argument; the conference-map file path is
`filepath.Join(dataPath, "qwk_conferences.json")`. Export and import each
**load → Sync → (save if changed)** the map at call time. Loading per call keeps
the service stateless and avoids cross-session cache staleness; these are
infrequent, user-initiated operations.

`MessageManager` gains `func (mm *MessageManager) DataPath() string` so the menu
can pass `e.MessageMgr.DataPath()` into `New`.

The `MessageStore` interface gains:

```go
AddPrivateMessage(areaID int, from, to, subject, body, replyToMsgID string) (int, error)
```

(`*message.MessageManager` already implements it; the test fake is extended.)

### 3. Export — `BuildPacket`

- Load + Sync + persist the map.
- For each exported area, resolve `qwkNumber := map.NumberForTag(area.Tag)`
  and use it for `pw.AddConference(qwkNumber, area.Name)` and
  `PacketMessage.Conference`.
- When the area is the private-mail conference, include a message only if
  `msg.IsPrivate && (equalFold(msg.To, handle) || equalFold(msg.From, handle))`.
  Public areas are unfiltered (unchanged from Phase 1).
- Last-read handling is unchanged (still keyed by `area.ID`).

### 4. Import — `ImportREP`

- Load the map.
- Resolve each `msg.Conference`:
  1. `entry, ok := map.EntryForNumber(msg.Conference)` → area by `entry.AreaTag`.
  2. Fallback if unmapped: `GetAreaByID(msg.Conference)` (back-compat with
     packets generated before the map existed; their public numbers already
     equal `area.ID`). A fallback hit is treated as `KindPublic`.
  3. Unknown → skip (counted), as today.
- Route by kind:
  - `KindPrivateMail` → `AddPrivateMessage(area.ID, handle, msg.To, subject, body, "")`
  - otherwise → `AddMessage(...)` (current behaviour)
- `Authorize` / `Notify` hooks and signature handling are unchanged.

### 5. Documentation

Update `docs/sysop/messages/qwk.md`:

- The stable conference map and the `data/qwk_conferences.json` file (auto-created,
  not hand-edited, numbers frozen once assigned).
- Conference `0` is the private-mail/email conference.
- Private-mail export only includes the downloading user's own mail.
- Imported replies to conference `0` post as private mail.

---

## Testing

**Conference map (`conference_map_test.go`):**
- Missing file loads as empty.
- `Sync` assigns `PRIVMAIL → 0` and public areas → their `area.ID`.
- `0`-collision: a public area with ID 0 is bumped to the next free positive int.
- Persistence round-trip (`Save` then `Load` equal).
- Stability: reload after reordering / renaming areas keeps existing numbers;
  only genuinely new areas get new numbers.
- New area appends an entry and reports `changed == true`.

**Export:**
- Private-mail area exports under conference `0`.
- Private-mail packing includes only `To==handle` / `From==handle` messages and
  excludes other users' private mail.
- Public areas use their stable numbers.

**Import:**
- A reply to conference `0` routes to `AddPrivateMessage` (fake records it
  separately from public posts).
- A reply to a public conference routes to `AddMessage`.
- An unmapped conference number falls back to `GetAreaByID` and posts public.

All new test files stay under the 300-line guideline (split if needed, reusing
the shared `fakeStore`).

---

## Risks / notes

- **Existing offline packets:** public conference numbers default to `area.ID`,
  so already-distributed public-area numbering is preserved. The private-mail
  area moves to `0` (from its former `area.ID`), which is the intended change.
- **Shared last-read:** QWK export and the interactive reader share the
  per-area last-read pointer (already true in Phase 1); private-mail export
  advancing it is consistent with existing public-area behaviour.
- **`PRIVMAIL` absence:** if no area is tagged `PRIVMAIL`, there is simply no
  conference `0` and no private routing occurs; import falls back to public.

## Acceptance criteria (from parent plan, Phase 2)

- An uploaded private reply no longer goes through the public post path.
- Exported conference numbers remain stable across restarts.
- Tests cover conference-map persistence and private-mail routing.
- Plus: private-mail export contains only the requesting user's own mail.
