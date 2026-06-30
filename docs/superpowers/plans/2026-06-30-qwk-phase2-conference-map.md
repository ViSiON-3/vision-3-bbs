# QWK Phase 2 — Stable Conference Mapping & Private-Mail Routing — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace raw local area IDs with a persisted, stable QWK conference map and make private mail a first-class QWK case (conference 0, per-user export filtering, `AddPrivateMessage` import routing).

**Architecture:** Add a `ConferenceMap` (`internal/qwkservice/conference_map.go`) persisted to `data/qwk_conferences.json`, keyed by area tag. `BuildPacket` and `ImportREP` load → sync → persist the map per call and use stable numbers; the `PRIVMAIL`-tagged area is reserved as conference 0. Private replies route to `AddPrivateMessage`; private-mail export includes only the requesting user's own mail.

**Tech Stack:** Go (stdlib `encoding/json`, `os`, `testing`); existing `internal/qwkservice`, `internal/qwk`, `internal/message`, `internal/menu`.

## Global Constraints

- Go files stay under 300 lines (CLAUDE.md); split test files reusing the shared `fakeStore`.
- Use `slog` for logging; no new dependencies.
- TDD: write the failing test first, watch it fail, implement minimally, watch it pass, commit.
- Scope guard — do NOT add in this plan: `HEADERS.DAT`, Synchronet legacy tags, reply/thread metadata, authored-date import, REP first-block validation, dedup, or any packet API. Those are Phases 3–5/7.
- Spec: `docs/superpowers/specs/2026-06-30-qwk-phase2-conference-map-design.md`.
- Reserved values: `PrivateMailTag = "PRIVMAIL"`, `PrivateMailConference = 0`.

---

## File Structure

- Create: `internal/qwkservice/conference_map.go` — the map type, load/save/sync, lookups.
- Create: `internal/qwkservice/conference_map_test.go` — map unit tests.
- Modify: `internal/message/manager.go` — add `DataPath()` accessor.
- Modify: `internal/message/manager_test.go` (or a new small test) — cover `DataPath()`.
- Modify: `internal/qwkservice/service.go` — `MessageStore` gains `AddPrivateMessage`; `New` gains `dataPath`; add `loadConfMap` helper; map-aware `BuildPacket` and `ImportREP`.
- Modify: `internal/qwkservice/fakestore_test.go` — fake `AddPrivateMessage`, `privPosted`, and a `newTestService` helper.
- Modify: `internal/qwkservice/service_export_test.go`, `internal/qwkservice/service_import_test.go` — use `newTestService`; add map-aware tests.
- Modify: `internal/menu/qwk_handler.go` — pass `e.MessageMgr.DataPath()` into `qwkservice.New`.
- Modify: `docs/sysop/messages/qwk.md` — document the map, conference 0, per-user private export.

---

## Task 1: Conference map type, persistence, and sync

**Files:**
- Create: `internal/qwkservice/conference_map.go`
- Test: `internal/qwkservice/conference_map_test.go`

**Interfaces:**
- Consumes: `message.MessageArea` (fields `ID int`, `Tag string`).
- Produces:
  - `type ConferenceKind string`; consts `KindPublic`, `KindPrivateMail`.
  - `const PrivateMailTag = "PRIVMAIL"`, `const PrivateMailConference = 0`.
  - `type ConferenceMapEntry struct { QWKNumber int; AreaTag string; Kind ConferenceKind }`.
  - `type ConferenceMap struct{ ... }`.
  - `func LoadConferenceMap(path string) (*ConferenceMap, error)`.
  - `func (m *ConferenceMap) Save(path string) error`.
  - `func (m *ConferenceMap) Sync(areas []*message.MessageArea) bool`.
  - `func (m *ConferenceMap) EntryForTag(tag string) (ConferenceMapEntry, bool)`.
  - `func (m *ConferenceMap) EntryForNumber(num int) (ConferenceMapEntry, bool)`.

- [ ] **Step 1: Write the failing test**

Create `internal/qwkservice/conference_map_test.go`:

