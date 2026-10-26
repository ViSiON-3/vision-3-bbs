# JAM Echomail Support

This document describes how ViSiON/3 formats and writes Fidonet-style echomail
into JAM message bases, and what it leaves alone when mail arrives from a
network. Most sysops never need it: the tosser and the message reader take care
of all of this. Read it when you are chasing a threading or duplicate problem,
or when another system's sysop asks what your board puts in a message.

## Overview

ViSiON/3 supports echomail and netmail formatting when writing messages into JAM. The core logic lives in `internal/jam` and is used by the message manager when posting to FTN areas.

Key goals:

- Correct JAM subfields for sender, recipient, subject, and addresses
- Proper FTN kludges and origin/tearline formatting
- Consistent MSGID generation
- Imported mail stored exactly as it was received

## Message Types

Message type is derived from the area configuration:

- **Local** - Standard local message base
- **Echomail** - Conference/echo messages
- **Netmail** - Direct network messages

Logic lives in `internal/jam/msgtype.go` and is called by `internal/message/manager.go`.

## What Gets Added for Echomail

When writing **echomail** with `WriteMessageExt`, ViSiON/3 automatically adds:

- `MSGID` (unique serial per base), generated when the message has none
- `PID`/`TID` identifiers
- Tearline (`--- ViSiON/3 vX.Y.Z/Platform`, assigned by the software)
- Origin line (`* Origin: ... (address)`)

`SEEN-BY` and `PATH` are the tosser's job and are not added here. Neither is
the `AREA:` line: the echo area is implicit in the message base, and the tosser
writes the FTS-0004 `AREA:` line from the area's echo tag when it packs the
message for export.

**Netmail** takes none of the above: `WriteMessageExt` writes an `MSGID`
subfield only when the message already carries one, and adds no kludges,
tearline, or origin line. Netmail is point-to-point, so the origin line — which
identifies a message's source conference-wide — does not apply.

Implementation: `internal/jam/echomail.go`, `internal/jam/format.go`, `internal/jam/msgid.go`.

## Imported Mail Is Stored As Received

Mail that arrives from a network (an FTN toss, or a QWK network poll) is
written with `WriteReceivedMessage` instead. It neither invents a `MSGID` for
an echomail message that came without one nor stamps ViSiON/3's `PID`/`TID` on
it. Both would claim the message for this system, and a made-up `MSGID` would
differ on every system that received the message, so it would thread with
nothing. Some software (TriToss, for example) sends echomail with no `MSGID`;
the tosser still catches duplicates of those by a hash of their content (see
[Duplicate messages](messages/ftn-echomail.md#duplicate-messages)).

`WriteReceivedMessage` also never writes the sender-handle subfield described
below, so mail from a network cannot claim to be from a local account.

## Configuration Inputs

These values come from configuration and are applied during message creation:

- **Origin address**: `configs/message_areas.json` (`origin_addr` per area;
  **Origin Addr** under Message Areas in the config editor)
- **Network origin text**: `configs/ftn.json` (`origin` per network; empty =
  board name; **Origin** under Echomail Networks)
- **BBS name**: `configs/config.json` (`boardName`)

The message manager passes these into the JAM writer. The tearline is not
configurable: FTS-0004 reserves it for the software that produced the message.

## Reading Messages

Incoming echomail tossed by `v3mail toss` is read from JAM and displayed by the message reader. If an origin address is missing from subfields, ViSiON/3 attempts to extract it from the origin line in the text.

Sysops (co-sysop level and above) can press **K** in the message reader to see
a message's header fields and kludges — `MSGID`, `REPLY`, `PID`, the rest in
wire order, then `SEEN-BY` and `PATH` — which is the quickest way to trace a
duplicate or a misrouted message. See
[Reading Messages](messages/message-areas.md#reading-messages).

Implementation: `internal/jam/message.go`.

## Technical Reference

### Message ID Serial

MSGIDs are generated using a serial counter stored in the JAM fixed header reserved space. The counter increments per message and persists across restarts.

Implementation: `internal/jam/msgid.go`.

### Subfields ViSiON/3 Writes

Beside the standard JAM subfields (sender, recipient, subject, addresses,
`MSGID`, `REPLYID`, `PID`, `TID`, `SEEN-BY`, `PATH`, and FTS kludges),
ViSiON/3 uses one subfield of its own:

| Subfield | ID | Written when | Meaning |
| --- | --- | --- | --- |
| `SfldV3SenderHandle` | `0x5633` | A local post's From differs from the poster's handle (a real name in a Real Name Only area) | The handle of the local account that wrote the message, so private mail (netmail) signed with a real name stays visible to its sender. Never written for imported mail; never exported by the tosser. |

The ID is outside the range the JAM specification assigns; other JAM software
skips subfields it does not know. `v3mail pack` keeps subfields as they are.
See [Real names and private mail](messages/private-mail.md#real-names-and-private-mail).

### Writers

| Function | Used for | Adds |
| --- | --- | --- |
| `Base.WriteMessageExt` | Posts made on this board (editor, `helper post`, QWK REP uploads from callers) | `MSGID` if missing, `PID`/`TID`, tearline and origin for echomail |
| `Base.WriteReceivedMessage` | Mail imported from a network (FTN toss, QWK network toss) | Nothing; stored as received |

Both leave `DateProcessed` at 0; the tosser marks a message processed once it
has exported it (local posts) or stored it (imports).

### Tests

Echomail support is covered by tests in `internal/jam/echomail_test.go`.

Run:

```bash
go test ./internal/jam/... -v
```
