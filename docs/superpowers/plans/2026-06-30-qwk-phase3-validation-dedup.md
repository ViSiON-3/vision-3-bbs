# QWK Phase 3 — REP First-Block Validation & Import Deduplication — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reject REP uploads addressed to another BBS, and make uploads retry-safe by deduplicating identical packets, returning a result that distinguishes posted / skipped / duplicate.

**Architecture:** The codec (`internal/qwk`) gains a packet-level reader that exposes the first-block BBS ID and the raw `.MSG` payload; `WriteREP` emits the BBS ID so packets round-trip. The service (`internal/qwkservice`) validates the destination (lenient) and atomically claims a SHA-256 fingerprint of the payload in a SQLite store (`data/qwk_dedup.db`) before posting. The menu surfaces wrong-BBS and duplicate outcomes.

**Tech Stack:** Go (stdlib `archive/zip`, `crypto/sha256`, `database/sql`); `modernc.org/sqlite` (already a direct, cgo-free dependency, used by `internal/v3net/dedup`).

## Global Constraints

- Go files under 300 lines; no new dependencies (`modernc.org/sqlite` already present); use `slog` for logging.
- TDD: failing test first → run it red → minimal implementation → run it green → commit.
- Reserved/known values: QWK BBS-ID max length is 8 chars; `BlockSize = 128`; dedup DB at `data/qwk_dedup.db`; dedup retention prune = 90 days.
- Validation is **lenient**: reject only when the first block carries an ID that does not match; accept an empty/absent ID.
- Dedup is **claim-before-post** and keyed on `(handle, sha256(payload))`; fingerprint is over the `.MSG` payload bytes, not the zip.
- Spec: `docs/superpowers/specs/2026-06-30-qwk-phase3-validation-dedup-design.md`.
- Mirror the existing SQLite idiom in `internal/v3net/dedup/dedup.go` (`SetMaxOpenConns(1)`, `PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000`, `INSERT OR IGNORE`).

---

## File Structure

- Modify: `internal/qwk/reader.go` — add `REPPacket`, `ReadREPPacket`, `firstBlockID`; make `ReadREP` a wrapper.
- Create: `internal/qwk/rep_packet_test.go` — tests for `firstBlockID` and `ReadREPPacket`.
- Modify: `internal/qwk/rep_writer.go` — write the BBS ID into the first block.
- Modify: `internal/qwk/rep_writer_test.go` — BBS-ID round-trip test.
- Create: `internal/qwkservice/dedup.go` — SQLite dedup store.
- Create: `internal/qwkservice/dedup_test.go` — dedup store tests.
- Modify: `internal/qwkservice/service.go` — `Service.dedupPath` + `New`.
- Modify: `internal/qwkservice/service_import.go` — `ImportResult.Duplicate`, `ErrWrongBBS`, `fingerprint`, validation + dedup in `ImportREP`.
- Modify: `internal/qwkservice/service_import_test.go` — wrong-BBS, duplicate, per-handle, persistence tests.
- Modify: `internal/menu/qwk_handler.go` — wrong-BBS + duplicate display branches.
- Modify: `docs/sysop/messages/qwk.md` — document validation + dedup.

---

## Task 1: Packet-level REP reader (first-block BBS ID + payload)

**Files:**
- Modify: `internal/qwk/reader.go`
- Test: `internal/qwk/rep_packet_test.go` (create)

**Interfaces:**
- Consumes: existing `parseREPMessages([]byte) ([]REPMessage, error)`, `BlockSize`, `formatMessage`.
- Produces:
  - `type REPPacket struct { BBSID string; Messages []REPMessage; Payload []byte }`
  - `func ReadREPPacket(r io.ReaderAt, size int64, bbsID string) (*REPPacket, error)`
  - `func firstBlockID(data []byte) string` (unexported)
  - `ReadREP` retained, now delegating to `ReadREPPacket`.

- [ ] **Step 1: Write the failing tests**

Create `internal/qwk/rep_packet_test.go`:

