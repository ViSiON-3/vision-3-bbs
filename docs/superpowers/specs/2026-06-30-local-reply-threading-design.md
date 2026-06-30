# Local Message Reply Threading

**Date:** 2026-06-30
**Status:** Approved design
**Branch:** `msg-reply-threading`

---

## Problem

Local message areas (the QWK use case: GENERAL, PRIVMAIL) carry no reply-chain
linkage. JAM threads on FTN `MSGID`↔`ReplyID` strings via `jam.Link()`
(`internal/jam/link.go:14`), but local messages get no MSGID (those are generated
only for echomail/netmail). The reader's reply path sets `ReplyID = parent.MsgID`
(`internal/menu/message_reader_nav.go:74`), which is `""` for a local parent — so
no link is created, `Header.ReplyTo` stays `0`, and the reader can only group by
normalized subject ("Re:" stripping).

The display side is already built: `buildMsgSubstitutions`
(`internal/menu/message_reader_subst.go:127`) reads `msg.ReplyToNum` and exposes
a "Reply to: #N" value as template substitution key `P`, and `GetMessage`
populates `ReplyToNum` from `Header.ReplyTo` (`internal/message/manager.go:763`).
The only gap is the **write side**: nothing ever sets `Header.ReplyTo` for local
messages.

This feature makes local replies record and display their parent message number.
It also unblocks QWK Phase 4 (which had no reply data to preserve).

## Goal

When a user replies to a message, record the parent message number
(`Header.ReplyTo`) so the reader shows "Reply to: #N" for local areas (and
echomail replies link immediately rather than only after `Link()`).

## Non-goals (deferred)

- Thread navigation commands (jump-to-parent, next-in-chain).
- QWK export/import of the reply reference (QWK Phase 4 — now unblocked).
- Changing the private-flag behavior of replies to private mail.
- Backfilling reply pointers for existing messages.

## Decisions (confirmed)

| Decision | Choice |
| --- | --- |
| Display | Display-only (header "Reply to: #N"); no new navigation keys |
| Area scope | Public + private mail (both reached via the single shared `handleReply`) |
| Reply API | New `AddReply(...)` carrying both `replyToMsgID` (echomail) and `replyToNum` (local) |

## Open decision for the review gate

**Consolidate vs standalone (`Add*` posting methods).** `AddMessage`,
`AddMessageWithDate`, and `AddPrivateMessage` (`manager.go:585/645/701`) are
near-duplicates. The recommended design **consolidates** them into one internal
`addMessage(...)` (with thin public wrappers) plus the new `AddReply`, removing
the pre-existing triplication. This touches the central post path; behavior is
locked with characterization tests. The lower-blast-radius alternative is to add
`AddReply` as a standalone method and leave the three existing methods untouched
(accepting some duplication). **Default: consolidate**; switch to standalone if
preferred at review.

---

## Design

### 1. JAM: preserve `Header.ReplyTo` on write

`internal/jam/message.go` `WriteMessage` builds a fresh `hdr` (line ~454) and
drops any pre-set `msg.Header.ReplyTo`. `internal/jam/echomail.go`
`WriteMessageExt` (line ~32) does the same. Patch both to copy a non-zero
`msg.Header.ReplyTo` into the header being written:

```go
if msg.Header != nil && msg.Header.ReplyTo > 0 {
    hdr.ReplyTo = msg.Header.ReplyTo
}
```

This is backward-compatible — `Header.ReplyTo` is always `0` at write time today.
`ReadMessageHeader` already returns `ReplyTo` (`message.go:79`), so the value
round-trips to disk and back.

### 2. Manager: consolidate posting + add `AddReply`

Introduce one internal implementation:

```go
func (mm *MessageManager) addMessage(areaID int, from, to, subject, body, replyToMsgID string,
    replyToNum int, dateTime time.Time, private bool) (int, error)
```

It reproduces the current `AddMessage` body, plus:

- `msg.DateTime = dateTime` if non-zero, else `time.Now()`.
- if `private`: `msg.Header = &jam.MessageHeader{Attribute: jam.MsgPrivate | jam.MsgLocal}`.
- if `replyToNum > 0`: ensure `msg.Header != nil`, then `msg.Header.ReplyTo = uint32(replyToNum)`.
- if `replyToMsgID != ""`: `msg.ReplyID = replyToMsgID`.
- fire `OnMessagePosted` only when `!private` (matching today: `AddPrivateMessage`
  does not fire it).

Public methods become thin wrappers preserving exact current signatures and
behavior:

