# Integrated binkd Server for FTN Networks — Design

**Date:** 2026-07-16
**Status:** Approved (brainstorm w/ sysop)

## Goal

Run the bundled binkd mailer (`bin/binkd`, already shipped in the
[vision-3-release](https://github.com/ViSiON-3/vision-3-release) archives) as a
supervised child process of the BBS, configured entirely from the config
editor, with binkd.conf generated automatically. Enabling one toggle should
make FTN mail flow in both directions with zero event-scheduler setup.

Reference implementation: Retrograde BBS (`internal/binkd`, `Servers → Binkd`
TUI menu, child-process launch in `cmd/server/main.go`). ViSiON/3 already has
the FTN data plane (packets, bundles, tosser, JAM, binkd.conf generation); this
design adds the transport/process layer.

## Out of scope

- binkd binary distribution — already bundled per-platform in release archives.
- Native Go binkp implementation.
- `poll-hubs.sh` generation — binkd in daemon mode calls hubs with pending
  outbound mail on its own; forced polls remain possible via the event
  scheduler.

## 1. Config schema (`configs/ftn.json`)

Add a `Binkd` section to `FTNConfig` in `internal/config/config.go`:

```go
// BinkdServerConfig controls the integrated binkd mailer daemon.
type BinkdServerConfig struct {
    Enabled    bool   `json:"enabled"`                 // default false
    Port       int    `json:"port"`                    // binkp listen port, default 24554
    BinaryPath string `json:"binary_path"`             // default "bin/binkd", relative to BBS root
    LogLevel   int    `json:"log_level"`               // binkd loglevel, default 4
    ExportSecs int    `json:"export_interval_seconds"` // outbound scan/pack cadence, default 300
}
```

- Lives in `ftn.json` (FTN domain), loaded/defaulted in `LoadFTNConfig`.
- `binkd.conf` path is fixed at `data/ftn/binkd.conf` (existing convention in
  the FTN wizard save path, `setup.sh`, and Docker entrypoint). No ConfigPath
  knob (YAGNI).

## 2. binkd.conf generation (`internal/ftn/binkd.go`)

Extend the existing `BinkdConfig` / `UpdateBinkdConf` — do not rewrite:

- `iport <Port>` from the new config.
- `loglevel <LogLevel>` and `log <BBSRoot>/data/logs/binkd.log`.
- `inbound` (secure) / `inbound-nonsecure` from `FTNConfig.SecureInboundPath`
  / `InboundPath`; outbound from `BinkdOutboundPath`.
- Inbound tosser hook (Retrograde's HPT pattern, invoking our own CLI):

  ```
  prescan
  exec "<abs-path-to-v3mail> toss" *.pkt *.[mwtfsMWTFS][oehrauOEHRAU][0-9a-zA-Z]
  ```

  `v3mail` is resolved as the binary sitting next to the running `vision3`
  executable.

Regeneration points: config-editor save (already wired via
`ftn_wizard_save.go` / `update_save.go`) and every daemon (re)start, so the
file is always fresh before binkd launches.

## 3. Mailer daemon (`internal/mailer`, new package)

Follows the V3Net / scheduler daemon pattern (`New` → `Start(ctx)` → `Close`).

**Supervision:**

- Regenerate `binkd.conf`, then `exec.Command(binkdPath, confPath)` — no `-D`
  flag, so binkd runs as a child and cannot outlive the BBS.
- Wait on the process; on unexpected exit, restart with exponential backoff
  (5s doubling to a 5-minute cap, reset after a healthy run), each restart
  logged via `slog`.
- On context cancel: SIGTERM, ~5s grace, then Kill.

**Preflight (warn + disable, never fatal to BBS startup):**

- Binary exists and is executable.
- At least one network in `ftn.json` has an `OwnAddress`.
- Port in valid range.

**Export loop:** a ticker goroutine every `ExportSecs` runs the existing
`internal/tosser` scan/export + pack into `BinkdOutboundPath` in-process (the
same code paths `v3mail scan` / `v3mail ftn-pack` use). Inbound needs no loop —
binkd's `exec` hook fires `v3mail toss` after each receive.

**Wiring:** `cmd/vision3/main.go`, next to the V3Net block — gated on
`ftnConfig.Binkd.Enabled`, `go svc.Start(ctx)`, `defer cancel()` /
`defer svc.Close()`. All goroutines respect ctx cancellation.

## 4. Config editor TUI

Add a "Binkd Server (FTN Mailer)" field group to the existing **Server Setup**
sub-screen (`sysFieldsNetwork` in `internal/configeditor/fields_system.go`),
after the V3Net fields:

| Field           | Type      | Backing                        |
| --------------- | --------- | ------------------------------ |
| Binkd Enabled   | ftYesNo   | `configs.FTN.Binkd.Enabled`    |
| Binkd Port      | ftInteger | `configs.FTN.Binkd.Port`       |
| Binary Path     | ftString  | `configs.FTN.Binkd.BinaryPath` |
| Log Level       | ftInteger | `configs.FTN.Binkd.LogLevel`   |
| Export Interval | ftInteger | `configs.FTN.Binkd.ExportSecs` |

Get/Set closures against `m.configs.FTN.Binkd`, same pattern as the V3Net
fields already on that screen. Save regenerates `binkd.conf` (extending the
existing sync in `update_save.go`).

## 5. Error handling summary

- Missing/broken binkd binary, no addresses, bad port → warning log, mailer
  disabled, BBS runs normally.
- binkd crash → supervised restart with backoff; persistent crash loop capped
  at 5-minute retry interval, visible in logs.
- binkd.conf write failure at TUI save stays best-effort/non-fatal (current
  behavior).

## 6. Testing (TDD, per project convention)

- **Config:** load/defaults round-trip tests for `BinkdServerConfig` in
  `internal/config`.
- **Generation:** golden/assertion tests for the new `binkd.conf` fields
  (`iport`, `loglevel`, `log`, `exec` hook, paths).
- **Supervisor:** tests with a fake "binkd" shell script — clean start,
  crash → restart with backoff, ctx-cancel → SIGTERM then exit. Run with
  `-race`.
- **Export loop:** temp JAM base → messages appear packed in
  `BinkdOutboundPath`.

## 7. Documentation updates

- `docs/sysop/messages/ftn-echomail.md` — "Mailer" section: external binkd →
  integrated server; how to enable.
- `docs/sysop/reference/architecture.md` — new `internal/mailer` daemon.
- `docs/sysop/advanced/event-scheduler.md` — note binkd poll/toss events are
  unnecessary when the integrated server is enabled.
