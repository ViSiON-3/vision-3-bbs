# FTN Setup Wizard — Design Plan

**Date**: 2026-03-21
**Status**: Implemented
**Scope**: New "FTN Setup Wizard" within the `./config` TUI under "Echomail and Networking"

---

## 1. Motivation

Setting up FTN (FidoNet Technology Network) echomail is one of the most
daunting tasks for new sysops. Synchronet's `ftn-setup.js` + `init-fidonet.js`
shows the right idea — a guided wizard that walks users through network
selection, address configuration, and echolist import — but the implementation
is a scrolling text-prompt flow with poor UX, scattered across multiple JS
scripts, and deeply entangled with Synchronet-specific INI files.

We will re-create this flow as a native Go TUI wizard integrated into the
existing config editor, re-using the proven **wizard pattern** established by
the V3Net leaf/hub setup wizards, and the **area browser pattern** from the
V3Net area browser.

### Data Source: Synchronet's init-fidonet.ini

The key insight is that Synchronet maintains `exec/init-fidonet.ini` — a
community-curated database of ~25 active FTN-compatible networks with:
- Zone numbers, network names, descriptions
- Coordinator contact info (name, email, FTN address)
- Hub addresses, hostnames, BinkP ports
- Echolist download URLs (backbone.na format)
- Area tag prefixes/exclusions/title cleanup rules

We will **convert this to an embedded JSON registry** that ships with Vision/3,
making the wizard work offline and allowing us to extend the format.

---

## 2. What Synchronet Does (Reference)

### ftn-setup.js — Network Picker
1. Loads zone database from `init-fidonet.ini`
2. Displays a tree of zones with info panel (name, desc, coordinator, email)
3. On select → runs `init-fidonet.js` for that zone

### init-fidonet.js — Setup Wizard (937 lines)
1. **Determine network/zone** — list or accept from arg
2. **Hub address** — prompt for hub's FTN address (pre-filled from ini)
3. **Hub details** — sysop name, BinkP hostname/port (pre-filled from ini)
4. **Your address** — prompt for your FTN address (zone:net/node.point)
5. **Sysop info** — confirm name, email
6. **Passwords** — AreaFix, BinkP session, packet, TIC passwords
7. **Origin line** — default to "SystemName - hostname"
8. **Create message group** — in Synchronet's SCFG
9. **Download echolist** — HTTP GET backbone.na from URL in ini
10. **Clean echolist** — remove excluded tags, strip title prefixes
11. **Import echolist** — run SCFG import to create sub-boards
12. **Update sbbsecho.ini** — add hub node, routes, echolist ref
13. **Install BinkIT** — mailer integration
14. **Send AreaFix netmail** — %+ALL to subscribe at hub
15. **Temp node handling** — if using temp node 9999, send application

### Strengths to Preserve
- Pre-filled network data massively reduces manual entry
- Echolist download + import creates areas automatically
- Full network setup in one guided flow

### Weaknesses to Fix
- Scrolling text prompts with no back-navigation
- Can't preview/select individual areas (sends %+ALL blindly)
- No validation until you hit errors
- Scattered across ftn-setup.js, init-fidonet.js, and SCFG
- No visual indication of progress
- No way to edit mistakes mid-flow without restarting
- Temp node / application netmail flow is confusing
- No binkd.conf integration (uses its own BinkIT)

---

## 3. Vision/3 FTN Setup Wizard — Design

### 3.1 Entry Point

Add a third menu item under "Echomail Networking":

```
Echomail Networking
  1. Echomail Networks
  2. Echomail Links
  3. FTN Setup Wizard        ← NEW
```

This launches a **wizard flow** (re-using `modeWizardForm` / `modeWizardField`
and adding FTN-specific modes), similar to the V3Net leaf wizard.

### 3.2 Wizard Steps

The wizard is a **single-screen form** (like V3Net leaf wizard) with fields
that guide the sysop through configuration. The "Network" field acts as the
entry point — selecting a network pre-fills most other fields.

#### Step 1: Network Selection

