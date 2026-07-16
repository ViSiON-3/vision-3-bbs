# Integrated binkd Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run the bundled `bin/binkd` mailer as a supervised child process of the BBS, toggled from the config editor's Server Setup screen, with binkd.conf settings synced automatically and outbound mail exported in-process.

**Architecture:** A new `internal/mailer` daemon package (V3Net/scheduler pattern: `New` → `Start(ctx)` → `Close`) supervises the binkd child with restart backoff and runs a periodic outbound export using the existing `internal/tosser`. Config lives in a new `Binkd` section of `configs/ftn.json`; the TUI Server Setup screen gains a Binkd field group; `internal/ftn` gains a `SyncBinkdSettings` helper for `iport`/`loglevel`. Inbound tossing already works via the `exec "v3mail toss"` hook the generator emits.

**Tech Stack:** Go stdlib only (`os/exec`, `context`, `time`, `log/slog`). No new dependencies.

**Spec:** `docs-internal/plans/2026-07-16-binkd-server-design.md`

## Global Constraints

- Follow `CLAUDE.md`: `slog` for logging, TDD, `gofmt`/`go vet` on changed files, files under 300 lines, errors wrapped `fmt.Errorf("context: %w", err)`.
- All goroutines must respect `context.Context` cancellation.
- Mailer failures are never fatal to BBS startup — warn and continue.
- Defaults (exact values): `Port` 24554, `BinaryPath` `"bin/binkd"`, `LogLevel` 4, `ExportSecs` 300, `Enabled` false.
- binkd.conf path is always `<BBSRoot>/data/ftn/binkd.conf`.
- Run `go test ./internal/... ./cmd/...` and `go vet ./...` before each commit; concurrent code with `-race`.

---

### Task 1: Config schema — `BinkdServerConfig` in ftn.json

**Files:**
- Modify: `internal/config/config.go` (FTNConfig at ~line 1055, LoadFTNConfig at ~line 1135)
- Test: `internal/config/config_binkd_test.go` (create)

**Interfaces:**
- Produces: `config.BinkdServerConfig{Enabled bool; Port int; BinaryPath string; LogLevel int; ExportSecs int}`, reachable as `FTNConfig.Binkd`. `LoadFTNConfig` always returns it with defaults applied (even when ftn.json is missing). Also `(c *FTNConfig) ResolvePaths(root string)` which makes the five FTN path fields absolute against `root`.

- [ ] **Step 1: Write the failing tests**

Create `internal/config/config_binkd_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFTNConfigBinkdDefaults(t *testing.T) {
	// Missing ftn.json → defaults still applied.
	cfg, err := LoadFTNConfig(t.TempDir())
	if err != nil {
		t.Fatalf("LoadFTNConfig: %v", err)
	}
	b := cfg.Binkd
	if b.Enabled {
		t.Error("Enabled should default to false")
	}
	if b.Port != 24554 {
		t.Errorf("Port = %d, want 24554", b.Port)
	}
	if b.BinaryPath != "bin/binkd" {
		t.Errorf("BinaryPath = %q, want bin/binkd", b.BinaryPath)
	}
	if b.LogLevel != 4 {
		t.Errorf("LogLevel = %d, want 4", b.LogLevel)
	}
	if b.ExportSecs != 300 {
		t.Errorf("ExportSecs = %d, want 300", b.ExportSecs)
	}
}

func TestLoadFTNConfigBinkdRoundTrip(t *testing.T) {
	dir := t.TempDir()
	body := `{"networks":{},"binkd":{"enabled":true,"port":24555,"binary_path":"/usr/local/sbin/binkd","log_level":6,"export_interval_seconds":60}}`
	if err := os.WriteFile(filepath.Join(dir, "ftn.json"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFTNConfig(dir)
	if err != nil {
		t.Fatalf("LoadFTNConfig: %v", err)
	}
	b := cfg.Binkd
	if !b.Enabled || b.Port != 24555 || b.BinaryPath != "/usr/local/sbin/binkd" || b.LogLevel != 6 || b.ExportSecs != 60 {
		t.Errorf("unexpected Binkd config: %+v", b)
	}
}

func TestLoadFTNConfigBinkdPartialDefaults(t *testing.T) {
	// Enabled set but numeric fields omitted → defaults fill the gaps.
	dir := t.TempDir()
	body := `{"networks":{},"binkd":{"enabled":true}}`
	if err := os.WriteFile(filepath.Join(dir, "ftn.json"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFTNConfig(dir)
	if err != nil {
		t.Fatalf("LoadFTNConfig: %v", err)
	}
	b := cfg.Binkd
	if !b.Enabled || b.Port != 24554 || b.BinaryPath != "bin/binkd" || b.LogLevel != 4 || b.ExportSecs != 300 {
		t.Errorf("unexpected Binkd config: %+v", b)
	}
}

func TestFTNConfigResolvePaths(t *testing.T) {
	cfg := FTNConfig{
		InboundPath:       "data/ftn/in",
		SecureInboundPath: "/abs/secure_in",
		OutboundPath:      "data/ftn/outbound",
		BinkdOutboundPath: "data/ftn/out",
		TempPath:          "data/ftn/temp",
	}
	cfg.ResolvePaths("/bbs")
	if cfg.InboundPath != filepath.Join("/bbs", "data/ftn/in") {
		t.Errorf("InboundPath = %q", cfg.InboundPath)
	}
	if cfg.SecureInboundPath != "/abs/secure_in" {
		t.Errorf("absolute path must be untouched, got %q", cfg.SecureInboundPath)
	}
	if cfg.BinkdOutboundPath != filepath.Join("/bbs", "data/ftn/out") {
		t.Errorf("BinkdOutboundPath = %q", cfg.BinkdOutboundPath)
	}
	// Empty paths stay empty.
	var empty FTNConfig
	empty.ResolvePaths("/bbs")
	if empty.InboundPath != "" {
		t.Errorf("empty path must stay empty, got %q", empty.InboundPath)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run 'Binkd|ResolvePaths' -v`