```go
package qwkservice

import (
	"path/filepath"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

func area(id int, tag string) *message.MessageArea {
	return &message.MessageArea{ID: id, Tag: tag, Name: tag}
}

func TestConferenceMap_LoadMissingIsEmpty(t *testing.T) {
	m, err := LoadConferenceMap(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("LoadConferenceMap on missing file: %v", err)
	}
	if _, ok := m.EntryForNumber(0); ok {
		t.Error("expected empty map")
	}
}

func TestConferenceMap_SyncAssignsNumbers(t *testing.T) {
	m, _ := LoadConferenceMap(filepath.Join(t.TempDir(), "m.json"))
	changed := m.Sync([]*message.MessageArea{
		area(1, "GENERAL"), area(2, "PRIVMAIL"), area(7, "TECH"),
	})
	if !changed {
		t.Fatal("Sync should report changed on first assignment")
	}
	if e, ok := m.EntryForTag("PRIVMAIL"); !ok || e.QWKNumber != 0 || e.Kind != KindPrivateMail {
		t.Errorf("PRIVMAIL: want {0 private_mail}, got %+v ok=%v", e, ok)
	}
	if e, ok := m.EntryForTag("GENERAL"); !ok || e.QWKNumber != 1 || e.Kind != KindPublic {
		t.Errorf("GENERAL: want {1 public}, got %+v ok=%v", e, ok)
	}
	if e, _ := m.EntryForTag("TECH"); e.QWKNumber != 7 {
		t.Errorf("TECH: want number 7 (area.ID), got %d", e.QWKNumber)
	}
}

func TestConferenceMap_ZeroIDCollisionBumped(t *testing.T) {
	m, _ := LoadConferenceMap(filepath.Join(t.TempDir(), "m.json"))
	// A public area whose ID is 0 must not claim the reserved 0 slot.
	m.Sync([]*message.MessageArea{area(0, "ODD")})
	if e, _ := m.EntryForTag("ODD"); e.QWKNumber == 0 {
		t.Errorf("public area with ID 0 must be bumped off 0, got %d", e.QWKNumber)
	}
}

func TestConferenceMap_StableAcrossResync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	m, _ := LoadConferenceMap(path)
	m.Sync([]*message.MessageArea{area(1, "GENERAL"), area(2, "PRIVMAIL")})
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadConferenceMap(path)
	if err != nil {
		t.Fatal(err)
	}
	// Re-sync with a new area added and an existing area renamed (Name changed).
	renamed := area(1, "GENERAL")
	renamed.Name = "General Chat"
	changed := reloaded.Sync([]*message.MessageArea{renamed, area(2, "PRIVMAIL"), area(3, "NEWS")})
	if !changed {
		t.Fatal("adding NEWS should report changed")
	}
	if e, _ := reloaded.EntryForTag("GENERAL"); e.QWKNumber != 1 {
		t.Errorf("GENERAL number must stay 1 across resync, got %d", e.QWKNumber)
	}
	if e, _ := reloaded.EntryForTag("PRIVMAIL"); e.QWKNumber != 0 {
		t.Errorf("PRIVMAIL number must stay 0, got %d", e.QWKNumber)
	}
	if _, ok := reloaded.EntryForTag("NEWS"); !ok {
		t.Error("NEWS should have been assigned a number")
	}
}

func TestConferenceMap_SyncNoChangeWhenComplete(t *testing.T) {
	m, _ := LoadConferenceMap(filepath.Join(t.TempDir(), "m.json"))
	areas := []*message.MessageArea{area(1, "GENERAL"), area(2, "PRIVMAIL")}
	m.Sync(areas)
	if m.Sync(areas) {
		t.Error("second Sync with no new areas should report no change")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/qwkservice/ -run TestConferenceMap -v`
Expected: FAIL — `undefined: LoadConferenceMap` (and the other symbols).

- [ ] **Step 3: Write the implementation**

Create `internal/qwkservice/conference_map.go`:

