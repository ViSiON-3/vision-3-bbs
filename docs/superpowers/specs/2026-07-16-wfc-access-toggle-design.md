# WFC Access Toggle — Design

**Date:** 2026-07-16
**Status:** Approved

## Goal

Let the sysop enable or disable remote WFC (Waiting For Call) console access
from the `./config` TUI, without restarting the BBS.

WFC access is the SSH `wfc-admin` subsystem served by the `vision3` daemon
(`cmd/vision3/wfc_admin.go`) and consumed by the standalone `wfc` client
binary. Today it is gated only by public-key registration plus
`ServerConfig.CoSysOpLevel`.

## Behavior

- **Enabled (default):** current behavior, unchanged.
- **Disabled:** admin public-key authentication is rejected at the SSH auth
  layer, so `wfc` clients cannot connect at all. The key falls through to the
  normal caller password-login flow, exactly like any non-admin key today.
  The subsystem handler's re-check also enforces the flag (defense in depth).
- Toggling takes effect on the next connection attempt via the existing
  live-getter / config hot-reload pattern; no daemon restart. Already-open WFC
  sessions are not force-dropped (consistent with how `CoSysOpLevel` changes
  behave).

## Changes

### 1. Config — `internal/config/config.go`

- Add `WFCEnabled bool` with JSON tag `wfcEnabled` to `ServerConfig`, placed
  near the access-level fields.
- Set `WFCEnabled: true` in the `LoadServerConfig` defaults struct. Because
  the loader unmarshals the file over the defaults, existing `config.json`
  files without the key keep WFC enabled (same pattern as `SSHEnabled`).

### 2. Daemon gate — `cmd/vision3/wfc_admin.go` + `cmd/vision3/main.go`

- Add a live getter `wfcEnabled func() bool` alongside `adminMinLevel`,
  wired in `main.go` as
  `wfcEnabled = func() bool { return menuExecutor.GetServerConfig().WFCEnabled }`.
- `authorizeAdmin` returns false when the getter is nil or returns false.
  Since both `wfcPublicKeyHandler` and `wfcAdminSubsystem` call
  `authorizeAdmin`, this covers auth-time rejection and the in-session
  re-check.
- `wfcPublicKeyHandler` logs the rejection reason (`wfc disabled`) at Info,
  matching the existing insufficient-level log line.

### 3. Config TUI — `internal/configeditor/fields_system.go`

- Add a boolean field to the **Access Levels** sub-screen
  (`sysFieldsLevels`), next to CoSysOp Level:
  - Label: `WFC Access`
  - Help: `Allow remote WFC sysop console connections`
  - Type: `ftYesNo` (the editor's existing Y/N toggle); Get/Set map to
    `cfg.WFCEnabled`.

## Testing

- `cmd/vision3`: table test for `authorizeAdmin` covering flag on/off and
  nil getter (extend the existing wfc_admin tests if present, else add).
- `internal/config`: assert `LoadServerConfig` defaults `WFCEnabled` to true
  when the key is absent from `config.json`.
- `internal/configeditor`: field round-trip test following the
  `fields_system_test.go` pattern.

## Out of Scope

- Force-disconnecting live WFC sessions when the flag is turned off.
- Per-user WFC toggles (already achievable via key removal / access level).
- Any change to the `wfc` client binary itself.
