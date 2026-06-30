# QWK Offline Mail

QWK is an offline mail format that lets users download message packets, read and reply offline, then upload replies back to the BBS. ViSiON/3 supports both sides of the exchange: **QWK download** (pack messages → send to user) and **REP upload** (receive replies → post to message areas).

## How It Works

1. User configures which areas to include via **Newscan Config** (key `C` in the QWK menu, or key `Z` in the message menu). These are the same tagged areas used by the NEWSCAN function.
2. User selects **Download** (key `D`). The system packs all new messages from tagged areas into a ZIP file named `BBSID.QWK` and sends it via the user's chosen transfer protocol.
3. User opens the packet in their QWK reader offline, reads messages, writes replies.
4. User connects and selects **Upload** (key `U`). The system receives the `BBSID.REP` ZIP, parses each reply, and posts it to the appropriate message area — checking write ACS on the destination area.

## Packet Format

The QWK packet is a standard ZIP archive containing:

| File | Description |
|------|-------------|
| `CONTROL.DAT` | BBS info, packet date/time, conference list |
| `DOOR.ID` | Software identification |
| `MESSAGES.DAT` | All messages in 128-byte block format |
| `NNN.NDX` | Per-conference message index (one file per conference) |
| `PERSONAL.NDX` | Index of messages addressed directly to the user |

REP packets follow the same block format: a ZIP containing `BBSID.MSG`.

## Menu Configuration

The QWK menu is `QWKM.CFG`. It is reached via key `Q` on the main menu.

Default key bindings in `QWKM.CFG`:

| Key | Command | Description |
|-----|---------|-------------|
| `C` | `RUN:NEWSCANCONFIG` | Configure which areas are included in downloads |
| `D` | `RUN:QWKDOWNLOAD` | Build and send a QWK packet |
| `U` | `RUN:QWKUPLOAD` | Receive and process a REP reply packet |
| `Q` | `GOTO:MAIN` | Return to main menu |

The menu ANSI art is `menus/v3/ansi/QWKM.ANS`.

## BBS ID

The BBS ID is the short identifier (max 8 characters, letters and digits) used for packet filenames (`<ID>.QWK` / `<ID>.REP`) and for the destination check on REP uploads.

Set it explicitly in the config editor (System → Registration → **QWK ID**), or in `configs/config.json` as `qwkID`. Leave it blank to derive it automatically from `BoardName` (alphanumeric only, max 8 characters, uppercased — e.g. `"ViSiON/3 BBS"` → `VISION3B`).

Treat the QWK ID as a **stable identity** — set it once, early. Changing it later re-keys your packets: offline readers will see a different BBS ID, and saved `.QWK`/`.REP` files keyed to the old ID stop matching. Setting an explicit ID also means renaming the board no longer changes the QWK ID.

## Tagged Areas and Newscan

QWK download uses the same area tagging as NEWSCAN. If a user has not tagged any areas, the download falls back to all message areas they have read access to. Users manage their tagged areas from either:

- The QWK menu (`C` → `NEWSCANCONFIG`)
- The message menu (`Z` → `NEWSCANCONFIG`)

Per-area lastread pointers are updated after each download, so subsequent downloads only include messages the user has not yet received.

## Configurable Strings

Three display strings in `configs/strings.json` control QWK messaging:

| Key | Token | Used when |
|-----|-------|-----------|
| `postingQWKMsg` | `\|BN` = area name | Displayed for each message posted from a REP upload |
| `totalQWKAdded` | `\|TO` = count | Displayed after REP processing completes |
| `sendQWKPacketPrompt` | — | Confirmation prompt before sending a QWK packet |

Edit these via the [String Editor](advanced/string-editor.md) (strings 147–149) or directly in `configs/strings.json`.

## Transfer Protocols

QWK download and upload use the same file transfer subsystem as file areas. Any protocol configured in `configs/doors.json` and available for the user's connection type (SSH or telnet) will be offered. See [File Transfer](files/file-transfer.md) for protocol setup.

## Per-Area Write Access

When processing a REP upload, ViSiON/3 checks `acs_write` on the destination message area for each reply. Replies to areas where the user lacks write access are silently skipped and logged at `WARN` level.

## Conference numbering and private mail

ViSiON/3 assigns each exported message area a **stable QWK conference number**
recorded in `data/qwk_conferences.json`. This file is created and maintained
automatically — do not hand-edit it. Once an area is assigned a number, that
number never changes, so offline readers and saved reply packets keep working
even if local area IDs are renumbered.

- Public areas are numbered from their local area ID the first time they are
  exported, then frozen. If that number is unavailable — it is reserved for
  conference 0, or already claimed by another area — the next free positive
  number is used instead, so an exported conference number may differ from the
  local area ID.
- The private-mail area (tag `PRIVMAIL`) is always exported as **conference 0**.

**Private mail is per-user.** A QWK packet only includes private messages
addressed to, or sent by, the downloading user — never other users' mail.
Replies uploaded to conference 0 are posted as private mail, not to a public
base.

## Upload safety: destination check and duplicate detection

When a `.REP` is uploaded, ViSiON/3 reads the destination BBS ID from the
packet's first block. If that ID is present and does not match this system, the
packet is rejected as addressed to another BBS and nothing is posted. Packets
that omit the ID (some readers leave it blank) are accepted.

Uploads are also de-duplicated: ViSiON/3 records a fingerprint of each imported
packet in `data/qwk_dedup.db` (a small SQLite database, created automatically).
Re-uploading the exact same `.REP` — for example after a dropped mobile
connection — posts nothing the second time and reports the packet as already
uploaded. Fingerprints are kept per user and pruned after 90 days.

## Troubleshooting

**User gets "No new messages to download"**
All new messages in tagged areas have already been downloaded (lastread pointers are current). Ask the user to check their newscan config or wait for new posts.

**REP upload posts 0 messages**
- The `.REP` file name must match `BBSID.REP` (case-insensitive). Confirm the user's QWK reader is configured with the correct BBS ID.
- The user may lack write ACS on the target areas. Check server logs for `WARN: QWK REP: user lacks write ACS`.
- The REP packet may not contain a `BBSID.MSG` file. Check logs for `ERROR: QWK: failed to parse REP`.

**Transfer fails with "Transfer program not found"**
The transfer protocol binary (e.g. `sz`, `rz`) is not installed or not in `PATH`. See [File Transfer](files/file-transfer.md).
