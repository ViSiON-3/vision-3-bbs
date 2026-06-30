# QWK Explicit BBS ID — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a sysop set an explicit, stable QWK BBS ID (config + config TUI) instead of always deriving it from the board name, with the derived behavior as a backward-compatible default.

**Architecture:** Add `ServerConfig.QWKID` plus a shared `config.NormalizeQWKID` normalizer. The menu resolves the effective ID as "explicit-if-set, else derive from board name, else BBS"; the config editor exposes an editable, normalize-on-set "QWK ID" field. Empty QWKID reproduces today's exact behavior.

**Tech Stack:** Go (stdlib `strings`, `encoding/json`, `testing`); existing `internal/config`, `internal/menu`, `internal/configeditor`.

## Global Constraints

- No new dependencies; `slog` for logging (none new here).
- TDD: failing test first → run red → minimal implementation → run green → commit.
- QWK BBS-ID rules (exact): keep ASCII `A–Z`, `a–z`, `0–9` only, upper-case, at most 8 chars. `NormalizeQWKID` returns `""` when nothing valid remains; the `"BBS"` fallback belongs to the resolver/`qwkBBSID`, not the normalizer.
- Empty `QWKID` ⇒ derive from `BoardName` ⇒ identical to current behavior (back-compat).
- Config TUI stores the **normalized** value (normalize-on-set; WYSIWYG).
- Spec: `docs/superpowers/specs/2026-06-30-qwk-explicit-bbs-id-design.md`.

---

## File Structure

- Modify: `internal/config/config.go` — add `QWKID` field to `ServerConfig`.
- Create: `internal/config/qwkid.go` — `NormalizeQWKID`.
- Create: `internal/config/qwkid_test.go` — `NormalizeQWKID` table test.
- Modify: `internal/config/config_test.go` — `QWKID` load/save round-trip + default.
- Modify: `internal/menu/qwk_handler.go` — reimplement `qwkBBSID` on the shared normalizer, add `resolveQWKID`, rewire both call sites.
- Modify: `internal/menu/qwk_handler_test.go` — `resolveQWKID` test (keep `TestQwkBBSID`).
- Modify: `internal/configeditor/fields_system.go` — add the "QWK ID" editable field.
- Create: `internal/configeditor/fields_system_test.go` — assert the field normalizes on set.
- Modify: `docs/sysop/messages/qwk.md` — document the setting.

---

## Task 1: Config field + shared normalizer

**Files:**
- Modify: `internal/config/config.go` (add `QWKID` to `ServerConfig`, ~line 841)
- Create: `internal/config/qwkid.go`
- Test: `internal/config/qwkid_test.go` (create), `internal/config/config_test.go` (modify)

**Interfaces:**
- Produces:
  - `ServerConfig` field `QWKID string` (`json:"qwkID,omitempty"`)
  - `func NormalizeQWKID(s string) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/config/qwkid_test.go`:

```go
package config

import "testing"

func TestNormalizeQWKID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "VISION3", "VISION3"},
		{"lowercased", "vision3", "VISION3"},
		{"spaces stripped", "My Cool BBS", "MYCOOLBB"},
		{"symbols stripped", "V!S!O#N/3", "VSON3"},
		{"truncated to 8", "LongBoardName123", "LONGBOAR"},
		{"empty", "", ""},
		{"all symbols", "!@#$%^&*()", ""},
		{"unicode stripped", "Café BBS", "CAFBBS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeQWKID(tt.in); got != tt.want {
				t.Errorf("NormalizeQWKID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
```

Add to `internal/config/config_test.go` (it already imports `encoding/json`, `os`, `path/filepath`, `testing`):

```go
func TestServerConfig_QWKID_RoundTrip(t *testing.T) {
	// Explicit qwkID in config.json loads through.
	dir := t.TempDir()
	data, _ := json.Marshal(map[string]interface{}{"qwkID": "VISION3"})
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadServerConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.QWKID != "VISION3" {
		t.Errorf("loaded QWKID: want VISION3, got %q", loaded.QWKID)
	}

	// Absent qwkID defaults to empty.
	def, err := LoadServerConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if def.QWKID != "" {
		t.Errorf("default QWKID: want empty, got %q", def.QWKID)
	}

	// Save then load preserves QWKID.
	saveDir := t.TempDir()
	if err := SaveServerConfig(saveDir, ServerConfig{QWKID: "ABC123"}); err != nil {
		t.Fatal(err)
	}
	back, err := LoadServerConfig(saveDir)
	if err != nil {
		t.Fatal(err)
	}
	if back.QWKID != "ABC123" {
		t.Errorf("round-trip QWKID: want ABC123, got %q", back.QWKID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run 'TestNormalizeQWKID|TestServerConfig_QWKID' -v`
Expected: FAIL — `undefined: NormalizeQWKID`, and `ServerConfig` has no field `QWKID`.

