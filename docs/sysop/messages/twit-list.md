# Twit Filter List

Each caller can hide messages from selected authors. Open **K (User Configuration)**
and choose **M (Twit List)**, or choose **I (Twit List)** from the stock Message Menu.
Custom menus can call `RUN:CFG_TWITLIST`. The current full-screen `USERCONFIG`
replaces the older `USERCFG` menu mentioned in issue #398.

The Twit List is drawn like the generated door menu: a title, a short note
that this is the caller's private ignore list (nobody is told and nothing is
deleted) with how many of the allowed entries are used, the numbered list (or
a note that it is empty), the commands, and the standard
`[time] Selection:` prompt. Type A to add a handle or network name, D to
delete an entry by number, C to clear with confirmation, and Q (or Enter) to
go back to the menu you came from. Lists too long for one screen show a page
line; `[` and `]` (or P and N) move between pages, and entry numbers stay the
same on every page. After each command the screen is repainted with the
result (added, removed, refused and why) above the prompt. Changes save immediately in the caller's `twitList` in `users.json`.
They are session preferences, not sysop-owned fields.

In the message reader, **Twit** on the bottom command bar (hotkey **I**, or
arrow to it) adds the author of the message on screen. The add is checked
first: yourself, a sysop or co-sysop, an author already on the list, or a full
list is refused on the reader's notice line without asking anything.
Otherwise it asks before adding, showing the author's name; answering N leaves
the list and the message as they were. After a successful add the author's
messages are hidden at once and the reader moves to the next visible message; if there is
none it says so and leaves the reader, as Next does at the end. **T** keeps its
existing Thread function. Anonymous authors cannot be quick-added and anonymous
posts are always visible. The quick-add is listed in the reader's ? help.
The bar fits in 79 columns with its Reading/NewScan label for callers and
sysops (whose bar adds Del). If the label would reach the last column, as on
a narrower screen or with a longer `msgReadingSuffix`/`msgNewScanSuffix`, it
is left off rather than wrapping. Boards upgraded from the older
`(Reading)`/`(NewScan)` strings fit at 80 columns for callers but drop the
label for sysops until those two strings are shortened.

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

In `config.json`:

| Setting | Default | Meaning |
| --- | --- | --- |
| `twitListEnabled` | `true` | Board-wide switch, also **CONFIG → System Setup → Default Settings → Twit Lists**. See below. |
| `twitListMax` | 50 | Maximum entries; zero or negative uses 50. |
| `twitExemptLevel` | co-sysop level | Local users at or above this level cannot be added. Sysops and co-sysops (`sysOpLevel`, `coSysOpLevel`) are always exempt; zero or negative protects only them, and a value above `coSysOpLevel` cannot unprotect co-sysops. |

### Turning Twit Lists off

`twitListEnabled` controls the feature for the whole board. It is a switch for
callers' personal lists, not a list of blocked authors. When it is off:

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
art is static, so the stock MSGMENU still shows [I] Twit List while the feature
is off; selecting it shows the notice.

You cannot add yourself ("You can't ignore yourself!"). Local exemptions are rechecked when the reader/list or
QWK export builds its matcher; promoted users become visible on the next entry.
Network entries have no account security level and are not exempt, even when their
name matches a sysop. Duplicate entries are refused.

Filtering applies to READMSGS, NEWSCAN, LISTMSGS, READPRIVMAIL, LISTPRIVMAIL,
thread searches, and QWK downloads (terminal and HTTPS API). The API resolves the
latest saved list on each packet request, including requests using an older login token. Privacy and search filters still apply.
Messages are skipped without placeholders. Newscan shows one summary of unique
visited messages hidden by the Twit List, across all scanned areas. Messages
excluded by privacy/search rules are not counted. Repeated navigation does not
inflate the total.

Area Total/New/Personal counts and the newscan area's “X new” count remain raw JAM
counts, so the reader may show fewer messages. QWK export applies the list before
its per-area cap; last-read advances still require a successful packet transfer.
Filtering does not delete messages, notify authors, block mail delivery, or affect
pages, node messages, chat, one-liners, rumors, user lists, or last callers.

## Screen art and prompts

The screen comes from the menu set, the same way as the generated door menu.
Customize copies in `menus.d/v3/` so upgrades keep them. Any file that is
missing falls back to a built-in copy of the shipped one, so a menu set from
before this screen existed still works.

- `mnu/TWITLIST.MNU`: `TITLE` (shown as `^TI`), `CLR`/`CLS`, `USEPROMPT`,
  `PROMPT1` and `PROMPT2`. `menuedit` lists and edits it. It has no `.CFG`;
  the commands are built in.
- `templates/TWITLIST.TOP`: title and description. `^TI` is the MNU title,
  `^CT` the number of entries and `^MX` the limit.
- `templates/TWITLIST.HDR`: the column heading, left out when the list is empty.
- `templates/TWITLIST.MID`: one row. `^ID` is the entry number, `^NA` the
  name (with `@address` for network entries). Names are shown as written,
  never read as pipe codes, and are cut to fit the screen width.
- `templates/TWITLIST.BOT`: footer and command list. Lines containing `^PG`
  (page) or `^PT` (pages) appear only when the list needs more than one page.

All four templates also take the usual `|TIME`, `|DATE` and similar tokens.

The empty-list note, prompts, confirmations, errors, the turned-off notice,
and the scan summary are the `twit*` entries in `strings.json`, editable in
the string editor.