Expected: FAIL — `cfg.Binkd undefined` / `cfg.ResolvePaths undefined` (compile errors).

- [ ] **Step 3: Implement**

In `internal/config/config.go`, immediately above `type FTNConfig`:

```go
// BinkdServerConfig controls the integrated binkd mailer daemon.
// Zero-valued numeric/string fields are filled with defaults by LoadFTNConfig.
type BinkdServerConfig struct {
	Enabled    bool   `json:"enabled"`                 // Run binkd as a supervised child of the BBS
	Port       int    `json:"port"`                    // binkp listen port (default 24554)
	BinaryPath string `json:"binary_path"`             // Path to binkd binary, relative to BBS root (default "bin/binkd")
	LogLevel   int    `json:"log_level"`               // binkd loglevel (default 4)
	ExportSecs int    `json:"export_interval_seconds"` // Outbound scan/pack cadence (default 300)
}
```

Add the field to `FTNConfig` (after `Networks`):

```go
	Binkd             BinkdServerConfig           `json:"binkd"`                         // Integrated binkd mailer daemon
```

Add a defaults helper and call it from `LoadFTNConfig` on **both** return paths (the `os.IsNotExist` early return and the successful parse, before `return config, nil`):

```go
// applyBinkdDefaults fills zero-valued BinkdServerConfig fields with defaults.
func applyBinkdDefaults(c *BinkdServerConfig) {
	if c.Port == 0 {
		c.Port = 24554
	}
	if c.BinaryPath == "" {
		c.BinaryPath = "bin/binkd"
	}
	if c.LogLevel == 0 {
		c.LogLevel = 4
	}
	if c.ExportSecs == 0 {
		c.ExportSecs = 300
	}
}
```

In `LoadFTNConfig`: change the missing-file branch to `applyBinkdDefaults(&defaultConfig.Binkd); return defaultConfig, nil` (keep the slog line), and add `applyBinkdDefaults(&config.Binkd)` just before the final `return config, nil`.

Add `ResolvePaths` after `ValidateFTNConfig`:

```go
// ResolvePaths makes the FTN path fields absolute by joining relative paths
// against root (the BBS root directory). Empty and absolute paths are unchanged.
func (c *FTNConfig) ResolvePaths(root string) {
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(root, p)
	}
	c.InboundPath = resolve(c.InboundPath)
	c.SecureInboundPath = resolve(c.SecureInboundPath)
	c.OutboundPath = resolve(c.OutboundPath)
	c.BinkdOutboundPath = resolve(c.BinkdOutboundPath)
	c.TempPath = resolve(c.TempPath)
	if c.DupeDBPath != "" {
		c.DupeDBPath = resolve(c.DupeDBPath)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run 'Binkd|ResolvePaths' -v` → PASS, then `go test ./internal/config/` → PASS (no regressions).

- [ ] **Step 5: Refactor `cmd/v3mail` to use ResolvePaths (DRY)**

In `cmd/v3mail/cmd_ftn.go` `loadFTNDeps` (line ~226), replace the five `ftnCfg.X = resolveFTNPath(bbsRoot, ftnCfg.X)` lines **and** the later `dupeDBPath = resolveFTNPath(bbsRoot, dupeDBPath)` else-branch with:

```go
	ftnCfg.ResolvePaths(bbsRoot)
```

and simplify the dupe DB block to:

```go
	dupeDBPath := ftnCfg.DupeDBPath
	if dupeDBPath == "" {
		dupeDBPath = filepath.Join(dataDir, "ftn", "dupes.json")
	}
```

Delete the now-unused `resolveFTNPath` function.

- [ ] **Step 6: Verify and commit**

Run: `go build ./... && go test ./internal/config/ ./cmd/... && go vet ./internal/config/ ./cmd/v3mail/`
Expected: PASS.

```bash
git add internal/config/config.go internal/config/config_binkd_test.go cmd/v3mail/cmd_ftn.go
git commit -m "feat(config): add BinkdServerConfig to ftn.json with defaults and FTNConfig.ResolvePaths"
```

---

### Task 2: `ftn.SyncBinkdSettings` — sync iport/loglevel into binkd.conf

**Files:**
- Modify: `internal/ftn/binkd.go` (add function at end, near `SyncBinkdConf`)
- Test: `internal/ftn/binkd_settings_test.go` (create)