```go
package qwk

import (
	"archive/zip"
	"bytes"
	"testing"
	"time"
)

func TestFirstBlockID(t *testing.T) {
	mk := func(s string) []byte {
		b := make([]byte, BlockSize)
		for i := range b {
			b[i] = ' '
		}
		copy(b, s)
		return b
	}
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"plain id", mk("VISION3"), "VISION3"},
		{"lowercased", mk("vision3"), "VISION3"},
		{"all spaces", mk(""), ""},
		{"leading spaces", mk("   ABC"), "ABC"},
		{"too long capped at 8", mk("LONGERNAME123"), "LONGERNA"},
		{"short input", []byte("xx"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstBlockID(tt.in); got != tt.want {
				t.Errorf("firstBlockID = %q, want %q", got, tt.want)
			}
		})
	}
}

// buildREPZip writes a zip containing <name> whose .MSG content is the given
// first block followed by one formatted message.
func buildREPZip(t *testing.T, name string, firstBlock []byte) []byte {
	t.Helper()
	var msgBuf bytes.Buffer
	msgBuf.Write(firstBlock)
	m := formatMessage(PacketMessage{
		Conference: 1, Number: 1, To: "SysOp", Subject: "Hi",
		DateTime: time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC), Body: "reply",
	})
	numBlocks := (len(m) + BlockSize - 1) / BlockSize
	padded := make([]byte, numBlocks*BlockSize)
	for i := range padded {
		padded[i] = ' '
	}
	copy(padded, m)
	msgBuf.Write(padded)

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(msgBuf.Bytes())
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return zipBuf.Bytes()
}

func TestReadREPPacket_ExtractsBBSIDAndPayload(t *testing.T) {
	first := make([]byte, BlockSize)
	for i := range first {
		first[i] = ' '
	}
	copy(first, "VISION3")
	data := buildREPZip(t, "VISION3.MSG", first)

	p, err := ReadREPPacket(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err != nil {
		t.Fatalf("ReadREPPacket: %v", err)
	}
	if p.BBSID != "VISION3" {
		t.Errorf("BBSID = %q, want VISION3", p.BBSID)
	}
	if len(p.Messages) != 1 {
		t.Errorf("messages = %d, want 1", len(p.Messages))
	}
	if len(p.Payload) == 0 {
		t.Error("payload should be non-empty")
	}
}

func TestReadREP_StillReturnsMessages(t *testing.T) {
	first := make([]byte, BlockSize)
	for i := range first {
		first[i] = ' '
	}
	data := buildREPZip(t, "VISION3.MSG", first)

	msgs, err := ReadREP(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err != nil {
		t.Fatalf("ReadREP: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("messages = %d, want 1", len(msgs))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/qwk/ -run 'TestFirstBlockID|TestReadREPPacket|TestReadREP_StillReturnsMessages' -v`
Expected: FAIL — `undefined: firstBlockID`, `undefined: ReadREPPacket`.

- [ ] **Step 3: Implement in `internal/qwk/reader.go`**

Add the type and packet reader, and refactor `ReadREP`. Replace the existing
`ReadREP` function (currently `func ReadREP(r io.ReaderAt, size int64, bbsID string) ([]REPMessage, error)` and its body that opens the zip, finds `<bbsID>.MSG`, reads it, and calls `parseREPMessages`) with the following:

