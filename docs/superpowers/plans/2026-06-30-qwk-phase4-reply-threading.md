# QWK Phase 4 — Reply Threading in Packets — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Round-trip a message's parent pointer through the base-QWK reference field (header positions 108–115), so reply chains survive QWK download and REP upload.

**Architecture:** The codec carries the reference: `PacketMessage`/`REPMessage` gain `ReplyToNumber`, `formatMessage` writes it, `parseREPMessages` reads it. The service copies it on export (`BuildPacket`) and, on import, posts via `AddReply`/`AddPrivateReply` with the parsed number (`replyToNum == 0` reproduces the old non-reply post).

**Tech Stack:** Go (stdlib `archive/zip`, `strconv`, `testing`); existing `internal/qwk`, `internal/qwkservice`, `internal/message`.

## Global Constraints

- No new dependencies; `slog` for logging; Go files under 300 lines.
- TDD: failing test first → red → minimal implementation → green → commit.
- Reference field = header positions 108–115 (8 chars); the value is the parent's QWK message number, which equals its local JAM `MsgNum` in our export. `ReplyToNumber == 0` means "no parent" and writes the same `"       0"` bytes as today (backward compatible).
- Import uses server-receive time (no authored-date preservation) and sets the reference as-is (no validation / degraded-thread reporting).
- `AddReply`/`AddPrivateReply` with `replyToNum == 0` behave exactly like `AddMessage`/`AddPrivateMessage` (the manager's `replyToNum > 0` guard skips the pointer).
- Spec: `docs/superpowers/specs/2026-06-30-qwk-phase4-reply-threading-design.md`.

---

## File Structure

- Modify: `internal/qwk/types.go` — `PacketMessage.ReplyToNumber`, `REPMessage.ReplyToNumber`.
- Modify: `internal/qwk/writer.go` — `formatMessage` writes the reference.
- Modify: `internal/qwk/reader.go` — `parseREPMessages` reads the reference.
- Modify: `internal/qwk/rep_writer_test.go` / `internal/qwk/writer_test.go` — codec tests.
- Modify: `internal/qwkservice/service.go` — `BuildPacket` copy; `MessageStore` swap.
- Modify: `internal/qwkservice/service_import.go` — route via `AddReply`/`AddPrivateReply`.
- Modify: `internal/qwkservice/fakestore_test.go` — `AddReply`/`AddPrivateReply` + `replyToNum`.
- Modify: `internal/qwkservice/service_export_test.go`, `service_import_test.go` — tests.
- Modify: `docs/sysop/messages/qwk.md` — one-line note.

---

## Task 1: Codec carries the reference field

**Files:**
- Modify: `internal/qwk/types.go`, `internal/qwk/writer.go`, `internal/qwk/reader.go`
- Test: `internal/qwk/rep_writer_test.go` (add), `internal/qwk/writer_test.go` (add)

**Interfaces:**
- Produces: `PacketMessage.ReplyToNumber int`, `REPMessage.ReplyToNumber int`; `formatMessage` writes/`parseREPMessages` reads positions 108–115.

- [ ] **Step 1: Write the failing tests**

Add to `internal/qwk/writer_test.go`:

```go
func TestFormatMessage_ReplyToNumber(t *testing.T) {
	data := formatMessage(PacketMessage{
		Conference: 1, Number: 2, To: "All", Subject: "Re",
		DateTime: time.Date(2026, 3, 5, 14, 30, 0, 0, time.UTC), Body: "x",
		ReplyToNumber: 7,
	})
	ref := strings.TrimSpace(string(data[108:116]))
	if ref != "7" {
		t.Errorf("reference field (108-115): want 7, got %q", ref)
	}
}
```

Add to `internal/qwk/rep_writer_test.go`:

```go
func TestREP_RoundTripReplyToNumber(t *testing.T) {
	in := []PacketMessage{
		{Conference: 1, Number: 1, From: "a", To: "b", Subject: "First",
			DateTime: time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC), Body: "root"},
		{Conference: 1, Number: 2, From: "a", To: "b", Subject: "Re: First",
			DateTime: time.Date(2026, 3, 5, 11, 0, 0, 0, time.UTC), Body: "reply",
			ReplyToNumber: 1},
	}
	data := buildREP(t, "VISION3", in)
	out, err := ReadREP(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err != nil {
		t.Fatalf("ReadREP: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 messages, got %d", len(out))
	}
	if out[0].ReplyToNumber != 0 {
		t.Errorf("root ReplyToNumber: want 0, got %d", out[0].ReplyToNumber)
	}
	if out[1].ReplyToNumber != 1 {
		t.Errorf("reply ReplyToNumber: want 1, got %d", out[1].ReplyToNumber)
	}
}
```

(`buildREP` already exists in `rep_writer_test.go`; `strings`/`time` are already imported in `writer_test.go`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/qwk/ -run 'TestFormatMessage_ReplyToNumber|TestREP_RoundTripReplyToNumber' -v`
Expected: FAIL — `ReplyToNumber` is not a field of `PacketMessage`/`REPMessage` (compile error).

- [ ] **Step 3: Add the struct fields**

In `internal/qwk/types.go`, add to `PacketMessage` (after `Private bool`):

```go
	ReplyToNumber int // QWK reference: parent message number (0 = none)
```

And to `REPMessage` (after `Body string`):

```go
	ReplyToNumber int // QWK reference: parent message number (0 = none)
```

- [ ] **Step 4: Write the reference in formatMessage**

In `internal/qwk/writer.go`, replace the hardcoded reference line (currently
`copyPadded(header[108:116], fmt.Sprintf("%8d", 0), 8)`):

```go
	// Reference number: positions 108-115 (8 chars) — parent message number.
	copyPadded(header[108:116], fmt.Sprintf("%8d", msg.ReplyToNumber), 8)
```

- [ ] **Step 5: Parse the reference in parseREPMessages**

In `internal/qwk/reader.go`, in the per-message loop, parse the reference
alongside the other fields and include it in the `REPMessage`. After the
`subject := ...` line, add:

```go
		refStr := strings.TrimSpace(string(header[108:116]))
		refNum, _ := strconv.Atoi(refStr) // 0 / unparsable => no parent
```

and add the field to the appended struct:

```go
		messages = append(messages, REPMessage{
			Conference:    confNum,
			To:            to,
			Subject:       subject,
			Body:          body,
			ReplyToNumber: refNum,
		})
```

(`strconv` and `strings` are already imported in `reader.go`.)

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/qwk/ 2>&1 | tail -5`
Expected: PASS — the two new tests plus all existing `internal/qwk` tests.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/qwk/types.go internal/qwk/writer.go internal/qwk/reader.go internal/qwk/writer_test.go internal/qwk/rep_writer_test.go
go vet ./internal/qwk/
git add internal/qwk/
git commit -m "feat(qwk): carry parent message number in the QWK reference field"
```

---

## Task 2: Service exports + imports the reference

**Files:**
- Modify: `internal/qwkservice/service.go` (`BuildPacket` copy; `MessageStore` swap)
- Modify: `internal/qwkservice/service_import.go` (route via reply methods)
- Modify: `internal/qwkservice/fakestore_test.go`
- Test: `internal/qwkservice/service_export_test.go`, `internal/qwkservice/service_import_test.go`

**Interfaces:**
- Consumes: `PacketMessage.ReplyToNumber`, `REPMessage.ReplyToNumber` (Task 1); `MessageManager.AddReply`/`AddPrivateReply` (already implemented).
- Produces: `MessageStore` declares `AddReply`/`AddPrivateReply` (replacing `AddMessage`/`AddPrivateMessage`).

- [ ] **Step 1: Update the fake store and write the failing tests**

In `internal/qwkservice/fakestore_test.go`, add `replyToNum` to the record and
replace the two `Add*` fakes with reply variants:

```go
type postedMessage struct {
	areaID                       int
	from, to, subject, body, rep string
	replyToNum                   int
}
```

```go
func (f *fakeStore) AddReply(areaID int, from, to, subject, body, replyToMsgID string, replyToNum int) (int, error) {
	if err := f.addErr[areaID]; err != nil {
		return 0, err
	}
	f.posted = append(f.posted, postedMessage{areaID, from, to, subject, body, replyToMsgID, replyToNum})
	return len(f.posted), nil
}

func (f *fakeStore) AddPrivateReply(areaID int, from, to, subject, body, replyToMsgID string, replyToNum int) (int, error) {
	if err := f.addErr[areaID]; err != nil {
		return 0, err
	}
	f.privPosted = append(f.privPosted, postedMessage{areaID, from, to, subject, body, replyToMsgID, replyToNum})
	return len(f.privPosted), nil
}
```

(Delete the old `AddMessage`/`AddPrivateMessage` fake methods. Existing import
tests assert on `store.posted`/`store.privPosted` lengths and the `from`/`to`/
`body` fields, which are unchanged.)

Add the export test to `internal/qwkservice/service_export_test.go` (add `io`,
`strconv`, `strings` to its imports):

```go
// firstMsgReplyRef parses the QWK reference field (positions 108-115) of the
// first message in a packet's MESSAGES.DAT.
func firstMsgReplyRef(t *testing.T, packet []byte) int {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(packet), int64(len(packet)))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != "MESSAGES.DAT" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		// Block 1 is the spacer; the first message header starts at offset 128.
		hdr := data[128:256]
		ref, _ := strconv.Atoi(strings.TrimSpace(string(hdr[108:116])))
		return ref
	}
	t.Fatal("MESSAGES.DAT not found")
	return 0
}

