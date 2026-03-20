# V3Net Area Browser — Config TUI

**Date:** 2026-03-20
**Status:** Draft

## Summary

Add an area browser to the config editor TUI that polls a V3Net hub for its
Network Area List (NAL) and lets the sysop subscribe to areas interactively.
The browser makes live `POST /v3net/v1/subscribe` calls so the sysop sees
immediate ACTIVE/PENDING status feedback. It is reachable from two places:
the leaf setup wizard (during initial setup) and the leaf subscription edit
view (for managing areas on an existing subscription).

## Decisions

| Question | Answer |
|---|---|
| Where does the browser appear? | Both the wizard and the edit view (option C) |
| Connectivity failure handling | Error + retry + manual text fallback (option C) |
| Interaction model | Arrow keys + Space toggle, matching AGENTS.v3net.md mockup |
| Auto-create local MsgAreas? | Yes, with sensible defaults; rename via `[E]` (option C) |
| Live subscribe to hub? | Yes, config editor calls the hub directly (option B) |

## Architecture

### New Editor Mode

`modeV3NetAreaBrowser` — a standalone full-screen mode following the same
pattern as `modeV3NetHubAreas`.

### New Types

```go
// areaBrowserItem represents one area in the browser list.
type areaBrowserItem struct {
    Tag         string // e.g. "fel.general"
    Name        string // e.g. "General"
    Description string
    Status      string // "", "ACTIVE", "PENDING", "DENIED"
    Subscribed  bool   // toggled by Space
    LocalBoard  string // auto-generated or user-edited board name
}
```

### New State on Model

```go
areaBrowserHub      string             // hub URL being browsed
areaBrowserNetwork  string             // network name
areaBrowserAreas    []areaBrowserItem  // fetched areas with status
areaBrowserCursor   int                // highlighted row
areaBrowserScroll   int                // scroll offset
areaBrowserLoading  bool               // true while NAL fetch in flight
areaBrowserError    string             // error from fetch/subscribe
areaBrowserManual   bool               // true when in manual fallback mode
```

The `wizardState` struct gains:

```go
selectedAreas []areaBrowserItem // areas selected during wizard flow
```

## Entry Points

### A) Leaf Setup Wizard

The current "Board Tag" field (row 3 in `fieldsLeafWizard()`) is replaced
with an `ftDisplay` field labeled "Areas". Pressing Enter on it validates
that Hub URL and Network are set, then transitions to `modeV3NetAreaBrowser`
and fires the NAL fetch command.

On wizard completion (`confirmLeafWizard`), the `Boards` slice in the saved
`V3NetLeafConfig` is populated from `wizard.selectedAreas` where
`Subscribed == true`. Each entry in `Boards` is the **NAL area tag** (e.g.
`fel.general`). The leaf runtime resolves area tags to local JAM bases via
`MsgArea.EchoTag` matching. A `MsgArea` entry is created for each selected
area.

`validateLeafWizard()` must also be updated: since the "Board Tag" field is
replaced with an `ftDisplay` field (no `Set`), add an explicit check that
`len(wizard.selectedAreas) > 0` in `submitWizardForm()` before calling
`confirmLeafWizard()`, analogous to the existing hub wizard guard
`len(m.wizard.areas) == 0`.

`wizardHasData()` must be updated: the `"leaf"` branch should check
`len(m.wizard.selectedAreas) > 0` instead of `m.wizard.boardTag != ""`.

### B) Leaf Subscription Edit View

