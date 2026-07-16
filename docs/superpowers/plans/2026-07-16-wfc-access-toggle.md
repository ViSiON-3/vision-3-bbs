# WFC Access Toggle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the sysop enable/disable remote WFC (Waiting For Call) console access from the `./config` TUI, hot-reloadable, defaulting to enabled.

**Architecture:** A `WFCEnabled bool` on `config.ServerConfig` (default true via the unmarshal-over-defaults loader). The `vision3` daemon enforces it in `authorizeAdmin` via a live getter (`wfcEnabled func() bool`), mirroring the existing `adminMinLevel` pattern, so both SSH public-key auth and the `wfc-admin` subsystem re-check reject when disabled. The config TUI exposes it as a Y/N field on the Access Levels sub-screen.

**Tech Stack:** Go stdlib, `slog` logging, existing `configeditor` bubbletea TUI, `uitext.BoolToYN`/`YNToBool` helpers.

**Spec:** `docs/superpowers/specs/2026-07-16-wfc-access-toggle-design.md`

## Global Constraints

- Go stdlib only; no new dependencies.
- Structured logging via `slog` only.
- Run `gofmt -w` and `go vet` on every changed file before each commit.
- All tests in affected packages must pass before each commit (`go test ./internal/config/... ./cmd/vision3/... ./internal/configeditor/...`).
- Keep files under 300 lines where practical (all edits here are small insertions into existing files).

---

### Task 1: `WFCEnabled` config field with default true

**Files:**
- Modify: `internal/config/config.go` (struct ~line 847, defaults ~line 1073)
- Test: `internal/config/config_test.go` (extend `TestLoadServerConfig_Defaults` ~line 124, `TestLoadServerConfig_CustomValues` ~line 142)

**Interfaces:**
- Consumes: nothing new.
- Produces: `config.ServerConfig.WFCEnabled bool` (JSON `wfcEnabled`), default `true` when absent from `config.json`. Tasks 2 and 3 read/write this exact field.

- [ ] **Step 1: Write the failing tests**

In `internal/config/config_test.go`, add to the end of `TestLoadServerConfig_Defaults` (before its closing brace, after the `BoardName` check at line ~139):

```go
	if !result.WFCEnabled {
		t.Error("expected WFCEnabled to default to true")
	}
```

Add to `TestLoadServerConfig_CustomValues`: extend the `cfg` map literal with an explicit override, and assert it:

```go
	cfg := map[string]interface{}{
		"boardName":  "Test BBS",
		"sshPort":    3333,
		"maxNodes":   50,
		"wfcEnabled": false,
	}
```

and at the end of the function:

```go
	if result.WFCEnabled {
		t.Error("expected WFCEnabled false when explicitly disabled in config.json")
	}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run 'TestLoadServerConfig_Defaults|TestLoadServerConfig_CustomValues' -v`
Expected: compile FAIL — `result.WFCEnabled undefined (type ServerConfig has no field or method WFCEnabled)`

- [ ] **Step 3: Add the field and default**

In `internal/config/config.go`, in the `ServerConfig` struct, insert directly after the `CoSysOpLevel` line (~847):

```go
	WFCEnabled          bool   `json:"wfcEnabled"` // Allow remote WFC sysop console (wfc-admin subsystem)
```

In `LoadServerConfig`'s `defaultConfig` literal (~line 1069), insert after `CoSysOpLevel: 250,`:

```go
		WFCEnabled:                true,
```

