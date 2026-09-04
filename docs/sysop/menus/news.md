# System News

The News system is ViSiON/3's implementation of ViSiON/2's `PrintNews` / `editnews` system. It provides a structured way for SysOps to post news items that users see at login or can browse from the main menu. News items have titles, authors, access level filters, and an **Always** flag — matching ViSiON/2's `newsrec` data structure.

> This is distinct from the message board (bulletin board). News items are one-way announcements composed by the SysOp, not user-posted messages.

---

## How It Works

News items are stored in `data/news.json`. Each item has:

| Field | Description |
|-------|-------------|
| **Title** | Up to 28 characters (matches V2 `String[28]`) |
| **From** | Author handle |
| **When** | Date and time posted |
| **Level** | Minimum access level required to see the item |
| **Max Level** | Maximum access level (0 = no upper limit / show to all) |
| **Always** | If `true`, shown every login. If `false`, shown only once (new since last login) |
| **Body** | Multi-line news text |

---

## Login Behaviour (`PRINTNEWS`)

When `PRINTNEWS` is in the login sequence, ViSiON/3 checks each news item against:

1. **Access level** — user's level must be `>= Level` and `<= MaxLevel` (if set)
2. **Already seen** — the item must not already be in the user's seen list, **or** `Always` is `true`

Each qualifying item is displayed using `NEWSHDR.ANS` (header) followed by the body text. The user presses a key after each item to continue. If no qualifying items exist, the login step is silent.

This matches ViSiON/2's `PrintNews(0, True)` call, with one deliberate difference: V2 decided what was new by comparing the item's date to the user's last login. ViSiON/3 instead records which item **IDs** each user has actually been shown, in `seen_news_ids` on the user record.

The date comparison could not work reliably here — the login timestamp is written at authentication, before the login sequence runs — and it also lost items whenever a caller dropped carrier or fast-logged past the news. With ID tracking, an item that was never displayed is still waiting on the next call.

Consequences:

- An item marked `always: false` is shown **exactly once per user**, whenever that user next completes the news step.
- Reading an item from `RUN:LISTNEWS` also marks it seen, so it will not reappear at the next login.
- `always: true` items are never recorded and display on every login.
- Deleting a news item prunes its ID from every user's seen list on their next news step, so the list cannot grow without bound.
- On a system upgrading to seen-tracking, each user's list is back-filled once from their previous login timestamp, so existing callers are not shown the entire news backlog.

---

## User Commands

### `RUN:LISTNEWS`

Clears the screen and displays a numbered list of all news items visible to the current user. Items newer than the user's last login are marked `[NEW]`. The user selects a number to read an item, or presses Enter to return to the menu.

After reading an item, the list is redisplayed. This matches ViSiON/2's `PrintNews(0, False)` — show all regardless of date.

**Default menu binding:** `N` and `J` on the Main Menu.

---

## SysOp Commands

### `RUN:EDITNEWS`

Opens the news management interface. Requires SysOp access (`isCoSysOpOrAbove`).

```
System News Management (N items)
──────────────────────────────────────────────────
[A]dd  [D]el  [E]dit  [L]ist  [V]iew  [Q]uit:
```

| Key | Action |
|-----|--------|
| `A` | Add a new news item |
| `D` | Delete an existing item |
| `E` | Edit an existing item |
| `L` | List all items with title, level range, and display mode |
| `V` | View item(s) as users see them (with `NEWSHDR.ANS` header) |
| `Q` | Quit |

You can also type an item number directly to view it.

**Default menu binding:** `W` on the Admin Menu.

---

### Adding a News Item

Pressing `A` prompts:

1. **Title** — up to 28 characters
2. **Minimum level** — default 0 (all users)
3. **Maximum level** — default 0 (no upper limit)
4. **Always display?** — `Y` = show every login; `N` = show once (until superseded by a newer item)
5. **Author** — defaults to your handle
6. **Body** — enter lines of text, blank line to finish

New items are prepended to the list (newest first), matching ViSiON/2 behaviour.

---

### Editing a News Item

Pressing `E` then entering an item number opens the field editor:

| Key | Field |
|-----|-------|
| `T` | Title |
| `F` | From (author) |
| `L` | Min access level |
| `X` | Max access level |
| `A` | Always flag (Y/N) |
| `E` | Replace body text |
| `Q` | Save and quit |

Changes are not written to disk until `Q` is pressed.

---

## NEWSHDR.ANS — Header Template

Each news item is displayed with a header loaded from `menus/v3/ansi/NEWSHDR.ANS`. This file is a standard pipe-coded ANSI art file with substitution tokens:

| Token | Value |
|-------|-------|
| `^NM` | Item number |
| `^TI` | Title |
| `^FR` | From (author) |
| `^DT` | Date (MM/DD/YYYY) |
| `^TM` | Time (12-hour with am/pm) |
| `^LV` | Minimum access level |
| `^MX` | Maximum access level (`All` if 0) |

If `NEWSHDR.ANS` is missing, a plain-text fallback header is used.

Edit `NEWSHDR.ANS` with any ANSI art editor (Moebius, PabloDraw, TheDraw) to customise the look. The file supports all standard ViSiON/3 pipe color codes (`|00`–`|15`).

---

## Data File

News items are stored in `data/news.json`:

```json
{
    "items": [
        {
            "id": 1,
            "title": "Welcome to the BBS",
            "from": "SysOp",
            "when": "2026-01-01T00:00:00Z",
            "level": 0,
            "max_level": 0,
            "always": true,
            "body": "Welcome! Please read the rules."
        }
    ]
}
```

Items are stored newest-first. The file is created automatically when the first news item is added via `EDITNEWS`.

---

## Login Sequence Configuration

`PRINTNEWS` ships in the default `configs/login.json` and in the built-in default sequence, so news displays at login out of the box. Installs created before it was added keep their existing `login.json`; add the item by hand:

```json
{"command": "PRINTNEWS"}
```

Leave `clear_screen` and `pause_after` off. `NEWSHDR.ANS` begins with `|CL`, so each item already clears the screen, and `PRINTNEWS` pauses after every item itself. Setting `clear_screen` would blank the previous step's output even on logins where there is no news to show.

---

## Menu Configuration

### Main Menu — user news browsing

```json
{
    "KEYS": "N",
    "CMD": "RUN:LISTNEWS",
    "ACS": "*",
    "HIDDEN": false,
    "NODE_ACTIVITY": "Reading News"
}
```

### Admin Menu — SysOp news management

```json
{
    "KEYS": "W",
    "CMD": "RUN:EDITNEWS",
    "ACS": "S255",
    "HIDDEN": false,
    "NODE_ACTIVITY": "Editing News"
}
```

---

## See Also

- [Login Sequence](users/login-sequence.md) — configuring `PRINTNEWS` in `login.json`
- [Admin Menu](users/admin-menu.md) — `W` key for news management
- [Pipe Color Codes](menus/menu-system.md#pipe-color-codes) — colors in `NEWSHDR.ANS`
