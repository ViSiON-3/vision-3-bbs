# V3Net Setup Wizard — Design Spec

**Date:** 2026-03-17
**Status:** Approved

## Problem

Setting up V3Net currently requires a sysop to:

1. Manually edit `configs/v3net.json`
2. Create `data/v3net_hub/` by hand (hub only)
3. Restart the BBS
4. Run `go run ./cmd/v3net-bootstrap` from the source tree (hub only)

This is too complex for the target audience. Sysops expect TUI-driven setup consistent with the rest of `./config`.

---

## Solution Overview

Two complementary changes:

1. **Hub startup auto-init** — the BBS silently handles infrastructure setup (data dir, self-registration, NAL seeding) on first start, requiring no sysop action.
2. **`./config` setup wizard** — a new guided TUI flow for both leaf and hub configuration, replacing all manual JSON editing.

---

## Part 1: Hub Startup Auto-Init

### Location

`internal/v3net/service.go` — inside `New()`, after the keystore is loaded, before `hub.New()` is called.

### Behaviour

When `hub.enabled: true`, the service performs three idempotent init steps in order:

#### 1. Data directory creation

```go
os.MkdirAll(cfg.Hub.DataDir, 0755)
```

- Always safe; `MkdirAll` is a no-op if the directory exists.
- Fixes the current "out of memory (14)" SQLite error caused by a missing parent directory.
- This must happen before `hub.New()` is called (which opens the SQLite database).

#### 2. Hub node self-registration

After `hub.New()` succeeds, for each configured network, the hub inserts its own node ID and public key into the subscribers table as `active`, if not already present.

- Uses `hub.Subscribers().Add(...)` which uses `INSERT OR IGNORE` — safe to call on every startup.
- Logged at INFO: `v3net: hub self-registered node_id=<id> network=<name>`
- Required because `POST /v3net/v1/{network}/nal` is auth-gated and checks the subscribers table.

#### 3. NAL seeding from `hub.initialAreas`

A new optional config field `hub.initialAreas` holds area specs written by the setup wizard:

```json
"initialAreas": [
  { "tag": "fel.general", "name": "FelonyNet General" },
  { "tag": "fel.art",     "name": "FelonyNet Art" }
]
```

On startup, if the hub has no existing NAL for a network **and** `initialAreas` is non-empty:

1. Build a `protocol.NAL` with `V3NetNAL: "1.0"` and `Network` set to the network name, populated with area specs using the same defaults as `v3net-bootstrap`: language `en`, open access, 64 000 byte max, ANSI allowed, hub node as manager and coordinator.
2. Sign with `nal.Sign`.
3. Store directly via `hub.NALStore().Put(network, &n)` — no HTTP round-trip needed.
4. Log at INFO: `v3net: seeded initial NAL network=<name> areas=N`
5. Remove `initialAreas` from the in-memory config and call `SaveV3NetConfig`. If the save fails, log `slog.Warn("v3net: could not remove initialAreas from config after seeding", "error", err)` — this is non-fatal; on next startup the NAL already exists so seeding is skipped (idempotent).

**`NALStore()` accessor:** `hub.Hub` currently exposes `Subscribers()` but not `NALStore()`. A new exported accessor `func (h *Hub) NALStore() *NALStore` must be added to `hub.go`.

**Edge cases:**
- If a NAL already exists for the network, seeding is skipped entirely (checked before construction).
- The `V3NetNAL: "1.0"` field is required on the NAL struct before calling `nal.Sign`; omitting it produces a structurally invalid signed NAL.
- If the keystore changes (disaster recovery), the existing NAL's `CoordNodeID` won't match the new node — this is an existing operational concern, not introduced by this change.
- All auto-init steps are logged clearly so sysops can see what happened.

**Note on startup config mutation:** Removing `initialAreas` from `v3net.json` at BBS startup is intentional one-time cleanup. Sysops using version control for their config files should be aware that the BBS modifies `v3net.json` on first hub start.

---

## Part 2: `./config` Setup Wizard

### Top-Level Menu Change

A new menu item `E — V3Net Setup` is added between `D` (V3Net Networks) and `Q` (Quit) in `internal/configeditor/model.go`. **This shifts the `Q — Quit` entry from index 13 to index 14.** The following must all be updated together:

- `topItems` slice: insert `{"E", "V3Net Setup"}` at position 12 (after `D`).
- `recordTypes` slice in `selectTopMenuItem()`: insert `"v3netwizard"` at position 12.
- `case 13: // Quit` in `selectTopMenuItem()`: update to `case 14`.
- Add `case 12` that transitions to `modeV3NetSetupFork` (initialises `wizardState`, clears any prior wizard input).

### New Modes

Two new `editorMode` constants:

```go
modeV3NetSetupFork    // Fork screen: leaf or hub
modeV3NetWizardStep   // Active wizard step (shared for both flows)
```

### Wizard State

```go
type wizardState struct {
    flow string // "leaf" or "hub"
    step int    // current step index (0-based)

    // Leaf fields (steps 0–4)
    hubURL       string
    networkName  string
    boardTag     string
    pollInterval string
    origin       string
    fetchError   string // non-empty if auto-fetch of network name failed

    // Hub fields (steps 0–3)
    netName     string
    netDesc     string
    port        string
    autoApprove bool
    areas       []wizardArea
    areaEditTag string // tag being entered in the area sub-form
    areaEditName string
    areaAdding  bool  // true when the area sub-form is open
    areaCursor  int   // highlighted area in the list
}

type wizardArea struct {
    Tag  string
    Name string
}
```

