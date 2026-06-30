# Local Message Reply Threading — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record a parent-message pointer when a user replies, so the reader shows "Reply#: N" for local message areas (and echomail replies link immediately).

**Architecture:** Add a `jam.Message.ReplyTo` convenience field that `WriteMessage`/`WriteMessageExt` write into the JAM header. Consolidate the `Add*` posting methods in the manager into one internal `addMessage` and add `AddReply` (carrying both the FTN `ReplyID` and the numeric `ReplyTo`). The reader's single shared reply path passes the parent's number. Display is already wired (substitution key `P`; the default MSGHDR template already shows `Reply#: @P@`).

**Tech Stack:** Go (stdlib `testing`, `time`); existing `internal/jam`, `internal/message`, `internal/menu`.

## Global Constraints

- No new dependencies; `slog` for logging.
- TDD: failing test first → run red → minimal implementation → run green → commit. For the consolidation refactor, write characterization tests that pass on the CURRENT code first, then refactor and keep them green.
- Backward compatible: `msg.ReplyTo` defaults to `0`, so every existing caller writes `ReplyTo == 0` (today's behavior). The three existing `Add*` methods keep identical signatures and behavior.
- Reply data flows through `jam.Message.ReplyTo` (a convenience field), NOT `msg.Header` — `Message.GetAttribute()` returns `Header.Attribute` when `Header != nil` (`internal/jam/types.go:109`), so routing `ReplyTo` through `Header` would corrupt message attributes.
- Spec: `docs/superpowers/specs/2026-06-30-local-reply-threading-design.md`.

---

## File Structure

- Modify: `internal/jam/types.go` — add `Message.ReplyTo uint32`.
- Modify: `internal/jam/message.go` — `WriteMessage` sets `hdr.ReplyTo`.
- Modify: `internal/jam/echomail.go` — `WriteMessageExt` sets `hdr.ReplyTo`.
- Create: `internal/jam/replyto_test.go` — write/read round-trip of `ReplyTo`.
- Modify: `internal/message/manager.go` — internal `addMessage`; `AddMessage`/`AddMessageWithDate`/`AddPrivateMessage` become wrappers; add `AddReply`.
- Create: `internal/message/reply_test.go` — characterization tests + `AddReply` test.
- Modify: `internal/menu/message_reader_nav.go` — `handleReply` calls `AddReply`.

No template or display-code changes (already wired).

---

## Task 1: JAM writes a `ReplyTo` parent pointer

**Files:**
- Modify: `internal/jam/types.go` (add field to `Message`, ~line 77)
- Modify: `internal/jam/message.go` (`WriteMessage`, ~line 454)
- Modify: `internal/jam/echomail.go` (`WriteMessageExt`, ~line 32)
- Test: `internal/jam/replyto_test.go` (create)

**Interfaces:**
- Produces: `jam.Message` field `ReplyTo uint32`; `WriteMessage`/`WriteMessageExt` persist it to `MessageHeader.ReplyTo`.

- [ ] **Step 1: Write the failing tests**

Create `internal/jam/replyto_test.go`:

```go
package jam

import (
	"path/filepath"
	"testing"
)

func TestWriteMessage_PreservesReplyTo(t *testing.T) {
	b, err := Open(filepath.Join(t.TempDir(), "base"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()

	msg := NewMessage()
	msg.From, msg.To, msg.Subject, msg.Text = "alice", "bob", "Hi", "body"
	msg.ReplyTo = 5

	n, err := b.WriteMessage(msg)
	if err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	got, err := b.ReadMessage(n)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if got.Header == nil || got.Header.ReplyTo != 5 {
		t.Errorf("ReplyTo: want 5, got %+v", got.Header)
	}
}

func TestWriteMessage_ZeroReplyTo(t *testing.T) {
	b, err := Open(filepath.Join(t.TempDir(), "base"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()

	msg := NewMessage()
	msg.From, msg.To, msg.Subject, msg.Text = "alice", "bob", "Hi", "body"

	n, err := b.WriteMessage(msg)
	if err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	got, err := b.ReadMessage(n)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if got.Header != nil && got.Header.ReplyTo != 0 {
		t.Errorf("ReplyTo: want 0, got %d", got.Header.ReplyTo)
	}
}

func TestWriteMessageExt_PreservesReplyTo(t *testing.T) {
	b := openTestBase(t)

	msg := NewMessage()
	msg.From, msg.To, msg.Subject, msg.Text = "u1", "u2", "Re", "reply"
	msg.ReplyTo = 3

	n, err := b.WriteMessageExt(msg, MsgTypeLocalMsg, "", "Test BBS", "")
	if err != nil {
		t.Fatalf("WriteMessageExt: %v", err)
	}
	got, err := b.ReadMessage(n)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if got.Header == nil || got.Header.ReplyTo != 3 {
		t.Errorf("ReplyTo: want 3, got %+v", got.Header)
	}
}
```

(`openTestBase(t)` is an existing helper in the jam test package — see
`internal/jam/echomail_test.go`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/jam/ -run 'TestWriteMessage_PreservesReplyTo|TestWriteMessageExt_PreservesReplyTo' -v`
Expected: FAIL — `msg.ReplyTo` undefined (compile error).

- [ ] **Step 3: Add the field**

In `internal/jam/types.go`, add to the `Message` struct (after the `ReplyID string` field, ~line 77):

```go
	ReplyTo  uint32 // JAM parent message number (0 = none); written to the header
```

- [ ] **Step 4: Persist it in WriteMessage**

In `internal/jam/message.go`, after the `hdr := &MessageHeader{...}` block and the
`copy(hdr.Signature[:], Signature)` line (~line 460), add:

```go
	hdr.ReplyTo = msg.ReplyTo
```

- [ ] **Step 5: Persist it in WriteMessageExt**

In `internal/jam/echomail.go`, after its `hdr := &MessageHeader{...}` block and the
`copy(hdr.Signature[:], Signature)` line (~line 40), add:

```go
	hdr.ReplyTo = msg.ReplyTo
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/jam/ 2>&1 | tail -5`
Expected: PASS — the three new tests plus all existing jam tests.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/jam/types.go internal/jam/message.go internal/jam/echomail.go internal/jam/replyto_test.go
go vet ./internal/jam/
git add internal/jam/types.go internal/jam/message.go internal/jam/echomail.go internal/jam/replyto_test.go
git commit -m "feat(jam): write Message.ReplyTo parent pointer into the header"
```

---

## Task 2: Manager consolidation + `AddReply`

**Files:**
- Modify: `internal/message/manager.go` (`AddMessage`/`AddMessageWithDate`/`AddPrivateMessage` → wrappers over new internal `addMessage`; add `AddReply`)
- Test: `internal/message/reply_test.go` (create)

**Interfaces:**
- Consumes: `jam.Message.ReplyTo` (Task 1).
- Produces: `func (mm *MessageManager) AddReply(areaID int, from, to, subject, body, replyToMsgID string, replyToNum int) (int, error)`. Existing `Add*` signatures unchanged.

- [ ] **Step 1: Write the characterization + AddReply tests**

Create `internal/message/reply_test.go`:

```go
package message

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newReplyTestManager(t *testing.T) *MessageManager {
	t.Helper()
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	areas := `[{"id":1,"tag":"GENERAL","name":"General","base_path":"general","area_type":"local"},
	           {"id":2,"tag":"PRIVMAIL","name":"Private Mail","base_path":"privmail","area_type":"local"}]`
	if err := os.WriteFile(filepath.Join(cfg, "message_areas.json"), []byte(areas), 0o644); err != nil {
		t.Fatal(err)
	}
	mm, err := NewMessageManager(tmp, cfg, "TestBBS", nil)
	if err != nil {
		t.Fatalf("NewMessageManager: %v", err)
	}
	return mm
}

// Characterization: lock the existing Add* behavior before consolidating.
func TestAddFamily_Characterization(t *testing.T) {
	mm := newReplyTestManager(t)

	posted := 0
	mm.OnMessagePosted = func(area *MessageArea, msgNum int, from, to, subject, body string) { posted++ }

	if _, err := mm.AddMessage(1, "a", "All", "s", "b", ""); err != nil {
		t.Fatal(err)
	}
	if posted != 1 {
		t.Errorf("AddMessage should fire OnMessagePosted once, got %d", posted)
	}

	pn, err := mm.AddPrivateMessage(2, "a", "b", "s", "b", "")
	if err != nil {
		t.Fatal(err)
	}
	if posted != 1 {
		t.Errorf("AddPrivateMessage must NOT fire OnMessagePosted; count=%d", posted)
	}
	pm, err := mm.GetMessage(2, pn)
	if err != nil {
		t.Fatal(err)
	}
	if !pm.IsPrivate {
		t.Error("AddPrivateMessage should set IsPrivate")
	}

	when := time.Date(2020, 1, 2, 3, 4, 0, 0, time.UTC)
	dn, err := mm.AddMessageWithDate(1, "a", "All", "s3", "b3", "", when)
	if err != nil {
		t.Fatal(err)
	}
	dmsg, err := mm.GetMessage(1, dn)
	if err != nil {
		t.Fatal(err)
	}
	if !dmsg.DateTime.Equal(when) {
		t.Errorf("AddMessageWithDate: want %v, got %v", when, dmsg.DateTime)
	}
}

func TestAddReply_SetsReplyTo(t *testing.T) {
	mm := newReplyTestManager(t)

	parent, err := mm.AddMessage(1, "alice", "All", "Topic", "first", "")
	if err != nil {
		t.Fatal(err)
	}
	reply, err := mm.AddReply(1, "bob", "alice", "Re: Topic", "second", "", parent)
	if err != nil {
		t.Fatal(err)
	}
	dm, err := mm.GetMessage(1, reply)
	if err != nil {
		t.Fatal(err)
	}
	if dm.ReplyToNum != parent {
		t.Errorf("ReplyToNum: want %d, got %d", parent, dm.ReplyToNum)
	}
}

func TestAddReply_AlsoSetsReplyID(t *testing.T) {
	mm := newReplyTestManager(t)
	parent, _ := mm.AddMessage(1, "alice", "All", "Topic", "first", "")
	reply, err := mm.AddReply(1, "bob", "alice", "Re: Topic", "second", "PARENTMSGID", parent)
	if err != nil {
		t.Fatal(err)
	}
	dm, err := mm.GetMessage(1, reply)
	if err != nil {
		t.Fatal(err)
	}
	if dm.ReplyID != "PARENTMSGID" {
		t.Errorf("ReplyID: want PARENTMSGID, got %q", dm.ReplyID)
	}
	if dm.ReplyToNum != parent {
		t.Errorf("ReplyToNum: want %d, got %d", parent, dm.ReplyToNum)
	}
}
```

- [ ] **Step 2: Run the characterization tests against the CURRENT code**

Run: `go test ./internal/message/ -run TestAddFamily_Characterization -v`
Expected: PASS (locks existing behavior). Then run the reply tests:

Run: `go test ./internal/message/ -run 'TestAddReply_' -v`
Expected: FAIL — `mm.AddReply undefined`.

- [ ] **Step 3: Add the internal `addMessage` and rewrite the wrappers**

In `internal/message/manager.go`, replace the three methods `AddMessage`
(~585-641), `AddMessageWithDate` (~645-694), and `AddPrivateMessage` (~701-747)
with one internal implementation plus thin wrappers and the new `AddReply`:

```go
// addMessage is the shared implementation behind AddMessage, AddMessageWithDate,
// AddPrivateMessage, and AddReply.
func (mm *MessageManager) addMessage(areaID int, from, to, subject, body, replyToMsgID string,
	replyToNum int, dateTime time.Time, private bool) (int, error) {
	b, area, err := mm.openBase(areaID)
	if err != nil {
		return 0, err
	}

	// Apply body transform (e.g. V3Net tearline/origin) for the local JAM copy.
	// The original body is preserved for OnMessagePosted so the wire message
	// carries tearline/origin as separate protocol fields, not inline.
	jamBody := body
	if mm.BodyTransform != nil {
		jamBody = mm.BodyTransform(areaID, body)
	}

	msg := jam.NewMessage()
	msg.From = from
	msg.To = to
	msg.Subject = subject
	msg.Text = jamBody
	msg.DateTime = dateTime

	if private {
		msg.Header = &jam.MessageHeader{Attribute: jam.MsgPrivate | jam.MsgLocal}
	}
	if replyToNum > 0 {
		msg.ReplyTo = uint32(replyToNum)
	}
	if replyToMsgID != "" {
		msg.ReplyID = replyToMsgID
	}

	msgType := jam.DetermineMessageType(area.AreaType, area.EchoTag)

	// For netmail, split "user@address" into separate To and DestAddr fields.
	if msgType.IsNetmail() {
		name, addr := splitNetmailTo(to)
		msg.To = name
		if addr != "" {
			msg.DestAddr = addr
		}
	}

	var msgNum int
	if msgType.IsEchomail() || msgType.IsNetmail() {
		msg.OrigAddr = area.OriginAddr
		msgNum, err = b.WriteMessageExt(msg, msgType, area.EchoTag, mm.boardName, mm.tearlineForNetwork(area.Network))
	} else {
		msgNum, err = b.WriteMessage(msg)
	}

	// Close the base before firing the callback. The V3Net hook calls
	// MarkMessageSent which re-opens the same JAM base, so having it
	// still open here can cause nested-open/file-sharing issues.
	b.Close()

	if err == nil {
		mm.invalidateThreadIndex(areaID)
		if !private && mm.OnMessagePosted != nil {
			mm.OnMessagePosted(area, msgNum, from, to, subject, body)
		}
	}
	return msgNum, err
}

// AddMessage creates and writes a new message to the specified area.
// For echomail areas, it automatically handles MSGID, kludges, tearline, and origin.
// For netmail areas, "user@zone:net/node" in the To field is automatically split
// into the username and destination address.
// Returns the 1-based message number assigned.
func (mm *MessageManager) AddMessage(areaID int, from, to, subject, body, replyToMsgID string) (int, error) {
	return mm.addMessage(areaID, from, to, subject, body, replyToMsgID, 0, time.Now(), false)
}

// AddMessageWithDate is like AddMessage but uses the provided timestamp instead
// of time.Now(). Used by V3Net to preserve the original authored date.
func (mm *MessageManager) AddMessageWithDate(areaID int, from, to, subject, body, replyToMsgID string, dateTime time.Time) (int, error) {
	return mm.addMessage(areaID, from, to, subject, body, replyToMsgID, 0, dateTime, false)
}

// AddPrivateMessage creates and writes a new private (MSG_PRIVATE) message.
// For netmail areas, "user@zone:net/node" in the To field is automatically split
// into the username and destination address. Returns the 1-based message number.
func (mm *MessageManager) AddPrivateMessage(areaID int, from, to, subject, body, replyToMsgID string) (int, error) {
	return mm.addMessage(areaID, from, to, subject, body, replyToMsgID, 0, time.Now(), true)
}

// AddReply creates and writes a reply, recording the parent's message number
// (replyToNum) as the JAM ReplyTo so local areas thread, and the parent's FTN
// MSGID (replyToMsgID, may be "") as ReplyID so echomail links via jam.Link().
func (mm *MessageManager) AddReply(areaID int, from, to, subject, body, replyToMsgID string, replyToNum int) (int, error) {
	return mm.addMessage(areaID, from, to, subject, body, replyToMsgID, replyToNum, time.Now(), false)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/message/ -run 'TestAddFamily_Characterization|TestAddReply_' -v`
Expected: PASS (characterization still green after the refactor; the two AddReply tests pass).

Run: `go test ./internal/message/ 2>&1 | tail -5`
Expected: all `internal/message` tests PASS (no regression in the consolidated post path).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/message/manager.go internal/message/reply_test.go
go vet ./internal/message/
git add internal/message/manager.go internal/message/reply_test.go
git commit -m "feat(message): consolidate Add* posting and add AddReply with parent pointer"
```

---

## Task 3: Reader records the parent on reply + full verification

**Files:**
- Modify: `internal/menu/message_reader_nav.go` (`handleReply`, ~line 74)

**Interfaces:**
- Consumes: `MessageManager.AddReply` (Task 2).

- [ ] **Step 1: Wire the reply call site**

In `internal/menu/message_reader_nav.go` `handleReply`, the current reply post is:

```go
	replyMsgID := currentMsg.MsgID
	_, err := e.MessageMgr.AddMessage(currentAreaID, currentUser.Handle, currentMsg.From,
		newSubject, replyBody, replyMsgID)
```

Replace it with `AddReply`, passing the parent's number:

```go
	replyMsgID := currentMsg.MsgID
	_, err := e.MessageMgr.AddReply(currentAreaID, currentUser.Handle, currentMsg.From,
		newSubject, replyBody, replyMsgID, currentMsg.MsgNum)
```

`currentMsg.MsgNum` is the parent's 1-based number in this area; `currentMsg.MsgID`
keeps echomail linking working. Because `READPRIVMAIL` and public reading both
route through `runMessageReader` → `handleReply`, this single change covers public
and private-mail replies. Leave the rest of `handleReply` unchanged.

- [ ] **Step 2: Build and run the menu tests**

Run: `go build ./internal/menu/ && go vet ./internal/menu/`
Expected: success, no issues.

Run: `go test ./internal/menu/ 2>&1 | tail -3`
Expected: PASS (existing menu tests unaffected; the reply-threading behavior is
covered by the `internal/message` AddReply tests and the `internal/jam` ReplyTo
tests).

- [ ] **Step 3: Commit**

```bash
gofmt -w internal/menu/message_reader_nav.go
git add internal/menu/message_reader_nav.go
git commit -m "feat(reader): record parent message number when replying"
```

- [ ] **Step 4: Full verification**

Run: `gofmt -l internal/jam internal/message internal/menu/message_reader_nav.go`
Expected: no output.

Run: `go vet ./... 2>&1 | tail -5`
Expected: no issues.

Run: `go test ./... 2>&1 | tail -10`
Expected: all packages PASS.

Run: `go test -race ./internal/jam ./internal/message 2>&1 | tail -5`
Expected: PASS, no race warnings.

- [ ] **Step 5: Final commit (only if cleanup was needed)**

```bash
git add -A
git commit -m "chore: reply-threading cleanup and verification"
```

---

## Self-Review (completed by plan author)

- **Spec coverage:** JAM `Message.ReplyTo` + write-side → Task 1; manager consolidation + `AddReply` (with characterization tests) → Task 2; reader reply wiring (covers public + private via the shared `handleReply`) → Task 3; display → already wired (no task needed; MSGHDR.1.ans shows `Reply#: @P@`). All spec sections covered.
- **Placeholder scan:** no TBD/TODO; every code step shows full code; the unchanged parts of `handleReply` and `addMessage` are shown in full, not elided.
- **Type consistency:** `Message.ReplyTo uint32`, `AddReply(... replyToMsgID string, replyToNum int)`, internal `addMessage(... replyToNum int, dateTime time.Time, private bool)`, and `DisplayMessage.ReplyToNum` are used consistently. Wrappers preserve the exact existing `Add*` signatures.
