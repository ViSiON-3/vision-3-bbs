# Handoff: QWK/REP Offline Mail — Mobile Client (ViSiON/3)

## Overview
This is the design handoff for the **mobile QWK offline-mail reader** (Phase 8 of the
QWK REP sync design — the React Native prototype; the full design plan is an internal
document, available from the maintainer on request). The app lets a BBS user log in, request a `.QWK` packet, download it, read and
reply to messages **entirely offline**, then upload a `.REP` packet of their replies and see
an import report. It is deliberately **packet-oriented** — there is no live message browsing,
no terminal scraping, and no online-only surface. Everything the user reads/writes happens
against a locally-cached packet.

The design covers the full offline loop plus the account/storage management the plan calls for:
**Connect → Sync → Conferences → Areas → Messages → Reader → Reply → Outbox → Upload**, with
**Settings**, **Boards** (multi-BBS, add/edit/delete), and **Storage** (delete downloaded packets).

## About the Design Files
The files in this bundle are **design references created in HTML** — a working prototype that
shows the intended look, layout, copy, and interaction flow. **They are not production code to
copy.** The `.dc.html` file is an internal "Design Component" format (a small runtime in
`support.js` renders inline-styled markup + a plain-JS logic class); do not port that runtime.

The task is to **recreate these designs in the ViSiON/3 mobile client's real environment**. The
plan specifies **React Native** (see "React Native App Shape" and Phase 8 in the plan doc), so
build these screens as React Native components using the project's chosen navigation and styling
libraries. Treat the HTML as the visual + behavioral spec; wire it to the real packet API and a
real offline store.

## Fidelity
**High-fidelity.** Colors, typography, spacing, iconography, and interaction states are all
final and intentional (a CP437 "terminal-on-phone" aesthetic laid over standard mobile
patterns — bottom tab bar, back-stack navigation, ≥44px touch targets). Recreate the UI closely,
substituting React Native primitives (`View`/`Text`/`Pressable`/`FlatList`/`TextInput`) and the
project's icon set for the raw HTML/SVG here. The device bezels, phone status bar, on-screen
keyboard mock, and the iOS/Android toggle in the prototype are **presentation scaffolding only** —
the OS provides those on a real device; do not build them.

---

## Design Tokens

### Color
| Token | Hex | Use |
|---|---|---|
| `bg/app` | `#06060a` | Screen background |
| `bg/app-radial` | `#0d0d18` → `#050507` | Outer page gradient |
| `surface/card` | `#0c0c18` | Cards, list rows, panels |
| `surface/inset` | `#0d0d18` | Input fields |
| `surface/console` | `#070710` | Terminal/progress/compose blocks |
| `surface/header` | `#0a0a14` | Contextual header bar |
| `surface/tabbar` | `#0a0a12` | Bottom tab bar + home indicator |
| `border/default` | `#20203c` | Card & header borders |
| `border/divider` | `#15152a` | List row dividers |
| `border/input` | `#2a2a4e` | Input borders |
| `border/strong` | `#26264a` | Secondary buttons, area chips |
| `text/primary` | `#e8e8f4` / `#e0e0f0` | Titles, active list text |
| `text/body` | `#c8c8d4` | Message body |
| `text/secondary` | `#9999b0` | Sub-labels |
| `text/muted` | `#7a7a95` / `#6b6b8a` | Metadata, hints |
| `text/dim` | `#55667a` / `#44557a` | Faint captions |
| `text/read` | `#76768f` | Read message subject |
| `accent/blue` | `#4466ff` | Primary buttons, unread-pill border, left-accent |
| `accent/blue-grad` | `#4466ff` → `#3450dd` | Primary button gradient |
| `accent/cyan` | `#55ffff` / `#44ccdd` | Board name, unread counts, NET chips, big stats |
| `accent/blue-text` | `#9fc0ff` / `#6fa8ff` | Handles, back chevron, links |
| `accent/magenta` | `#ff66cc` | Field labels, `[Header]` field names |
| `accent/magenta-brk` | `#aa44aa` | The `[ ]` brackets around reader header fields |
| `accent/amber` | `#ffaa44` / `#ffcc44` | Outbox / drafts / OFFLINE status dot |
| `accent/green` | `#55ff99` / `#3aa85a` / `#7fe0a0` | Success, REP accepted, upload, terminal echo |
| `accent/red` | `#ff7777` / `#aa5555` / `#cc5555` | Destructive (disconnect, delete, skipped) |
| `status/gradient` | `#10204a` → `#1a1a4e` | BBS status line background |
| `toggle/on` | `#2a6fdb` | Settings toggle "on" track |