```go
package qwkservice

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ViSiON-3/vision-3-bbs/internal/message"
)

// ConferenceKind classifies how a QWK conference maps to a local area.
type ConferenceKind string

const (
	KindPublic      ConferenceKind = "public"
	KindPrivateMail ConferenceKind = "private_mail"
)

// PrivateMailTag is the message-area tag treated as the private-mail/email
// conference. PrivateMailConference is the QWK conference number reserved for it.
const (
	PrivateMailTag        = "PRIVMAIL"
	PrivateMailConference = 0
)

// ConferenceMapEntry is the stable mapping from a local area (by tag) to a QWK
// conference number.
type ConferenceMapEntry struct {
	QWKNumber int            `json:"qwk_number"`
	AreaTag   string         `json:"area_tag"`
	Kind      ConferenceKind `json:"kind"`
}

// ConferenceMap is the persisted tag->number contract used for QWK export and
// import. Numbers, once assigned, are never changed.
type ConferenceMap struct {
	entries []ConferenceMapEntry
	byTag   map[string]ConferenceMapEntry
	byNum   map[int]ConferenceMapEntry
}

func newConferenceMap() *ConferenceMap {
	return &ConferenceMap{
		byTag: map[string]ConferenceMapEntry{},
		byNum: map[int]ConferenceMapEntry{},
	}
}

func (m *ConferenceMap) reindex() {
	m.byTag = make(map[string]ConferenceMapEntry, len(m.entries))
	m.byNum = make(map[int]ConferenceMapEntry, len(m.entries))
	for _, e := range m.entries {
		m.byTag[e.AreaTag] = e
		m.byNum[e.QWKNumber] = e
	}
}

// LoadConferenceMap reads the map from path. A missing file yields an empty map
// (not an error); a malformed file is an error.
func LoadConferenceMap(path string) (*ConferenceMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newConferenceMap(), nil
		}
		return nil, fmt.Errorf("read conference map: %w", err)
	}
	var entries []ConferenceMapEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse conference map: %w", err)
	}
	m := newConferenceMap()
	m.entries = entries
	m.reindex()
	return m, nil
}

// Save writes the map to path atomically (temp file + rename).
func (m *ConferenceMap) Save(path string) error {
	data, err := json.MarshalIndent(m.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal conference map: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "qwk_conferences_*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp conference map: %w", err)
	}
	tmpName := tmp.Name()
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp conference map: %w", err)
	}
	if err = tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp conference map: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename conference map: %w", err)
	}
	return nil
}

// Sync ensures every area has an entry, never renumbering an existing one.
// Returns true if the map changed and should be persisted.
func (m *ConferenceMap) Sync(areas []*message.MessageArea) bool {
	changed := false
	for _, a := range areas {
		if _, ok := m.byTag[a.Tag]; ok {
			continue
		}
		var e ConferenceMapEntry
		if a.Tag == PrivateMailTag {
			e = ConferenceMapEntry{QWKNumber: PrivateMailConference, AreaTag: a.Tag, Kind: KindPrivateMail}
		} else {
			e = ConferenceMapEntry{QWKNumber: m.nextNumber(a.ID), AreaTag: a.Tag, Kind: KindPublic}
		}
		m.entries = append(m.entries, e)
		m.byTag[e.AreaTag] = e
		m.byNum[e.QWKNumber] = e
		changed = true
	}
	if changed {
		sort.Slice(m.entries, func(i, j int) bool { return m.entries[i].QWKNumber < m.entries[j].QWKNumber })
	}
	return changed
}

// nextNumber prefers the area's local ID, falling back to the next free
// positive integer when that ID is <= 0 or already taken.
func (m *ConferenceMap) nextNumber(preferred int) int {
	if preferred > 0 {
		if _, taken := m.byNum[preferred]; !taken {
			return preferred
		}
	}
	for n := 1; ; n++ {
		if _, taken := m.byNum[n]; !taken {
			return n
		}
	}
}

// EntryForTag returns the entry for an area tag.
func (m *ConferenceMap) EntryForTag(tag string) (ConferenceMapEntry, bool) {
	e, ok := m.byTag[tag]
	return e, ok
}

// EntryForNumber returns the entry for a QWK conference number.
func (m *ConferenceMap) EntryForNumber(num int) (ConferenceMapEntry, bool) {
	e, ok := m.byNum[num]
	return e, ok
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/qwkservice/ -run TestConferenceMap -v`
Expected: PASS (all 5 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/qwkservice/conference_map.go internal/qwkservice/conference_map_test.go
git commit -m "feat(qwk): add persisted stable conference map"
```

---

## Task 2: `MessageManager.DataPath()` accessor

**Files:**
- Modify: `internal/message/manager.go` (add method near other accessors)
- Test: `internal/message/manager_datapath_test.go` (create)

**Interfaces:**
- Produces: `func (mm *MessageManager) DataPath() string`.

- [ ] **Step 1: Write the failing test**

Create `internal/message/manager_datapath_test.go`:

```go
package message

import "testing"

func TestMessageManager_DataPath(t *testing.T) {
	mm := &MessageManager{dataPath: "/srv/bbs/data"}
	if got := mm.DataPath(); got != "/srv/bbs/data" {
		t.Errorf("DataPath() = %q, want %q", got, "/srv/bbs/data")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/message/ -run TestMessageManager_DataPath -v`
Expected: FAIL — `mm.DataPath undefined`.

- [ ] **Step 3: Add the accessor**

In `internal/message/manager.go`, add (place it just after the `NewMessageManager` constructor or near other small accessors):

```go
// DataPath returns the base data directory this manager was constructed with
// (e.g. "data"). Used by adjacent subsystems that persist their own state
// alongside the message bases.
func (mm *MessageManager) DataPath() string {
	return mm.dataPath
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/message/ -run TestMessageManager_DataPath -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/message/manager.go internal/message/manager_datapath_test.go
git commit -m "feat(message): expose MessageManager.DataPath accessor"
```

---

## Task 3: Wire `dataPath` + private-mail store method through the service

This task changes the `MessageStore` interface and `New` signature, so it updates
every call site (service tests + menu) to keep the tree compiling and green. It
does not yet change export/import behaviour.

**Files:**
- Modify: `internal/qwkservice/service.go` (interface, `New`, struct field)
- Modify: `internal/qwkservice/fakestore_test.go` (fake `AddPrivateMessage`, `privPosted`, `newTestService` helper)
- Modify: `internal/qwkservice/service_export_test.go`, `internal/qwkservice/service_import_test.go` (use `newTestService`)
- Modify: `internal/menu/qwk_handler.go` (pass `e.MessageMgr.DataPath()`)

**Interfaces:**
- Consumes: `ConferenceMap` (Task 1), `MessageManager.DataPath()` (Task 2).
- Produces:
  - `MessageStore` interface gains `AddPrivateMessage(areaID int, from, to, subject, body, replyToMsgID string) (int, error)`.
  - `func New(store MessageStore, bbsID, bbsName, sysOpName, dataPath string) *Service`.
  - `Service` gains unexported `confMapPath string`.
  - Test helper `func newTestService(t *testing.T, store *fakeStore) *Service`.
  - Fake gains `privPosted []postedMessage` and `AddPrivateMessage`.

- [ ] **Step 1: Extend the `MessageStore` interface and `New`**

In `internal/qwkservice/service.go`, add the method to the interface:

```go
	AddMessage(areaID int, from, to, subject, body, replyToMsgID string) (int, error)
	AddPrivateMessage(areaID int, from, to, subject, body, replyToMsgID string) (int, error)
}
```

Add a field to `Service` (after `sysOpName string`):

```go
	confMapPath string
```

Replace `New` with:

```go
// New creates a QWK service. bbsID is the short packet identifier (e.g.
// "VISION3"); bbsName and sysOpName populate CONTROL.DAT; dataPath is the base
// data directory used to persist the stable conference map.
func New(store MessageStore, bbsID, bbsName, sysOpName, dataPath string) *Service {
	return &Service{
		store:       store,
		bbsID:       bbsID,
		bbsName:     bbsName,
		sysOpName:   sysOpName,
		confMapPath: filepath.Join(dataPath, "qwk_conferences.json"),
	}
}
```

Add `"path/filepath"` to the `service.go` import block.

- [ ] **Step 2: Extend the fake store and add a test-service helper**

In `internal/qwkservice/fakestore_test.go`, add a field to `fakeStore`:

```go
	posted     []postedMessage
	privPosted []postedMessage
	setReads   []setRead
```

Add the method (next to `AddMessage`):

```go
func (f *fakeStore) AddPrivateMessage(areaID int, from, to, subject, body, replyToMsgID string) (int, error) {
	if err := f.addErr[areaID]; err != nil {
		return 0, err
	}
	f.privPosted = append(f.privPosted, postedMessage{areaID, from, to, subject, body, replyToMsgID})
	return len(f.privPosted), nil
}
```

Add the helper (at the end of the file):

```go
// newTestService builds a Service backed by the fake store, with the conference
// map persisted under a per-test temp dir.
func newTestService(t *testing.T, store *fakeStore) *Service {
	t.Helper()
	return New(store, "VISION3", "ViSiON/3 BBS", "SysOp", t.TempDir())
}
```

- [ ] **Step 3: Update every `New(...)` call site in the service tests**

In `internal/qwkservice/service_export_test.go` and
`internal/qwkservice/service_import_test.go`, replace each
`New(store, "VISION3", "ViSiON/3 BBS", "SysOp")` with `newTestService(t, store)`.

Verify none remain:

Run: `grep -rn 'New(store, "VISION3"' internal/qwkservice/`
Expected: no matches (all migrated to `newTestService`), except the helper definition itself which uses the 5-arg `New`.

- [ ] **Step 4: Update the menu call sites**

In `internal/menu/qwk_handler.go`, both constructions become:

```go
svc := qwkservice.New(e.MessageMgr, bbsID, e.ServerCfg.BoardName, e.ServerCfg.SysOpName, e.MessageMgr.DataPath())
```

(one in `runQWKDownload`, one in `runQWKUpload`).

- [ ] **Step 5: Build and run the affected tests**

Run: `go build ./... && go test ./internal/qwkservice/ ./internal/menu/ -v 2>&1 | tail -20`
Expected: build succeeds; all existing service + menu tests PASS (behaviour unchanged).

- [ ] **Step 6: Commit**

```bash
git add internal/qwkservice/service.go internal/qwkservice/fakestore_test.go \
        internal/qwkservice/service_export_test.go internal/qwkservice/service_import_test.go \
        internal/menu/qwk_handler.go
git commit -m "refactor(qwk): thread dataPath + AddPrivateMessage through the service"
```

---

## Task 4: Map-aware export + private-mail filtering in `BuildPacket`

**Files:**
- Modify: `internal/qwkservice/service.go` (`loadConfMap` helper, `BuildPacket`)
- Test: `internal/qwkservice/service_export_test.go` (add tests)

**Interfaces:**
- Consumes: `ConferenceMap`, `EntryForTag`, `Sync`, `Save` (Task 1); `Service.confMapPath` (Task 3).
- Produces: unexported `func (s *Service) loadConfMap() (*ConferenceMap, error)`.

- [ ] **Step 1: Write the failing tests**

In `internal/qwkservice/service_export_test.go`, add:

```go
// hasNDX reports whether the packet zip contains the given per-conference NDX.
func hasNDX(t *testing.T, packet []byte, name string) bool {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(packet), int64(len(packet)))
	if err != nil {
		t.Fatalf("packet not a zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			return true
		}
	}
	return false
}

func privMsg(num int, from, to string) *message.DisplayMessage {
	m := dm(num, from, to, "subj", "body")
	m.IsPrivate = true
	return m
}

func TestBuildPacket_PublicUsesStableNumber(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 5, Tag: "GENERAL", Name: "General"})
	store.seed(5, dm(1, "a", "All", "s", "b"))

	svc := newTestService(t, store)
	res, err := svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"GENERAL"}})
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	// Public area keeps its area.ID (5) as the conference number -> 005.NDX.
	if !hasNDX(t, res.Packet, "005.NDX") {
		t.Error("expected public area to export under conference 5 (005.NDX)")
	}
}