**Interfaces:**
- Consumes: existing `writeFileAtomic(path, content string, perm os.FileMode) error` in the same file.
- Produces: `ftn.SyncBinkdSettings(confPath string, port, logLevel int) error` — rewrites `iport` and `loglevel` lines in an existing binkd.conf; returns nil (no-op) if the file doesn't exist or nothing changed; ignores non-positive port/logLevel (leaves line as-is).

- [ ] **Step 1: Write the failing tests**

Create `internal/ftn/binkd_settings_test.go`:

```go
package ftn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const settingsConf = `# binkd.conf
sysname "Test BBS"
loglevel 4
iport 24554
node 21:1/100@fsxnet host:24554 secret
`

func writeConf(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "binkd.conf")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSyncBinkdSettingsUpdatesLines(t *testing.T) {
	path := writeConf(t, settingsConf)
	if err := SyncBinkdSettings(path, 24555, 6); err != nil {
		t.Fatalf("SyncBinkdSettings: %v", err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	if !strings.Contains(s, "iport 24555\n") {
		t.Errorf("iport not updated:\n%s", s)
	}
	if !strings.Contains(s, "loglevel 6\n") {
		t.Errorf("loglevel not updated:\n%s", s)
	}
	if !strings.Contains(s, "node 21:1/100@fsxnet host:24554 secret") {
		t.Errorf("node line must be untouched:\n%s", s)
	}
}

func TestSyncBinkdSettingsNoChangeLeavesFile(t *testing.T) {
	path := writeConf(t, settingsConf)
	before, _ := os.Stat(path)
	if err := SyncBinkdSettings(path, 24554, 4); err != nil {
		t.Fatalf("SyncBinkdSettings: %v", err)
	}
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("file must not be rewritten when values already match")
	}
}

func TestSyncBinkdSettingsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "binkd.conf")
	if err := SyncBinkdSettings(path, 24554, 4); err != nil {
		t.Fatalf("missing file must be a no-op, got: %v", err)
	}
}

func TestSyncBinkdSettingsNonPositiveIgnored(t *testing.T) {
	path := writeConf(t, settingsConf)
	if err := SyncBinkdSettings(path, 0, 0); err != nil {
		t.Fatalf("SyncBinkdSettings: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "iport 24554\n") || !strings.Contains(string(got), "loglevel 4\n") {
		t.Errorf("zero values must leave lines untouched:\n%s", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ftn/ -run SyncBinkdSettings -v`
Expected: FAIL — `undefined: SyncBinkdSettings`.

- [ ] **Step 3: Implement**

Append to `internal/ftn/binkd.go` (before `writeFileAtomic`):

```go
// SyncBinkdSettings updates the iport and loglevel lines in binkd.conf to
// match the configured values. The file is only rewritten when a value
// differs; a missing binkd.conf is a no-op (the FTN Setup Wizard creates it).
// Non-positive port/logLevel values leave the corresponding line untouched.
func SyncBinkdSettings(confPath string, port, logLevel int) error {
	existing, err := os.ReadFile(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No binkd.conf to sync.
		}
		return fmt.Errorf("reading binkd.conf: %w", err)
	}

	var out strings.Builder
	changed := false
	scanner := bufio.NewScanner(strings.NewReader(string(existing)))

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "iport ") && port > 0 {
			newLine := fmt.Sprintf("iport %d", port)
			if trimmed != newLine {
				out.WriteString(newLine)
				out.WriteByte('\n')
				changed = true
				continue
			}
		}
		if strings.HasPrefix(trimmed, "loglevel ") && logLevel > 0 {
			newLine := fmt.Sprintf("loglevel %d", logLevel)
			if trimmed != newLine {
				out.WriteString(newLine)
				out.WriteByte('\n')
				changed = true
				continue
			}
		}

		out.WriteString(line)
		out.WriteByte('\n')
	}

	if !changed {
		return nil
	}
	return writeFileAtomic(confPath, out.String(), 0600)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ftn/ -v` → PASS (new tests and existing binkd tests).

- [ ] **Step 5: Commit**

```bash
git add internal/ftn/binkd.go internal/ftn/binkd_settings_test.go
git commit -m "feat(ftn): add SyncBinkdSettings to sync iport/loglevel into binkd.conf"
```

---

### Task 3: `internal/mailer` — supervised binkd daemon with export loop

**Files:**
- Create: `internal/mailer/mailer.go` (service, preflight)
- Create: `internal/mailer/supervisor.go` (child process loop)
- Create: `internal/mailer/export.go` (outbound export ticker)
- Test: `internal/mailer/mailer_test.go`, `internal/mailer/supervisor_test.go`

**Interfaces:**
- Consumes: `config.FTNConfig` (with `.Binkd`, paths already resolved via `ResolvePaths` — Task 1), `ftn.SyncBinkdSettings(confPath string, port, logLevel int) error` (Task 2), `tosser.New(name string, cfg config.FTNNetworkConfig, globalCfg config.FTNConfig, dupeDB *tosser.DupeDB, msgMgr *message.MessageManager) (*tosser.Tosser, error)`, `(*tosser.Tosser).ScanAndExport() tosser.TossResult`, `(*tosser.Tosser).PackOutbound() tosser.PackResult`, `(*tosser.DupeDB).Save() error`.
- Produces: `mailer.Config{BBSRoot string; FTN config.FTNConfig; MsgMgr *message.MessageManager; DupeDB *tosser.DupeDB}`, `mailer.New(cfg Config) (*Service, error)` (preflight errors returned here), `(*Service).Start(ctx context.Context)` (blocks until ctx done), `(*Service).Close() error`. Used by Task 4.

