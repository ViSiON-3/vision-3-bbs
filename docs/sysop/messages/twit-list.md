# Twit Filter List

A Twit List is a caller's private ignore list. Anyone who has spent time in an
echo knows the one poster whose every message they would rather skip; the Twit
List lets each caller hide messages from the authors they choose, without the
author knowing and without anything being deleted. The list applies wherever
the caller reads mail: the reader, newscan, message lists, thread searches,
private mail, and QWK packets built for them in the terminal or through the
HTTPS API. The sysop decides whether the board offers the feature at all, and
staff accounts can never be put on a list.

## Turning it on or off

Twit Lists are on by default. The switch is in the Configuration Editor:
`./config` → **System Setup** → **Default Settings** → **Twit Lists** (saved as
`twitListEnabled` in `config.json`). It is a switch for callers' personal
lists, not a list of blocked authors. When it is off:

- the reader's Twit command is removed from the bar and its help, and the I
  key does nothing;
- `CFG_TWITLIST` (and so M in USERCONFIG and I in the message menu) shows
  "Twit Lists are turned off" and returns, and USERCONFIG shows the field as Off;
- no Twit List filtering is applied anywhere: reading, newscan, message lists,
  thread search, private mail, and QWK downloads from the terminal and the
  HTTPS API.

Saved lists are not touched. Turning the switch back on applies them again.
A `config.json` from before this setting existed has no `twitListEnabled` key;
that is read as on, so upgraded boards keep the behaviour they had. The menu
art is static, so the stock MSGMENU still shows [T] Twit List while the feature
is off; selecting it shows the notice.