func TestBuildPacket_PrivateMailUsesConferenceZero(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 3, Tag: "PRIVMAIL", Name: "Private Mail"})
	store.seed(3, privMsg(1, "someone", "tester"))

	svc := newTestService(t, store)
	res, err := svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"PRIVMAIL"}})
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	if res.MessageCount != 1 {
		t.Fatalf("MessageCount: want 1, got %d", res.MessageCount)
	}
	if !hasNDX(t, res.Packet, "000.NDX") {
		t.Error("expected PRIVMAIL to export under conference 0 (000.NDX)")
	}
}

func TestBuildPacket_PrivateMailFiltersToUser(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 3, Tag: "PRIVMAIL", Name: "Private Mail"})
	store.seed(3,
		privMsg(1, "someone", "tester"), // to me -> included
		privMsg(2, "tester", "friend"),  // from me -> included
		privMsg(3, "alice", "bob"),      // neither -> excluded
	)

	svc := newTestService(t, store)
	res, err := svc.BuildPacket(ExportOptions{Handle: "tester", TaggedTags: []string{"PRIVMAIL"}})
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	if res.MessageCount != 2 {
		t.Errorf("private-mail export should include only the user's own mail: want 2, got %d", res.MessageCount)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/qwkservice/ -run 'TestBuildPacket_(PublicUsesStableNumber|PrivateMail)' -v`
Expected: FAIL — public area currently exports under conf 5 only if the map is wired (it isn't yet); PRIVMAIL exports under conf 3 (its ID), not 0, and includes all 3 messages. Specifically `000.NDX` is missing and `MessageCount` is 3.

- [ ] **Step 3: Add `loadConfMap` and make `BuildPacket` map-aware**

In `internal/qwkservice/service.go`, add the helper (after `New`):

```go
// loadConfMap loads the conference map, syncs it against the current areas, and
// persists it if anything changed.
func (s *Service) loadConfMap() (*ConferenceMap, error) {
	cm, err := LoadConferenceMap(s.confMapPath)
	if err != nil {
		return nil, err
	}
	if cm.Sync(s.store.ListAreas()) {
		if err := cm.Save(s.confMapPath); err != nil {
			return nil, err
		}
	}
	return cm, nil
}
```

In `BuildPacket`, load the map at the top (after computing `maxPerArea`):

```go
	cm, err := s.loadConfMap()
	if err != nil {
		return nil, err
	}
```

Inside the per-area loop, after `area, exists := ...; if !exists { continue }`,
resolve the stable number and kind, and replace the `pw.AddConference(area.ID, ...)`
and `Conference: area.ID` usages:

```go
		entry, ok := cm.EntryForTag(area.Tag)
		if !ok {
			// Sync guarantees an entry for every area; skip defensively.
			continue
		}
		pw.AddConference(entry.QWKNumber, area.Name)
		isPrivateConf := entry.Kind == KindPrivateMail
```

Then in the message-packing loop, after the `if msg.IsDeleted { continue }` check,
add the private-mail filter, and use `entry.QWKNumber` for the conference:

```go
			if msg.IsDeleted {
				continue
			}
			if isPrivateConf && !ownsPrivateMessage(msg, opts.Handle) {
				continue
			}

			pw.AddMessage(qwk.PacketMessage{
				Conference: entry.QWKNumber,
				Number:     msg.MsgNum,
				From:       msg.From,
				To:         msg.To,
				Subject:    msg.Subject,
				DateTime:   msg.DateTime,
				Body:       msg.Body,
				Private:    msg.IsPrivate,
			})
```

Add the helper near the bottom of `service.go`:

```go
// ownsPrivateMessage reports whether a private message belongs to the given
// user (addressed to or sent by them).
func ownsPrivateMessage(msg *message.DisplayMessage, handle string) bool {
	return msg.IsPrivate && (strings.EqualFold(msg.To, handle) || strings.EqualFold(msg.From, handle))
}
```

Add `"strings"` to the `service.go` imports if not already present.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/qwkservice/ -run TestBuildPacket -v`
Expected: PASS — new tests pass and all pre-existing `TestBuildPacket_*` still pass (public areas with ID 1 still produce `001.NDX`; counts unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/qwkservice/service.go internal/qwkservice/service_export_test.go
git commit -m "feat(qwk): export with stable conference numbers and per-user private mail"
```

---

## Task 5: Map-aware import + private-mail routing in `ImportREP`

**Files:**
- Modify: `internal/qwkservice/service.go` (`ImportREP`)
- Test: `internal/qwkservice/service_import_test.go` (add tests)

**Interfaces:**
- Consumes: `loadConfMap`, `EntryForNumber`, `KindPrivateMail` (Tasks 1/4); `store.GetAreaByTag`, `store.GetAreaByID`, `store.AddPrivateMessage` (Task 3).

- [ ] **Step 1: Write the failing tests**

In `internal/qwkservice/service_import_test.go`, add:

```go
func TestImportREP_PrivateConferenceRoutesToPrivate(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 3, Tag: "PRIVMAIL", Name: "Private Mail"})

	// Conference 0 is the private-mail conference after Sync.
	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 0, Number: 1, From: "tester", To: "friend", Subject: "Hey", DateTime: time.Now(), Body: "private reply"},
	})

	svc := newTestService(t, store)
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("ImportREP: %v", err)
	}
	if res.Posted != 1 {
		t.Errorf("want posted=1, got %+v", res)
	}
	if len(store.privPosted) != 1 {
		t.Fatalf("want 1 private post, got %d", len(store.privPosted))
	}
	if len(store.posted) != 0 {
		t.Errorf("private reply must not go through the public path, got %d public posts", len(store.posted))
	}
	if store.privPosted[0].to != "friend" {
		t.Errorf("private post To: want 'friend', got %q", store.privPosted[0].to)
	}
}