### Typography
- **`IBM Plex Mono`** (Google Fonts; weights 400/500/600/700) — UI chrome, list rows, labels, buttons, reader header, stats.
- **`IBMVGA`** (`WebPlus_IBM_VGA_8x16.woff`, bundled) — the CP437 "terminal" face. Used for: BBS status line, section headers/eyebrows, screen titles, message **body text**, terminal/progress readouts. This is what sells the retro feel — keep it on those elements. On React Native, load it via a font asset.
- Type sizes (px in prototype → dp on device): screen title 15 · section eyebrow 12 · list primary 14 · list meta 10.5–11.5 · body 14 · big stat 42/24/22 · button 13–14 · tab label 9.5.
- Letter-spacing: `0.5px` on labels/eyebrows, `1–2px` on titles/primary buttons.

### Spacing / Radius / Effects
- Screen content padding: `16px` (`14–18px` on some screens).
- List row padding: `13px 16px`; card padding `12–15px`.
- Radius: buttons/cards `8–10px`; inputs `7px`; chips/pills `6px`/`11px`(pill); toggles `14px`; icon tiles `6–7px`.
- Primary button shadow: `0 6px 22px rgba(68,102,255,.35)`; green upload `0 6px 20px rgba(58,168,90,.3)`.
- **CRT overlays** (toggleable, non-interactive, above content): (1) scanlines — `repeating-linear-gradient(0deg, rgba(0,0,0,.16) 0 1px, transparent 1px 3px)`; (2) a slow vertical sweep glow; (3) a vignette `radial-gradient(..., transparent 62%, rgba(0,0,0,.45))`. **CRT glow** = a `text-shadow: 0 0 12px rgba(85,160,255,.4)` on the screen title. Both are user settings (default on).

---

## Screens / Views

Navigation model: a **bottom tab bar** with four roots — **SYNC · MAIL · CONF · OUTBOX** — plus a
**back-stack** for drill-downs (Areas, Messages, Reader, Compose) and for the utility screens
(Settings, Boards, Add/Edit Board, Storage). The tab bar + gear are shown only on root screens;
drill-down screens show a back chevron. A **BBS status line** (board name · id · node · OFFLINE)
sits under the OS status bar on every logged-in screen. Toasts appear bottom-center.

### 1. Connect / Dial-in
- **Purpose:** pick the active board and connect. **No credential fields** — handle/password live on the board record (managed in Boards).
- **Layout:** centered `ViSiON/3` logo + "OFFLINE MAIL TERMINAL" tagline; a **BOARD** selector button (`board name` + "switch ▾", left-accent cyan) that opens Boards; a read-only summary card showing **LOGIN** (handle) and **HOST** (`host:port`); a full-width **▸ DIAL IN** primary button; a subtle "credentials managed per board — edit in Boards ›" link.
- **Connecting state:** replaces the form with a console panel that types out a fake modem handshake line-by-line (`ATDT host:port`, `CONNECT 57600 / SSH-2.0`, `login: …`, `verifying credentials … OK`, `Welcome back, …`) with a blinking block cursor, then routes to Sync. In production this is real `POST /api/qwk/login`; keep a progress affordance but the copy can be honest.