- [ ] **Step 1: Write failing preflight tests**

Create `internal/mailer/mailer_test.go`:

```go
package mailer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// newTestRoot builds a BBS root with a fake binkd binary and a binkd.conf.
func newTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	ftnDir := filepath.Join(root, "data", "ftn")
	for _, d := range []string{binDir, ftnDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	// Fake binkd: a shell script that sleeps.
	script := "#!/bin/sh\nsleep 60\n"
	if err := os.WriteFile(filepath.Join(binDir, "binkd"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ftnDir, "binkd.conf"), []byte("iport 24554\nloglevel 4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func testFTNConfig() config.FTNConfig {
	return config.FTNConfig{
		Networks: map[string]config.FTNNetworkConfig{
			"fsxnet": {OwnAddress: "21:4/158", InternalTosserEnabled: true},
		},
		Binkd: config.BinkdServerConfig{
			Enabled: true, Port: 24554, BinaryPath: "bin/binkd", LogLevel: 4, ExportSecs: 300,
		},
	}
}

func TestNewPreflightOK(t *testing.T) {
	root := newTestRoot(t)
	svc, err := New(Config{BBSRoot: root, FTN: testFTNConfig()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestNewPreflightMissingBinary(t *testing.T) {
	root := newTestRoot(t)
	if err := os.Remove(filepath.Join(root, "bin", "binkd")); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{BBSRoot: root, FTN: testFTNConfig()}); err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestNewPreflightMissingConf(t *testing.T) {
	root := newTestRoot(t)
	if err := os.Remove(filepath.Join(root, "data", "ftn", "binkd.conf")); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{BBSRoot: root, FTN: testFTNConfig()}); err == nil {
		t.Fatal("expected error for missing binkd.conf")
	}
}

func TestNewPreflightNoAddresses(t *testing.T) {
	root := newTestRoot(t)
	cfg := testFTNConfig()
	cfg.Networks = map[string]config.FTNNetworkConfig{"fsxnet": {}}
	if _, err := New(Config{BBSRoot: root, FTN: cfg}); err == nil {
		t.Fatal("expected error when no network has an own address")
	}
}

func TestNewPreflightBadPort(t *testing.T) {
	root := newTestRoot(t)
	cfg := testFTNConfig()
	cfg.Binkd.Port = 99999
	if _, err := New(Config{BBSRoot: root, FTN: cfg}); err == nil {
		t.Fatal("expected error for out-of-range port")
	}
}

func TestExportLoopDisabledWithoutDeps(t *testing.T) {
	// With no MsgMgr/DupeDB the export loop must return immediately, not tick.
	root := newTestRoot(t)
	svc, err := New(Config{BBSRoot: root, FTN: testFTNConfig()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	done := make(chan struct{})
	go func() { svc.exportLoop(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("exportLoop must return immediately when deps are nil")
	}
}

func TestNewPreflightAbsoluteBinaryPath(t *testing.T) {
	root := newTestRoot(t)
	cfg := testFTNConfig()
	cfg.Binkd.BinaryPath = filepath.Join(root, "bin", "binkd") // absolute
	if _, err := New(Config{BBSRoot: root, FTN: cfg}); err != nil {
		t.Fatalf("absolute binary path must work: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/mailer/ -v`
Expected: FAIL — package doesn't exist / `undefined: New`.

- [ ] **Step 3: Implement `mailer.go`**

Create `internal/mailer/mailer.go`:

```go
// Package mailer supervises the bundled binkd FTN mailer as a child process
// of the BBS and periodically exports outbound echomail/netmail into binkd's
// outbound queue using the internal tosser.
package mailer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/tosser"
)

// Config holds the dependencies for the mailer service. FTN paths must
// already be resolved to absolute paths (FTNConfig.ResolvePaths).
type Config struct {
	BBSRoot string                  // absolute BBS root directory
	FTN     config.FTNConfig        // FTN config including the Binkd section
	MsgMgr  *message.MessageManager // for the export loop (may be nil in tests)
	DupeDB  *tosser.DupeDB          // for the export loop (may be nil in tests)
}

// Service runs binkd under supervision plus the outbound export loop.
type Service struct {
	cfg        Config
	binkdPath  string // resolved absolute path to the binkd binary
	confPath   string // absolute path to binkd.conf
	backoffMin time.Duration
	backoffMax time.Duration
	healthyRun time.Duration // process uptime that resets the backoff

	mu sync.Mutex
	wg sync.WaitGroup
}

// New validates the environment (preflight) and returns a ready service.
// Errors are expected to be logged as warnings by the caller; they must not
// abort BBS startup.
func New(cfg Config) (*Service, error) {
	b := cfg.FTN.Binkd

	binkdPath := b.BinaryPath
	if !filepath.IsAbs(binkdPath) {
		binkdPath = filepath.Join(cfg.BBSRoot, binkdPath)
	}
	info, err := os.Stat(binkdPath)
	if err != nil {
		return nil, fmt.Errorf("binkd binary not found at %s: %w", binkdPath, err)
	}
	if info.Mode()&0111 == 0 {
		return nil, fmt.Errorf("binkd binary %s is not executable", binkdPath)
	}

	confPath := filepath.Join(cfg.BBSRoot, "data", "ftn", "binkd.conf")
	if _, err := os.Stat(confPath); err != nil {
		return nil, fmt.Errorf("binkd.conf not found (run the FTN Setup Wizard first): %w", err)
	}

	if b.Port < 1 || b.Port > 65535 {
		return nil, fmt.Errorf("binkd port %d out of range 1-65535", b.Port)
	}

	hasAddress := false
	for _, net := range cfg.FTN.Networks {
		if net.OwnAddress != "" {
			hasAddress = true
			break
		}
	}
	if !hasAddress {
		return nil, fmt.Errorf("no FTN network has an own address configured")
	}

	return &Service{
		cfg:        cfg,
		binkdPath:  binkdPath,
		confPath:   confPath,
		backoffMin: 5 * time.Second,
		backoffMax: 5 * time.Minute,
		healthyRun: time.Minute,
	}, nil
}

// Start runs the binkd supervisor and the export loop until ctx is cancelled.
// It blocks; run it in a goroutine.
func (s *Service) Start(ctx context.Context) {
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		s.superviseLoop(ctx)
	}()
	go func() {
		defer s.wg.Done()
		s.exportLoop(ctx)
	}()
	s.wg.Wait()
}

// Close waits for the loops to finish and persists the dupe database.
func (s *Service) Close() error {
	s.wg.Wait()
	if s.cfg.DupeDB != nil {
		if err := s.cfg.DupeDB.Save(); err != nil {
			return fmt.Errorf("saving dupe db: %w", err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Note on compile order**

The package won't compile until `superviseLoop` (Step 6) and `exportLoop` (Step 7) exist — write Steps 5-7 before running the tests.

- [ ] **Step 5: Write failing supervisor tests**

Create `internal/mailer/supervisor_test.go`:

```go
package mailer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// newSupervisedService builds a service whose fake binkd writes its PID to
// pidFile and then sleeps (or exits immediately when crash is true).
func newSupervisedService(t *testing.T, crash bool) (*Service, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("supervisor tests use shell scripts; skipped on windows")
	}
	root := newTestRoot(t)
	pidFile := filepath.Join(root, "binkd.pid")
	script := "#!/bin/sh\necho $$ >> " + pidFile + "\n"
	if !crash {
		script += "sleep 60\n"
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "binkd"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := testFTNConfig()
	cfg.Networks = map[string]config.FTNNetworkConfig{
		"fsxnet": {OwnAddress: "21:4/158"}, // tosser disabled: export loop idles
	}
	svc, err := New(Config{BBSRoot: root, FTN: cfg})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	svc.backoffMin = 20 * time.Millisecond
	svc.backoffMax = 100 * time.Millisecond
	return svc, pidFile
}

// waitForLines polls pidFile until it has at least n lines or times out.
func waitForLines(t *testing.T, pidFile string, n int) []byte {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			lines := 0
			for _, c := range data {
				if c == '\n' {
					lines++
				}
			}
			if lines >= n {
				return data
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d starts in %s", n, pidFile)
	return nil
}

func TestSupervisorStartsAndStops(t *testing.T) {
	svc, pidFile := newSupervisedService(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.Start(ctx); close(done) }()

	waitForLines(t, pidFile, 1)
	cancel()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSupervisorRestartsAfterCrash(t *testing.T) {
	svc, pidFile := newSupervisedService(t, true) // script exits immediately
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.Start(ctx); close(done) }()

	// The crashing script should be relaunched at least 3 times.
	waitForLines(t, pidFile, 3)
	cancel()
	<-done
}
```

- [ ] **Step 6: Implement `supervisor.go`**

Create `internal/mailer/supervisor.go`:

```go
package mailer

import (
	"context"
	"log/slog"
	"os/exec"
	"syscall"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ftn"
)

// termGrace is how long binkd gets after SIGTERM before being killed.
const termGrace = 5 * time.Second

// superviseLoop keeps binkd running until ctx is cancelled, restarting with
// exponential backoff on unexpected exits.
func (s *Service) superviseLoop(ctx context.Context) {
	backoff := s.backoffMin

	for {
		if ctx.Err() != nil {
			return
		}

		// Sync dynamic settings into binkd.conf before each launch (best-effort).
		b := s.cfg.FTN.Binkd
		if err := ftn.SyncBinkdSettings(s.confPath, b.Port, b.LogLevel); err != nil {
			slog.Warn("binkd.conf settings sync failed", "error", err)
		}

		started := time.Now()
		err := s.runOnce(ctx)
		if ctx.Err() != nil {
			return // shutdown requested; exit regardless of process error
		}

		if time.Since(started) >= s.healthyRun {
			backoff = s.backoffMin // healthy run resets the backoff
		}
		slog.Error("binkd exited unexpectedly, restarting", "error", err, "backoff", backoff)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > s.backoffMax {
			backoff = s.backoffMax
		}
	}
}

// runOnce starts binkd and blocks until it exits or ctx is cancelled.
// On cancellation it sends SIGTERM, waits termGrace, then kills.
func (s *Service) runOnce(ctx context.Context) error {
	// No -D flag: binkd runs as a child and cannot outlive the BBS.
	cmd := exec.Command(s.binkdPath, s.confPath)
	cmd.Stdout = nil // binkd logs to file per binkd.conf
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return err
	}
	slog.Info("binkd mailer started", "pid", cmd.Process.Pid, "port", s.cfg.FTN.Binkd.Port)

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	select {
	case err := <-waitErr:
		return err
	case <-ctx.Done():
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			_ = cmd.Process.Kill() // SIGTERM unsupported (e.g. windows) or gone
		}
		select {
		case <-waitErr:
		case <-time.After(termGrace):
			_ = cmd.Process.Kill()
			<-waitErr
		}
		slog.Info("binkd mailer stopped")
		return nil
	}
}
```

- [ ] **Step 7: Implement `export.go`**

Create `internal/mailer/export.go`:

```go
package mailer