func TestBuildPacket_WritesReplyReference(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	m := dm(1, "a", "All", "s", "b")
	m.ReplyToNum = 7
	store.seed(1, m)

	svc := newTestService(t, store)
	res, err := svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"GENERAL"}})
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	if got := firstMsgReplyRef(t, res.Packet); got != 7 {
		t.Errorf("exported reply reference: want 7, got %d", got)
	}
}
```

Add the import tests to `internal/qwkservice/service_import_test.go`:

```go
func TestImportREP_SetsReplyPointer(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 1, Tag: "GENERAL", Name: "General"})
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Re", DateTime: time.Now(), Body: "reply", ReplyToNumber: 5},
	})

	svc := newTestService(t, store)
	if _, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"}); err != nil {
		t.Fatal(err)
	}
	if len(store.posted) != 1 || store.posted[0].replyToNum != 5 {
		t.Errorf("want AddReply with replyToNum 5, got %+v", store.posted)
	}
}

func TestImportREP_PrivateReplyPointer(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 3, Tag: "PRIVMAIL", Name: "Private Mail"})
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 0, Number: 1, To: "friend", Subject: "Re", DateTime: time.Now(), Body: "x", ReplyToNumber: 4},
	})

	svc := newTestService(t, store)
	if _, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"}); err != nil {
		t.Fatal(err)
	}
	if len(store.privPosted) != 1 || store.privPosted[0].replyToNum != 4 {
		t.Errorf("want AddPrivateReply with replyToNum 4, got %+v", store.privPosted)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/qwkservice/ -run 'TestBuildPacket_WritesReplyReference|TestImportREP_SetsReplyPointer|TestImportREP_PrivateReplyPointer' -v`
Expected: FAIL — `MessageStore` has no `AddReply`/`AddPrivateReply` and `BuildPacket` does not set the reference, so the fake no longer satisfies the interface / the assertions fail.

- [ ] **Step 3: Swap the `MessageStore` interface**

In `internal/qwkservice/service.go`, replace the two interface lines:

```go
	AddReply(areaID int, from, to, subject, body, replyToMsgID string, replyToNum int) (int, error)
	AddPrivateReply(areaID int, from, to, subject, body, replyToMsgID string, replyToNum int) (int, error)
```

(removing the old `AddMessage`/`AddPrivateMessage` declarations).

- [ ] **Step 4: Copy the reference on export**

In `internal/qwkservice/service.go` `BuildPacket`, add the field to the
`qwk.PacketMessage` literal (after `Private: msg.IsPrivate,`):

```go
				ReplyToNumber: msg.ReplyToNum,
```

- [ ] **Step 5: Route import through the reply methods**

In `internal/qwkservice/service_import.go`, replace the post block:

```go
		var perr error
		if kind == KindPrivateMail {
			_, perr = s.store.AddPrivateReply(area.ID, opts.Handle, msg.To, msg.Subject, body, "", msg.ReplyToNumber)
		} else {
			_, perr = s.store.AddReply(area.ID, opts.Handle, msg.To, msg.Subject, body, "", msg.ReplyToNumber)
		}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/qwkservice/ 2>&1 | tail -5`
Expected: PASS — the three new tests plus all existing `internal/qwkservice` tests (existing import tests still pass: `AddReply`/`AddPrivateReply` append to the same `posted`/`privPosted` slices, and non-reply REPs carry `ReplyToNumber == 0`).

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/qwkservice/service.go internal/qwkservice/service_import.go internal/qwkservice/fakestore_test.go internal/qwkservice/service_export_test.go internal/qwkservice/service_import_test.go
go vet ./internal/qwkservice/
git add internal/qwkservice/
git commit -m "feat(qwk): export and import the reply reference, threading REP replies"
```

---

## Task 3: Docs + full verification

**Files:**
- Modify: `docs/sysop/messages/qwk.md`

- [ ] **Step 1: Add a one-line note**

In `docs/sysop/messages/qwk.md`, near the conference-numbering / upload section,
add:

```markdown
## Reply threading

Reply relationships are preserved across packets: a reply's parent message number
travels in the QWK reference field, so a reply read or composed in an offline
reader keeps its "Reply#: N" linkage when it is downloaded or uploaded.
```

- [ ] **Step 2: Commit the docs**

```bash
git add docs/sysop/messages/qwk.md
git commit -m "docs(qwk): document reply threading across packets"
```

- [ ] **Step 3: Full verification**

Run: `gofmt -l internal/qwk internal/qwkservice`
Expected: no output.

Run: `go vet ./... 2>&1 | tail -5`
Expected: no issues.

Run: `go test ./... 2>&1 | tail -10`
Expected: all packages PASS.

Run: `go test -race ./internal/qwk ./internal/qwkservice 2>&1 | tail -5`
Expected: PASS, no races.

- [ ] **Step 4: Final commit (only if cleanup was needed)**

```bash
git add -A
git commit -m "chore(qwk): Phase 4 cleanup and verification"
```

---

## Self-Review (completed by plan author)

- **Spec coverage:** reference field round-trip (struct fields + write + parse) → Task 1; `BuildPacket` export copy + `MessageStore` swap + `ImportREP` routing via `AddReply`/`AddPrivateReply` + fake store → Task 2; docs + verification → Task 3. All spec sections covered.
- **Placeholder scan:** no TBD/TODO; every code step shows full code.
- **Type consistency:** `ReplyToNumber int` on both `PacketMessage` and `REPMessage`; `AddReply`/`AddPrivateReply(... replyToMsgID string, replyToNum int)` match `*message.MessageManager` and the fake; `BuildPacket` reads `DisplayMessage.ReplyToNum`; import passes `msg.ReplyToNumber`. Consistent across tasks.