```go
// REPPacket is a parsed REP upload: the destination BBS ID found in the first
// block, the reply messages, and the raw .MSG payload (used for fingerprinting).
type REPPacket struct {
	BBSID    string
	Messages []REPMessage
	Payload  []byte
}

// ReadREP extracts messages from a QWK REP packet (ZIP archive). It is a thin
// wrapper over ReadREPPacket retained for callers that only need the messages.
func ReadREP(r io.ReaderAt, size int64, bbsID string) ([]REPMessage, error) {
	p, err := ReadREPPacket(r, size, bbsID)
	if err != nil {
		return nil, err
	}
	return p.Messages, nil
}

// ReadREPPacket reads a REP packet's <bbsID>.MSG payload and returns the
// first-block BBS ID, the parsed messages, and the raw payload bytes.
func ReadREPPacket(r io.ReaderAt, size int64, bbsID string) (*REPPacket, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("failed to open REP archive: %w", err)
	}

	msgFileName := strings.ToUpper(bbsID) + ".MSG"
	var msgFile *zip.File
	for _, f := range zr.File {
		if strings.EqualFold(f.Name, msgFileName) {
			msgFile = f
			break
		}
	}
	if msgFile == nil {
		return nil, fmt.Errorf("REP packet missing %s", msgFileName)
	}

	rc, err := msgFile.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", msgFileName, err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", msgFileName, err)
	}

	msgs, err := parseREPMessages(data)
	if err != nil {
		return nil, err
	}
	return &REPPacket{BBSID: firstBlockID(data), Messages: msgs, Payload: data}, nil
}

// firstBlockID extracts the BBS ID from the first 128-byte block of a REP
// payload: the leading whitespace-delimited token, upper-cased and capped at the
// 8-character QWK BBS-ID length. Returns "" when the block is blank or absent.
func firstBlockID(data []byte) string {
	if len(data) < BlockSize {
		return ""
	}
	block := data[:BlockSize]
	start := 0
	for start < len(block) && block[start] == ' ' {
		start++
	}
	end := start
	for end < len(block) && block[end] != ' ' && end-start < 8 {
		end++
	}
	return strings.ToUpper(string(block[start:end]))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/qwk/ -run 'TestFirstBlockID|TestReadREPPacket|TestReadREP' -v`
Expected: PASS — new tests plus the existing `TestReadREP_*` (the wrapper preserves behaviour).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/qwk/reader.go internal/qwk/rep_packet_test.go
git add internal/qwk/reader.go internal/qwk/rep_packet_test.go
git commit -m "feat(qwk): expose REP first-block BBS ID and payload via ReadREPPacket"
```

---

## Task 2: WriteREP emits the BBS ID in the first block

**Files:**
- Modify: `internal/qwk/rep_writer.go`
- Test: `internal/qwk/rep_writer_test.go`

**Interfaces:**
- Consumes: `ReadREPPacket` (Task 1).
- Produces: `WriteREP` packets whose first block carries the upper-cased BBS ID.

Note: the committed fixture `internal/qwk/testdata/vision3/VISION3.REP` was
generated before this change, so its first block reads "Produced by ViSiON/3
BBS". No test asserts that fixture's first block (only `ReadREP` message
extraction, which ignores it), so it is left as-is and is harmless.

- [ ] **Step 1: Write the failing test**

Add to `internal/qwk/rep_writer_test.go`:

```go
func TestWriteREP_EmitsBBSIDInFirstBlock(t *testing.T) {
	data := buildREP(t, "vision3", []PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Hi",
			DateTime: time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC), Body: "reply"},
	})

	p, err := ReadREPPacket(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err != nil {
		t.Fatalf("ReadREPPacket: %v", err)
	}
	if p.BBSID != "VISION3" {
		t.Errorf("first-block BBSID = %q, want VISION3", p.BBSID)
	}
}
```

(`buildREP` already exists in `rep_writer_test.go` and calls `WriteREP`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/qwk/ -run TestWriteREP_EmitsBBSIDInFirstBlock -v`
Expected: FAIL — `BBSID = "PRODUCED"` (the current spacer text), want `VISION3`.

- [ ] **Step 3: Implement the change**

In `internal/qwk/rep_writer.go`, replace the spacer-text line. The current code is:

```go
	copy(spacer, "Produced by ViSiON/3 BBS")
```

Replace it with:

```go
	// First block carries the destination BBS ID; readers validate it.
	copy(spacer, strings.ToUpper(bbsID))
```