```
┌─ FTN Setup Wizard ────────────────────────────────────────┐
│                                                           │
│  Network      ▸ [press Enter to browse known networks]    │
│  Description    Fun, Simple and eXperimental network      │
│  Coordinator    Paul Hayton <avon@bbs.nz>                 │
│  Info           http://fsxnet.nz/                         │
│                                                           │
│  Your Address   21:___/___._                              │
│  Hub Address    21:1/100                                  │
│  Hub Hostname   agency.bbs.nz                             │
│  Hub BinkP Port 24556                                     │
│                                                           │
│  Areafix Pwd    ________                                  │
│  Session Pwd    ________                                  │
│  Packet Pwd     ________                                  │
│                                                           │
│  Origin Line    My BBS - mybbs.example.com                │
│                                                           │
│  Echo Areas   ▸ [press Enter to browse/download areas]    │
│                                                           │
│  S - Save & Apply │ ESC - Cancel                          │
└───────────────────────────────────────────────────────────┘
```

**Field Details:**

| #   | Field                | Type                  | Pre-fill                      | Notes                                                                           |
| --- | -------------------- | --------------------- | ----------------------------- | ------------------------------------------------------------------------------- |
| 1   | **Network**          | ftDisplay (action)    | —                             | Opens network browser (new mode). On select, pre-fills fields 2–9 from registry |
| 2   | **Description**      | ftDisplay (read-only) | from registry                 | Shows network blurb                                                             |
| 3   | **Coordinator**      | ftDisplay (read-only) | from registry                 | Name + email                                                                    |
| 4   | **Info**             | ftDisplay (read-only) | from registry                 | URL                                                                             |
| 5   | **Your Address**     | ftString              | zone:net/node format          | Validates FTN address format. Zone pre-filled from network                      |
| 6   | **Hub Address**      | ftString              | from registry `addr`          | Validates FTN address format                                                    |
| 7   | **Hub Hostname**     | ftString              | from registry `host`          | Hostname or IP                                                                  |
| 8   | **Hub BinkP Port**   | ftInteger             | from registry `port` or 24554 | 1–65535                                                                         |
| 9   | **Areafix Password** | ftString              | —                             | Required. Case-insensitive                                                      |
| 10  | **Session Password** | ftString              | —                             | Required. BinkP session password                                                |
| 11  | **Packet Password**  | ftString              | —                             | Optional. Max 8 chars                                                           |
| 12  | **Origin Line**      | ftString              | "{BBS name} - {hostname}"     | Shown in echomail origin lines                                                  |
| 13  | **Echo Areas**       | ftDisplay (action)    | —                             | Opens area browser. Shows "N areas selected" after selection                    |

#### Step 2: Network Browser (Sub-Screen)

New mode: `modeFTNNetworkBrowser`

A scrollable list similar to the V3Net area browser, but showing FTN networks:

```
┌─ Known FTN Networks ──────────────────────────────────────┐
│                                                           │
│  Zone  Network     Description                            │
│  ────  ──────────  ─────────────────────────────────────  │
│    1   FidoNet     North America                          │
│    2   FidoNet     Europe, Former Soviet Union, Israel    │
│    3   FidoNet     Australasia                            │
│    4   FidoNet     Latin America                          │
│   11   WWIVnet     WWIVnet                                │
│ ▸ 21   fsxNet      Fun, Simple and eXperimental network   │
│   25   MetroNet    Support Net for Renegade                │
│   39   AmigaNet    Supporting the AMIGA computer platform │
│   ...                                                     │
│                                                           │
│  ──────────────────────────────────────────────────────── │
│  fsxNet — Fun, Simple and eXperimental network            │
│  Coordinator: Paul Hayton <avon@bbs.nz>                   │
│  Hub: 21:1/100 at agency.bbs.nz:24556                     │
│  Info: http://fsxnet.nz/                                  │
│                                                           │
│  Enter - Select │ ESC - Back │ C - Custom Network         │
└───────────────────────────────────────────────────────────┘
```

