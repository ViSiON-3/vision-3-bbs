# QWK Phase 5 — HEADERS.DAT Extended Headers

**Date:** 2026-06-30
**Status:** Approved design
**Branch:** `qwk-phase5-headers-dat` (stacked on `qwk-phase4-reply-metadata` / PR #67)
**Parent plan:** `docs-internal/plans/2026-06-29-qwk-rep-sync-mobile-design.md` (Phase 5)

---

## Problem

The base QWK message header truncates `To`/`From`/`Subject` to **25 characters**
(`internal/qwk/writer.go:220-226`), so long subjects are lost in a packet. The
QWKE/Synchronet `HEADERS.DAT` file carries full-length header fields (and more)
without kludge lines, and takes precedence over the base header. Adding it lets
long subjects survive a QWK download / REP upload and moves ViSiON/3 toward
mainstream reader (MultiMail/Synchronet) interoperability.

## Goal

Write and read a `HEADERS.DAT` file so full `To`/`From`/`Subject` (plus a
`Message-ID` and `WhenWritten` timestamp) round-trip through QWK packets.

## Scope decisions (confirmed)

| Decision | Choice |
| --- | --- |
| Fields per section | `Message-ID`, `Subject`, `To`, `From`, `WhenWritten` |
| Emit policy | One `HEADERS.DAT` section per message, always |
| Layer | Codec only (`internal/qwk`); no `qwkservice` change (full fields already flow through `PacketMessage`/`REPMessage`) |
| Interop | **Unverified** — implemented to the documented Synchronet format; validated only by a ViSiON/3 write→read round-trip. Real MultiMail/Synchronet validation is deferred until captured fixtures exist (`testdata/external/`). |

## Non-goals

- Synchronet legacy `@`-tag parsing (dropped from the roadmap — see the project
  note; QWK-networking is the future trigger).
- `In-Reply-To` / network-address / netmail fields (threading is already handled
  by the Phase 4 reference field; net-addr is echomail-only).
- Any `qwkservice` change (the service already carries full `Subject`/`To`).
- A config gate (readers that don't understand `HEADERS.DAT` ignore the extra
  zip entry — adding it is non-breaking).

---

## Format (authoritative — Synchronet `ref:qwk`)

`HEADERS.DAT` is an INI-style text file. Each section corresponds to one message
in `MESSAGES.DAT` (or `<bbsID>.MSG` for a REP). The section name is the message
header block's **byte offset into `MESSAGES.DAT`, in lowercase hexadecimal,
without a `0x` prefix**. The first message begins at offset 128 (after the
128-byte spacer block).

Example:

```
[80]
Message-ID: <1.1@vision3>
Subject: A very long subject line that exceeds the 25-character base limit
To: SomebodyWithARatherLongHandle
From: SysOp
WhenWritten: 20260305143000-0800  21e0

[180]
Message-ID: <2.1@vision3>
Subject: Re: A very long subject line that exceeds the 25-character base limit
To: SysOp
From: SomebodyWithARatherLongHandle
WhenWritten: 20260305150000-0800  21e0
```

- **`Message-ID`** — RFC822-style. Local messages have no MSGID, so synthesize a
  deterministic one: `<{number}.{conference}@{lowercased-bbsID}>`.
- **`Subject`/`To`/`From`** — full, untruncated values.
- **`WhenWritten`** — `YYYYMMDDhhmmss±hhmm` (ISO-8601, from the message
  timestamp) then two spaces then the SMB timezone as lowercase hex (see below).

### SMB timezone hex

The SMB timezone is a 16-bit value: bits 0–11 hold the absolute minutes offset
from UTC; bit `0x2000` marks *west* of UTC (non-US), bit `0x1000` marks *east*
of UTC (non-US); bits `0x4000` (US) and `0x8000` (daylight) are additional flags.

We emit the non-US west/east form and do **not** set the US or daylight flags —
they cannot be reliably inferred for an arbitrary BBS, and the ISO-8601 offset
already conveys the actual offset:

```go
func smbTimezone(offsetSeconds int) string {
    mins := offsetSeconds / 60
    var v uint16
    if mins < 0 {
        v = 0x2000 | uint16(-mins) // west of UTC
    } else {
        v = 0x1000 | uint16(mins)  // east of UTC
    }
    return fmt.Sprintf("%x", v)
}
```

(e.g. UTC-0800 → 480 min west → `0x2000|0x1E0 = 0x21E0` → `"21e0"`.)

---

## Design

### 1. `internal/qwk/headers.go` (new)

Pure encode/parse plus the two helpers, all unit-testable in isolation:

```go
// ExtHeader is the subset of HEADERS.DAT fields ViSiON/3 emits/consumes.
type ExtHeader struct {
    Offset      int    // byte offset of the message header in MESSAGES.DAT
    MessageID   string
    Subject     string
    To          string
    From        string
    WhenWritten string
}

// encodeHeadersDAT renders sections (ordered by offset) to INI bytes.
func encodeHeadersDAT(hs []ExtHeader) []byte

// parseHeadersDAT parses HEADERS.DAT into a map keyed by byte offset. Unknown
// keys and malformed lines are ignored (lenient); a missing file is handled by
// the caller (empty map).
func parseHeadersDAT(data []byte) map[int]ExtHeader

func smbTimezone(offsetSeconds int) string
func synthMessageID(bbsID string, conference, number int) string // "<num.conf@bbsid>"
```

Encoding rules: `[<hex offset>]` section line; `Key: Value` field lines
(`strings.SplitN(line, ":", 2)` on read; only the first `:` splits, so subjects
containing `:` are preserved); blank line between sections. CRLF line endings to
match the other packet text files.

### 2. Writer emits `HEADERS.DAT` — `internal/qwk/writer.go`, `rep_writer.go`

- `writeMessagesDAT` already tracks `currentBlock` (block 1 = spacer, messages
  from block 2). Record each message's header offset as `(currentBlock-1)*128`
  and return the per-message offsets alongside the NDX data.
- `WritePacket` builds `[]ExtHeader` from `pw.messages` + offsets (full
  `Subject`/`To`/`From`; `synthMessageID(pw.bbsID, msg.Conference, msg.Number)`;
  `WhenWritten` from `msg.DateTime`) and writes a `HEADERS.DAT` zip entry.
- `WriteREP` (the REP builder) computes the same offsets for its single `.MSG`
  and writes a `HEADERS.DAT` entry into the `.REP` zip too, so the write→read
  round trip carries the extended headers.

The base header still writes the 25-char-truncated `To`/`From`/`Subject`
(`formatMessage` unchanged) — plain readers keep working; `HEADERS.DAT` is
purely additive.

### 3. Reader parses + applies precedence — `internal/qwk/reader.go`

- `ReadREPPacket` locates a `HEADERS.DAT` entry in the zip (case-insensitive; may
  be absent) and parses it into `map[int]ExtHeader`.
- `parseREPMessages` gains the headers map. It already tracks each message's
  byte offset (`pos`); when a section exists for that offset, it **overrides**
  the block-header `Subject` and `To` with the full `HEADERS.DAT` values
  (precedence per spec). `From` is not surfaced on import (a REP's author is the
  uploading user, applied by the service); `Message-ID`/`WhenWritten` are parsed
  but not consumed (import uses server-receive time, per Phase 4).

No `internal/qwkservice` change: `BuildPacket` already sets the full
`PacketMessage.Subject`/`To`/`From`, and `ImportREP` already posts
`REPMessage.Subject`/`To`, so overridden (full) values flow through
automatically.

---

## Testing

**`internal/qwk` (unit — headers.go):**
- `synthMessageID` → `<2.1@vision3>` form.
- `smbTimezone`: UTC-0800 → `"21e0"`, UTC+0100 → `"103c"` (60 min east → `0x1000|0x3C`).
- `encodeHeadersDAT`→`parseHeadersDAT` round-trip preserves offset + all fields;
  a `Subject` containing `:` survives.
- `parseHeadersDAT` on malformed/empty input returns an empty map (no panic).

**`internal/qwk` (round-trip — the headline verification):**
- A packet/REP written with a `>25`-char `Subject` reads back the **full**
  subject via `HEADERS.DAT` override (and the base block still holds the 25-char
  form).
- `HEADERS.DAT` section offsets match message positions for a multi-message
  packet (second message keyed at `[180]` etc.).
- A REP with **no** `HEADERS.DAT` still reads (falls back to block header) — the
  existing REP tests continue to pass unchanged.

**`internal/qwkservice`:** existing tests unchanged (no service change); a
long-subject export→import round trip is covered at the codec layer.

---

## Risks / notes

- **Interop is unverified.** The format follows the Synchronet reference, but has
  not been checked against a real MultiMail/Synchronet packet. `testdata/external/`
  remains the slot for captured fixtures; a follow-up should validate and, if the
  format needs adjustment, correct it before claiming MultiMail interop.
- **SMB tz flags:** the US and daylight-savings flag bits are intentionally not
  set (cannot be reliably inferred); the ISO-8601 offset carries the real offset.
- **Synthesized Message-ID:** deterministic per `(bbsID, conference, number)`;
  not globally unique across BBSes, which is acceptable for local packets.
- **Offset agreement:** the write-side offset `(currentBlock-1)*128` and the
  read-side `pos` must reference the same thing (the message's header block start
  in `MESSAGES.DAT`/`.MSG`). Tests assert the multi-message offsets to lock this.

## Acceptance criteria

- ViSiON/3 packets include a `HEADERS.DAT` with a section per message.
- A long (`>25`-char) `Subject` (and `To`) survives a ViSiON/3 write→read round
  trip via `HEADERS.DAT`; the base header still carries the truncated form.
- REP packets without `HEADERS.DAT` still import (base-header fallback);
  existing codec/service tests pass unchanged.
- The format is documented as following the Synchronet reference with real-reader
  interop flagged unverified.