import (
	"context"
	"log/slog"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/tosser"
)

// exportLoop periodically scans JAM bases for unsent mail and packs it into
// binkd's outbound directory. Inbound needs no loop: the binkd.conf exec hook
// runs "v3mail toss" after each receive.
func (s *Service) exportLoop(ctx context.Context) {
	if s.cfg.MsgMgr == nil || s.cfg.DupeDB == nil {
		slog.Warn("binkd export loop disabled: message manager or dupe db unavailable")
		return
	}
	interval := time.Duration(s.cfg.FTN.Binkd.ExportSecs) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("binkd export loop started", "interval", interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.exportOnce()
		}
	}
}

// exportOnce runs scan+pack for every tosser-enabled network.
func (s *Service) exportOnce() {
	for name, netCfg := range s.cfg.FTN.Networks {
		if !netCfg.InternalTosserEnabled {
			continue
		}
		t, err := tosser.New(name, netCfg, s.cfg.FTN, s.cfg.DupeDB, s.cfg.MsgMgr)
		if err != nil {
			slog.Error("binkd export: tosser init failed", "network", name, "error", err)
			continue
		}
		scan := t.ScanAndExport()
		pack := t.PackOutbound()
		if scan.MessagesExported > 0 || pack.BundlesCreated > 0 {
			slog.Info("binkd export cycle", "network", name,
				"exported", scan.MessagesExported, "bundles", pack.BundlesCreated)
		}
		for _, e := range append(scan.Errors, pack.Errors...) {
			slog.Error("binkd export error", "network", name, "msg", e)
		}
	}
}
```

- [ ] **Step 8: Run all mailer tests with race detector**

Run: `go test ./internal/mailer/ -race -v`
Expected: PASS (preflight + supervisor start/stop + crash-restart).

- [ ] **Step 9: Commit**

```bash
git add internal/mailer/
git commit -m "feat(mailer): supervised binkd daemon with restart backoff and outbound export loop"
```

---

### Task 4: Wire the mailer into `cmd/vision3/main.go`

**Files:**
- Modify: `cmd/vision3/main.go` (imports at ~line 42; tosser-disabled notice at ~line 1564; insert mailer block after the V3Net section that starts at ~line 1605 — place it after the closing brace of `if v3netCfgErr == nil && v3netConfig.Enabled { ... }`)

**Interfaces:**
- Consumes: `mailer.New(mailer.Config{...}) (*mailer.Service, error)`, `(*Service).Start(ctx)`, `(*Service).Close() error` (Task 3), `ftnConfig` / `ftnErr` (already loaded at line 1466), `messageMgr` (line 1495), `tosser.NewDupeDBFromPath(path string) (*tosser.DupeDB, error)`, `(FTNConfig).ResolvePaths(root string)` (Task 1), existing `basePath` variable (BBS root).

- [ ] **Step 1: Add imports**

In the import block of `cmd/vision3/main.go` add:

```go
	"github.com/ViSiON-3/vision-3-bbs/internal/mailer"
	"github.com/ViSiON-3/vision-3-bbs/internal/tosser"
```

- [ ] **Step 2: Insert the startup block**

After the V3Net service block (immediately after its closing `}`), insert:

```go
	// Start the integrated binkd mailer if enabled (configs/ftn.json "binkd").
	// Failures are warnings: the BBS must come up even if the mailer can't.
	if ftnErr == nil && ftnConfig.Binkd.Enabled {
		mailerFTN := ftnConfig
		mailerFTN.ResolvePaths(basePath)
		dupeDBPath := mailerFTN.DupeDBPath
		if dupeDBPath == "" {
			dupeDBPath = filepath.Join(dataPath, "ftn", "dupes.json")
		}
		dupeDB, dupeErr := tosser.NewDupeDBFromPath(dupeDBPath)
		if dupeErr != nil {
			slog.Warn("binkd mailer: dupe db unavailable, export loop disabled", "error", dupeErr)
		}
		mailerSvc, mErr := mailer.New(mailer.Config{
			BBSRoot: basePath,
			FTN:     mailerFTN,
			MsgMgr:  messageMgr,
			DupeDB:  dupeDB,
		})
		if mErr != nil {
			slog.Warn("binkd mailer disabled", "error", mErr)
		} else {
			mailerCtx, mailerCancel := context.WithCancel(context.Background())
			go mailerSvc.Start(mailerCtx)
			defer func() {
				slog.Info("shutting down binkd mailer")
				mailerCancel()
				if err := mailerSvc.Close(); err != nil {
					slog.Error("binkd mailer shutdown", "error", err)
				}
			}()
			slog.Info("binkd mailer enabled", "port", ftnConfig.Binkd.Port)
		}
	}