- [ ] **Step 3: Add the config field**

In `internal/config/config.go`, add the field to `ServerConfig` immediately after the `SysOpName` line (currently `SysOpName string \`json:"sysOpName"\`` at line ~841):

```go
	QWKID string `json:"qwkID,omitempty"` // Explicit QWK packet ID; blank = derive from BoardName
```

No change to `defaultConfig` (the zero value `""` is the intended default).

- [ ] **Step 4: Add the normalizer**

Create `internal/config/qwkid.go`:

```go
package config

import "strings"

// NormalizeQWKID reduces a string to a valid QWK BBS ID: ASCII letters and
// digits only, upper-cased, at most 8 characters. It returns "" when nothing
// valid remains; callers decide the fallback (e.g. derive from another field or
// use "BBS").
func NormalizeQWKID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			if b.Len() >= 8 {
				break
			}
		}
	}
	return strings.ToUpper(b.String())
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/ -run 'TestNormalizeQWKID|TestServerConfig_QWKID' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/config/config.go internal/config/qwkid.go internal/config/qwkid_test.go internal/config/config_test.go
go vet ./internal/config/
git add internal/config/config.go internal/config/qwkid.go internal/config/qwkid_test.go internal/config/config_test.go
git commit -m "feat(config): add explicit QWKID field and NormalizeQWKID"
```

---

## Task 2: Menu resolver + rewire call sites

**Files:**
- Modify: `internal/menu/qwk_handler.go`
- Test: `internal/menu/qwk_handler_test.go`

**Interfaces:**
- Consumes: `config.NormalizeQWKID` (Task 1), `config.ServerConfig` (with `QWKID`).
- Produces: `func resolveQWKID(cfg config.ServerConfig) string`; `qwkBBSID` reimplemented on the shared normalizer (signature unchanged).

- [ ] **Step 1: Write the failing test**

Add to `internal/menu/qwk_handler_test.go`. It currently imports `os`, `path/filepath`, `testing`; add the config import:

```go
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
```

Add the test:

```go
func TestResolveQWKID(t *testing.T) {
	// Explicit QWKID wins and is normalized.
	if got := resolveQWKID(config.ServerConfig{QWKID: "my id!", BoardName: "Whatever"}); got != "MYID" {
		t.Errorf("explicit: want MYID, got %q", got)
	}
	// Blank QWKID falls back to board-name derivation.
	if got := resolveQWKID(config.ServerConfig{QWKID: "", BoardName: "ViSiON/3 BBS"}); got != "VISION3B" {
		t.Errorf("derive: want VISION3B, got %q", got)
	}
	// Both blank/invalid → BBS.
	if got := resolveQWKID(config.ServerConfig{QWKID: "!!!", BoardName: "###"}); got != "BBS" {
		t.Errorf("fallback: want BBS, got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/menu/ -run TestResolveQWKID -v`
Expected: FAIL — `undefined: resolveQWKID`.

- [ ] **Step 3: Reimplement `qwkBBSID` and add `resolveQWKID`**

In `internal/menu/qwk_handler.go`, add the config import to the import block:

```go
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
```

Replace the existing `qwkBBSID` function (the comment block + body, currently lines ~22–39 — `func qwkBBSID(boardName string) string { ... }`) with:

```go
// qwkBBSID returns a short BBS identifier derived from the board name
// (alphanumeric, max 8 chars, uppercase), falling back to "BBS".
func qwkBBSID(boardName string) string {
	if id := config.NormalizeQWKID(boardName); id != "" {
		return id
	}
	return "BBS"
}

// resolveQWKID returns the BBS's QWK packet ID: the explicitly configured ID
// (normalized) if set, otherwise one derived from the board name (qwkBBSID).
func resolveQWKID(cfg config.ServerConfig) string {
	if id := config.NormalizeQWKID(cfg.QWKID); id != "" {
		return id
	}
	return qwkBBSID(cfg.BoardName)
}
```

- [ ] **Step 4: Rewire both call sites**

In `internal/menu/qwk_handler.go`, replace both occurrences of:

```go
	bbsID := qwkBBSID(e.ServerCfg.BoardName)
```

(one in `runQWKDownload` ~line 59, one in `runQWKUpload` ~line 183) with:

```go
	bbsID := resolveQWKID(e.ServerCfg)
```

Verify none remain: `grep -n 'qwkBBSID(e.ServerCfg' internal/menu/qwk_handler.go` → no output.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/menu/ -run 'TestResolveQWKID|TestQwkBBSID' -v`
Expected: PASS — both the new `TestResolveQWKID` and the existing `TestQwkBBSID` (qwkBBSID's behavior is unchanged: derive + "BBS" fallback).

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/menu/qwk_handler.go internal/menu/qwk_handler_test.go
go vet ./internal/menu/
git add internal/menu/qwk_handler.go internal/menu/qwk_handler_test.go
git commit -m "feat(qwk): resolve QWK ID from explicit config, deriving as fallback"
```

