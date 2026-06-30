# QWK Phase 4 — Reply Threading in Packets

**Date:** 2026-06-30
**Status:** Approved design
**Branch:** `qwk-phase4-reply-metadata`
**Parent plan:** `docs-internal/plans/2026-06-29-qwk-rep-sync-mobile-design.md` (Phase 4)

---

## Problem

Local messages now carry a reply pointer (`Header.ReplyTo`, set by `AddReply`),
but QWK packets discard it. The base-QWK message header has a "reference number"
field (positions 108–115) meant to hold the parent message's number — ViSiON/3
hardcodes it to `0` on export (`internal/qwk/writer.go:231`) and never parses it
on import (`internal/qwk/reader.go`). So a reply downloaded in a `.QWK`, or
uploaded in a `.REP`, loses its thread linkage.

Now that `DisplayMessage.ReplyToNum` is populated and the reader displays
"Reply#: N", there is real data to carry. This phase round-trips it through the
base-QWK reference field.

## Goals

- Export each message's parent number into the QWK reference field.
- Parse the reference field on REP import and set the reply pointer on the posted
  message, so reply chains survive the offline-reader round trip.

## Non-goals (later phases)

- `HEADERS.DAT` / Synchronet `@` tags (Phase 5; and see the project note dropping
  the `@` tags).
- MSGID / network-address fields — local areas (the QWK use case) have no MSGID;
  those belong to echomail / `HEADERS.DAT`.
- Preserving the reader's authored compose date (decided: use server-receive
  time; a small follow-up could add it).
- Validating the reference / reporting degraded threads (decided: set it as-is;
  readers produce valid references for messages we exported, and a dangling
  reference is harmless — the reader tolerates it).

## Decisions (confirmed)

| Decision | Choice |
| --- | --- |
| Threading channel | Base-QWK reference field (positions 108–115); the parent's QWK number, which equals its local JAM message number in our export |
| Import timestamp | Server-receive time (no authored-date preservation) |
| Bad/dangling reference | Set as-is, no validation, no degraded-thread reporting |
| Import posting API | Always use `AddReply`/`AddPrivateReply`; `replyToNum == 0` means "no parent" (behaves exactly like the non-reply post) |

---

## Design

### 1. Export — `internal/qwk` + `qwkservice`

- `PacketMessage` (`internal/qwk/types.go`) gains `ReplyToNumber int`.
- `formatMessage` (`internal/qwk/writer.go:231`) writes it into positions
  108–115 instead of `0`:

  ```go
  copyPadded(header[108:116], fmt.Sprintf("%8d", msg.ReplyToNumber), 8)
  ```

  `ReplyToNumber == 0` writes the same `"       0"` as today, so non-replies are
  unchanged and existing readers see no difference.
- `BuildPacket` (`internal/qwkservice/service.go:180`) copies it from the
  `DisplayMessage`:

  ```go
  pw.AddMessage(qwk.PacketMessage{
      ...
      ReplyToNumber: msg.ReplyToNum,
  })
  ```

  Because exported QWK message numbers equal the local JAM `MsgNum`, the
  reference field is the parent's local number directly — no remapping.

### 2. Import — `internal/qwk` + `qwkservice`

- `REPMessage` (`internal/qwk/types.go`) gains `ReplyToNumber int`.
- `parseREPMessages` (`internal/qwk/reader.go`) parses positions 108–115
  alongside the existing To/Subject/conference fields:

  ```go
  refStr := strings.TrimSpace(string(header[108:116]))
  refNum, _ := strconv.Atoi(refStr) // 0 / unparsable => no parent
  ```

  (`strconv` is already imported.) Set `REPMessage.ReplyToNumber = refNum`.
- `ImportREP` (`internal/qwkservice/service_import.go`) posts through the reply
  methods, passing the parsed number:

  ```go
  if kind == KindPrivateMail {
      _, perr = s.store.AddPrivateReply(area.ID, opts.Handle, msg.To, msg.Subject, body, "", msg.ReplyToNumber)
  } else {
      _, perr = s.store.AddReply(area.ID, opts.Handle, msg.To, msg.Subject, body, "", msg.ReplyToNumber)
  }
  ```

  A reference from an incoming `.REP` is the parent's local message number (it is
  a message ViSiON/3 exported), so the reply threads against the right parent.
  When `ReplyToNumber == 0`, `AddReply`/`AddPrivateReply` behave exactly like
  `AddMessage`/`AddPrivateMessage` (the manager's `replyToNum > 0` guard skips the
  pointer), so non-reply imports are unchanged.

### 3. `MessageStore` interface

`internal/qwkservice/service.go`'s `MessageStore` currently declares
`AddMessage` and `AddPrivateMessage`. Since import is their only caller and it
now uses the reply variants, replace them:

```go
- AddMessage(areaID int, from, to, subject, body, replyToMsgID string) (int, error)
- AddPrivateMessage(areaID int, from, to, subject, body, replyToMsgID string) (int, error)
+ AddReply(areaID int, from, to, subject, body, replyToMsgID string, replyToNum int) (int, error)
+ AddPrivateReply(areaID int, from, to, subject, body, replyToMsgID string, replyToNum int) (int, error)
```

`*message.MessageManager` already implements both. The test `fakeStore` is
updated to match (recording the `replyToNum` so import tests can assert it).

### 4. `WriteREP` (test helper) parity

`WriteREP` builds the `.MSG` via `formatMessage`, so it carries `ReplyToNumber`
automatically once `formatMessage` writes it — no change needed beyond passing
`ReplyToNumber` in the `PacketMessage`s a test constructs.

---

## Testing

**`internal/qwk`:**
- `formatMessage` writes `ReplyToNumber` into positions 108–115 (parse the bytes
  back).
- `parseREPMessages` reads the reference field into `REPMessage.ReplyToNumber`.
- `WriteREP` → `ReadREP` round-trip preserves `ReplyToNumber` (incl. 0).

**`internal/qwkservice`:**
- Export: `BuildPacket` from a `DisplayMessage` with `ReplyToNum = N` produces a
  packet whose message reference is `N` (read it back via `ReadREP`).
- Import (public): a REP message with `ReplyToNumber = N` calls `AddReply` with
  `replyToNum = N` (fake records it); `ReplyToNumber = 0` calls `AddReply` with 0.
- Import (private): conference 0 routes to `AddPrivateReply` with the number.
- Existing import tests updated for the `AddReply`/`AddPrivateReply` fake methods;
  their behavior (posted/skipped/duplicate, ACS, signature) is unchanged.

---

## Risks / notes

- **Partial packets:** a newscan packet may omit the parent (read earlier); the
  reference still points at the parent's number, which the reader resolves
  against its own accumulated store — standard QWK behavior.
- **Reference width:** positions 108–115 hold 8 digits — ample for JAM message
  numbers.
- **Backward compatible:** `ReplyToNumber` defaults to 0 → identical bytes to
  today's hardcoded `0`; import of older packets (no reference) yields 0 → no
  parent.

## Acceptance criteria

- A reply exported in a `.QWK` carries its parent number in the reference field.
- A reply uploaded in a `.REP` is posted with its parent pointer set (verifiable
  via `GetMessage(...).ReplyToNum`), threading public and private replies.
- Non-reply messages and older packets behave exactly as before.
- Tests cover export reference-field write, import reference parse + routing, and
  the round-trip.