A new `ftDisplay` field "Browse Areas" is added to `fieldsV3NetLeaf()` in
`fields_v3net.go`, below the existing fields. Pressing Enter reads the
current leaf's `HubURL` and `Network` and opens the same area browser.
Already-subscribed areas (from the leaf's `Boards` slice) are pre-populated
as `Subscribed: true`.

Both entry points set `areaBrowserHub`, `areaBrowserNetwork`, pre-populate
the items list, then transition to `modeV3NetAreaBrowser`.

**Return path:** Before transitioning, set `returnMode` to `modeWizardForm`
(wizard entry) or `modeRecordEdit` (edit view entry). On ESC in the
browser, restore this mode via `m.backMode()`.

## NAL Fetch

A new `tea.Cmd` function:

```go
func fetchHubNAL(hubURL, network string) tea.Cmd
```

Makes `GET /v3net/v1/{network}/nal` (unauthenticated — public endpoint).
Returns a `fetchNALMsg` containing parsed `[]protocol.Area` or an error.

**NAL signature verification** is out of scope for the config editor. The
NAL is used here only for area discovery (names, tags, descriptions) — not
for access control decisions. The leaf runtime verifies NAL signatures when
it connects at startup. This is an acceptable trade-off to avoid importing
the full `nal` package into the config editor.

On success, the browser merges fetched areas with any already-subscribed
local boards, producing the `areaBrowserAreas` list. On failure, sets
`areaBrowserError` and shows retry/manual fallback prompt.

## Subscribe Flow

When the user presses Space to subscribe to an area:

1. The config editor loads or creates the keystore via
   `loadOrCreateIdentityKeystore()` (same as seed phrase interstitial).
2. If this is the first keystore creation, the seed phrase interstitial is
   shown before proceeding.
3. A `tea.Cmd` fires `POST /v3net/v1/subscribe` with the selected area tags.
   The keystore is needed to populate `node_id` and `pubkey_b64` in the
   request body — **the subscribe endpoint is unauthenticated** (it is the
   bootstrap step), so no HTTP signing headers are sent. This matches the
   existing `leaf.subscribe()` pattern in `leaf/sender.go`.
4. The response's per-area `[]AreaSubscriptionStatus` updates each item's
   `Status` field in the browser.

```go
func subscribeToAreas(hubURL, network string, areaTags []string,
    ks *keystore.Keystore, bbsName, bbsHost string) tea.Cmd
```

Returns a `subscribeAreasMsg` with `[]protocol.AreaSubscriptionStatus`.
The response must be decoded as `protocol.SubscribeWithAreasResponse` (not
`SubscribeResponse`) to access the per-area `Areas` slice.

### Unsubscribe

Pressing Space on an already-subscribed area toggles it off locally (removes
from `Boards`, sets `Subscribed: false`). No hub call — there is no
unsubscribe endpoint in the V3Net protocol. The hub simply stops receiving
polls for that area.

### Error Handling

If the NAL fetch fails: show error message, offer `R` to retry, `M` to
switch to manual text entry mode.

If the subscribe call fails: show error inline in the message bar. The area
remains in the list with its previous status. The user can retry by toggling
Space again.

If the hub is unreachable during subscribe: same inline error. Manual
fallback (`M`) is always available.

## View Layout

Centered box list, 70 chars wide, 10 visible rows, matching the pattern
from `viewV3NetHubAreas`:

```
┌──────────────────────────────────────────────────────────────────────┐
│                    Area Browser — felonynet                         │
│   TAG                 NAME              STATUS     LOCAL BOARD      │
│  ──────────────────────────────────────────────────────────────────  │
│   [x] fel.general     General           ACTIVE     FelonyNet General│
│   [ ] fel.phreaking   Phreaking                                    │
│   [ ] fel.art         ANSI/ASCII Art                               │
│   [ ] fel.wanted      Wanted                                       │
│                                                                    │
│                                                                    │
└──────────────────────────────────────────────────────────────────────┘
 Space - Subscribe/Unsubscribe | E - Edit Board Name | M - Manual | ESC - Done
```

- `[x]`/`[ ]` checkbox reflects `Subscribed` state
- `STATUS` shows ACTIVE, PENDING, DENIED, or blank
- `LOCAL BOARD` shows auto-generated name for subscribed areas
- Loading state: centered "Fetching areas..." message
- Error state: error text + `R` retry + `M` manual
- Manual mode: replaces list with text input for comma-separated tags
- `E` on subscribed area: one-field text input to rename local board,
  guarded by an `areaBrowserEditing bool` flag on Model (similar to
  `wizard.areaAdding`). While editing, key events are routed to the
  textInput; ESC or Enter exits back to the browser list.

## MsgArea Auto-Creation

### Default Naming

Local board name = title-cased network name + area display name.
Example: `fel.general` with name "General" on "felonynet" → "FelonyNet General".

### Creation

On browser exit (ESC), for each newly subscribed area, create a
`config.MsgArea` if one doesn't already exist:

| Field | Value |
|---|---|
| `Tag` | Area tag (e.g. `fel.general`) — matches `EchoTag`, used for dedup |
| `Name` | Local board name |
| `AreaType` | `"v3net"` |
| `Network` | Network name |
| `EchoTag` | Area tag (e.g. `fel.general`) |
| `BasePath` | Data directory + tag (existing convention) |

### Unsubscribe Behavior

Unsubscribed areas keep their MsgArea entries (preserving existing messages).
They are only removed from the leaf's `Boards` list.

### Edit Flow

`E` opens a single-field text input overlay (reusing shared `textInput`) to
rename the local board. Updates both `areaBrowserItem.LocalBoard` and the
corresponding MsgArea `Name`.

### Saving

Changes persist on browser exit. The leaf's `Boards` slice is rebuilt from
all `Subscribed: true` items — each entry is the NAL area tag (e.g.
`fel.general`). The leaf runtime maps these to local JAM bases by matching
`MsgArea.EchoTag`. `dirty` is set and `saveAll()` is called.

## File Changes

### New Files

| File | Purpose |
|---|---|
| `view_v3net_area_browser.go` | `viewV3NetAreaBrowser()` render function |
| `update_v3net_area_browser.go` | Key handler, `fetchHubNAL()`, `subscribeToAreas()` cmds, message types |

### Modified Files

| File | Change |
|---|---|
| `model.go` | Add `modeV3NetAreaBrowser`, `areaBrowserItem` type, browser state fields, `areaBrowserEditing bool` |
| `fields_wizard.go` | Replace "Board Tag" with "Areas" `ftDisplay` field; add `selectedAreas` to `wizardState` |
| `fields_v3net.go` | Add "Browse Areas" `ftDisplay` field to `fieldsV3NetLeaf()` |
| `update_wizard_form.go` | Handle Enter on "Areas" field; update `confirmLeafWizard()` to use `selectedAreas`; update `wizardHasData()` to check `selectedAreas`; add `selectedAreas` length guard in `submitWizardForm()` |
| `update.go` | Route `modeV3NetAreaBrowser`; handle `fetchNALMsg` and `subscribeAreasMsg` |
| `view.go` | Route `modeV3NetAreaBrowser` to view function |

### Unchanged

- V3Net protocol types, hub, leaf packages — config editor uses raw HTTP
- Config types — `V3NetLeafConfig.Boards []string` stays the same
