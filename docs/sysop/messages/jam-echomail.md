# JAM Echomail Support

This document describes how Vision3 formats and writes Fidonet-style echomail into JAM message bases.

## Overview

Vision3 supports echomail and netmail formatting when writing messages into JAM. The core logic lives in `internal/jam` and is used by the message manager when posting to FTN areas.

Key goals:

- Correct JAM subfields for sender, recipient, subject, and addresses
- Proper FTN kludges and origin/tearline formatting
- Consistent MSGID generation

## Message Types

Message type is derived from the area configuration:

- **Local** - Standard local message base
- **Echomail** - Conference/echo messages
- **Netmail** - Direct network messages

Logic lives in `internal/jam/msgtype.go` and is called by `internal/message/manager.go`.

## What Gets Added for Echomail

When writing **echomail** with `WriteMessageExt`, Vision3 automatically adds:

- `AREA:` kludge
- `MSGID` (unique serial per base), generated when the message has none
- `PID`/`TID` identifiers
- Tearline (`--- ViSiON/3 vX.Y.Z/Platform`, assigned by the software)
- Origin line (`* Origin: ... (address)`)

`SEEN-BY` and `PATH` are the tosser's job and are not added here.

**Netmail** takes none of the above: `WriteMessageExt` writes an `MSGID`
subfield only when the message already carries one, and adds no kludges,
tearline, or origin line. Netmail is point-to-point, so the origin line — which
identifies a message's source conference-wide — does not apply.

Implementation: `internal/jam/echomail.go`, `internal/jam/format.go`, `internal/jam/msgid.go`.

## Configuration Inputs

These values come from configuration and are applied during message creation:

- **Origin address**: `configs/message_areas.json` (`origin_addr` per area)
- **Network origin text**: `configs/ftn.json` (`origin` per network; empty = board name)
- **BBS name**: `configs/config.json` (`boardName`)

The message manager passes these into the JAM writer.

## Message ID Serial

MSGIDs are generated using a serial counter stored in the JAM fixed header reserved space. The counter increments per message and persists across restarts.

Implementation: `internal/jam/msgid.go`.

## Reading Messages

Incoming echomail tossed by `v3mail toss` is read from JAM and displayed by the message reader. If an origin address is missing from subfields, Vision3 attempts to extract it from the origin line in the text.

Implementation: `internal/jam/message.go`.

## Tests

Echomail support is covered by tests in `internal/jam/echomail_test.go`.

Run:

```bash
go test ./internal/jam/... -v
```