### 2. Sync / Dashboard (tab: SYNC)
- **Purpose:** the home screen — see unread totals and pull a new packet.
- **Components:** board identity card (logo + name + sysop/host); a big **UNREAD MESSAGES** stat (cyan) beside two tappable tiles — **PRIVATE MAIL** (`n new`) and **OUTBOX** (`n queued`); a **LAST PACKET** card (`<BBSID>.QWK`, msg/conf/area counts, timestamp, ✓); a full-width **⟱ PACK NEW MAIL** button.
- **Syncing state:** button → console progress block with a `█████░░░░` bar, percentage, and streaming status lines. On done: new packet added to Storage, `LAST PACKET` timestamp updates, toast. Maps to `POST /api/qwk/packets` (export) then `GET …/download`.

### 3. Private Mail (tab: MAIL)
- **Purpose:** conference-0 email, kept **separate** from public conferences (per plan's private-mail handling).
- **Layout:** "CONFERENCE 0 · EMAIL" eyebrow; message rows with unread marker (`▸` cyan / `·` dim), subject, `from <handle>`, date, one-line preview. Rows open the Reader.

### 4. Conferences (tab: CONF)
- **Purpose:** top-level list of **conferences** (message networks) — the higher-order grouping above areas. Sample data: **Local**, **fsxNet** (NET), **V3Net** (NET), **FidoNet** (NET).
- **Components:** each row = a network icon tile (blue for local, cyan for net), conference name, a **NET** chip for networked confs, `n areas · TAG` sub-line, and a **rolled-up unread pill** (sum of unread across the conference's areas). Tapping drills into Areas.

### 5. Areas (within a conference)
- **Purpose:** the message areas belonging to one conference.
- **Layout:** "`CONFNAME` · n AREAS · n UNREAD" eyebrow; rows = numbered area tile, area name, `TAG`, per-area unread pill. Tapping drills into Messages.

### 6. Message list
- **Purpose:** the messages in one area (flat, chronological — classic QWK ordering).
- **Layout:** "n MESSAGES · n UNREAD" eyebrow; rows = unread marker, subject (dim if read), `from <handle> → <to>`, message `#num`, date, one-line preview. Opening a message marks it read.

### 7. Reader
- **Purpose:** read one message; navigate the area; reply.
- **Components:** a Synchronet-style header block in `IBM Plex Mono` with magenta `[Subj] [From] [To] [When] [Area]` fields and an "`n of total`" position (red), matching classic QWK readers; the **body** in `IBMVGA` mono, `white-space: pre-wrap` (preserve the CP437 line art / signatures verbatim — do not reflow). Footer: **◄ PREV**, **↩ REPLY** (primary), **NEXT ►** (prev/next dim at list ends). A **REPLY** button also sits in the header bar.

### 8. Compose (reply)
- **Purpose:** write an offline reply that becomes part of the next `.REP`.
- **Components:** a header block showing `To <handle>` (+ **PRIVATE** chip when replying in mail), `Area`, and an editable **Subj** field (pre-filled `Re: …`); a large multiline body `TextInput` in `IBMVGA`; **⬇ SAVE TO OUTBOX**. The prototype renders a mock on-screen keyboard — **omit that**, the OS keyboard handles it. Empty body → toast "Write a reply first". Save → draft added to Outbox, pop back, toast.

### 9. Outbox (tab: OUTBOX)
- **Purpose:** review queued replies, upload the `.REP`, and see the import report.
- **Components:** if replies exist — "PENDING REPLIES · n" list (each: subject, **PENDING** chip, `to <handle> · area`, preview, amber left-accent) + **⟰ UPLOAD REP PACKET** (green). Uploading → green console progress block. On done → an **import report** card: 2×2 grid of **POSTED / THREADS LINKED / DUPLICATES / SKIPPED** (mirrors the plan's import-result JSON: `posted`, `skipped`, `duplicates`, `thread_link_failures`), drafts cleared. Empty state = centered icon + "Outbox is empty" hint. Maps to `POST /api/qwk/packets/{id}/rep` → render its JSON response.

### 10. Settings (gear icon on root screens)
- **Purpose:** display prefs + entry to account/storage management.
- **Components:** **DISPLAY** group — Platform segmented control (prototype-only), **Scanlines** toggle, **CRT glow** toggle (both drive the real overlays). **CONNECTION** group — **Boards** row (`active board name`, `n ›`) and **Downloaded packets** row (`storage used`, `n ›`). A red **⏻ DISCONNECT** button (returns to Connect). Footer version string.

### 11. Boards (multi-BBS)
- **Purpose:** manage saved boards; switch the active one.
- **Components:** "SAVED BOARDS · n" list. Each card: board name, **ACTIVE** chip on the current one (green left-accent), `host:port`, `login <handle>`; beside it an **edit (pencil)** and a **delete (trash)** button. Tapping a card **selects** it — and if you're currently logged into a different board, it disconnects and returns you to that board's Connect screen (with a toast) so you can dial in and pull its packets. A dashed **+ ADD BOARD** button at the bottom. Deleting is blocked when only one board remains.

### 12. Add / Edit Board
- **Purpose:** create or edit a board's connection + credentials.
- **Components:** **BOARD NAME**, **HOST** + **PORT** (row), **HANDLE** + **PASSWORD** (row), **SAVE BOARD**. Title reads "ADD BOARD" or "EDIT BOARD"; edit pre-fills all fields and updates in place (recomputing the derived `bbsId`). Name + host are required (toast on missing). This is the single source of truth for credentials used by Connect and Sync.

### 13. Storage
- **Purpose:** free device space by deleting cached packets.
- **Components:** summary card — total **storage used** (KB/MB), packet count, a usage bar. Then a list of packets (file name, `n msgs · KB · date`) each with a **delete** button, plus **CLEAR ALL DOWNLOADS** (red). Empty state = "No packets stored" hint. Backs the plan's offline packet cache.

---

## Interactions & Behavior
- **Navigation:** four tab roots reset the stack; drill-downs push; back chevron pops. Gear pushes Settings. Selecting a non-active board while logged in performs a disconnect + route to Connect.
- **Read state:** opening a message (from a list, or via Prev/Next) marks it read; markers, subject color, per-area/conference unread pills, the dashboard total, and the MAIL tab badge all derive from read state live.
- **Progress animations:** connect / sync / upload each stream status lines on a fixed cadence with a `█`-block progress bar and blinking cursor. Replace the fake handshake with real request lifecycle states, but keep the terminal styling.
- **Toasts:** transient bottom-center confirmations (queued reply, packet received, board saved/updated, deleted, etc.), auto-dismiss ~1.9s.
- **CRT toggles:** Scanlines/Glow are live display settings (default on); persist them.
- **Empty/edge states:** empty Outbox, empty Storage, prev/next disabled at list ends, single-board delete guard, required-field validation on Add/Edit Board.

## State Management
Per the plan's "React Native App Shape," the app is packet-oriented. Suggested state:
- **Boards[]** — `{id, name, host, port, handle, pass, bbsId, sysop, node}`; `activeId`. Persisted. `bbsId` is derived from name (uppercased alnum, ≤8 chars) but the plan's real `bbs_id` comes from the packet/login response — prefer the server value when available.
- **Session** — `loggedIn`, connecting/progress state. Real auth = token/session distinct from sysop web tools.
- **Packets[]** — cached packet metadata `{id, bbsId, file, msgs, kb, date}`; `lastSync`. Backed by SQLite or file cache; deletable.
- **Messages / read pointers** — conferences → areas → messages parsed from the `.QWK` (`HEADERS.DAT` where present); `readIds` set. In production these come from the **local QWK engine**, not hardcoded.
- **Drafts[]** — queued replies `{to, subject, areaName, priv, body}` compiled into a `.REP` by the **reply compiler** (preserve `HEADERS.DAT`); cleared on successful import.
- **Report** — last import result `{posted, linked (thread_link_failures inverse), dup, skipped}` from the REP upload response.
- **Settings** — `platform` (prototype-only), `scan`, `glow`. Persisted.

## API (from the plan — implement against these)
- `POST /api/qwk/login` — auth, returns session/token + `bbs_id`.
- `POST /api/qwk/packets` — request export (build `.QWK`); returns `{bbs_id, packet_id, message_count, conference_count, generated_at}`.
- `GET /api/qwk/packets/{id}` — packet status/metadata.
- `GET /api/qwk/packets/{id}/download` — fetch the `.QWK` bytes.
- `POST /api/qwk/packets/{id}/rep` — upload `.REP`; returns `{posted, skipped, duplicates, thread_link_failures, results[]}`.
- (Alt job-free shape: `POST /api/qwk/export`, `POST /api/qwk/import`.)
- Constraints: disabled by default in config; **packet-only** (no message-browsing endpoints); rate-limited; audited; retry-safe uploads via packet/import IDs (don't double-post).

## Assets
- `assets/ViSiON3.png` — the ViSiON/3 wordmark logo (used on Connect + Sync). Copied from the project's `docs/`.
- `fonts/WebPlus_IBM_VGA_8x16.woff` — the CP437 IBM VGA bitmap font (the "IBMVGA" family). Bundle as an app font asset. From the project's `docs/fonts/`.
- `IBM Plex Mono` — Google Fonts; bundle the needed weights.
- Icons in the prototype are inline SVG (tab icons, gear, network/stack, pencil, trash, mail, arrows). Replace with the project's icon library at equivalent sizes.

## Screenshots
Reference captures of every screen live in `screenshots/` (each is the full device frame; ignore the iOS/ANDROID toggle + bezel chrome — that's prototype scaffolding):

| # | File | Screen |
|---|---|---|
| 01 | `01-connect.png` | Connect / dial-in (board picker, no credential fields) |
| 02 | `02-sync-dashboard.png` | Sync dashboard (unread total, packet card, PACK NEW MAIL) |
| 03 | `03-private-mail.png` | Private Mail (conference 0) |
| 04 | `04-conferences.png` | Conferences (Local / fsxNet / V3Net / FidoNet) |
| 05 | `05-areas.png` | Areas within a conference |
| 06 | `06-message-list.png` | Message list (flat, chronological) |
| 07 | `07-reader.png` | Reader (Synchronet-style header + CP437 body) |
| 08 | `08-compose-reply.png` | Compose reply |
| 09 | `09-outbox-pending.png` | Outbox — pending replies |
| 10 | `10-outbox-import-report.png` | Outbox — REP accepted / import report |
| 11 | `11-settings.png` | Settings (display toggles + connection) |
| 12 | `12-storage.png` | Storage (delete downloaded packets) |
| 13 | `13-boards.png` | Boards (multi-BBS, add/edit/delete/switch) |
| 14 | `14-edit-board.png` | Edit board (prefilled) |
| 15 | `15-add-board.png` | Add board |

## Files
- `QWK Offline Reader.dc.html` — the full interactive prototype (all 13 screens, live state, animations). Open in a browser to click through the flow. **Reference only — do not ship.**
- `support.js` — the Design Component runtime needed to *run* the prototype locally. Not part of the deliverable; do not port.
- `assets/`, `fonts/` — the real image + font assets, ready to reuse.
- Plan of record: the internal QWK REP sync design plan (not tracked in this repo — available from the maintainer) — read Phase 1 (correctness core), Phase 8 (mobile prototype), "Recommended endpoints," and "React Native App Shape."

## Notes
- State in the prototype resets on reload — it has **no persistence**. The real app must persist boards, settings, drafts, packet cache, and read pointers.
- The iOS/Android toggle, device bezel, phone status bar, and mock keyboard are prototype scaffolding — the OS provides these.
- Keep message bodies monospaced and un-reflowed so CP437 art/signatures render correctly.
