# Logging Overhaul — Design

**Date:** 2026-06-28
**Status:** Approved (design); implementation plan pending
**Author:** brainstormed with Claude

## Goal

Overhaul logging in ViSiON/3 so log management — file location, level,
caching, and rolling — is configurable through the config TUI, modeled after
[Mystic BBS MUTIL logging](https://wiki.mysticbbs.com/doku.php?id=mutil_howto).
At the same time, migrate the codebase from stdlib `log.Printf` with manual
string prefixes to structured `slog`, per the project's `CLAUDE.md` mandate.

## Current State (problems being solved)

- Logging uses stdlib `log.Printf` with informal string prefixes
  (`INFO:`, `WARN:`, `ERROR:`, `DEBUG:`, `TRACE:`, `SECURITY:`, `FATAL:`).
- Output is dual-written to stderr + a **hardcoded** file
  (`data/logs/vision3.log`, `data/logs/v3mail.log`) via per-binary
  `log.SetOutput`/`os.OpenFile` blocks in each `main.go`.
- **No log rotation** — files grow indefinitely.
- **No runtime level control** and **no central configuration**.

Relevant existing locations:

- Main server log init: `cmd/vision3/main.go` (~lines 1585–1623).
- Mail util log init: `cmd/v3mail/main.go` (~lines 29–38).
- Config editor suppresses logs: `cmd/config/main.go` (~line 42).
- Server config struct: `internal/config/config.go` (`ServerConfig`, ~line 839).
- Config persistence: `LoadServerConfig`/`SaveServerConfig` (JSON, 2-space indent).
- Config TUI: `internal/configeditor/` (BubbleTea); system-config fields built in
  `fields_system.go` (`buildSysFields(screen int)`), field types in `fields.go`
  (`ftString`, `ftInteger`, `ftYesNo`, `ftLookup`).

## Decisions

These were settled during brainstorming:

| Topic | Decision |
|---|---|
| Scope | Full `slog` migration **and** the configurable rolling/level system. |
| On-disk format | `slog` built-in **`JSONHandler`** (one JSON object per line). |
| Config scope | **One shared** `LoggingConfig` for all binaries; each binary supplies only its own default filename. |
| Level model | **Named slog levels** `DEBUG/INFO/WARN/ERROR` as the minimum level. |
| Rolling impl | **Custom in-house** rolling writer (no new dependency); supports all 3 Mystic logtypes + 8KB cache. |
| Phasing | **Phase A** = logging package + writer + config + TUI; **Phase B** = mechanical slog call-site migration. |
| Timestamps | Keep standard RFC3339 in JSON; **no** configurable `logstamp` field. |

## Architecture

### New package: `internal/logging`

A single package owns all logging concerns. Binaries call it once at startup;
all other packages log through stdlib `slog` (no import coupling to
`internal/logging`).

- **`logging.go`** —
  `Init(cfg config.LoggingConfig, defaultFile string) (*slog.Logger, func() error, error)`.
  Builds the rolling writer, wraps it in a `slog.JSONHandler` at the configured
  level, installs it as `slog.Default()`, and returns a flush/close func the
  caller `defer`s. `defaultFile` is the binary-specific log filename
  (`vision3.log`, `v3mail.log`); all other settings come from the shared config.
- **`writer.go`** — the custom rolling `io.WriteCloser` (see below).
- **`level.go`** — `ParseLevel(string) (slog.Level, error)` for
  `DEBUG/INFO/WARN/ERROR`; helper `Fatal(msg string, args ...any)`
  (logs at Error then `os.Exit(1)`) and `Security(msg string, args ...any)`
  (logs at Warn with a `category=security` attribute). These absorb the old
  `FATAL:`/`SECURITY:` prefixes that have no native slog level.

Rationale for the stdlib-`slog`-everywhere approach: after `Init` sets the
default logger, every package uses `slog.Info(...)` etc. directly. The
migration becomes a near-mechanical find-and-replace, and no package other than
the three `main.go`s depends on `internal/logging`.

### Rolling writer semantics (`writer.go`)

A mutex-guarded `io.WriteCloser`:

- **Cache** — when `Cache=true`, the file is wrapped in a `bufio.Writer` (8KB).
  It is flushed (a) by a background ticker, (b) on `Close()`, and (c) on every
  `Error`-level write, for crash safety. When `Cache=false`, every line is
  written through immediately.
- **Type 0 (none)** — append to a single file indefinitely (today's behavior).
- **Type 1 (size + count)** — track bytes written; when a write would exceed
  `MaxSizeKB`, roll `file → file.1 → file.2 …` up to `MaxFiles`, deleting the
  oldest.
- **Type 2 (daily)** — write to a date-stamped file
  (`<base>.YYYY-MM-DD.log`); on a calendar-day change, open a fresh file and
  prune dated files older than `MaxFiles` days.

Self-contained, no new dependency, unit-testable with a temp dir and an
injected clock (so `Date.now`-style nondeterminism is avoided in tests).

Note on Error-level flush: slog handlers see only serialized bytes at the
writer, not the record level. The flush-on-error behavior is implemented by
having `Init`'s handler tier signal the writer (e.g. the writer exposes a
`Flush()` the handler/Init layer calls after Error records, or the writer
inspects a level hint). The exact mechanism is an implementation detail for the
plan; the requirement is: Error-level records must not sit unflushed in the
cache.

## Config schema + TUI

New struct persisted under a `"logging"` key in `configs/config.json`:

```go
type LoggingConfig struct {
    Dir       string `json:"dir"`       // default "data/logs"
    Level     string `json:"level"`     // DEBUG|INFO|WARN|ERROR, default INFO
    Cache     bool   `json:"cache"`     // 8KB buffered writes, default true
    Type      int    `json:"type"`      // 0=none 1=size 2=daily, default 0
    MaxFiles  int    `json:"maxFiles"`  // retained files (type 1) / days (type 2)
    MaxSizeKB int    `json:"maxSizeKb"` // rotate threshold in KB (type 1)
}
```

- The per-binary **log filename** stays in code (not user-editable), consistent
  with the shared-config decision.
- Defaults are applied when the `"logging"` key is **absent**, so existing
  `config.json` files keep working unchanged (back-compat).
- A new **"Logging"** screen is added to the System Configuration section of the
  TUI via `buildSysFields` in `internal/configeditor/fields_system.go`, reusing
  existing field types: `Level` and `Type` as `ftLookup` dropdowns, `Cache` as
  `ftYesNo`, `Dir` as `ftString`, `MaxFiles`/`MaxSizeKB` as `ftInteger`.
- Loaded/saved through the existing `LoadServerConfig`/`SaveServerConfig` path
  (JSON, 2-space indent).

## slog migration (Phase B)

Mechanical, package-by-package, each step verified by `go build` + `go test`:

| Old | New |
|---|---|
| `log.Printf("INFO: …")` | `slog.Info(…)` |
| `log.Printf("WARN: …")` | `slog.Warn(…)` |
| `log.Printf("ERROR: …")` | `slog.Error(…)` |
| `log.Printf("DEBUG: …")` / `TRACE:` | `slog.Debug(…)` |
| `log.Printf("SECURITY: …")` | `logging.Security(…)` |
| `log.Fatalf` / `FATAL:` | `logging.Fatal(…)` |

- Free-form `log.Printf` arguments become the slog message plus structured
  attributes where the mapping is obvious (e.g. `node`, `user`); a literal
  message string is acceptable where structuring would be intrusive, to keep the
  migration tractable.
- The per-binary `log.SetOutput`/`os.OpenFile` blocks in the three `main.go`s
  are replaced by a single `logging.Init` call (+ deferred flush).
- The config editor continues to suppress logs during TUI operation.

## Testing

- **`writer.go`** — unit tests for each logtype (none/size/daily), the
  size-rotation suffix shift, daily pruning beyond `MaxFiles`, and cache flush
  behavior (ticker, close, error). Uses a temp dir + injected clock.
- **`level.go`** — `ParseLevel` round-trip and invalid input; `Fatal`/`Security`
  behavior (Security attaches `category=security`).
- **`Init`** — produces a working JSON logger that honors level filtering
  (sub-threshold records are dropped).
- **Config** — JSON round-trip of `LoggingConfig`; defaults applied when the
  `"logging"` key is absent.
- **TUI** — extend existing `configeditor` tests to cover the new Logging screen
  fields (display, edit, save).

## Phasing

- **Phase A — New functionality (self-contained):** `internal/logging` package
  (writer, level, Init), `LoggingConfig` + load/save defaults, new TUI Logging
  screen, and wiring `logging.Init` into the three binaries. Delivers the
  Mystic-style configurable logging + rolling even before every call site is
  converted (binaries route their startup through `Init`; legacy `log.Printf`
  calls still emit until Phase B converts them).
- **Phase B — slog call-site migration:** convert remaining `log.Printf` call
  sites to `slog`/`logging` helpers, package by package, each verified by build
  + tests.

## Out of Scope

- Configurable `logstamp` / custom timestamp formats (JSON keeps RFC3339).
- Selectable text-vs-JSON output (`JSONHandler` only).
- Per-binary independent logging configs (single shared config chosen).
- Log shipping / external sinks (syslog, network).