Using named fields instead of a positional `[]string` eliminates ambiguity about which index holds which value.

### Fork Screen

```
  V3Net Setup

  [J] Join an existing network  (leaf node)
  [H] Host your own network     (hub operator)

  ESC — Back
```

### Leaf Wizard (5 steps)

| Step | Field | Prompt | Default | Validation |
|------|-------|--------|---------|------------|
| 0 | `hubURL` | Hub URL | — | Non-empty; must begin with `http://` or `https://` |
| 1 | `networkName` | Network | Auto-fetched; else manual | Non-empty |
| 2 | `boardTag` | Board tag prefix | — | Non-empty |
| 3 | `pollInterval` | Poll interval | `5m` | Valid Go duration via `time.ParseDuration`, > 0 |
| 4 | `origin` | Origin line | BBS name from `config.json` | Optional |

**Step 1 auto-fetch:** After step 0 is confirmed, a `tea.Cmd` fires an HTTP GET to `{hubURL}/v3net/v1/networks` with a **5-second timeout** (`http.Client{Timeout: 5 * time.Second}`). If it returns one network, `networkName` is pre-filled. If it returns multiple, a picker is shown using the existing `modeLookupPicker` mechanism. If unreachable or timed out, `fetchError` is set to `"(could not reach hub — enter network name manually)"` and the field is left blank for manual input.

**On confirm:**
- Check for a duplicate: if a leaf with the same `hubURL` + `network` already exists in `configs.V3Net.Leaves`, show a flash message `"Already subscribed to this network on this hub"` and return to the wizard without saving.
- Appends a `V3NetLeafConfig` entry to `configs.V3Net.Leaves`.
- Sets `configs.V3Net.Enabled = true`.
- Saves `v3net.json`.
- Shows flash message: `Saved — restart the BBS to activate.`

### Hub Wizard (4 steps)

| Step | Fields | Prompt | Default | Validation |
|------|--------|--------|---------|------------|
| 0 | `netName`, `netDesc` | Network name / Description | — | Name: non-empty, lowercase alphanumeric only |
| 1 | `port` | Listen port | `8765` | Integer 1–65535 |
| 2 | `autoApprove` | Auto-approve new nodes? | `No` | Y/N toggle; inline: "Yes = instant join, recommended for testing only" |
| 3 | `areas` | Initial areas | — | At least one area required |

**Step 3 area list:** A mini add/remove list within the wizard step:
- `A` to open the area sub-form (`areaAdding = true`): enter `Tag` (validated against `protocol.ValidateAreaTag`) then `Name` (non-empty), `Enter` to append to `areas`.
- `D` to remove the highlighted area (`areaCursor`).
- `Enter` (when not in the sub-form) to confirm and proceed.

Area tags are validated using **`protocol.ValidateAreaTag`** (enforces `^[a-z0-9]{1,8}\.[a-z0-9-]{1,24}$`), matching what the hub and NAL signing enforce. Weaker validation would allow tags that silently fail downstream.

**Note:** The hub wizard creates **exactly one network**. A hub can serve multiple networks; additional networks can be added after setup via the existing `D — V3Net Networks` editor.

**On confirm:**
- Writes `hub` block to `configs.V3Net.Hub`:
  - `enabled: true`
  - `port`: parsed from wizard input
  - `dataDir: "data/v3net_hub"` (fixed default)
  - `autoApprove`: from wizard toggle
  - `networks`: single entry with `netName` + `netDesc`
  - `initialAreas`: wizard area list as `[]V3NetHubArea`
- Sets `configs.V3Net.Enabled = true`.
- Sets `configs.V3Net.KeystorePath = "data/v3net.key"` if currently blank.
- Sets `configs.V3Net.DedupDBPath = "data/v3net_dedup.sqlite"` if currently blank.
- Saves `v3net.json`.
- Shows flash message: `Saved — start the BBS to initialize your hub and seed the NAL.`

---

## Config Schema Addition

`V3NetHubConfig` gains one new field:

```go
InitialAreas []V3NetHubArea `json:"initialAreas,omitempty"`
```

```go
type V3NetHubArea struct {
    Tag  string `json:"tag"`
    Name string `json:"name"`
}
```

This field is consumed once at startup (NAL seeding) and then removed. It is not exposed in the existing hub networks editor (`D`) — only the setup wizard writes it.

---

## Files Changed

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `V3NetHubArea` struct and `InitialAreas []V3NetHubArea` to `V3NetHubConfig` |
| `internal/v3net/service.go` | Add `hubAutoInit()` — data dir, self-registration, NAL seeding; call from `New()` |
| `internal/v3net/hub/hub.go` | Add `NALStore() *NALStore` accessor |
| `internal/configeditor/model.go` | Insert menu item E; add `wizardState` struct + new modes; update `selectTopMenuItem` index |
| `internal/configeditor/update_v3net_wizard.go` | New file: wizard step logic, auto-fetch cmd, area list management, confirm handlers |
| `internal/configeditor/view_v3net_wizard.go` | New file: fork screen and wizard step rendering |

---

## Out of Scope

- TLS certificate configuration (hub wizard sets up plaintext HTTP; TLS paths remain manual via the existing hub editor).
- Registry submission (listing a new hub in the public registry remains a manual PR process).
- Editing an existing leaf or hub configuration (covered by existing `C` and `D` editors).
- Multiple networks per hub wizard run (use `D — V3Net Networks` to add more after initial setup).
