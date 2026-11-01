# Message Areas

ViSiON/3's message system lets users post and read messages across topic
areas, the heart of any board since the first Fido nodes traded mail at night.
An area is a message base on disk plus a handful of settings: who may read and
post, which conference it sits in, whether it is local or fed by a network, and
how long its messages are kept. Areas are configured in
`configs/message_areas.json` and stored as JAM (Joaquim-Andrew-Mats) binary
message bases. This page covers setting areas up, what callers see in the
reader and editor, and the file formats and command-line tools behind them.

## Configuring Message Areas

Use the [Configuration Editor](configuration/configuration.md#configuration-editor-tui) (`./config`, section 3 — Areas and Conferences → Message Areas) to add, edit, and remove message areas interactively. This is the recommended approach.

Each area has these fields in the editor:

| Field | What it sets |
| ----- | ------------ |
| **Position** | Display order in the area lists |
| **Tag**, **Name**, **Description** | Short unique tag (upper case), display name, longer description |
| **Area Type** | `local`, `echomail`, `netmail`, `v3net` or `qwknet` (see [Area Types](#area-types)) |
| **ACS Read**, **ACS Write** | Who may read and who may post ([Access Control](#access-control)) |
| **Base Path** | Where the JAM base lives under `data/` (blank = `msgbases/<tag>`) |
| **Conference** | The conference the area is grouped under |
| **Max Messages**, **Max Age** | Purge limits ([Message Purge Configuration](#message-purge-configuration)). The editor warns when no enabled event runs `v3mail purge`, because nothing else enforces them. |
| **Auto Join** | Newscan default for new callers ([Auto Join Behavior](#auto-join-behavior)) |
| **Real Name Only**, **Allow Anonymous** | How posts are signed ([Posting names](#posting-names-real-names-and-anonymous-posts)) |
| **Network**, **Echo Tag**, **Origin Addr**, **QWK Conference** | Shown for networked types only; see [Networked Areas](#networked-areas) |
| **Sponsor** | Handle of the area's moderator ([Sponsor Menus](users/sponsor-menus.md)) |

SysOps and area sponsors can also edit area settings live from within the BBS via the [Sponsor Menu](users/sponsor-menus.md) (`%` in the Messages Menu) — no restart required.

The JSON behind the editor is described in the
[Technical Reference](#message_areasjson).

### Area Types

- **local** — Messages stay on this BBS only. No FTN processing.
- **echomail** — Conference-style networked messages. The tosser imports/exports packets. Messages get MSGID, tearline, origin line, and SEEN-BY/PATH.
- **netmail** — Point-to-point private FTN mail between addresses.
- **v3net** — Networked V3Net message area. Messages sync with a V3Net hub.
- **qwknet** — Conference on a QWK-based network (e.g. DOVE-Net). `v3mail qwk-poll` exchanges packets with the hub; `qwk_conference` selects the hub conference. See [QWK Networking](messages/qwk-networking.md).

### Posting names: real names and anonymous posts

Posts are signed with the caller's handle unless the area says otherwise:

- **Real Name Only** (`real_name_only`) signs new posts *and replies* with
  the caller's real name, when they have one. Most FTN networks expect real
  names in their echoes. A caller with no real name on file is warned and
  posts under their handle. Private mail outside netmail areas is always
  signed by handle, whatever the area says; netmail follows the setting, and
  a netmail signed with a real name stays visible to its sender (see
  [Private Mail](messages/private-mail.md#real-names-and-private-mail)).
  Replies made from the reader and posts made by scripts (`v3.message.post`)
  follow the same rule as new posts.
- **Allow Anonymous** (`allow_anonymous`) lets callers at or above the
  board's `anonymousLevel` (`./config` → **2 - Access & Security** →
  **Anonymous Lvl**; default 5, 0 disables) choose to post
  anonymously. It is off unless the area turns it on, and a conference can
  forbid it for all its areas with its own `allow_anonymous: false`. An
  anonymous post is signed with the `anonymousName` string, gets no
  auto-signature, and records nothing about who wrote it. Callers cannot add
  anonymous authors to a [Twit List](messages/twit-list.md).

To change either setting on many areas at once, see
[Changing Many Areas at Once](#changing-many-areas-at-once).

### Auto Join Behavior

When a user joins an area automatically (at signup, or when an existing user
gains a new area at login), their last-read pointer is seeded one week back,
so the first newscan shows only recent messages instead of the area's entire
history.

Existing users are handled at login:

- New **local** areas flagged Auto Join are added to their newscan silently.
- A new **network** (FTN or V3Net) triggers a one-time prompt — "New network
  X is available - add to your newscan?" — covering all of that network's
  Auto Join areas. The answer is remembered; declining never re-prompts, and
  areas a user removes from their newscan are never re-added.

The FTN wizard and V3Net subscription flows ask whether imported areas
should be newscan defaults; the V3Net setting is per-network on the leaf
subscription (`autoJoinAreas`, default yes).

### Creating a New Message Area

1. Run `./config`, open **3 - Areas and Conferences → Message Areas** and
   choose to add an area.
2. Give it a **Tag**, **Name** and **Description**, pick the **Area Type**
   and **Conference**, and set **ACS Read** / **ACS Write** (blank means
   everyone).
3. Save. The running BBS notices the changed `message_areas.json`, checks
   it at once, and applies it as soon as no callers are online, so there is
   nothing to restart.
4. JAM base files are created automatically on first access.

To add an area by hand instead, append an object to the array in
`configs/message_areas.json` (see the [Technical Reference](#message_areasjson)
for every key):

```json
{
  "id": 2,
  "tag": "TECH",
  "name": "Technical Support",
  "description": "Technical questions and help",
  "acs_read": "",
  "acs_write": "s10",
  "conference_id": 1,
  "base_path": "msgbases/tech",
  "area_type": "local"
}
```

> **Tip:** Once the BBS is running, you can also edit existing area settings live via the [Sponsor Menu](users/sponsor-menus.md) (`%` in the Messages Menu).

---

## Editing Areas from the BBS

SysOps, Co-SysOps, and designated area sponsors can edit area settings live from within the BBS — no JSON editing or restart required.

From the **Messages Menu**, press `%` to open the Sponsor Menu for the currently selected area. From there, press `E` to open the area editor, where you can update the name, description, ACS strings, max message limits, sponsor handle, and more. Changes are saved atomically to `configs/message_areas.json`.

See [Sponsor Menus](users/sponsor-menus.md) for the full key reference and field details.

> **Sysop access:** SysOps and Co-SysOps automatically have sponsor access to all areas. The `%` key is hidden from the menu listing — press it directly.

## Changing Many Areas at Once

After importing a network's echoes, editing each area one by one is tedious.
`helper areas set` applies a setting to every area matching a filter —
by conference (tag or ID), network, area type, or tag pattern:

```bash
# Preview, then require real names in every Fidonet echo
./helper areas set --conference FIDONET --type echomail --real-name on --dry-run
./helper areas set --conference FIDONET --type echomail --real-name on

# Several settings in one pass, matched by tag pattern
./helper areas set --tag 'FD_*' --max-messages 500 --acs-write s20

# See what a filter matches
./helper areas list --network fidonet
```

Filters combine (an area must match all of them); `set` refuses to run
without a filter unless `--all` is given. Settings: `--real-name`,
`--anonymous`, `--auto-join` (`on`/`off`), `--acs-read`, `--acs-write`,
`--max-messages`, `--max-age`, and `--sponsor`. Only the settings you pass
are changed. A running BBS reloads `message_areas.json` on its own. The full
flag list is in the [Technical Reference](#helper-areas).

It is safe to run while the BBS is up or the config editor is open. Every
program that writes `message_areas.json` takes a shared lock
(`message_areas.json.lock`) and, before saving, merges in changes others made
since it loaded the file, so one tool does not undo another's edits. If the
same setting on the same area was changed in two places, the most recent save
wins, and the BBS log or the config editor's status line says so.

The config editor offers the same thing for an FTN network's echo areas when
you change an **Auto-Create** default: see
[Auto-creating echo areas](messages/ftn-echomail.md#auto-creating-echo-areas).

## Posting from the Command Line

`helper post` posts a text file as a message, without logging in. Use it for
scripted announcements, bulletins, or a network ad:

```bash
# Post an ANSI ad to an echo; the subject defaults to the file's SAUCE title
./helper post --area FD_OTHERNETS --file micronet.ans --subj "[ANSI] Micronet Information Network"

# Send netmail: post to the network's netmail area with Name@address
./helper post --area fidonet_netmail --to "Sean Dennis@1:18/200" --subj "Your ad" --file note.txt

# Read the body from stdin, check first without writing
fortune | ./helper post --area GENERAL --subj "Today's fortune" --file - --dry-run
```

The message is written exactly as the BBS editor writes it, so echomail gets
its MSGID, tearline and origin and goes out on the next `v3mail scan`. In
a netmail area, the tosser routes the message to the right link, and a copy
stays in the area. For netmail, `helper post` looks the address up in the
compiled nodelist and shows whose system it is. `--from` defaults to
`sysOpName` in `config.json`, and `--to` to `All`.

How the file is treated depends on its extension (override with `--format`):

| Format | Handling |
|--------|----------|
| `.ANS` | Stored byte for byte: escape codes, CP437 characters and long lines are kept, and never re-wrapped. A colour reset is added at the end so the tearline isn't drawn in the art's colours. |
| `.ASC`, `.TXT` (and stdin) | Plain text. UTF-8 is converted to CP437 (`--charset` to force either); `--wrap N` re-wraps long lines. |

For every format, CRLF, LF and a lone CR each become one line break, and the
SAUCE record and DOS EOF marker are removed. Echomail and netmail are marked
`CHRS: CP437 2` so other systems don't translate the art. Files with NUL
bytes are refused. From, To and Subject in echomail and netmail are sent as
CP437, so a character CP437 doesn't have (an emoji, say) is refused there.
Lines that other software reads as message structure (`---`, `* Origin:`,
`SEEN-BY:`), and a clear-screen code, produce a warning.
In a V3Net area, the post waits until V3Net is enabled and the area's network
is connected; then the running BBS tries to send it to the hub every minute
until the hub accepts it.

Posts to `PRIVMAIL`, and posts made with `--private`, are private. A private
message in a local area must go to an existing user: put their handle in
`--to`. `--private` is refused in echomail areas, because echomail is sent to
every link and doesn't stay private.

The flags and exit codes are in the [Technical Reference](#helper-post).

---

## Networked Areas

An area of type `echomail`, `netmail`, `v3net` or `qwknet` belongs to a
network, named in its `network` field: an `ftn.json` network key for FTN
areas, a `qwknet.json` key for QWK network areas. The setup wizards create
these areas for you; this is what they fill in.

Echomail areas in `message_areas.json` must include a `"network"` field matching
the network key in `ftn.json`:

```json
{
  "area_type": "echomail",
  "echo_tag": "FSX_GEN",
  "origin_addr": "21:3/110",
  "network": "fsxnet"
}
```

This ties each area to a specific FTN network, allowing the same BBS to
participate in multiple networks (e.g. fsxNet and FidoNet) simultaneously.
Echo area routing is configured here, by the area's `network` field, not on
individual links. A `qwknet` area carries `qwk_conference` (the hub's
conference number) instead of an origin address.

Setting up the networks themselves — `ftn.json`, the tosser, binkd, hub
polling — is covered in [FTN Echomail](messages/ftn-echomail.md),
[QWK Networking](messages/qwk-networking.md) and
[V3Net Networking](v3net/message-areas.md). The tearline is not configurable:
FTS-0004 reserves it for the software that produced the message, so ViSiON/3
always stamps `--- ViSiON/3 vX.Y.Z/Platform`.

---

## Access Control

### Read Access

- Empty `acs_read` means public access
- Use ACS strings to restrict: `s10`, `fA`, etc.
- Checked when listing areas and reading messages

### Posting Messages

- Empty `acs_write` means all users can post
- Typically requires validation: `s10`
- Can require specific flags: `s10&fM`

---

## Message Management

### JAM Base Maintenance

JAM bases are binary files that may need periodic maintenance:

- **Backups**: Backup the `data/msgbases/` directory regularly
- **Integrity**: If a base becomes corrupted, delete the 4 JAM files and the base will be recreated (messages will be lost)
- **Growth**: JAM text files (.jdt) grow as messages are added. Deleted messages leave gaps that can be reclaimed by packing.

The `v3mail` command handles all message base maintenance. See [Nightly Message Base Maintenance](advanced/event-scheduler.md#nightly-message-base-maintenance) in the event scheduler docs for the recommended automated maintenance sequence.

### Message Purge Configuration

Per-area purge limits are set in `message_areas.json` via `max_messages` and `max_age`. Both default to `0` (no limit). Nothing enforces them until `v3mail purge --all` runs, normally from the nightly purge event in `events.json`. The template ships that event (`example_nightly_msgbase_purge`) disabled, so enable it along with the fix and pack events that go with it. While areas have limits and no enabled event runs `v3mail purge`, the BBS logs a warning at startup and the config editor says so when you set a limit.

```json
{
  "tag": "FSX_GEN",
  "max_age": 90
}
```

```json
{
  "tag": "GENERAL",
  "max_messages": 500
}
```

Both limits can be set simultaneously. Age is applied first; if the remaining count still exceeds `max_messages`, the oldest messages are removed until the limit is met.

Areas imported with `helper ftnsetup` start with `max_messages: 1000` and
`max_age: 365`; areas the tosser auto-creates take the network's Auto-Create
defaults ([FTN Echomail](messages/ftn-echomail.md#auto-creating-echo-areas)).

### Dupe Database

The FTN dupe database (`data/ftn/dupes.json`) tracks MSGIDs to prevent duplicate imports. Some software (TriToss, for example) sends echomail with no MSGID; those messages are tracked by a hash of their sender, recipient, subject, date and text instead. Old entries are automatically purged after 30 days.

---

## Private Mail

See [Private Mail](messages/private-mail.md) for setup and configuration.

---

## Message Functions

### Listing Message Areas

The `LISTMSGAR` function shows areas the user has read access to, grouped by conference. Uses templates: `MSGAREA.TOP`, `MSGAREA.MID`, `MSGAREA.BOT`, `MSGCONF.HDR`.

### Selecting a Message Area

The `SELECTMSGAREA` function displays the area list as a full-screen lightbar picker (arrow keys navigate, left/right switch conference, Enter joins), validates read ACS, and updates the user's current area and conference. When the `MSGAREA` templates are missing it falls back to a text listing that prompts for a tag or ID.

Both forms show a `Total` / `New` / `Yours` column set per area, and the picker flags areas holding unread messages with `NEW` to the left of the area name.

### Quick Navigation

The message menu supports quick-navigation keys for cycling through areas and conferences without opening a selection list:

| Key | Command | Behavior |
|-----|---------|----------|
| `]` | `NEXTMSGAREA` | Move to next area in current conference (wraps) |
| `[` | `PREVMSGAREA` | Move to previous area in current conference (wraps) |
| `}` | `NEXTMSGCONF` | Move to next conference (wraps, selects first area) |
| `{` | `PREVMSGCONF` | Move to previous conference (wraps, selects first area) |

Navigation respects ACS — only areas and conferences the user has access to are included. When switching conferences, the first accessible area in the new conference is automatically selected.

### Reading Messages

The `READMSGS` function provides random-access message reading:

- Starts at the first unread message if any exist
- Shows message header with sender, date, and subject, drawn from the
  caller's chosen `MSGHDR` template ([Header Placeholders](messages/placeholders.md))
- Tracks lastread via the JAM `.jlr` file (per-user, per-area)
- Hides messages from authors on the caller's [Twit List](messages/twit-list.md)
  and private mail that is not theirs ([Private Mail](messages/private-mail.md))

The command bar at the bottom is a lightbar; arrow to an item or press its
hot key:

| Key | Command | Notes |
|-----|---------|-------|
| `N` (Enter) | Next | Next visible message; at the end, "End of messages" and leave |
| `S` | Prev | Previous message |
| `R` | Reply | Reply in the same area |
| `M` | Mail | Reply by private mail to the author |
| `T` / Tab | Thread | Move focus to the thread menu, or back to the message (see below). Greyed out on the bar, and does nothing, when the message has no thread to show or the screen has no room for the pane |
| `P` | Post | Post a new message |
| `J` / `#` | Jump | Go to a message number |
| `L` | List | List message titles, cursor on this message (flat or threaded, as the caller last had it) |
| `I` | Twit | Add the author to your Twit List (hidden while Twit Lists are off) |
| `D` | Del | Delete the message (sysop and co-sysop only; shown in red) |
| `K` | Kludges | Show the message's control information (sysop and co-sysop only; not on the bar) |
| `?` | Help | List the commands |
| `Q` | Quit | Leave the reader |

**Reply threads (T).** The thread follows the reply links a message
carries (the JAM `ReplyTo`, or the `REPLY` kludge of echomail), not its
subject, so it shows the actual conversation. When the message is part of
a thread, a pane under the header draws it as a tree, up to five rows
(three on a short screen; a shorter thread takes only the rows it fills)
windowed around the current message, which carries the `►` marker, with
the root's subject in its title and the message's place in the thread.

Tab or `T` (Thread on the bar) moves focus to the **thread menu**. Its frame
turns bright cyan, its closing row lists its keys, a blue bar marks the
message being read, and the reader's bar below is greyed out while the menu
has the arrows. ↑/↓, Home and End move through the thread, stepping over
`#n deleted` placeholders, and each move shows that message straight away,
with focus left on the menu. Tab (or Enter, `T` or Esc) gives focus back to
the message, where the arrows scroll the body again. The key row names what
Tab does from there: `[Tab] Close` when the pane goes away with the menu
(Thread View off), `[Tab] Message View` when it stays. While Tab would open
the menu, the row above the reader's bar reads `[Tab] Thread View`. Any other reader
command works as usual and closes the menu. While the menu is closed, the
whole pane is drawn in dark grey with no bar (the message being read keeps
its `►` marker). A blank row separates the pane from the message.

The caller's **Thread View** setting in User Konfig decides whether the
pane shows (not as a menu) while the menu is closed; with it off, the pane
appears only while the menu is open. A message with no thread to show (one
nobody has answered and that answers nothing, or whose every other message
is deleted or hidden from the caller) has no pane, and Thread is greyed out
on the bar. Mail the caller may not read (another user's private message, a
post from an author on their Twit List) is never shown or counted in any of
this; a reply to such a message hangs from the nearest message the caller
can see. The direct-reply count is available to header templates as `@H@`.

**Listing from the reader (L).** `L` opens the message list, flat or
threaded as the caller last had it (`T` in the list switches), with the
cursor on the message being read. Enter there returns to the same reader
on the highlighted message; `Q` or Esc return to the message the list was
opened from. No second reader is opened underneath, however many times the
caller goes back and forth. A reader opened from the menu's list
(`LISTMSGS`) returns to that list on `L` or `Q`, in the mode it was in,
with the cursor on the message that was being read.

### Listing Messages

The `LISTMSGS` function lists the current area, newest first, in a framed
lightbar list: arrows, PgUp/PgDn, Home and End move the cursor, Enter reads
the message and `Q` or Esc leaves. The list opens as the **threaded
list**; `T` switches between it and the flat list, keeping the cursor on
the same message. The choice holds for the rest of the call, and the
reader's `L` opens in it.

The threaded list shows each thread as a tree, newest root first, replies
in the order the reader's `]` walks them. The `Thread` column holds the
tree glyphs and then the root's subject, or a reply's own first line of
text, so `Re:` is not repeated down the column; a reply that changed the
subject shows the new subject instead. A deleted message that a listed
reply answers (one not yet packed away) appears as a `#n deleted` row that
the cursor steps over and the page counter leaves out. Private mail that is
not the caller's, and posts from authors on their Twit List, are left out,
and their replies hang from the nearest listed message. The titles, column
heading, help lines and placeholder text are the `msgList*` strings.

**Header fields.** A message's From, To and Subject never reach a caller with
control characters in them, wherever the message came from: each one becomes a
space when the message is stored, read or exported. The message body can still
carry ANSI.

**Kludges (K).** Sysops chasing a duplicate or misrouted message need its
MSGID, PATH and SEEN-BY. `K` shows the current message's control information
full screen: origin and destination addresses; written, received and processed
dates; attributes in hex plus flag names; reply links; then the kludges in
wire order — MSGID, REPLY and PID first, then the rest from the JAM header and
the message text, then SEEN-BY and PATH — with the `^A` lead-ins removed. Long
lines wrap with an indent; longer than a screen, it pages (Space, Enter or Down
for the next page, PgUp or Up to go back). `Q` or Esc returns to the message.
A caller's `K` does nothing.

**ANSI art in messages.** Messages that use cursor positioning, or cursor
save/restore (`ESC[s`/`ESC[u`, the way Synchronet splits an ad into message
lines), are rendered on a virtual screen and shown as whole rows, so scrolling
does not shear them. ANSI messages render at 80 columns at most, since that is
what message art is drawn for; a wider terminal shows them at 80.

**Terminal control in message bodies.** The reader sends a caller's terminal only
what a body needs to draw: colour, cursor movement and position, erasing the
line or screen (not the scrollback), and cursor save and restore. Everything else
is left out without a trace, so a message cannot set a window title, change a
terminal's modes or character set, reset it, ring its bell, or ask it a question
whose answer would land in the caller's input. Text inside an OSC, DCS, APC, PM
or SOS string is not shown; a string that never ends costs the rest of its line,
and the message carries on from the next one. Raw CP437 is untouched, including
0x80 to 0x9F. The stored message does not change and networks get it as it
arrived, so a sysop's `K` view, exports and packets see the original.

### Composing Messages

The `COMPOSEMSG` function launches the built-in editor. Subject line required. For echomail areas, automatically adds MSGID, tearline, and origin line.

The **To:** prompt depends on the area:

- **Public areas:** To starts as `All`, and a blank answer means `All`.
- **`PRIVMAIL`:** To starts empty and must be an existing user; an unknown
  name is reported and asked again. The message is saved as private mail, the
  same as `SENDPRIVMAIL`.
- **Netmail areas:** To starts empty. Enter `Name@zone:net/node`, or just the
  name, in which case you are asked for the address. An address in a
  different zone from the area's origin address asks "Send anyway?" first,
  since the mail still goes out through this network's links. Answering No
  asks for the address again. When the network has a compiled
  [nodelist](messages/nodelists.md#writing-netmail), the system the address
  belongs to is shown, and an address that is not listed, or is listed as
  Down, asks "Send anyway?" the same way.

In `PRIVMAIL` and netmail, leaving To (or the netmail address) blank abandons
the post.

The From name follows the area's **Real Name Only** setting, and an area that
allows anonymous posts asks whether to post anonymously
([Posting names](#posting-names-real-names-and-anonymous-posts)). After the
message is saved, the caller's [auto-signature](messages/autosig.md) is added,
or offered if they have **Ask each post** on.

### Quoting in the Editor

When a caller replies, the editor's quote picker lets them pull lines from the
original into their message, prefixed with the author's initials
(`QuotePrefix` in `strings.json`; `^I` = initials, `^N` = name). Two things
happen to the quoted text:

- **Paragraphs are reflowed.** A run of source lines from one paragraph is
  joined and rewrapped under the quote prefix, so a near-full source line does
  not leave a one-word stub on the next line. Paragraph breaks are inferred
  from the source: a blank line, a change of quote depth or indent, a list
  item (`- `, `* `, `1. `), or a line that ended early when the next word
  would have fitted. Nothing is joined when no source line is at least 40
  columns wide (a poem, a list, one-liners), and lines over 80 columns are
  taken as whole paragraphs. Only consecutive source lines join; skipping a
  line starts a new paragraph, so a quote never closes up over omitted text.
  Backspace takes the last line back out of its paragraph. A nested quote
  prefix (`Sh> `) is carried onto every wrapped line (`Bu> Sh> …`).
- **Banners depend on the area.** In local and V3Net areas the quoted block
  is wrapped in the `quoteTop` and `quoteBottom` strings (shipped as
  `[Name Said]` / `[Name Done]`, with pipe colours). In echomail, netmail and
  QWK network areas the plain `quoteTopNet` and `quoteBottomNet` strings are
  used instead, shipped as `^N wrote:` and no bottom line. Colours are
  stripped from the net banners so no ANSI goes out over the network, and a
  blank value means no line at all (there is no fallback to the default).
  Keep them free of a leading `---`, which other software reads as a tearline.
  All four are in the String Editor next to **Quote Prefix** (**Quoting Top
  Line**, **Quoting Bottom Line**, **Net Quoting Top Line**, **Net Quoting
  Bottom Line**).

### New Message Scan

The `NEWSCAN` function (or `NEWSCAN CURRENT` for the current area only) opens a
scan setup menu before reading. Enter starts the scan; each option does exactly
this:

- **[D] Date** — what to scan. `N` (the default) reads only messages past your
  last-read pointer, `A` reads every message, or enter a date to read messages
  written on or after that day. Accepted formats, exactly as the invalid-date
  notice lists them: `MM/DD/YY`, `MM/DD/YYYY`, `MM-DD-YY`, `MM-DD-YYYY`,
  `YYYY-MM-DD`, `MMDDYY` and `MMDDYYYY`. Month and day may drop their leading
  zero in the separated forms (`9/1/26`); the all-digit forms need every digit
  (`090126`). An unrecognised date is reported and the setting is left
  unchanged.
- **[T] To** — show only messages whose To field contains the text
  (case-insensitive substring). Enter on its own clears it.
- **[F] From** — the same for the From field, so `bob` matches "Bob Jones".
- **[R] Range** — first and last message number to read, validated against the
  current area's message count. Enter on its own at either prompt clears the
  range, ESC leaves it unchanged, and an out-of-range entry clears it with a
  notice. A range overrides the New/All choice: the numbered messages are shown
  whether or not they have been read. In a multi-area scan the same numbers
  apply to each area.
- **[U] Update NewScan Pointers** — `Yes` (the default) leaves the last-read
  pointer where reading stopped. `No` puts each area's pointer back where it was
  once that area is finished, so the messages stay new for the next scan.
- **[S] Scan Which Areas** — `M`arked (areas tagged in newscan config), `A`ll
  areas in the current conference, or the `C`urrent area only.
- **[A] Abort** — leave without scanning (`Q` and ESC do the same).

Date, To, From and range combine: a message must satisfy all of them. The
reader and its **L**ist show only matching messages, areas with no match are
skipped, and a scan in which nothing matches says so. A plain newscan with
nothing new simply reports "Newscan complete". Messages hidden by the
caller's Twit List are counted once in a summary at the end of the scan.

The `UPDATENEWSCAN` pointer command accepts the same date formats when moving
pointers to a date.

### Newscan Configuration

The `NEWSCANCONFIG` function (key **Z** in the message menu) lets users manage their personal newscan preferences:

- Scrollable list of all accessible areas grouped by conference
- **SPACE** / **ENTER** — Tag/untag an area
- **A** — Tag all areas
- **N** — Clear all tags
- **ESC** — Save and exit

Tagged areas are saved to the user's profile in `users.json`.

---

## Current Limitations

### Not Yet Implemented

- Per-message editing (sponsors can edit area settings and sysops can delete
  a message from the reader; editing a posted message is not available)
- Full-text message search (newscan can filter by To, From, date and range)
- File attachments

### In Development

- Network message support is functional (echomail tosser built in)

---

## Troubleshooting

### Messages Not Showing

- Check user's access level vs `acs_read`
- Verify JAM base files exist in `data/msgbases/`
- Check BBS logs for JAM open errors
- Ensure area ID matches configuration
- Ask whether the author is on the caller's Twit List; the reader hides those
  messages, while the area's counts still include them

### Can't Post Messages

- Verify `acs_write` requirements are met
- Check user is validated if required
- Ensure area exists in configuration
- Check write permissions on `data/msgbases/` directory

### Posts Go Out Under the Wrong Name

- Real names: set **Real Name Only** on the area (or on every area of the
  network with `helper areas set --network <net> --real-name on`); new posts
  and replies then use the real name. A caller with no real name on file
  still posts under their handle.
- Anonymous: the area must have **Allow Anonymous** on, the conference must
  not forbid it, and the caller must reach `anonymousLevel`.

### Echomail Not Working

- Verify the network's `"internal_tosser_enabled": true` is set in `configs/ftn.json`
- Check `own_address` is set correctly
- Ensure echo area tags match between config and message_areas.json
- Verify inbound/outbound directories exist and are writable
- Check logs for tosser errors
- See [FTN Echomail → Troubleshooting](messages/ftn-echomail.md#troubleshooting)

### A Change to message_areas.json Was Lost

Every writer (the BBS, the config editor, `helper`) locks the file and merges
its changes with what others saved, so this should not happen. If two places
changed the same field of the same area, the later save wins and the BBS log
or the editor's status line names the conflict. See
[How message_areas.json is written](#how-message_areasjson-is-written).

---

## Technical Reference

### message_areas.json

Message areas are defined in `configs/message_areas.json` as an array:

```json
[
  {
    "id": 1,
    "tag": "GENERAL",
    "name": "General Chat",
    "description": "General discussion area for all users.",
    "acs_read": "",
    "acs_write": "",
    "conference_id": 1,
    "base_path": "msgbases/general",
    "area_type": "local",
    "echo_tag": "",
    "origin_addr": ""
  },
  {
    "id": 2,
    "tag": "FSX_GEN",
    "name": "fsxNet General",
    "description": "fsxNet general discussion echo.",
    "acs_read": "",
    "acs_write": "s10",
    "conference_id": 2,
    "base_path": "msgbases/fsx_gen",
    "area_type": "echomail",
    "echo_tag": "FSX_GEN",
    "origin_addr": "21:3/110"
  }
]
```

#### Area Properties

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `id` | int | — | Unique numeric identifier |
| `position` | int | — | Display/sort order (1-based) |
| `tag` | string | — | Short tag name (uppercase) |
| `name` | string | — | Display name |
| `description` | string | — | Area description |
| `acs_read` | string | `""` | ACS required to read messages (empty = everyone) |
| `acs_write` | string | `""` | ACS required to post messages (empty = everyone) |
| `allow_anonymous` | bool | absent (no) | Allow anonymous posts. The area must opt in; a conference's own `allow_anonymous: false` overrides it |
| `real_name_only` | bool | `false` | Sign posts and replies with the caller's real name |
| `conference_id` | int | `0` | Conference this area belongs to (0 or omitted = ungrouped) |
| `base_path` | string | `msgbases/<tag>` | Relative path to JAM base files (under `data/`) |
| `max_messages` | int | `0` | Maximum number of messages to retain (0 = no limit). Oldest messages are removed when the count is exceeded |
| `max_age` | int | `0` | Maximum message age in days (0 = no limit). Messages older than this are removed |
| `auto_join` | bool | `false` | Flags this area as a newscan default. See [Auto Join Behavior](#auto-join-behavior) |
| `area_type` | string | — | `"local"`, `"echomail"`, `"netmail"`, `"v3net"`, or `"qwknet"` |
| `echo_tag` | string | `""` | FTN echo tag for echomail areas (e.g., `"FSX_GEN"`); for `qwknet` areas, the hub's conference name |
| `origin_addr` | string | `""` | FTN origin address for echomail (e.g., `"21:3/110"`) |
| `network` | string | `""` | Network key this area belongs to (`ftn.json` network for echomail/netmail, `qwknet.json` key for qwknet) |
| `qwk_conference` | int | `0` | For `qwknet` areas, the hub's conference number. Required; it is what messages are routed on |
| `sponsor` | string | `""` | Handle of the area sponsor/moderator (optional). See [Sponsor Menus](users/sponsor-menus.md) |

Keys marked `omitempty` in the source (`allow_anonymous`, `real_name_only`,
`conference_id`, `max_messages`, `max_age`, `auto_join`, `echo_tag`,
`origin_addr`, `network`, `sponsor`, `qwk_conference`) are left out of the file
when they hold their default.

### How message_areas.json Is Written

The BBS (sponsor area editor, auto-created areas), the config editor, and
`helper` (`ftnsetup`, `areas set`) all write `message_areas.json`. So that the
last one to save does not undo the others:

- Every writer holds a lock on the file (the sidecar
  `configs/message_areas.json.lock`) across its read-modify-write. The
  sidecar is created as needed; leave it alone.
- Writers that keep areas in memory (the BBS and the config editor) merge
  three ways before saving, per area ID and then per field. A change made on
  only one side wins, so edits to different areas or fields all survive. If
  both sides changed a field, the saver's value wins and the conflict is
  logged (BBS) or shown on the status line (editor). An edit beats a delete.
  Two areas added under one ID keep the one already on disk and renumber the
  saver's, including a default `msgbases/area_<id>` base path.
- The BBS merges only when the file's bytes differ from what it last read or
  wrote. A file that vanished is recreated from memory rather than read as
  "every area deleted". The config editor skips the write when nothing would
  change.
- The running BBS watches the file. A change is validated at once (a bad file
  is reported in the log) and applied when no callers are online.

### Message Storage (JAM Format)

Each message area is backed by a JAM message base consisting of 4 files:

| File     | Extension | Contents                                                                |
| -------- | --------- | ----------------------------------------------------------------------- |
| Header   | `.jhr`    | 1024-byte fixed header + variable-length message headers with subfields |
| Text     | `.jdt`    | Raw message text (CP437, CR line endings)                               |
| Index    | `.jdx`    | 8-byte index records (ToCRC + header offset) per message                |
| Lastread | `.jlr`    | 16-byte records tracking per-user lastread pointers                     |

JAM bases are stored under `data/msgbases/` (or the path specified by `base_path`). For example, area `GENERAL` with `base_path: "msgbases/general"` creates:

```text
data/msgbases/general.jhr
data/msgbases/general.jdt
data/msgbases/general.jdx
data/msgbases/general.jlr
```

JAM bases are created automatically on first use if they don't exist.

#### Key JAM Properties

- **1-based message numbering** — Messages are numbered starting from 1
- **Random access** — Any message can be read by number without scanning
- **Binary indexed** — Index file provides O(1) lookup by message number
- **Per-user lastread** — Tracked in `.jlr` file using CRC32 of username
- **Thread-safe** — All operations use `sync.RWMutex` for concurrent SSH/telnet sessions
- **FTN-native** — Subfields store MSGID, REPLY, AREA kludges, SEEN-BY, PATH directly

See [JAM Echomail](messages/jam-echomail.md) for what is written into a
message and the one subfield of ViSiON/3's own.

### Message Templates

Message display templates live in `menus/v3/templates/`.

#### Message Header

The message header is rendered using the `MSGHDR.*.ans` style system in `menus/v3/templates/message_headers/`. Users can select a header style via message reader settings. See [Message Header Placeholders](messages/placeholders.md) for available tokens.

#### Read Prompt

The read prompt at the bottom of the message reader is a hardcoded lightbar rendered by the Go message reader. It is not template-driven.

#### Area List Templates

- `MSGAREA.TOP` — Header
- `MSGAREA.MID` — Row template
- `MSGAREA.BOT` — Footer

#### Area List Template Placeholders

- `|CA` — Current area tag
- `^COLS` — (`MSGAREA.TOP` only) The rendered column-title row
- `^ID` — Left gutter: the list number in the text listing, or the `NEW` flag in
  the `SELECTMSGAREA` lightbar picker (shown when the area holds unread
  messages). 3 columns wide.
- `^NA` — Area name, 34 columns
- `^CF` — Conference the area belongs to, 12 columns
- `^TM` — Total messages in the area, right-aligned in 7 columns
- `^NM` — Messages past the user's last-read pointer, right-aligned in 6 columns
- `^YM` — Messages addressed to the user, right-aligned in 6 columns
- `^TAG` — Area tag, 16 columns
- `^DE` — Area description, 32 columns
- `^DS` — Area type (`local`, `echomail`, …), up to 8 columns

`MSGAREA.TOP` places the column titles with the `^COLS` placeholder. The BBS
renders that row itself from the widths above — `Area`, `Conf`, `Total`, `New`,
`Yours` — so the titles always line up with the data columns. Put `^COLS` on its
own line where the titles belong; a `MSGAREA.TOP` without the token is drawn
unchanged, so hand-written title rows still work, they just have to be aligned
by hand.

`^YM` counts every message ever addressed to the user's handle in that area,
read or not, matched on the recipient CRC stored in the JAM `.jdx` index.

### helper areas

`helper areas list [filters]` prints the areas a filter matches;
`helper areas set [filters] [settings]` changes them.

| Flag | Applies to | Meaning |
| ---- | ---------- | ------- |
| `--conference <tag or id>` | both | Areas in this conference |
| `--network <key>` | both | Areas with this network key |
| `--type <type>` | both | `local`, `echomail`, `netmail`, `v3net` or `qwknet` |
| `--tag <glob>` | both | Case-insensitive glob on the area tag (`FD_*`) |
| `--all` | both | Match every area (`set` needs this or another filter) |
| `--real-name on\|off` | `set` | `real_name_only` |
| `--anonymous on\|off` | `set` | `allow_anonymous` |
| `--auto-join on\|off` | `set` | `auto_join` |
| `--acs-read <acs>`, `--acs-write <acs>` | `set` | `acs_read`, `acs_write` |
| `--max-messages <n>`, `--max-age <days>` | `set` | Purge limits (0 = unlimited) |
| `--sponsor <handle>` | `set` | `sponsor` |
| `--dry-run` | `set` | Show what would change without writing |
| `--quiet` | `set` | Only print the summary |
| `--config <dir>` | both | Config directory (default `configs`) |

Filters are ANDed. `set` reports how many areas changed and how many already
matched. Exit status: `0` on success, `1` when the configuration could not be
read or written or a value was invalid, `2` for a bad flag or a stray argument.

### helper post

`helper post --area <tag or id> --file <path|-> [options]`

| Flag | Default | Meaning |
| ---- | ------- | ------- |
| `--area` | required | Message area tag or ID |
| `--file` | required | Text file to post as the body, or `-` for stdin |
| `--subj` | SAUCE title of an `.ANS` file | Subject |
| `--to` | `All` | Recipient; in a netmail area `"Name@zone:net/node"`; in `PRIVMAIL` or with `--private`, an existing handle |
| `--from` | `sysOpName` from `config.json` | Sender |
| `--private` | off | Mark the message private (refused in echomail areas) |
| `--reply-to <n>` | 0 | Thread the post under message *n* in the same area (JAM ReplyTo plus the parent's MSGID) |
| `--format ans\|asc\|txt` | from the extension; `txt` for stdin | Body format |
| `--charset auto\|cp437\|utf8` | `auto` | Plain-text input charset (`ans` is always stored raw) |
| `--wrap <n>` | 0 | Re-wrap plain text to *n* columns (`asc`/`txt` only; 0 keeps lines) |
| `--dry-run` | off | Check and describe the post without writing it |
| `--config <dir>`, `--data <dir>` | `configs`, `data` | Directories |

Exit status: `0` when the message was posted (or `--dry-run` passed), `1`
when it was refused or could not be written (unknown area, bad address, text
that CP437 cannot hold, a NUL byte, `--private` in echomail), `2` for a bad
flag or a stray argument. Only warnings and errors are logged.

### API Reference

#### MessageManager Methods

- `NewMessageManager(dataPath, configPath, boardName, networkOrigins)` — Initialize with JAM bases; `networkOrigins` maps each `ftn.json` network key to its origin text
- `Close()` — Close all JAM bases (call on shutdown)
- `ListAreas()` — Returns all message areas sorted by ID
- `GetAreaByID(id)` — Get area by numeric ID
- `GetAreaByTag(tag)` — Get area by tag name
- `AddArea(area)` — Add an area under the file lock, merging with concurrent writers; returns the ID actually assigned
- `SaveAreas()` — Persist all areas back to `message_areas.json` (locked, merged, atomic write)
- `AddMessage(areaID, from, to, subject, body, replyMsgID)` — Add new message (returns msgNum)
- `Post(areaID, from, to, subject, body, opts)` — Like AddMessage, with `PostOptions` for private, reply, extra kludges and the sender handle (used by the editor and `helper post`)
- `GetMessage(areaID, msgNum)` — Read single message by number
- `GetMessageKludges(areaID, msgNum)` — The header fields and kludge lines the reader's `K` command shows
- `GetMessageCountForArea(areaID)` — Total message count
- `GetNewMessageCount(areaID, username)` — Unread count for user
- `GetAreaCounts(areaID, username)` — Total, unread and personal counts in one base open (used by the area lists)
- `GetLastRead(areaID, username)` — Last read message number
- `SetLastRead(areaID, username, msgNum)` — Update lastread pointer
- `GetNextUnreadMessage(areaID, username)` — Next unread message number
- `GetBase(areaID)` — Get underlying JAM base (for tosser)