func TestImportREP_PublicConferenceRoutesToPublic(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 5, Tag: "GENERAL", Name: "General"})

	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 5, Number: 1, To: "All", Subject: "Hi", DateTime: time.Now(), Body: "public reply"},
	})

	svc := newTestService(t, store)
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("ImportREP: %v", err)
	}
	if res.Posted != 1 || len(store.posted) != 1 || len(store.privPosted) != 0 {
		t.Errorf("public reply should use AddMessage: res=%+v public=%d priv=%d", res, len(store.posted), len(store.privPosted))
	}
}

func TestImportREP_UnmappedNumberFallsBackToPublic(t *testing.T) {
	store := newFakeStore()
	store.addArea(&message.MessageArea{ID: 5, Tag: "GENERAL", Name: "General"})

	// Pre-seed a map that assigns GENERAL a number (9) different from its ID (5),
	// so a REP using the old area.ID (5) misses the map and exercises the
	// GetAreaByID fallback.
	dir := t.TempDir()
	preMap := `[{"qwk_number":9,"area_tag":"GENERAL","kind":"public"}]` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "qwk_conferences.json"), []byte(preMap), 0o644); err != nil {
		t.Fatal(err)
	}

	rep := makeREP(t, "VISION3", []qwk.PacketMessage{
		{Conference: 5, Number: 1, To: "All", Subject: "Hi", DateTime: time.Now(), Body: "legacy-numbered reply"},
	})

	svc := New(store, "VISION3", "ViSiON/3 BBS", "SysOp", dir)
	res, err := svc.ImportREP(rep, ImportOptions{Handle: "tester"})
	if err != nil {
		t.Fatalf("ImportREP: %v", err)
	}
	if res.Posted != 1 || len(store.posted) != 1 {
		t.Errorf("unmapped number should fall back to GetAreaByID and post public: res=%+v public=%d", res, len(store.posted))
	}
}
```

Add `"os"`, `"path/filepath"` to the import-test file's import block if not present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/qwkservice/ -run 'TestImportREP_(PrivateConference|PublicConference|UnmappedNumber)' -v`
Expected: FAIL — current `ImportREP` resolves areas via `GetAreaByID(msg.Conference)` and always calls `AddMessage`; conference 0 won't resolve (no area ID 0) so the private test sees `Posted=0`, and the private routing/`privPosted` expectations fail.