```go
func (mm *MessageManager) AddMessage(areaID int, from, to, subject, body, replyToMsgID string) (int, error) {
    return mm.addMessage(areaID, from, to, subject, body, replyToMsgID, 0, time.Time{}, false)
}
func (mm *MessageManager) AddMessageWithDate(areaID int, from, to, subject, body, replyToMsgID string, dateTime time.Time) (int, error) {
    return mm.addMessage(areaID, from, to, subject, body, replyToMsgID, 0, dateTime, false)
}
func (mm *MessageManager) AddPrivateMessage(areaID int, from, to, subject, body, replyToMsgID string) (int, error) {
    return mm.addMessage(areaID, from, to, subject, body, replyToMsgID, 0, time.Time{}, true)
}
// New: a reply carrying both the FTN ReplyID (for echomail Link()) and the
// numeric parent (for local threading).
func (mm *MessageManager) AddReply(areaID int, from, to, subject, body, replyToMsgID string, replyToNum int) (int, error) {
    return mm.addMessage(areaID, from, to, subject, body, replyToMsgID, replyToNum, time.Time{}, false)
}
```

Behavior to preserve verbatim (lock with characterization tests before
refactoring): the netmail `To`/`DestAddr` split; echomail/netmail →
`WriteMessageExt` with origin/tearline, else `WriteMessage`; `BodyTransform`
applied to the JAM copy while `OnMessagePosted` receives the original `body`;
`b.Close()` before the callback; `invalidateThreadIndex` on success; the private
path's `MsgPrivate|MsgLocal` attribute and *no* `OnMessagePosted`.

### 3. Reader: pass the parent on reply

In `internal/menu/message_reader_nav.go` `handleReply`, replace the
`AddMessage(..., replyMsgID)` call with:

```go
_, err := e.MessageMgr.AddReply(currentAreaID, currentUser.Handle, currentMsg.From,
    newSubject, replyBody, currentMsg.MsgID, currentMsg.MsgNum)
```

`currentMsg.MsgID` keeps echomail linking working; `currentMsg.MsgNum` sets the
local parent pointer. Because `READPRIVMAIL` and public reading both route
through `runMessageReader` → `handleReply`, this single change covers public and
private-mail replies.

### 4. Display: ensure the header shows the reference

`ReplyToNum` is already substituted as key `P` ("Reply to: #N", else "None").
Confirm the shipped default MSGHDR template renders the reply reference; if it
does not, add the field to the default template so the relationship is visible
out of the box. (Sysop custom templates remain free to place/omit it.)

---

## Testing

**`internal/jam`:** a message written with `Header.ReplyTo = N` reads back with
`ReplyTo == N` (WriteMessage and WriteMessageExt); a message with no/zero
`Header.ReplyTo` still writes `0` (no regression).

**`internal/message`:**
- `AddReply` sets the new message's `Header.ReplyTo` to the parent number
  (verify via `GetMessage(...).ReplyToNum`).
- `AddReply` with a non-empty `replyToMsgID` also sets `ReplyID`.
- Characterization: `AddMessage` (local), `AddMessageWithDate` (date preserved),
  `AddPrivateMessage` (private attribute set, `OnMessagePosted` NOT fired) behave
  exactly as before the consolidation — including `OnMessagePosted` firing for
  the non-private wrappers and the netmail To/DestAddr split.

**`internal/menu`:** `handleReply` posts a reply whose `ReplyToNum` equals the
parent's `MsgNum` (a focused test against a seeded area, if the reader has a
testable seam; otherwise covered by build + the manager/jam tests, with the
one-line call-site change verified by inspection).

---

## Risks / notes

- **Central post path:** the consolidation is the main risk; mitigated by
  characterization tests and exact behavior preservation. The standalone
  alternative is available if lower risk is preferred.
- **Echomail double-set:** `AddReply` sets `ReplyTo` directly to the local parent
  number *and* `ReplyID`; `Link()` would compute the same `ReplyTo`, so this is
  consistent (and makes the link immediate rather than deferred).
- **Existing messages:** not backfilled — only new replies thread.

## Acceptance criteria

- Replying to a local message records the parent number; `GetMessage` returns a
  non-zero `ReplyToNum` for it.
- The message header shows "Reply to: #N" for replies (default template).
- Echomail replies still link via `ReplyID`/`Link()`; private-mail and public
  replies both set the parent pointer.
- The three existing `Add*` methods behave identically (characterization tests
  pass); full suite green.