The list size (`twitListMax`, 50 by default) and the level at which local
users become exempt (`twitExemptLevel`) are not in the editor; set them in
`config.json` (see [Technical Reference](#technical-reference)).

## Managing the list

Each caller manages their own list. Open **K (User Configuration)** and choose
**M (Twit List)**, or choose **I (Twit List)** from the stock Message Menu.
Custom menus can call `RUN:CFG_TWITLIST`. The current full-screen `USERCONFIG`
replaces the older `USERCFG` menu mentioned in issue #398.

The Twit List is drawn like the generated door menu: a title, a short note
that this is the caller's private ignore list (nobody is told and nothing is
deleted) with how many of the allowed entries are used, the numbered list in
three columns (or a note that it is empty), the commands, and the standard
`[time] Selection:` prompt. Type A to add a handle or network name, D to
delete an entry by number, C to clear with confirmation, and Q (or Enter) to
go back to the menu you came from. Lists too long for one screen show a page
line; `[` and `]` (or P and N) move between pages, and entry numbers stay the
same on every page. At 80x24 a page holds 30 entries, so a full list of 50
takes two pages. Entries run down each column; a terminal too narrow for
three 19-character columns gets two, or one. After each command the screen is
repainted with the result (added, removed, refused and why) above the prompt.
Changes save immediately in the caller's `twitList` in `users.json`. They are
session preferences, not sysop-owned fields.

### Adding an author from the reader

In the message reader, **Twit** on the bottom command bar (hotkey **I**, or
arrow to it) adds the author of the message on screen. The add is checked
first: yourself, a sysop or co-sysop, an author already on the list, or a full
list is refused on the reader's notice line without asking anything.
Otherwise it asks before adding, showing the author's name; answering N leaves
the list and the message as they were. After a successful add the author's
messages are hidden at once and the reader moves to the next visible message;
if there is none it says so and leaves the reader, as Next does at the end.
**T** keeps its existing Thread function. Anonymous authors cannot be
quick-added and anonymous posts are always visible. The quick-add is listed in
the reader's ? help.

The bar fits in 79 columns with its Reading/NewScan label for callers and
sysops (whose bar adds Del). If the label would reach the last column, as on
a narrower screen or with a longer `msgReadingSuffix`/`msgNewScanSuffix`, it
is left off rather than wrapping. Boards upgraded from the older
`(Reading)`/`(NewScan)` strings fit at 80 columns for callers but drop the
label for sysops until those two strings are shortened.

## How matching works

An exact local handle is stored by user ID, including handles containing `@`.
Otherwise, `@` separates a network name from its address. Matching uses
that user's current handle, current real name (including RealNameOnly areas), and
the name saved at add time. This survives renames. Other names are network entries.
Use `name@address` to match only that origin; without an address the name matches
on every network and in local areas. Quick-add includes the origin address of
imported network messages, keeping those authors separate from similarly named
accounts. Quick-adding an author from a locally written echomail/netmail post
stores a local-user entry, so local-user matching and exemption checks still
apply even when an origin address is present.
Matching ignores case and leading/trailing whitespace. Anonymous means the
configured `anonymousName` or the standard `Anonymous` signature; the board does
not retain a separate anonymous author identity for filtering.

You cannot add yourself ("You can't ignore yourself!"). Local exemptions are
rechecked when the reader/list or QWK export builds its matcher; promoted
users become visible on the next entry. Network entries have no account
security level and are not exempt, even when their name matches a sysop.
Duplicate entries are refused.

## What is filtered

Filtering applies to READMSGS, NEWSCAN, LISTMSGS, READPRIVMAIL, LISTPRIVMAIL,
thread searches, and QWK downloads (terminal and HTTPS API). The API resolves the
latest saved list on each packet request, including requests using an older
login token, and always applies the board switch, the exemption levels,
`twitListMax` and `anonymousName` exactly as the terminal does. Privacy and
search filters still apply. Messages are skipped without placeholders. Newscan
shows one summary of unique visited messages hidden by the Twit List, across
all scanned areas. Messages excluded by privacy/search rules are not counted.
Repeated navigation does not inflate the total.

Area Total/New/Personal counts and the newscan area's “X new” count remain raw JAM
counts, so the reader may show fewer messages. QWK export applies the list before
its per-area cap. Last-read pointers advance after a successful terminal
transfer, or as soon as the API builds a packet.
Filtering does not delete messages, notify authors, block mail delivery, or affect
pages, node messages, chat, one-liners, rumors, user lists, or last callers.

## Troubleshooting

- **A caller says the Twit command is missing.** Twit Lists are off for the
  board (**Default Settings → Twit Lists**), or the reader bar was too narrow
  for the label; the I key still works whenever the feature is on.
- **A caller cannot add a particular author.** The author is themselves, a
  sysop or co-sysop, a local user at or above `twitExemptLevel`, already on
  the list, or anonymous; or the list is full (`twitListMax`). The refusal
  reason is shown on the notice line.
- **Area counts do not match what the reader shows.** Expected: counts are raw
  JAM counts and the reader hides filtered messages.
- **An author keeps appearing under a slightly different name.** Network
  entries match the name exactly (ignoring case); add the variant too, or add
  `name@address` entries for each origin.

## Technical Reference

### `config.json`

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `twitListEnabled` | bool | `true` (key absent = on) | Board-wide switch, also **CONFIG → System Setup → Default Settings → Twit Lists**. See [Turning it on or off](#turning-it-on-or-off). |
| `twitListMax` | int | 50 | Maximum entries; zero or negative uses 50. |
| `twitExemptLevel` | int | co-sysop level | Local users at or above this level cannot be added. Sysops and co-sysops (`sysOpLevel`, `coSysOpLevel`) are always exempt; zero or negative protects only them, and a value above `coSysOpLevel` cannot unprotect co-sysops. |

### `users.json`

Each caller's list is the `twitList` array in their record in
`data/users/users.json`. Local entries are stored by user ID with the name
saved at add time; network entries store the name and, optionally, the
origin address. The BBS writes it; there is nothing for the sysop to edit.

### Screen art and prompts

The screen comes from the menu set, the same way as the generated door menu.
Customize copies in `menus.d/v3/` so upgrades keep them. Any file that is
missing falls back to a built-in copy of the shipped one, so a menu set from
before this screen existed still works.

- `mnu/TWITLIST.MNU`: `TITLE` (shown as `^TI`), `CLR`/`CLS`, `USEPROMPT`,
  `PROMPT1` and `PROMPT2`. `menuedit` lists and edits it. It has no `.CFG`;
  the commands are built in.
- `templates/TWITLIST.TOP`: title and description. `^TI` is the MNU title,
  `^CT` the number of entries and `^MX` the limit.
- `templates/TWITLIST.HDR`: the column heading. Each text line is repeated
  over every column the page fills; blank lines are kept as spacing. Left
  out when the list is empty.
- `templates/TWITLIST.MID`: one entry, drawn in a column cell. `^ID` is the
  entry number, `^NA` the name (with `@address` for network entries). Names
  are shown as written, never read as pipe codes, and are cut to the cell
  width (about 25 characters at 80 columns).
- `templates/TWITLIST.BOT`: footer and command list. Lines containing `^PG`
  (page) or `^PT` (pages) appear only when the list needs more than one page.

All four templates also take the usual `|TIME`, `|DATE` and similar tokens.

### Strings

The empty-list note, prompts, confirmations, errors, the turned-off notice,
and the scan summary are the `twit*` entries in `strings.json`, editable in
the [String Editor](advanced/string-editor.md):

| Key | Shown when |
| --- | --- |
| `twitNamePrompt` | Asking for a name to add (`Name (optionally name@address): `) |
| `twitDeletePrompt` | Asking which entry number to delete |
| `twitClearConfirm` | Confirming a clear |
| `twitQuickConfirm` | Confirming a quick-add from the reader (`%s` is the author) |
| `twitAdded`, `twitRemoved`, `twitCleared` | After a successful add, delete or clear |
| `twitSelf`, `twitExempt`, `twitFull`, `twitDuplicate`, `twitAnonymous`, `twitInvalid` | The refusal reasons |
| `twitSaveFailed` | The list could not be written; nothing changed |
| `twitHiddenSummary` | The newscan summary (`%d` is the count) |
| `twitEmpty` | The list is empty |
| `twitDisabled` | Twit Lists are turned off on this BBS |

The User Konfig item uses `konfigTwitListLabel` and `konfigTwitListHelp`.

### Menu commands

| Command | Where it is bound in the stock menus |
| --- | --- |
| `RUN:CFG_TWITLIST` | `T` in `MSGMENU.CFG` (`I` still works, for hands used to the earlier key); item `M` of `RUN:USERCONFIG` (`K` on the main menu) |