- [ ] **Step 3: Make `ImportREP` map-aware**

In `internal/qwkservice/service.go`, at the top of `ImportREP` (after the
`qwk.ReadREP` call and its error check), load the map:

```go
	cm, err := s.loadConfMap()
	if err != nil {
		return nil, err
	}
```

Replace the area-resolution + posting block inside the loop. The existing block is:

```go
		area, exists := s.store.GetAreaByID(msg.Conference)
		if !exists {
			slog.Warn("qwk import: unknown conference, skipping", "conference", msg.Conference)
			res.Skipped++
			continue
		}
```

Replace it (through the `AddMessage` call) with:

```go
		area, kind, ok := s.resolveConference(cm, msg.Conference)
		if !ok {
			slog.Warn("qwk import: unknown conference, skipping", "conference", msg.Conference)
			res.Skipped++
			continue
		}

		if opts.Authorize != nil && !opts.Authorize(area) {
			slog.Warn("qwk import: not authorized to post, skipping", "tag", area.Tag)
			res.Skipped++
			continue
		}

		if opts.Notify != nil {
			opts.Notify(area)
		}

		body := msg.Body
		if opts.Signature != "" {
			body = body + "\n\n" + opts.Signature
		}

		var perr error
		if kind == KindPrivateMail {
			_, perr = s.store.AddPrivateMessage(area.ID, opts.Handle, msg.To, msg.Subject, body, "")
		} else {
			_, perr = s.store.AddMessage(area.ID, opts.Handle, msg.To, msg.Subject, body, "")
		}
		if perr != nil {
			slog.Error("qwk import: failed to post", "area", area.ID, "error", perr)
			res.Skipped++
			continue
		}
		res.Posted++
```