---

## Task 3: Config TUI field + docs + verification

**Files:**
- Modify: `internal/configeditor/fields_system.go`
- Create: `internal/configeditor/fields_system_test.go`
- Modify: `docs/sysop/messages/qwk.md`

**Interfaces:**
- Consumes: `config.NormalizeQWKID`, `ServerConfig.QWKID` (Task 1).

- [ ] **Step 1: Write the failing test**

Create `internal/configeditor/fields_system_test.go`:

```go
package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestSysFieldsRegistration_QWKIDNormalizesOnSet(t *testing.T) {
	cfg := &config.ServerConfig{}
	fields := sysFieldsRegistration(cfg)

	var f *fieldDef
	for i := range fields {
		if fields[i].Label == "QWK ID" {
			f = &fields[i]
			break
		}
	}
	if f == nil {
		t.Fatal("QWK ID field not found in registration screen")
	}
	if err := f.Set("my id!"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if cfg.QWKID != "MYID" {
		t.Errorf("Set should normalize: want cfg.QWKID == MYID, got %q", cfg.QWKID)
	}
	if got := f.Get(); got != "MYID" {
		t.Errorf("Get: want MYID, got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/configeditor/ -run TestSysFieldsRegistration_QWKIDNormalizesOnSet -v`
Expected: FAIL — "QWK ID field not found in registration screen".

- [ ] **Step 3: Add the editable field**

In `internal/configeditor/fields_system.go`, inside the slice returned by
`sysFieldsRegistration` (after the `Timezone` field at `Row: 4`, before the
closing `}` of the slice), add:

```go
		{
			Label: "QWK ID", Help: "Stable QWK packet ID (max 8, A-Z/0-9); blank = derive from Board Name",
			Type: ftString, Col: 3, Row: 5, Width: 8,
			Get: func() string { return cfg.QWKID },
			Set: func(val string) error { cfg.QWKID = config.NormalizeQWKID(val); return nil },
		},
```

(`config` is already imported in this file — `sysFieldsRegistration` takes
`*config.ServerConfig`.)

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/configeditor/ -run TestSysFieldsRegistration_QWKIDNormalizesOnSet -v`
Expected: PASS.

- [ ] **Step 5: Document the setting**

In `docs/sysop/messages/qwk.md`, add a short subsection (adapt heading level to
the file; a good spot is near the conference-numbering / upload notes):

```markdown
## QWK ID

The **QWK ID** is the short identifier (max 8 characters, letters and digits)
used for packet filenames (`<ID>.QWK` / `<ID>.REP`) and for the destination
check on REP uploads. Set it in the config editor (System → Registration → QWK
ID). Leave it blank to derive it automatically from the board name.

Treat the QWK ID as a **stable identity** — set it once, early. Changing it later
re-keys your packets: offline readers will see a different BBS ID, and saved
`.QWK`/`.REP` files keyed to the old ID stop matching.
```

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/configeditor/fields_system.go internal/configeditor/fields_system_test.go
git add internal/configeditor/fields_system.go internal/configeditor/fields_system_test.go docs/sysop/messages/qwk.md
git commit -m "feat(configeditor): add editable QWK ID field; document it"
```

- [ ] **Step 7: Full verification**

Run: `gofmt -l internal/config internal/menu internal/configeditor`
Expected: no output.

Run: `go vet ./... 2>&1 | tail -5`
Expected: no issues.

Run: `go test ./... 2>&1 | tail -10`
Expected: all packages PASS.

- [ ] **Step 8: Final commit (only if cleanup was needed)**

```bash
git add -A
git commit -m "chore(qwk): explicit BBS ID cleanup and verification"
```

---

## Self-Review (completed by plan author)

- **Spec coverage:** config field + `NormalizeQWKID` → Task 1; resolver + rewire + `qwkBBSID` reuse → Task 2; config TUI field (normalize-on-set) + docs → Task 3. Tests for normalizer, round-trip, resolver, and TUI-field-normalizes are all present. All spec sections covered.
- **Refinement vs spec:** the spec said "keep `qwkBBSID`"; the resolver here *delegates* its board-name path to `qwkBBSID` so the function stays live (no dead code) and its existing test stays meaningful — fully consistent with the spec's intent.
- **Placeholder scan:** no TBD/TODO; every code step shows full code; the TUI row (`Row: 5`) is pinned (Rows 1–4 are Board Name / SysOp / Location / Timezone).
- **Type consistency:** `NormalizeQWKID(string) string`, `resolveQWKID(config.ServerConfig) string`, `qwkBBSID(string) string`, and `ServerConfig.QWKID` are used consistently across tasks; `fieldDef`'s `Get`/`Set`/`Label`/`Type`/`Col`/`Row`/`Width` match the existing descriptor shape.