- **Enter**: Select network, populate wizard fields, return to wizard form
- **ESC**: Return to wizard without selection
- **C**: Skip browser, manually enter all fields (for unlisted networks)
- Lower panel shows details of highlighted network (like Synchronet's info_frame)
- If a network for this zone already exists in ftn.json, show a warning marker

#### Step 3: Echo Area Browser (Sub-Screen)

New mode: `modeFTNAreaBrowser`

Re-uses the V3Net area browser visual pattern but with FTN-specific data flow:

```
┌─ Echo Areas — fsxNet ─────────────────────────────────────┐
│                                                           │
│  [x]  FSX_GEN     General Discussion                     │
│  [x]  FSX_BOT     Bot/AI Discussion                      │
│  [ ]  FSX_BBS     BBS Discussion                         │
│  [x]  FSX_DAD     Dad Jokes                              │
│  [ ]  FSX_ENG     Engineering & Electronics              │
│  [x]  FSX_FYI     Informational                          │
│  [ ]  FSX_GAM     Gaming                                 │
│  [ ]  FSX_HCK     Hacking & Security                     │
│  [ ]  FSX_LNX     Linux                                  │
│  [x]  FSX_MYS     Mystic BBS                             │
│                                                           │
│  5 of 28 areas selected                                   │
│                                                           │
│  Space - Toggle │ A - Select All │ N - Select None        │
│  Enter - Confirm │ ESC - Back                             │
└───────────────────────────────────────────────────────────┘
```

**Data Flow:**
1. On first open: download echolist from registry URL (with progress indicator)
2. Parse backbone.na format: `TAG  Description`
3. Apply registry cleanup rules (areatag_exclude, areatitle_prefix)
4. Cache parsed list in wizard state for re-entry
5. If download fails: allow manual file path entry or skip

**Controls:**
- Space: toggle individual area
- A: select all
- N: deselect all
- Enter: confirm selection, return to wizard
- ESC: discard changes, return to wizard

#### Step 4: Save & Apply

When the sysop presses **S** (save) on the wizard form:

1. **Validate** all required fields
2. **Check for conflicts** — warn if network name or zone already exists
3. **Create atomically** (all-or-nothing):

   a. **FTN Network** in `ftn.json`:
   ```json
   {
     "networks": {
       "fsxnet": {
         "internal_tosser_enabled": true,
         "own_address": "21:4/158",
         "poll_interval_seconds": 300,
         "tearline": "ViSiON/3",
         "links": [{
           "address": "21:1/100",
           "packet_password": "MYPASS",
           "areafix_password": "FIXPASS",
           "name": "fsxNet Hub",
           "flavour": "Crash"
         }]
       }
     }
   }
   ```

   b. **Conference** for the network (if not existing):
   ```json
   {
     "tag": "FSXNET",
     "name": "fsxNet",
     "description": "Fun, Simple and eXperimental network"
   }
   ```

   c. **Message Areas** for each selected echo:
   ```json
   {
     "tag": "fsxnet_fsx_gen",
     "name": "FSX General Discussion",
     "area_type": "echomail",
     "network": "fsxnet",
     "echo_tag": "FSX_GEN",
     "origin_addr": "21:4/158",
     "conference_id": 3,
     "base_path": "msgbases/fn.fsx_gen",
     "acs_read": "s10",
     "acs_write": "s20"
   }
   ```

   d. **Netmail Area** for the network (one per network):
   ```json
   {
     "tag": "fsxnet_netmail",
     "name": "fsxNet Netmail",
     "area_type": "netmail",
     "network": "fsxnet",
     "conference_id": 3,
     "base_path": "msgbases/fn.fsxnet_netmail"
   }
   ```

   e. **Update binkd.conf** with hub node entry (see §3.4)

4. **Show success** with summary of what was created
5. Return to Echomail Networking category menu

### 3.3 Backbone.NA Parser

New package or file: `internal/ftn/echolist.go`

```go
// EchoArea represents a single area from a backbone.na file
type EchoArea struct {
    Tag         string
    Description string
}

// ParseEcholist parses a backbone.na format file.
// Format: TAG<whitespace>Description (one per line)
// Lines starting with ; are comments. Blank lines skipped.
func ParseEcholist(r io.Reader) ([]EchoArea, error)

// CleanEcholist applies network-specific cleanup rules
func CleanEcholist(areas []EchoArea, excludeTags []string, titlePrefix string) []EchoArea

// DownloadEcholist fetches an echolist from a URL with timeout
func DownloadEcholist(ctx context.Context, url string) ([]EchoArea, error)
```

### 3.4 Binkd.conf Management

New file: `internal/ftn/binkd.go`

Rather than requiring sysops to manually edit binkd.conf, the wizard appends
a node definition block:

```
# --- fsxNet (added by FTN Setup Wizard) ---
node 21:1/100@fsxnet agency.bbs.nz:24556 SESSIONPWD
defnode 21:*/* agency.bbs.nz:24556 -
```

**Approach**: Append-only. Read existing binkd.conf, check if node already
defined, append if not. Use section comment markers for idempotency.

### 3.5 FTN Network Registry (Embedded Data)

New file: `internal/ftn/registry.go` with `//go:embed registry.json`

Convert init-fidonet.ini to JSON format:

```json
[
  {
    "zone": 21,
    "name": "fsxNet",
    "description": "Fun, Simple and eXperimental network",
    "info_url": "http://fsxnet.nz/",
    "pack_url": "https://fsxnet.nz/fsxnet.zip",
    "coordinator": "Paul Hayton",
    "coordinator_email": "avon@bbs.nz",
    "coordinator_ftn": "3:770/100",
    "hub_address": "21:1/100",
    "hub_hostname": "agency.bbs.nz",
    "hub_port": 24556,
    "dns_suffix": "fsxnet.nz",
    "echolist_url": "https://raw.githubusercontent.com/fsxnet/infopack/master/fsxnet.na",
    "areatag_prefix": "FSX_",
    "areatag_exclude": [],
    "areatitle_prefix": "",
    "handles_allowed": false,
    "area_manager": ""
  }
]
```

This ships with the binary via `embed.FS`. Can be overridden by a local
`configs/ftn_networks.json` file if sysops want to add custom entries.

### 3.6 Release Bundle Considerations

The network registry (`internal/ftn/registry.json`) is compiled into the
`config` binary via Go's `//go:embed` directive. Because `build_all.sh`
builds all Go binaries from `VISION3_SRC` with `go build ./cmd/config/`,
the registry data is automatically included via the embed.

#### Automated INI → JSON Conversion

Synchronet's `exec/init-fidonet.ini` is the upstream source of truth and is
actively maintained by the Synchronet community. Rather than converting it
once and letting it go stale, `build_all.sh` will **regenerate
`internal/ftn/registry.json` from the latest `init-fidonet.ini` on every
build** that includes Go compilation.

`build_all.sh` already clones/pulls the Synchronet repo into `$SYNC_SRC`
(for sexyz and door JS libraries), so the INI file is available at
`$SYNC_SRC/exec/init-fidonet.ini`. The conversion flow:

1. A standalone Go tool `cmd/ini2ftnreg/main.go` reads `init-fidonet.ini`
   and writes `registry.json` (the same JSON format described in §3.5).
2. In `build_all.sh`, a new `generate_ftn_registry()` function runs
   **before** `build_go()`:
   ```bash
   generate_ftn_registry() {
     ensure_sync_src
     ensure_vision3_src
     local ini="$SYNC_SRC/exec/init-fidonet.ini"
     local out="$VISION3_SRC/internal/ftn/registry.json"
     if [[ ! -f "$ini" ]]; then
       info "init-fidonet.ini not found — keeping existing registry.json"
       return 0
     fi
     info "Generating FTN network registry from init-fidonet.ini"
     (cd "$VISION3_SRC" && go run ./cmd/ini2ftnreg/ -in "$ini" -out "$out")
     ok "registry.json updated ($(wc -l < "$out") lines)"
   }
   ```
3. The main pipeline calls `generate_ftn_registry` before `build_go`:
   ```bash
   $DO_GO && generate_ftn_registry
   $DO_GO && build_go
   ```

This ensures every release build picks up the latest community-maintained
network data without manual intervention. If `init-fidonet.ini` is missing
(e.g. `SYNC_SRC` not configured), the converter is skipped and the existing
committed `registry.json` is used as a fallback.

The converter tool is a build-time utility only — it is not included in
release bundles (it is not listed in the `GO_BINS` array).

#### Optional Sysop Override

To allow sysops to add custom networks or update entries without rebuilding,
the wizard falls back to `configs/ftn_networks.json` at runtime. A seed copy
of this file should be added to `templates/configs/ftn_networks.json` in the
vision-3-bbs repo. The existing `copy_full_bundle_content()` function in
`build_all.sh` already copies all `templates/configs/*.json` files into the
bundle's `configs/` directory, so the override file will be included in
release bundles automatically.

#### Summary of Bundle Touchpoints

| Artifact                              | How it reaches the bundle                                | `build_all.sh` change needed?                                |
| ------------------------------------- | -------------------------------------------------------- | ------------------------------------------------------------ |
| `internal/ftn/registry.json`          | Regenerated from `init-fidonet.ini`, then `//go:embed`'d | Yes — add `generate_ftn_registry()` step before `build_go()` |
| `cmd/ini2ftnreg/`                     | Build-time tool only; runs via `go run` during build     | Yes — called by `generate_ftn_registry()`                    |
| `templates/configs/ftn_networks.json` | Copied by `copy_full_bundle_content()`                   | No — existing `*.json` glob covers it                        |

---

## 4. Implementation Plan

### Phase 1: Foundation (internal/ftn/)

Files to create/modify:

| File                                  | Purpose                                                      |
| ------------------------------------- | ------------------------------------------------------------ |
| `cmd/ini2ftnreg/main.go`              | Build-time tool: converts init-fidonet.ini → registry.json   |
| `internal/ftn/registry.go`            | Embedded network registry + loader                           |
| `internal/ftn/registry.json`          | JSON network database (auto-generated from init-fidonet.ini) |
| `templates/configs/ftn_networks.json` | Seed override file for sysop customization (ships in bundle) |
| `internal/ftn/echolist.go`            | Backbone.NA parser + HTTP downloader                         |
| `internal/ftn/binkd.go`               | Binkd.conf node management                                   |
| `internal/ftn/address.go`             | FTN address parsing/validation (zone:net/node.point)         |
| `internal/ftn/registry_test.go`       | Registry loading tests                                       |
| `internal/ftn/echolist_test.go`       | Parser tests with sample data                                |
| `internal/ftn/address_test.go`        | Address validation tests                                     |

### Phase 2: Config Editor Integration

Files to create/modify:

| File                                                  | Purpose                                            |
| ----------------------------------------------------- | -------------------------------------------------- |
| `internal/configeditor/model.go`                      | Add new modes, add wizard menu item under Echomail |
| `internal/configeditor/ftn_wizard_state.go`           | FTN wizard transient state struct                  |
| `internal/configeditor/fields_ftn_wizard.go`          | Wizard field definitions                           |
| `internal/configeditor/update_ftn_wizard.go`          | Wizard form update handler                         |
| `internal/configeditor/update_ftn_network_browser.go` | Network browser update handler                     |
| `internal/configeditor/update_ftn_area_browser.go`    | Echo area browser update handler                   |
| `internal/configeditor/view_ftn_wizard.go`            | Wizard form view rendering                         |
| `internal/configeditor/view_ftn_network_browser.go`   | Network browser view                               |
| `internal/configeditor/view_ftn_area_browser.go`      | Area browser view                                  |
| `internal/configeditor/ftn_wizard_cmds.go`            | BubbleTea Cmds for async HTTP fetches              |
| `internal/configeditor/view.go`                       | Add mode→view dispatch entries                     |
| `internal/configeditor/update.go`                     | Add mode→update dispatch entries                   |

### Phase 3: Save Logic

Files to modify:

| File                                       | Purpose                                                      |
| ------------------------------------------ | ------------------------------------------------------------ |
| `internal/configeditor/ftn_wizard_save.go` | Atomic save: FTN network + link + conference + areas + binkd |

### Phase 4: Testing & Polish

- Unit tests for all `internal/ftn/` packages
- Manual TUI testing with real network data
- Test with at least: FidoNet (zone 1), fsxNet (zone 21), a pack-only network
- Edge cases: duplicate network detection, address validation, download failures

---

## 5. New Editor Modes

Add to the `editorMode` enum in `model.go`:

```go
modeFTNWizardForm           // FTN wizard form navigation
modeFTNWizardField          // FTN wizard field editing
modeFTNNetworkBrowser       // Known FTN network list with info panel
modeFTNAreaBrowser          // Echo area selection from downloaded echolist
modeFTNAreaDownloading      // Progress state while downloading echolist
```

### Mode Transitions

```
modeCategoryMenu (Echomail Networking)
  └─ "FTN Setup Wizard" ──→ modeFTNWizardForm
                                ├─ Enter on Network field ──→ modeFTNNetworkBrowser
                                │     └─ Enter ──→ modeFTNWizardForm (fields populated)
                                │     └─ ESC ──→ modeFTNWizardForm
                                │     └─ C ──→ modeFTNWizardForm (manual entry)
                                ├─ Enter on Echo Areas field ──→ modeFTNAreaDownloading
                                │     └─ (success) ──→ modeFTNAreaBrowser
                                │     │     └─ Enter ──→ modeFTNWizardForm
                                │     │     └─ ESC ──→ modeFTNWizardForm
                                │     └─ (failure) ──→ modeFTNWizardForm (error msg)
                                ├─ Tab/Enter on text fields ──→ modeFTNWizardField
                                │     └─ Enter/ESC ──→ modeFTNWizardForm
                                ├─ S ──→ validate + save + return to modeCategoryMenu
                                └─ ESC ──→ modeWizardExitConfirm (if dirty)
                                      └─ modeCategoryMenu
```

---

## 6. FTN Wizard State

```go
type ftnWizardState struct {
    // Network identity (from registry or manual)
    zone            int
    networkName     string
    networkDesc     string
    coordinator     string
    coordinatorEmail string
    infoURL         string

    // Your node
    ownAddress      string   // "21:4/158"

    // Hub configuration
    hubAddress      string   // "21:1/100"
    hubHostname     string   // "agency.bbs.nz"
    hubPort         int      // 24556
    areafixPassword string
    sessionPassword string
    packetPassword  string

    // Echomail
    originLine      string
    echolistURL     string   // from registry, may be overridden

    // Area selection
    availableAreas  []ftn.EchoArea  // parsed from downloaded echolist
    selectedAreas   []bool          // parallel array, true = subscribed
    areasFetched    bool
    areasFetchErr   string

    // Registry data (for pre-fill)
    registryEntry   *ftn.NetworkInfo // nil if manual/custom

    // UI state
    areaBrowserScroll int
    networkBrowserScroll int
    networkBrowserCursor int
}
```

---

## 7. Key Differences from Synchronet

| Aspect               | Synchronet                          | Vision/3                                     |
| -------------------- | ----------------------------------- | -------------------------------------------- |
| **UI**               | Scrolling text prompts, no back-nav | TUI wizard form with arrow-key navigation    |
| **Network DB**       | INI file read at runtime            | Embedded JSON, ships with binary             |
| **Area Selection**   | Downloads entire list, sends %+ALL  | Browse & cherry-pick individual areas        |
| **Config Output**    | sbbsecho.ini (proprietary)          | ftn.json + message_areas.json + binkd.conf   |
| **Area Creation**    | Runs external SCFG import command   | Direct atomic JSON config update             |
| **Conference**       | Creates "message group" separately  | Auto-creates conference for network          |
| **Netmail Area**     | Not explicitly created              | Auto-creates netmail area per network        |
| **Validation**       | Post-hoc, error-on-fail             | Real-time field validation                   |
| **Back Navigation**  | Can't go back, must restart         | Full ESC/arrow navigation                    |
| **Progress**         | Print statements                    | Visual progress for downloads                |
| **Error Recovery**   | Retry prompts scattered             | Centralized error display, retry option      |
| **Binkd**            | Uses its own BinkIT mailer          | Updates existing binkd.conf                  |
| **Node Application** | Sends netmail to coordinator        | Not in scope (sysops get address externally) |

---

## 8. Backbone.NA Format Reference

Standard format used by most FTN networks for area lists:

```
; Comment lines start with semicolon
; TAG            Description
FSX_GEN          General Discussion
FSX_BOT          Bot/AI Discussion
FSX_BBS          BBS Discussion
FSX_DAD          Dad Jokes
```

Rules:
- One area per line
- Tag and description separated by whitespace (typically spaces/tabs)
- Tag is the first non-whitespace token
- Everything after the tag (trimmed) is the description
- Lines starting with `;` are comments
- Blank lines are skipped
- Tags are case-insensitive for matching but preserved as-is

---

## 9. Open Questions

1. **Poll interval**: Should the wizard prompt for a poll interval, or use a
   sensible default (e.g., 300 seconds)? The existing FTN network editor has
   this field — maybe default to 300 and let them change later.

2. **AreaFix netmail**: Should the wizard offer to compose an AreaFix %+ALL
   (or per-area) netmail message? This would require the tosser to be
   functional. Could be a Phase 2 enhancement or a separate "Send AreaFix"
   action on the Echomail Links screen.

3. **Binkd management scope**: Should the wizard only append to binkd.conf, or
   should it fully manage binkd.conf sections? Append-only is safer and
   simpler for v1.

4. **Network registry updates**: Should there be a mechanism to update the
   embedded registry (e.g., download a newer version from a Vision/3 URL)?
   Nice-to-have for later.

5. **Re-running the wizard**: If a sysop runs the wizard for a network they
   already configured, should it detect this and offer to add areas / update
   config? Or should it only work for new networks?

---

## 10. Dependencies

- No new Go dependencies required
- Uses stdlib: `net/http`, `embed`, `encoding/json`, `bufio`, `strings`
- Re-uses existing config editor patterns (wizard form, area browser)
- Re-uses existing FTN config types from `internal/config/config.go`