```

Note: if `basePath` is not the exact variable name for the BBS root at that point in `main()`, use the variable that `rootConfigPath := filepath.Join(<root>, "configs")` is derived from.

- [ ] **Step 3: Update the tosser-disabled notice**

Replace line ~1564-1566:

```go
	if ftnErr == nil && len(ftnConfig.Networks) > 0 {
		slog.Info("internal FTN tosser disabled, use v3mail for toss/scan")
	}
```

with:

```go
	if ftnErr == nil && len(ftnConfig.Networks) > 0 && !ftnConfig.Binkd.Enabled {
		slog.Info("internal FTN tosser disabled, use v3mail for toss/scan or enable the binkd mailer")
	}
```

- [ ] **Step 4: Build and test**

Run: `go build ./cmd/vision3/ && go vet ./cmd/vision3/ && go test ./cmd/vision3/`
Expected: PASS.

- [ ] **Step 5: Smoke-test startup gating**

Run: `go run ./cmd/vision3 --help 2>/dev/null || true` (binary must at least build/run). Then verify the disabled path: with no `binkd` section in a test `configs/ftn.json`, startup logs must NOT mention "binkd mailer enabled". (Manual check of slog output is sufficient; the enable path is covered by mailer unit tests.)

- [ ] **Step 6: Commit**

```bash
git add cmd/vision3/main.go
git commit -m "feat(vision3): start supervised binkd mailer at boot when ftn.json binkd.enabled"
```

---

### Task 5: Config editor — Binkd fields on the Server Setup screen

**Files:**
- Modify: `internal/configeditor/fields_system.go` (`sysFieldsNetwork`, ~line 161 — V3Net hub fields end at Row 19)
- Modify: `internal/configeditor/update_save.go` (binkd sync block, lines 51-69)
- Test: `internal/configeditor/fields_system_binkd_test.go` (create)

**Interfaces:**
- Consumes: `m.configs.FTN.Binkd` (`config.BinkdServerConfig`, Task 1), `ftn.SyncBinkdSettings` (Task 2), existing helpers `uitext.BoolToYN` / `uitext.YNToBool`, `fieldDef{Label, Help, Type, Col, Row, Width, Min, Max, Get, Set}`.
- Produces: five new fields at Rows 21-25 of the Server Setup screen (the view scrolls via `fieldScroll`, so rows > `maxFieldRows` are fine).

- [ ] **Step 1: Write the failing test**

Create `internal/configeditor/fields_system_binkd_test.go`:

```go
package configeditor