(`strings` is already imported in `rep_writer.go`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/qwk/ -v 2>&1 | tail -5`
Expected: PASS — the new test plus all existing `internal/qwk` tests (none assert the REP spacer text).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/qwk/rep_writer.go internal/qwk/rep_writer_test.go
git add internal/qwk/rep_writer.go internal/qwk/rep_writer_test.go
git commit -m "feat(qwk): write destination BBS ID into REP first block"
```

---

## Task 3: SQLite dedup store

**Files:**
- Create: `internal/qwkservice/dedup.go`
- Test: `internal/qwkservice/dedup_test.go`

**Interfaces:**
- Produces:
  - `func openREPDedup(path string) (*repDedup, error)`
  - `func (d *repDedup) Close() error`
  - `func (d *repDedup) RecordIfNew(handle, hash string) (bool, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/qwkservice/dedup_test.go`:

```go
package qwkservice

import (
	"path/filepath"
	"testing"
)

func TestREPDedup_RecordIfNew(t *testing.T) {
	d, err := openREPDedup(filepath.Join(t.TempDir(), "dedup.db"))
	if err != nil {
		t.Fatalf("openREPDedup: %v", err)
	}
	defer d.Close()

	isNew, err := d.RecordIfNew("tester", "hash1")
	if err != nil {
		t.Fatal(err)
	}
	if !isNew {
		t.Error("first record should be new")
	}

	isNew, err = d.RecordIfNew("tester", "hash1")
	if err != nil {
		t.Fatal(err)
	}
	if isNew {
		t.Error("second identical record should be a duplicate")
	}
}

func TestREPDedup_PerHandleIsolation(t *testing.T) {
	d, err := openREPDedup(filepath.Join(t.TempDir(), "dedup.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if _, err := d.RecordIfNew("alice", "h"); err != nil {
		t.Fatal(err)
	}
	isNew, err := d.RecordIfNew("bob", "h") // same hash, different handle
	if err != nil {
		t.Fatal(err)
	}
	if !isNew {
		t.Error("same hash under a different handle must not be a duplicate")
	}
}

func TestREPDedup_PersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dedup.db")
	d1, err := openREPDedup(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d1.RecordIfNew("tester", "h"); err != nil {
		t.Fatal(err)
	}
	d1.Close()

	d2, err := openREPDedup(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	isNew, err := d2.RecordIfNew("tester", "h")
	if err != nil {
		t.Fatal(err)
	}
	if isNew {
		t.Error("record should persist across reopen")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/qwkservice/ -run TestREPDedup -v`
Expected: FAIL — `undefined: openREPDedup`.

- [ ] **Step 3: Implement `internal/qwkservice/dedup.go`**

```go
package qwkservice

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const repDedupSchema = `
CREATE TABLE IF NOT EXISTS rep_uploads (
	handle   TEXT NOT NULL,
	rep_hash TEXT NOT NULL,
	seen_at  DATETIME DEFAULT (datetime('now')),
	PRIMARY KEY (handle, rep_hash)
);`

// repDedup is a SQLite-backed store of recently imported REP fingerprints,
// keyed by (uploader handle, payload hash), used to make uploads retry-safe.
type repDedup struct {
	db *sql.DB
}

// openREPDedup opens or creates the dedup database, configures it for safe
// concurrent access, and prunes records older than the retention window.
func openREPDedup(path string) (*repDedup, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("qwk dedup: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("qwk dedup: configure pragmas: %w", err)
	}
	if _, err := db.Exec(repDedupSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("qwk dedup: create schema: %w", err)
	}
	if _, err := db.Exec("DELETE FROM rep_uploads WHERE seen_at < datetime('now','-90 days')"); err != nil {
		db.Close()
		return nil, fmt.Errorf("qwk dedup: prune: %w", err)
	}
	return &repDedup{db: db}, nil
}

// Close closes the underlying database.
func (d *repDedup) Close() error {
	return d.db.Close()
}

// RecordIfNew atomically records (handle, hash). It returns true when the row
// was newly inserted and false when it already existed (a duplicate upload).
func (d *repDedup) RecordIfNew(handle, hash string) (bool, error) {
	res, err := d.db.Exec(
		"INSERT OR IGNORE INTO rep_uploads (handle, rep_hash) VALUES (?, ?)",
		handle, hash,
	)
	if err != nil {
		return false, fmt.Errorf("qwk dedup: record: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("qwk dedup: rows affected: %w", err)
	}
	return n == 1, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/qwkservice/ -run TestREPDedup -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/qwkservice/dedup.go internal/qwkservice/dedup_test.go
git add internal/qwkservice/dedup.go internal/qwkservice/dedup_test.go
git commit -m "feat(qwk): add SQLite REP upload dedup store"
```

---

## Task 4: Validation + dedup in ImportREP

**Files:**
- Modify: `internal/qwkservice/service.go` (add `dedupPath` field, set in `New`)
- Modify: `internal/qwkservice/service_import.go` (`ImportResult.Duplicate`, `ErrWrongBBS`, `fingerprint`, `ImportREP` head)
- Test: `internal/qwkservice/service_import_test.go`

**Interfaces:**
- Consumes: `ReadREPPacket` (Task 1), `openREPDedup`/`RecordIfNew` (Task 3).
- Produces:
  - `var ErrWrongBBS error`
  - `ImportResult` field `Duplicate int`
  - `func fingerprint([]byte) string` (unexported)
  - `Service` field `dedupPath string`

- [ ] **Step 1: Add `dedupPath` to the Service**

In `internal/qwkservice/service.go`, add the field to `Service` (after `confMapPath string`):

```go
	dedupPath   string
```

And set it in `New` (after the `confMapPath:` line):

```go
		dedupPath:   filepath.Join(dataPath, "qwk_dedup.db"),
```

- [ ] **Step 2: Write the failing tests**

Add to `internal/qwkservice/service_import_test.go` (imports `bytes`, `archive/zip`, `errors` may need adding alongside the existing `fmt`, `os`, `path/filepath`, `testing`, `time`, `message`, `qwk`):

```go
func TestImportREP_WrongBBSRejected(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	// REP built for a different BBS ID; the service's bbsID is VISION3.
	rep := makeREP(t, "OTHERBBS", []qwk.PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "x"},
	})

	svc := newTestService(t, store)
	_, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if !errors.Is(err, ErrWrongBBS) {
		t.Fatalf("want ErrWrongBBS, got %v", err)
	}
	if len(store.posted) != 0 {
		t.Error("nothing should be posted for a wrong-BBS packet")
	}
}

func TestImportREP_EmptyFirstBlockAccepted(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})

	// Build a REP whose first block is blank, but whose .MSG filename matches.
	first := make([]byte, 128)
	for i := range first {
		first[i] = ' '
	}
	rep := buildBlankFirstBlockREP(t, "VISION3", first)

	svc := newTestService(t, store)
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("blank first block should be accepted: %v", err)
	}
	if res.Posted != 1 {
		t.Errorf("want posted=1, got %+v", res)
	}
}

func TestImportREP_DuplicateNotReposted(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "x"},
	})

	svc := newTestService(t, store)
	first, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Posted != 1 {
		t.Fatalf("first import: want posted=1, got %+v", first)
	}

	second, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Duplicate != 1 || second.Posted != 0 {
		t.Errorf("second import: want duplicate=1 posted=0, got %+v", second)
	}
	if len(store.posted) != 1 {
		t.Errorf("duplicate upload must not double-post: posts=%d", len(store.posted))
	}
}

func TestImportREP_DuplicatePerHandle(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Hi", DateTime: time.Now(), Body: "x"},
	})

	svc := newTestService(t, store)
	if _, err := svc.ImportREP(rep, ImportOptions{Handle: "alice"}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Posted != 1 || res.Duplicate != 0 {
		t.Errorf("a different handle must not be a duplicate: %+v", res)
	}
}

// buildBlankFirstBlockREP builds a REP zip whose <bbsID>.MSG has the given first
// block followed by one message, bypassing WriteREP (which writes the BBS ID).
func buildBlankFirstBlockREP(t *testing.T, bbsID string, firstBlock []byte) []byte {
	t.Helper()
	var msgBuf bytes.Buffer
	msgBuf.Write(firstBlock)
	// One minimal message block: set block-count=1 at 116-121, conference=1.
	blk := make([]byte, 128)
	for i := range blk {
		blk[i] = ' '
	}
	copy(blk[116:122], []byte("     1"))
	blk[123] = 1
	msgBuf.Write(blk)

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	w, err := zw.Create(bbsID + ".MSG")
	if err != nil {
		t.Fatal(err)
	}
	w.Write(msgBuf.Bytes())
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return zipBuf.Bytes()
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/qwkservice/ -run 'TestImportREP_(WrongBBS|EmptyFirstBlock|Duplicate)' -v`
Expected: FAIL — `undefined: ErrWrongBBS`, and `ImportResult` has no `Duplicate` field.

- [ ] **Step 4: Implement in `internal/qwkservice/service_import.go`**

Update the import block to include the new stdlib packages:

```go
import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)
```

Add the sentinel error and the `Duplicate` field. Replace the `ImportResult`
struct with:

```go
// ImportResult summarizes a REP import.
type ImportResult struct {
	Posted    int
	Skipped   int
	Duplicate int
}

// ErrWrongBBS is returned by ImportREP when a REP packet's first-block BBS ID
// is present and does not match this system's ID.
var ErrWrongBBS = errors.New("qwk: REP packet addressed to another BBS")
```

Replace the head of `ImportREP` — the current lines from the function signature
through `res := &ImportResult{}` (the `qwk.ReadREP(...)` call, the `loadConfMap`
call, and the `res` initialization) — with:

```go
func (s *Service) ImportREP(data []byte, opts ImportOptions) (*ImportResult, error) {
	packet, err := qwk.ReadREPPacket(bytes.NewReader(data), int64(len(data)), s.bbsID)
	if err != nil {
		return nil, err
	}

	// Lenient destination check: reject only a present, mismatching ID.
	if packet.BBSID != "" && !strings.EqualFold(packet.BBSID, s.bbsID) {
		return nil, fmt.Errorf("%w: packet for %q, this is %q", ErrWrongBBS, packet.BBSID, s.bbsID)
	}

	// Atomically claim this packet's fingerprint before posting so a retried or
	// concurrent identical upload is detected as a duplicate.
	dedup, err := openREPDedup(s.dedupPath)
	if err != nil {
		return nil, err
	}
	defer dedup.Close()
	isNew, err := dedup.RecordIfNew(opts.Handle, fingerprint(packet.Payload))
	if err != nil {
		return nil, err
	}
	if !isNew {
		return &ImportResult{Duplicate: len(packet.Messages)}, nil
	}

	cm, err := s.loadConfMap()
	if err != nil {
		return nil, err
	}

	msgs := packet.Messages
	res := &ImportResult{}
```

Then change the existing posting loop header from `for _, msg := range msgs {`
(it already iterates a variable named `msgs`, now assigned from
`packet.Messages`) — no change needed if the loop already says `for _, msg :=
range msgs`. Leave the loop body and the trailing `return res, nil` unchanged.

Add `"strings"` to the import block (used by `strings.EqualFold`). Final import
block:

```go
import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/qwk"
)
```

Add the fingerprint helper at the end of the file:

```go
// fingerprint returns the hex SHA-256 of a REP payload, used as the dedup key.
func fingerprint(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/qwkservice/ -run 'TestImportREP' -v`
Expected: PASS — the new tests plus all pre-existing `TestImportREP_*` (they build packets with `makeREP(t, "VISION3", ...)`, whose first block now matches the service's `VISION3` ID, so validation passes and dedup sees each unique payload once).

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/qwkservice/service.go internal/qwkservice/service_import.go internal/qwkservice/service_import_test.go
go vet ./internal/qwkservice/
git add internal/qwkservice/service.go internal/qwkservice/service_import.go internal/qwkservice/service_import_test.go
git commit -m "feat(qwk): validate REP destination and dedup uploads in ImportREP"
```

---

## Task 5: Menu surfaces wrong-BBS and duplicate outcomes

**Files:**
- Modify: `internal/menu/qwk_handler.go`

**Interfaces:**
- Consumes: `qwkservice.ErrWrongBBS`, `ImportResult.Duplicate` (Task 4).

- [ ] **Step 1: Update the import-result handling in `runQWKUpload`**

Find the block that calls `svc.ImportREP(...)` and handles its error/result. The
current error branch is:

```go
	if err != nil {
		slog.Error("failed to parse REP", "node", nodeNumber, "error", err)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Error reading REP packet.|07\r\n")), outputMode)
		time.Sleep(2 * time.Second)
		return currentUser, "", nil
	}
```

Replace it with a branch that distinguishes a wrong-BBS rejection:

```go
	if err != nil {
		if errors.Is(err, qwkservice.ErrWrongBBS) {
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01This REP packet is addressed to another BBS.|07\r\n")), outputMode)
			time.Sleep(2 * time.Second)
			return currentUser, "", nil
		}
		slog.Error("failed to parse REP", "node", nodeNumber, "error", err)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|01Error reading REP packet.|07\r\n")), outputMode)
		time.Sleep(2 * time.Second)
		return currentUser, "", nil
	}
```

Then, immediately after the existing `if importRes.Posted+importRes.Skipped == 0`
"no messages" branch, add a duplicate branch (before the `posted := importRes.Posted`
line):

```go
	if importRes.Duplicate > 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|07This packet was already uploaded — nothing posted.|07\r\n")), outputMode)
		time.Sleep(2 * time.Second)
		return currentUser, "", nil
	}
```

(`errors` and `qwkservice` are already imported in this file.)

- [ ] **Step 2: Build and vet**

Run: `go build ./internal/menu/ && go vet ./internal/menu/`
Expected: success, no issues. (These UI branches are thin terminal output; they
are verified by build + the unchanged `internal/menu` test suite.)

- [ ] **Step 3: Run the menu package tests**

Run: `go test ./internal/menu/ 2>&1 | tail -3`
Expected: PASS (existing tests unaffected).

- [ ] **Step 4: Commit**

```bash
gofmt -w internal/menu/qwk_handler.go
git add internal/menu/qwk_handler.go
git commit -m "feat(qwk): show wrong-BBS and duplicate-upload messages on REP upload"
```

---

## Task 6: Sysop docs + full verification

**Files:**
- Modify: `docs/sysop/messages/qwk.md`

- [ ] **Step 1: Read the upload section to find the insertion point**

Run: `sed -n '40,92p' docs/sysop/messages/qwk.md`
Expected: locate the REP-upload / ACS paragraph to place a new note after.

- [ ] **Step 2: Add the documentation**

Insert a subsection (adapt heading level to the file's style):

```markdown
## Upload safety: destination check and duplicate detection

When a `.REP` is uploaded, ViSiON/3 reads the destination BBS ID from the
packet's first block. If that ID is present and does not match this system, the
packet is rejected as addressed to another BBS and nothing is posted. Packets
that omit the ID (some readers leave it blank) are accepted.

Uploads are also de-duplicated: ViSiON/3 records a fingerprint of each imported
packet in `data/qwk_dedup.db` (a small SQLite database, created automatically).
Re-uploading the exact same `.REP` — for example after a dropped mobile
connection — posts nothing the second time and reports the packet as already
uploaded. Fingerprints are kept per user and pruned after 90 days.
```

- [ ] **Step 3: Commit the docs**

```bash
git add docs/sysop/messages/qwk.md
git commit -m "docs(qwk): document REP destination check and upload dedup"
```

- [ ] **Step 4: Full verification**

Run: `gofmt -l internal/qwk internal/qwkservice internal/menu`
Expected: no output.

Run: `go vet ./... 2>&1 | tail -5`
Expected: no issues.

Run: `go test ./... 2>&1 | tail -10`
Expected: all packages PASS.

Run: `go test -race ./internal/qwk ./internal/qwkservice 2>&1 | tail -5`
Expected: PASS, no race warnings.

Run: `wc -l internal/qwkservice/*.go internal/qwk/reader.go`
Expected: every file under 300 lines.

- [ ] **Step 5: Final commit (only if cleanup was needed)**

```bash
git add -A
git commit -m "chore(qwk): Phase 3 cleanup and verification"
```

---

## Self-Review (completed by plan author)

- **Spec coverage:** first-block extraction → Task 1; WriteREP parity → Task 2; SQLite dedup store → Task 3; lenient validation + claim-before-post dedup + `Duplicate` field + `ErrWrongBBS` → Task 4; menu surfacing → Task 5; docs + verification → Task 6. All spec sections covered.
- **Placeholder scan:** no TBD/TODO; every code step shows full code; the unchanged posting loop is referenced explicitly, not elided.
- **Type consistency:** `REPPacket{BBSID,Messages,Payload}`, `ReadREPPacket`, `firstBlockID`, `openREPDedup`/`RecordIfNew`, `fingerprint`, `ErrWrongBBS`, `ImportResult.Duplicate`, and `Service.dedupPath` are used consistently across tasks. `RecordIfNew(handle, hash) (bool, error)` matches its callers; `ImportREP` keeps its `(data []byte, opts ImportOptions)` signature.