(Match the literal's existing alignment; run `gofmt -w internal/config/config.go` to settle it.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v -run TestLoadServerConfig`
Expected: PASS (all `TestLoadServerConfig_*` tests, including the partial-overlay test which must still pass — it proves files without the key keep the default).

- [ ] **Step 5: Lint and commit**

```bash
gofmt -l internal/config/ && go vet ./internal/config/
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add WFCEnabled server setting, default true"
```

---

### Task 2: Daemon enforcement via live getter

**Files:**
- Modify: `cmd/vision3/wfc_admin.go` (getter var ~line 19, `wfcPublicKeyHandler` ~line 30, `authorizeAdmin` ~line 53)
- Modify: `cmd/vision3/main.go` (~line 1529, next to the `adminMinLevel` wiring)
- Test: `cmd/vision3/wfc_admin_test.go`

**Interfaces:**
- Consumes: `config.ServerConfig.WFCEnabled` from Task 1, via `menuExecutor.GetServerConfig()`.
- Produces: package-level `var wfcEnabled func() bool` in `cmd/vision3`; `authorizeAdmin(handle string) bool` now also requires `wfcEnabled != nil && wfcEnabled()`.

- [ ] **Step 1: Update existing tests and write the failing test**

Replace the body of `cmd/vision3/wfc_admin_test.go` with (existing tests gain `wfcEnabled` setup since a nil getter now denies; new test covers the disabled path):

```go
package main

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

func TestAuthorizeAdmin(t *testing.T) {
	// Seed userMgr with a low-access user and a sysop.
	userMgr = user.NewUserMgrForTest(
		&user.User{Handle: "lowly", AccessLevel: 10},
		&user.User{Handle: "boss", AccessLevel: 255},
	)
	adminMinLevel = func() int { return 250 }
	wfcEnabled = func() bool { return true }

	if authorizeAdmin("lowly") {
		t.Error("expected lowly (level 10) to be denied admin access")
	}
	if !authorizeAdmin("boss") {
		t.Error("expected boss (level 255) to be granted admin access")
	}
}

func TestAuthorizeAdmin_UnknownUser(t *testing.T) {
	userMgr = user.NewUserMgrForTest()
	adminMinLevel = func() int { return 250 }
	wfcEnabled = func() bool { return true }

	if authorizeAdmin("ghost") {
		t.Error("expected unknown user to be denied admin access")
	}
}

func TestAuthorizeAdmin_WFCDisabled(t *testing.T) {
	userMgr = user.NewUserMgrForTest(
		&user.User{Handle: "boss", AccessLevel: 255},
	)
	adminMinLevel = func() int { return 250 }

	wfcEnabled = func() bool { return false }
	if authorizeAdmin("boss") {
		t.Error("expected admin access denied when WFC is disabled")
	}

	wfcEnabled = nil
	if authorizeAdmin("boss") {
		t.Error("expected admin access denied when wfcEnabled getter is nil")
	}
}
```

- [ ] **Step 2: Run tests to verify the new test fails**

Run: `go test ./cmd/vision3/ -run TestAuthorizeAdmin -v`
Expected: compile FAIL — `undefined: wfcEnabled`

- [ ] **Step 3: Implement the gate**

In `cmd/vision3/wfc_admin.go`, add directly below the `adminMinLevel` declaration (~line 19):

```go
// wfcEnabled reports whether remote WFC admin access is allowed at all.
// Like adminMinLevel it is a live getter so config hot-reloads take effect
// without a restart. Nil (not yet wired, or a test that left it unset) denies.
var wfcEnabled func() bool
```

Replace the body of `authorizeAdmin` so the flag is checked first:

```go
func authorizeAdmin(handle string) bool {
	if userMgr == nil || adminMinLevel == nil || wfcEnabled == nil || !wfcEnabled() {
		return false
	}
	u, found := userMgr.GetUser(handle)
	if !found || u == nil {
		return false
	}
	return u.AccessLevel >= adminMinLevel()
}
```

Also update its doc comment's last sentence to mention the flag, e.g.:

```go
// authorizeAdmin returns true when WFC admin access is enabled and the user
// identified by handle exists with an access level >= the live adminMinLevel
// threshold. It denies access if either live getter is nil (daemon not yet
// initialised or running in a test that deliberately left it unset).
```

In `wfcPublicKeyHandler`, distinguish the disabled case in the rejection branch so the log line isn't misleading. Replace the `if !authorizeAdmin(u.Handle) { ... }` block with:

```go
	if !authorizeAdmin(u.Handle) {
		if wfcEnabled == nil || !wfcEnabled() {
			slog.Info("wfc-admin: public key rejected, wfc access disabled",
				"user", u.Handle, "addr", ctx.RemoteAddr())
			return false
		}
		minLevel := 0
		if adminMinLevel != nil {
			minLevel = adminMinLevel()
		}
		slog.Info("wfc-admin: public key rejected, insufficient access level",
			"user", u.Handle, "level", u.AccessLevel, "required", minLevel)
		return false
	}
```

In `cmd/vision3/main.go`, directly after the `adminMinLevel = ...` line (~1529), add:

```go
	wfcEnabled = func() bool { return menuExecutor.GetServerConfig().WFCEnabled }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/vision3/ -v -run TestAuthorizeAdmin`
Expected: PASS (all three tests). Then run the whole package: `go test ./cmd/vision3/` — expected: ok.

- [ ] **Step 5: Lint and commit**

```bash
gofmt -l cmd/vision3/ && go vet ./cmd/vision3/
git add cmd/vision3/wfc_admin.go cmd/vision3/wfc_admin_test.go cmd/vision3/main.go
git commit -m "feat(wfc): gate wfc-admin access on WFCEnabled config flag"
```

---

### Task 3: Config TUI field on Access Levels screen

**Files:**
- Modify: `internal/configeditor/fields_system.go` (`sysFieldsLevels`, ~lines 355–441)
- Test: `internal/configeditor/fields_system_test.go`

**Interfaces:**
- Consumes: `config.ServerConfig.WFCEnabled` from Task 1; existing `uitext.BoolToYN` / `uitext.YNToBool` helpers (already imported in `fields_system.go`).
- Produces: a `WFC Access` field (Type `ftYesNo`) in the slice returned by `sysFieldsLevels`.

- [ ] **Step 1: Write the failing test**

Append to `internal/configeditor/fields_system_test.go`:

```go
func TestSysFieldsLevels_WFCAccess(t *testing.T) {
	cfg := &config.ServerConfig{WFCEnabled: true}
	fields := sysFieldsLevels(cfg)

	var f *fieldDef
	for i := range fields {
		if fields[i].Label == "WFC Access" {
			f = &fields[i]
			break
		}
	}
	if f == nil {
		t.Fatal("WFC Access field not found in Access Levels screen")
	}
	if f.Type != ftYesNo {
		t.Errorf("WFC Access type: want ftYesNo, got %v", f.Type)
	}
	if got := f.Get(); got != "Y" {
		t.Errorf("Get with WFCEnabled=true: want Y, got %q", got)
	}
	if err := f.Set("N"); err != nil {
		t.Fatalf("Set(N): %v", err)
	}
	if cfg.WFCEnabled {
		t.Error("Set(N) should clear cfg.WFCEnabled")
	}
	if got := f.Get(); got != "N" {
		t.Errorf("Get after Set(N): want N, got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/configeditor/ -run TestSysFieldsLevels_WFCAccess -v`
Expected: FAIL — `WFC Access field not found in Access Levels screen`

- [ ] **Step 3: Add the field**

In `sysFieldsLevels` in `internal/configeditor/fields_system.go`, insert a new field directly after the `CoSysOp Level` entry (Row 2) and renumber the rows of every subsequent field in the slice (+1 each):

```go
		{
			Label: "WFC Access", Help: "Allow remote WFC sysop console connections", Type: ftYesNo, Col: 3, Row: 3, Width: 1,
			Get: func() string { return uitext.BoolToYN(cfg.WFCEnabled) },
			Set: func(val string) error { cfg.WFCEnabled = uitext.YNToBool(val); return nil },
		},
```

Row renumbering in the same function: `Invisible Lvl` 3→4, `New User Level` 4→5, `Regular Level` 5→6, `Logon Level` 6→7, `Anonymous Lvl` 7→8. (The Access Levels box auto-sizes from the max Row, so no view changes are needed.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/configeditor/`
Expected: ok (new test passes; golden/view tests still pass since the box height is derived from max row — if a golden test for the system screen fails, inspect the diff: it should only show the added row, then regenerate per that test's documented `-update` flag if it has one, otherwise update the golden expectation by hand).

- [ ] **Step 5: Lint, full test sweep, and commit**

```bash
gofmt -l internal/configeditor/ && go vet ./internal/configeditor/
go test ./internal/config/... ./cmd/vision3/... ./internal/configeditor/...
git add internal/configeditor/fields_system.go internal/configeditor/fields_system_test.go
git commit -m "feat(configeditor): add WFC Access toggle to Access Levels screen"
```

---

### Task 4: Full verification

**Files:** none new.

**Interfaces:** n/a — verification only.

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: all packages ok.

- [ ] **Step 2: Build both binaries**

Run: `go build ./cmd/vision3 ./cmd/wfc ./cmd/config`
Expected: clean build, no output. Remove any produced binaries from the repo root afterwards (`rm -f vision3 wfc config`).

- [ ] **Step 3: Commit (only if anything changed)**

Nothing should need committing; if a golden file was regenerated in Task 3 it was already committed there.