import (
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

func TestSysFieldsNetworkBinkdFields(t *testing.T) {
	m := Model{configs: &allConfigs{}}
	m.configs.FTN.Binkd = config.BinkdServerConfig{
		Enabled: false, Port: 24554, BinaryPath: "bin/binkd", LogLevel: 4, ExportSecs: 300,
	}
	cfg := &config.ServerConfig{}
	fields := m.sysFieldsNetwork(cfg)

	byLabel := make(map[string]fieldDef)
	for _, f := range fields {
		byLabel[f.Label] = f
	}

	f, ok := byLabel["Binkd Mailer"]
	if !ok {
		t.Fatal("missing 'Binkd Mailer' field")
	}
	if got := f.Get(); got != "N" {
		t.Errorf("Binkd Mailer initial = %q, want N", got)
	}
	if err := f.Set("Y"); err != nil {
		t.Fatal(err)
	}
	if !m.configs.FTN.Binkd.Enabled {
		t.Error("Set(Y) did not enable binkd")
	}

	p, ok := byLabel["Binkd Port"]
	if !ok {
		t.Fatal("missing 'Binkd Port' field")
	}
	if got := p.Get(); got != "24554" {
		t.Errorf("Binkd Port = %q, want 24554", got)
	}
	if err := p.Set("24555"); err != nil {
		t.Fatal(err)
	}
	if m.configs.FTN.Binkd.Port != 24555 {
		t.Errorf("Port = %d, want 24555", m.configs.FTN.Binkd.Port)
	}

	for _, label := range []string{"Binkd Binary", "Binkd Log Lvl", "Export Secs"} {
		if _, ok := byLabel[label]; !ok {
			t.Errorf("missing %q field", label)
		}
	}
}
```

(`allConfigs` is the container type defined in `internal/configeditor/fileio.go:20`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/configeditor/ -run BinkdFields -v`
Expected: FAIL — missing 'Binkd Mailer' field.

- [ ] **Step 3: Add the fields**

In `sysFieldsNetwork` in `internal/configeditor/fields_system.go`, add `binkd := &m.configs.FTN.Binkd` next to the existing `v3 := ...` line, and append to the returned slice (after the "Auto Approve" field, Row 19):

```go
		{
			Label: "Binkd Mailer", Help: "Run bundled binkd FTN mailer at startup", Type: ftYesNo, Col: 3, Row: 21, Width: 1,
			Get: func() string { return uitext.BoolToYN(binkd.Enabled) },
			Set: func(val string) error { binkd.Enabled = uitext.YNToBool(val); return nil },
		},
		{
			Label: "Binkd Port", Help: "binkp listen port (default: 24554)", Type: ftInteger, Col: 3, Row: 22, Width: 5, Min: 1, Max: 65535,
			Get: func() string { return strconv.Itoa(binkd.Port) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				binkd.Port = n
				return nil
			},
		},
		{
			Label: "Binkd Binary", Help: "Path to binkd binary (default: bin/binkd)", Type: ftString, Col: 3, Row: 23, Width: 40,
			Get: func() string { return binkd.BinaryPath },
			Set: func(val string) error { binkd.BinaryPath = val; return nil },
		},
		{
			Label: "Binkd Log Lvl", Help: "binkd loglevel 1-9 (default: 4)", Type: ftInteger, Col: 3, Row: 24, Width: 2, Min: 1, Max: 9,
			Get: func() string { return strconv.Itoa(binkd.LogLevel) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				binkd.LogLevel = n
				return nil
			},
		},
		{
			Label: "Export Secs", Help: "Outbound scan/pack interval in seconds (default: 300)", Type: ftInteger, Col: 3, Row: 25, Width: 6, Min: 30, Max: 86400,
			Get: func() string { return strconv.Itoa(binkd.ExportSecs) },
			Set: func(val string) error {
				n, err := strconv.Atoi(val)
				if err != nil {
					return err
				}
				binkd.ExportSecs = n
				return nil
			},
		},
```

- [ ] **Step 4: Sync settings on save**

In `internal/configeditor/update_save.go`, inside the existing best-effort binkd sync block (after the `binkdSyncErr = ftn.SyncBinkdConf(...)` line, still inside the braces), add:

```go
		if binkdSyncErr == nil {
			binkdSyncErr = ftn.SyncBinkdSettings(binkdPath, m.configs.FTN.Binkd.Port, m.configs.FTN.Binkd.LogLevel)
		}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/configeditor/ -v`
Expected: PASS — new test plus all existing configeditor tests (goldens must not change: the new rows only render when the Server Setup screen is scrolled past row 19; if a golden test renders full Server Setup output and now differs, regenerate per that test's documented procedure and eyeball the diff).

- [ ] **Step 6: Commit**

```bash
git add internal/configeditor/fields_system.go internal/configeditor/fields_system_binkd_test.go internal/configeditor/update_save.go
git commit -m "feat(configeditor): binkd mailer settings on Server Setup screen"
```

---

### Task 6: Documentation + final verification

**Files:**
- Modify: `docs/sysop/messages/ftn-echomail.md` (the "Mailer (external — binkd)" section)
- Modify: `docs/sysop/reference/architecture.md` (component list)
- Modify: `docs/sysop/advanced/event-scheduler.md` (binkd examples)

**Interfaces:** none (docs only).

- [ ] **Step 1: Update ftn-echomail.md**

Rewrite the mailer section to state: binkd ships in the release bundle as `bin/binkd`; enable it in `config` → System Configuration → Server Setup → "Binkd Mailer: Y" (or set `"binkd": {"enabled": true}` in `configs/ftn.json`). Document the settings table (enabled / port / binary_path / log_level / export_interval_seconds with defaults), that the BBS supervises binkd (auto-restart with backoff, stopped on shutdown), that inbound mail is tossed automatically via the binkd.conf `exec "v3mail toss"` hook, that outbound is exported every `export_interval_seconds`, and that the FTN Setup Wizard must be run first (it creates `data/ftn/binkd.conf` with hub hostnames). Keep the existing manual/external instructions as an alternative for sysops running binkd under systemd.

- [ ] **Step 2: Update architecture.md**

Add `internal/mailer` to the component list: "Supervised binkd FTN mailer daemon — launches the bundled binkd as a child process when `ftn.json` `binkd.enabled` is true, restarts on crash with backoff, and periodically exports outbound echomail/netmail via the internal tosser."

- [ ] **Step 3: Update event-scheduler.md**

Where the doc shows scheduling binkd polls / v3mail toss events, add a note: "If the integrated binkd mailer is enabled (Server Setup → Binkd Mailer), these events are unnecessary — the BBS runs binkd and exports outbound mail itself. Scheduler events remain useful for forced polls (`binkd -p`) of specific hubs."

- [ ] **Step 4: Full verification**

Run: `go build ./... && go test ./... && go vet ./...` and `gofmt -l internal/ cmd/` (expect no output).
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add docs/sysop/messages/ftn-echomail.md docs/sysop/reference/architecture.md docs/sysop/advanced/event-scheduler.md
git commit -m "docs: integrated binkd mailer (enablement, settings, scheduler notes)"
```