Add the resolver helper near the bottom of `service.go`:

```go
// resolveConference maps a QWK conference number to a local area and its kind.
// It prefers the stable conference map; if the number is unmapped (e.g. a packet
// produced before the map existed, whose public numbers equal area IDs), it
// falls back to a direct area-ID lookup and treats the result as public.
func (s *Service) resolveConference(cm *ConferenceMap, number int) (*message.MessageArea, ConferenceKind, bool) {
	if entry, ok := cm.EntryForNumber(number); ok {
		if area, exists := s.store.GetAreaByTag(entry.AreaTag); exists {
			return area, entry.Kind, true
		}
	}
	if area, exists := s.store.GetAreaByID(number); exists {
		return area, KindPublic, true
	}
	return nil, KindPublic, false
}
```

Ensure `"github.com/ViSiON-3/vision-3-bbs/internal/message"` is imported in
`service.go` (it is used by `ownsPrivateMessage` from Task 4; add it if missing).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/qwkservice/ -run TestImportREP -v`
Expected: PASS — new tests pass and all pre-existing `TestImportREP_*` still pass (public area ID 1 maps to number 1; unknown conference 99 misses both lookups and is skipped).

- [ ] **Step 5: Commit**

```bash
git add internal/qwkservice/service.go internal/qwkservice/service_import_test.go
git commit -m "feat(qwk): route REP imports via conference map; private mail to AddPrivateMessage"
```

---

## Task 6: Sysop documentation + full verification

**Files:**
- Modify: `docs/sysop/messages/qwk.md`

- [ ] **Step 1: Read the current QWK doc to find the right insertion point**

Run: `sed -n '1,80p' docs/sysop/messages/qwk.md`
Expected: see the existing structure (overview / how packets are built / REP upload). Identify a logical place to add a "Conference mapping & private mail" subsection.

- [ ] **Step 2: Add the documentation section**

Insert a section (adapt headings to match the file's existing style):

```markdown
## Conference numbering and private mail

