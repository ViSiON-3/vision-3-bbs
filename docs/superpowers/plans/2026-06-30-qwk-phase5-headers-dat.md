# QWK Phase 5 — HEADERS.DAT Extended Headers — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Write and read a `HEADERS.DAT` file so full `To`/`From`/`Subject` (plus `Message-ID` and `WhenWritten`) round-trip through QWK packets, lifting the 25-char base-header limit.

**Architecture:** A new `internal/qwk/headers.go` encodes/parses the Synchronet INI form (`[<hex-offset>]` sections keyed by each message's `MESSAGES.DAT` header offset). The packet writer and REP writer emit `HEADERS.DAT`; the REP reader parses it and overrides the truncated `Subject`/`To`. Codec-only — no `qwkservice` change (full fields already flow through `PacketMessage`/`REPMessage`).

**Tech Stack:** Go (stdlib `archive/zip`, `strconv`, `strings`, `time`, `testing`); `internal/qwk` only.

## Global Constraints

- No new dependencies; Go files under 300 lines; `slog` for logging.
- TDD: failing test first → red → minimal implementation → green → commit.
- Format (Synchronet `ref:qwk`): INI; section `[<hexoffset>]` (lowercase hex, no `0x`); offset = the message header's byte offset in `MESSAGES.DAT` (first message at 128, after the 128-byte spacer); fields `Message-ID`, `Subject`, `To`, `From`, `WhenWritten`; `HEADERS.DAT` overrides the base header. CRLF line endings.
- `WhenWritten` = `YYYYMMDDhhmmss±hhmm` then two spaces then the SMB timezone as lowercase hex; SMB tz = `0x2000|absMinutes` (west) or `0x1000|absMinutes` (east), US/daylight flags NOT set.
- `Message-ID` synthesized deterministically: `<{number}.{conference}@{lowercased-bbsID}>`.
- Emit one `HEADERS.DAT` section per message, always. Adding the file is non-breaking (plain readers ignore it).
- Spec: `docs/superpowers/specs/2026-06-30-qwk-phase5-headers-dat-design.md`.

---

## File Structure

- Create: `internal/qwk/headers.go` — encode/parse + `smbTimezone`/`synthMessageID`/`formatWhenWritten`/`extHeadersFor`.
- Create: `internal/qwk/headers_test.go` — unit tests for the above.
- Modify: `internal/qwk/writer.go` — `writeMessagesDAT` returns offsets; `WritePacket` writes `HEADERS.DAT`.
- Modify: `internal/qwk/rep_writer.go` — `WriteREP` writes `HEADERS.DAT`.
- Modify: `internal/qwk/reader.go` — `ReadREPPacket` parses `HEADERS.DAT`; `parseREPMessages` applies the override.
- Modify: `internal/qwk/rep_writer_test.go` / `writer_test.go` — round-trip + emit tests.
- Modify: `docs/sysop/messages/qwk.md` — one-line note.

---

## Task 1: `headers.go` — encode/parse + helpers

**Files:**
- Create: `internal/qwk/headers.go`
- Test: `internal/qwk/headers_test.go`

**Interfaces:**
- Produces: `type ExtHeader struct { Offset int; MessageID, Subject, To, From, WhenWritten string }`; `encodeHeadersDAT([]ExtHeader) []byte`; `parseHeadersDAT([]byte) map[int]ExtHeader`; `smbTimezone(offsetSeconds int) string`; `synthMessageID(bbsID string, conference, number int) string`; `formatWhenWritten(t time.Time) string`; `extHeadersFor(msgs []PacketMessage, offsets []int, bbsID string) []ExtHeader`.

- [ ] **Step 1: Write the failing tests**

Create `internal/qwk/headers_test.go`:

```go
package qwk

import (
	"testing"
	"time"
)

func TestSynthMessageID(t *testing.T) {
	if got := synthMessageID("VISION3", 1, 2); got != "<2.1@vision3>" {
		t.Errorf("synthMessageID = %q, want <2.1@vision3>", got)
	}
}

func TestSmbTimezone(t *testing.T) {
	if got := smbTimezone(-8 * 3600); got != "21e0" { // 480 min west
		t.Errorf("smbTimezone(-0800) = %q, want 21e0", got)
	}
	if got := smbTimezone(1 * 3600); got != "103c" { // 60 min east
		t.Errorf("smbTimezone(+0100) = %q, want 103c", got)
	}
}

func TestHeadersDAT_RoundTrip(t *testing.T) {
	in := []ExtHeader{
		{Offset: 384, MessageID: "<2.1@v>", Subject: "Second", To: "Bob", From: "Al", WhenWritten: "20260305150000-0800  21e0"},
		{Offset: 128, MessageID: "<1.1@v>", Subject: "First: with a colon", To: "Al", From: "Bob", WhenWritten: "20260305143000-0800  21e0"},
	}
	data := encodeHeadersDAT(in)
	out := parseHeadersDAT(data)

	if len(out) != 2 {
		t.Fatalf("want 2 sections, got %d", len(out))
	}
	h := out[128]
	if h.Subject != "First: with a colon" {
		t.Errorf("subject with colon not preserved: %q", h.Subject)
	}
	if h.To != "Al" || h.From != "Bob" || h.MessageID != "<1.1@v>" {
		t.Errorf("fields not preserved: %+v", h)
	}
	if _, ok := out[384]; !ok {
		t.Error("second section (offset 384) missing")
	}
}

func TestParseHeadersDAT_LenientOnGarbage(t *testing.T) {
	if got := parseHeadersDAT(nil); len(got) != 0 {
		t.Errorf("nil input: want empty map, got %v", got)
	}
	if got := parseHeadersDAT([]byte("not ini\nmore junk\n")); len(got) != 0 {
		t.Errorf("garbage input: want empty map, got %v", got)
	}
}

func TestFormatWhenWritten(t *testing.T) {
	tt := time.Date(2026, 3, 5, 14, 30, 0, 0, time.FixedZone("PST", -8*3600))
	if got := formatWhenWritten(tt); got != "20260305143000-0800  21e0" {
		t.Errorf("formatWhenWritten = %q, want 20260305143000-0800  21e0", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/qwk/ -run 'TestSynthMessageID|TestSmbTimezone|TestHeadersDAT_RoundTrip|TestParseHeadersDAT_LenientOnGarbage|TestFormatWhenWritten' -v`
Expected: FAIL — the functions/types are undefined (compile error).

- [ ] **Step 3: Implement `internal/qwk/headers.go`**

```go
package qwk

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ExtHeader is the subset of HEADERS.DAT fields ViSiON/3 emits and consumes.
type ExtHeader struct {
	Offset      int // byte offset of the message header in MESSAGES.DAT / .MSG
	MessageID   string
	Subject     string
	To          string
	From        string
	WhenWritten string
}

// encodeHeadersDAT renders HEADERS.DAT sections (ordered by offset) as INI bytes.
func encodeHeadersDAT(hs []ExtHeader) []byte {
	sorted := make([]ExtHeader, len(hs))
	copy(sorted, hs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Offset < sorted[j].Offset })

	var buf bytes.Buffer
	for i, h := range sorted {
		if i > 0 {
			buf.WriteString("\r\n")
		}
		fmt.Fprintf(&buf, "[%x]\r\n", h.Offset)
		writeHeaderField(&buf, "Message-ID", h.MessageID)
		writeHeaderField(&buf, "Subject", h.Subject)
		writeHeaderField(&buf, "To", h.To)
		writeHeaderField(&buf, "From", h.From)
		writeHeaderField(&buf, "WhenWritten", h.WhenWritten)
	}
	return buf.Bytes()
}

func writeHeaderField(buf *bytes.Buffer, key, val string) {
	if val == "" {
		return
	}
	buf.WriteString(key)
	buf.WriteString(": ")
	buf.WriteString(val)
	buf.WriteString("\r\n")
}

// parseHeadersDAT parses HEADERS.DAT into a map keyed by message byte offset.
// Malformed lines and unknown keys are ignored; empty/garbage input yields an
// empty map.
func parseHeadersDAT(data []byte) map[int]ExtHeader {
	out := make(map[int]ExtHeader)
	var cur ExtHeader
	have := false
	flush := func() {
		if have {
			out[cur.Offset] = cur
		}
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			off, err := strconv.ParseInt(line[1:len(line)-1], 16, 64)
			if err != nil {
				have = false
				continue
			}
			cur = ExtHeader{Offset: int(off)}
			have = true
			continue
		}
		if !have {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "message-id":
			cur.MessageID = val
		case "subject":
			cur.Subject = val
		case "to":
			cur.To = val
		case "from":
			cur.From = val
		case "whenwritten":
			cur.WhenWritten = val
		}
	}
	flush()
	return out
}

// smbTimezone encodes a UTC offset (in seconds) as the SMB hex timezone. Bits
// 0-11 hold the absolute minutes; 0x2000 marks west of UTC, 0x1000 east. The US
// and daylight flag bits are intentionally not set (they cannot be reliably
// inferred; the ISO-8601 offset carries the real offset).
func smbTimezone(offsetSeconds int) string {
	mins := offsetSeconds / 60
	var v uint16
	if mins < 0 {
		v = 0x2000 | uint16(-mins)
	} else {
		v = 0x1000 | uint16(mins)
	}
	return fmt.Sprintf("%x", v)
}

// synthMessageID builds a deterministic RFC822-style Message-ID for a local
// message, which has no FTN MSGID.
func synthMessageID(bbsID string, conference, number int) string {
	return fmt.Sprintf("<%d.%d@%s>", number, conference, strings.ToLower(bbsID))
}

// formatWhenWritten renders a timestamp as ISO-8601 with offset plus the SMB
// hex timezone.
func formatWhenWritten(t time.Time) string {
	_, off := t.Zone()
	return t.Format("20060102150405-0700") + "  " + smbTimezone(off)
}

// extHeadersFor builds the HEADERS.DAT entries for a set of packet messages
// given their byte offsets in MESSAGES.DAT / .MSG.
func extHeadersFor(msgs []PacketMessage, offsets []int, bbsID string) []ExtHeader {
	hs := make([]ExtHeader, 0, len(msgs))
	for i, m := range msgs {
		hs = append(hs, ExtHeader{
			Offset:      offsets[i],
			MessageID:   synthMessageID(bbsID, m.Conference, m.Number),
			Subject:     m.Subject,
			To:          m.To,
			From:        m.From,
			WhenWritten: formatWhenWritten(m.DateTime),
		})
	}
	return hs
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/qwk/ -run 'TestSynthMessageID|TestSmbTimezone|TestHeadersDAT_RoundTrip|TestParseHeadersDAT_LenientOnGarbage|TestFormatWhenWritten' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/qwk/headers.go internal/qwk/headers_test.go
go vet ./internal/qwk/
git add internal/qwk/headers.go internal/qwk/headers_test.go
git commit -m "feat(qwk): add HEADERS.DAT encode/parse and helpers"
```

---

## Task 2: Writers emit `HEADERS.DAT`

**Files:**
- Modify: `internal/qwk/writer.go` (`writeMessagesDAT` returns offsets; `WritePacket` writes `HEADERS.DAT`)
- Modify: `internal/qwk/rep_writer.go` (`WriteREP` writes `HEADERS.DAT`)
- Test: `internal/qwk/rep_writer_test.go`

**Interfaces:**
- Consumes: `encodeHeadersDAT`, `extHeadersFor` (Task 1).

- [ ] **Step 1: Write the failing test**

Add to `internal/qwk/rep_writer_test.go`:

```go
func TestWriteREP_EmitsHeadersDAT(t *testing.T) {
	longSubject := "A very long subject line beyond the 25-character base limit"
	data := buildREP(t, "VISION3", []PacketMessage{
		{Conference: 1, Number: 1, From: "SysOp", To: "SomebodyWithALongHandle",
			Subject: longSubject, DateTime: time.Date(2026, 3, 5, 14, 30, 0, 0, time.UTC), Body: "x"},
	})

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	var hdr []byte
	for _, f := range zr.File {
		if f.Name == "HEADERS.DAT" {
			rc, _ := f.Open()
			hdr, _ = io.ReadAll(rc)
			rc.Close()
		}
	}
	if hdr == nil {
		t.Fatal("REP packet missing HEADERS.DAT")
	}
	// First message header is at offset 128 -> section [80].
	got := parseHeadersDAT(hdr)
	h, ok := got[128]
	if !ok {
		t.Fatalf("no section at offset 128; sections=%v", got)
	}
	if h.Subject != longSubject {
		t.Errorf("HEADERS.DAT subject: want full, got %q", h.Subject)
	}
	if h.To != "SomebodyWithALongHandle" {
		t.Errorf("HEADERS.DAT to: want full, got %q", h.To)
	}
}
```

(`io` is already imported in the qwk package tests via other files; if not, add
`"io"` to `rep_writer_test.go` imports.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/qwk/ -run TestWriteREP_EmitsHeadersDAT -v`
Expected: FAIL — no `HEADERS.DAT` entry in the REP zip.

- [ ] **Step 3: Track offsets and emit in `writeMessagesDAT` / `WritePacket`**

In `internal/qwk/writer.go`, change `writeMessagesDAT` to also return the
per-message offsets. Update its signature and body:

```go
func (pw *PacketWriter) writeMessagesDAT(zw *zip.Writer) (map[int][]byte, []byte, []int, error) {
```

Inside the message loop, record the offset **before** advancing `currentBlock`
(add right after `msgBytes := formatMessage(msg)` / before writing, e.g. just
before `msgBuf.Write(padded)`):

```go
		offsets = append(offsets, (currentBlock-1)*BlockSize)
```

Declare `var offsets []int` next to `ndxData`, and change the two `return`
statements to include it (`return ndxData, personalNDX, offsets, nil` and the
error return `return nil, nil, nil, err`).

In `WritePacket`, update the call and write `HEADERS.DAT` after `PERSONAL.NDX`:

```go
	ndxData, personalNDX, offsets, err := pw.writeMessagesDAT(zw)
	if err != nil {
		return fmt.Errorf("MESSAGES.DAT: %w", err)
	}
```

```go
	if hdrs := encodeHeadersDAT(extHeadersFor(pw.messages, offsets, pw.bbsID)); len(hdrs) > 0 {
		if err := writeZipEntry(zw, "HEADERS.DAT", hdrs); err != nil {
			return fmt.Errorf("HEADERS.DAT: %w", err)
		}
	}
```

- [ ] **Step 4: Emit in `WriteREP`**

In `internal/qwk/rep_writer.go`, collect offsets in the message loop and write a
`HEADERS.DAT` entry. Before the loop add `var offsets []int`; inside the loop,
capture the offset before appending the message's blocks:

```go
	for _, msg := range msgs {
		offsets = append(offsets, msgBuf.Len())
		msgBytes := formatMessage(msg)
		numBlocks := (len(msgBytes) + BlockSize - 1) / BlockSize

		padded := make([]byte, numBlocks*BlockSize)
		for i := range padded {
			padded[i] = ' '
		}
		copy(padded, msgBytes)
		msgBuf.Write(padded)
	}
```

After the `<bbsID>.MSG` zip entry is written successfully (before `zw.Close()`),
add:

```go
	if hdrs := encodeHeadersDAT(extHeadersFor(msgs, offsets, bbsID)); len(hdrs) > 0 {
		if err := writeZipEntry(zw, "HEADERS.DAT", hdrs); err != nil {
			zw.Close()
			return fmt.Errorf("HEADERS.DAT: %w", err)
		}
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/qwk/ 2>&1 | tail -5`
Expected: PASS — the new emit test plus all existing `internal/qwk` tests (the
extra zip entry does not affect `ReadREP`, which reads only `<bbsID>.MSG`).

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/qwk/writer.go internal/qwk/rep_writer.go internal/qwk/rep_writer_test.go
go vet ./internal/qwk/
git add internal/qwk/writer.go internal/qwk/rep_writer.go internal/qwk/rep_writer_test.go
git commit -m "feat(qwk): emit HEADERS.DAT from the packet and REP writers"
```

---

## Task 3: Reader parses `HEADERS.DAT` and applies precedence

**Files:**
- Modify: `internal/qwk/reader.go` (`ReadREPPacket` reads `HEADERS.DAT`; `parseREPMessages` overrides)
- Test: `internal/qwk/rep_writer_test.go`

**Interfaces:**
- Consumes: `parseHeadersDAT` (Task 1), the `HEADERS.DAT` written by Task 2.

- [ ] **Step 1: Write the failing test**

Add to `internal/qwk/rep_writer_test.go`:

```go
func TestREP_RoundTripLongSubjectViaHeaders(t *testing.T) {
	longSubject := "A very long subject line beyond the 25-character base limit"
	data := buildREP(t, "VISION3", []PacketMessage{
		{Conference: 1, Number: 1, From: "SysOp", To: "SomebodyWithALongHandle",
			Subject: longSubject, DateTime: time.Date(2026, 3, 5, 14, 30, 0, 0, time.UTC), Body: "x"},
	})

	out, err := ReadREP(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err != nil {
		t.Fatalf("ReadREP: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 message, got %d", len(out))
	}
	if out[0].Subject != longSubject {
		t.Errorf("subject not restored from HEADERS.DAT: got %q (len %d)", out[0].Subject, len(out[0].Subject))
	}
	if out[0].To != "SomebodyWithALongHandle" {
		t.Errorf("to not restored from HEADERS.DAT: got %q", out[0].To)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/qwk/ -run TestREP_RoundTripLongSubjectViaHeaders -v`
Expected: FAIL — `Subject` comes back truncated to 25 chars (no override yet).

- [ ] **Step 3: Read `HEADERS.DAT` in `ReadREPPacket` and pass it to `parseREPMessages`**

In `internal/qwk/reader.go` `ReadREPPacket`, after `data` is read and before
calling `parseREPMessages`, locate and parse the (optional) `HEADERS.DAT` entry:

```go
	headers := map[int]ExtHeader{}
	for _, f := range zr.File {
		if strings.EqualFold(f.Name, "HEADERS.DAT") {
			hrc, err := f.Open()
			if err == nil {
				hdata, _ := io.ReadAll(hrc)
				hrc.Close()
				headers = parseHeadersDAT(hdata)
			}
			break
		}
	}

	msgs, err := parseREPMessages(data, headers)
	if err != nil {
		return nil, err
	}
```

Change `parseREPMessages` to accept the headers map and apply the override. Its
signature becomes:

```go
func parseREPMessages(data []byte, headers map[int]ExtHeader) ([]REPMessage, error) {
```

Inside the loop, after computing `to` and `subject` (and before building the
`REPMessage`), apply the precedence:

```go
		if h, ok := headers[pos]; ok {
			if h.Subject != "" {
				subject = h.Subject
			}
			if h.To != "" {
				to = h.To
			}
		}
```

`From` is not surfaced on import; `Message-ID`/`WhenWritten` are parsed but not
consumed (import uses server-receive time).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/qwk/ 2>&1 | tail -5`
Expected: PASS — the new round-trip test plus all existing `internal/qwk` tests.
The existing REP tests build zips without a `HEADERS.DAT`, so `parseREPMessages`
receives an empty map and behaves exactly as before (base-header fallback).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/qwk/reader.go internal/qwk/rep_writer_test.go
go vet ./internal/qwk/
git add internal/qwk/reader.go internal/qwk/rep_writer_test.go
git commit -m "feat(qwk): apply HEADERS.DAT precedence on REP import (full To/Subject)"
```

---

## Task 4: Docs + full verification

**Files:**
- Modify: `docs/sysop/messages/qwk.md`

- [ ] **Step 1: Add a note**

In `docs/sysop/messages/qwk.md`, near the packet-format / reply-threading section,
add:

```markdown
## Long headers (HEADERS.DAT)

The base QWK message header limits To/From/Subject to 25 characters. ViSiON/3 also
writes a `HEADERS.DAT` file carrying the full-length fields (and a Message-ID and
timestamp) per the Synchronet extended-header format, and reads it back on REP
upload — so long subjects survive the round trip. Readers that don't understand
`HEADERS.DAT` simply ignore the extra file.

(Interoperability with third-party readers such as MultiMail follows the
documented format but has not yet been validated against a real reader.)
```

- [ ] **Step 2: Commit the docs**

```bash
git add docs/sysop/messages/qwk.md
git commit -m "docs(qwk): document HEADERS.DAT long-header support"
```

- [ ] **Step 3: Full verification**

Run: `gofmt -l internal/qwk`
Expected: no output.

Run: `go vet ./... 2>&1 | tail -5`
Expected: no issues.

Run: `go test ./... 2>&1 | tail -10`
Expected: all packages PASS.

Run: `go test -race ./internal/qwk ./internal/qwkservice 2>&1 | tail -5`
Expected: PASS, no race warnings.

- [ ] **Step 4: Final commit (only if cleanup was needed)**

```bash
git add -A
git commit -m "chore(qwk): Phase 5 cleanup and verification"
```

---

## Self-Review (completed by plan author)

- **Spec coverage:** encode/parse + helpers (`smbTimezone`, `synthMessageID`, `formatWhenWritten`, `extHeadersFor`) → Task 1; writer emit (`WritePacket` + `WriteREP`, offset tracking) → Task 2; reader parse + `Subject`/`To` override with precedence → Task 3; docs + verification → Task 4. Codec-only, no service change — matches the spec. All sections covered.
- **Placeholder scan:** no TBD/TODO; every code step shows full code; the unchanged parts of the writer/reader loops are shown in context.
- **Type consistency:** `ExtHeader{Offset,MessageID,Subject,To,From,WhenWritten}`, `encodeHeadersDAT`/`parseHeadersDAT`, `extHeadersFor(msgs, offsets, bbsID)`, `parseREPMessages(data, headers)` and the `writeMessagesDAT` 4-value return are used consistently across tasks. Offsets: write `(currentBlock-1)*128` / `msgBuf.Len()`, read `pos` — both the message-header start, asserted by the round-trip at offset 128.