ViSiON/3 assigns each exported message area a **stable QWK conference number**
recorded in `data/qwk_conferences.json`. This file is created and maintained
automatically — do not hand-edit it. Once an area is assigned a number, that
number never changes, so offline readers and saved reply packets keep working
even if local area IDs are renumbered.

- Public areas are numbered from their local area ID the first time they are
  exported, then frozen.
- The private-mail area (tag `PRIVMAIL`) is always exported as **conference 0**.

**Private mail is per-user.** A QWK packet only includes private messages
addressed to, or sent by, the downloading user — never other users' mail.
Replies uploaded to conference 0 are posted as private mail, not to a public
base.
```

- [ ] **Step 3: Commit the docs**

```bash
git add docs/sysop/messages/qwk.md
git commit -m "docs(qwk): document stable conference map and private-mail handling"
```

- [ ] **Step 4: Full verification**

Run: `gofmt -l internal/qwk internal/qwkservice internal/menu internal/message`
Expected: no output (all formatted).

Run: `go vet ./... 2>&1 | tail -5`
Expected: no issues.

Run: `go test ./... 2>&1 | tail -10`
Expected: all packages PASS.

Run: `go test -race ./internal/qwkservice ./internal/qwk 2>&1 | tail -5`
Expected: PASS, no race warnings.

Run: `wc -l internal/qwkservice/*.go`
Expected: every file under 300 lines (split a test file if any exceeds it, reusing the shared `fakeStore`).

- [ ] **Step 5: Final commit (if the split or any cleanup was needed)**

```bash
git add -A
git commit -m "chore(qwk): Phase 2 cleanup and verification"
```

---

## Self-Review (completed by plan author)

- **Spec coverage:** map file (`data/qwk_conferences.json`) → Tasks 1,3; conf 0 = private mail → Tasks 1,4,5; stable numbers → Tasks 1,4; private export filter → Task 4; private import routing → Task 5; `DataPath`/interface wiring → Tasks 2,3; docs → Task 6. All spec sections covered.
- **Placeholder scan:** no TBD/TODO; every code step shows full code.
- **Type consistency:** `ConferenceMapEntry{QWKNumber,AreaTag,Kind}`, `EntryForTag`/`EntryForNumber`, `KindPublic`/`KindPrivateMail`, `loadConfMap`, `resolveConference`, `ownsPrivateMessage`, and the 5-arg `New` are used consistently across tasks. `AddPrivateMessage` signature matches `*message.MessageManager`.
