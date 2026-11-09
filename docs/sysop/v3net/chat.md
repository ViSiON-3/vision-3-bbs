# V3Net Chat

V3Net Chat provides real-time inter-BBS chat across all nodes on a V3Net
network: a caller on your board types a line and callers on every other board
in the room see it at once, in the same full-screen teleconference-style
interface the old multi-line boards had. Users can also chat locally (this
BBS only) without any V3Net configuration, so the screen is useful on a
single-node board too.

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use. APIs, configuration, and wire formats
> may change without notice.

---

## How It Works

Chat is room-based. Each network has one or more named rooms; users join a room
and see messages from everyone in that room across all connected BBSes in
real time.

When a user enters chat from the main menu, they are presented with:

1. **Network chat notice**, the first time only — what other boards see,
   what the hub keeps and who moderates; they take part only after
   answering Y. See [Network Chat](v3net/central.md#network-chat).
2. **Network picker** — choose a V3Net network where the sysop offers
   chat, or local-only chat
3. **Room picker** — choose a room on that network (or type a name to create one)

After selecting, the full-screen chat interface opens.

---

## Menu Command

Chat is the `CHAT` menu command. The shipped main menu already carries it on
**C**; to add it to another menu, add an entry like this to the menu's `.CFG`
file (or add the command with the menu editor):

```json
{
    "KEYS": "C",
    "CMD": "RUN:CHAT",
    "ACS": "*",
    "HIDDEN": false,
    "NODE_ACTIVITY": "Chatting"
}
```

No arguments are needed. The network and room pickers run automatically at
launch. A subscription appears in the network picker only when its
**Lobby Chat** switch is on (see [Offering It](v3net/central.md#offering-it));
**Local** is always offered. With no network offering chat, the picker is
skipped and chat is local.

Two arguments change what is offered:

| Command | Effect |
|---------|--------|
| `RUN:CHAT` | The network picker over every offered network, and Local. |
| `RUN:CHAT NET` | Network chat only: no Local entry anywhere, including `/network`, and a caller who declines the notice, or whose room cannot be joined, is sent back to the menu rather than into local chat. This is what **Chat** on the V3Net Central menu runs. |
| `RUN:CHAT LOCAL` | Local chat only; no notice, no network picker. |

---

## Full-Screen Interface

```
┌─────────────────────────────── ViSiON/3 Chat ───────────────────────────────┐
│  Network: felonynet                    Room: #lobby                          │
│  Topic: Welcome to FelonyNet chat                                            │
│─────────────────────────────────────────────────────────────────────────────│
│  [chat scroll area]                                                          │
│                                                                              │
│─────────────────────────────────────────────────────────────────────────────│
│  Users: Sysop, Traveler, Zephyr                                              │
│  /rooms /join /msg /topic /network /q                                        │
│  [input prompt]                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

- **Header** (rows 1–5): ANSI art showing network, room, and topic, with
  optional Users, Mentions, Lag and Buffer counters
- **Scroll area**: incoming messages; scrolls automatically. The last 500
  lines are kept, and PgUp and PgDn page through them; the hint bar says when
  the view is paged back, and new messages wait below until you return
- **Status bar**: current room members, hint line
- **Input row**: typed text, sent with Enter. A word that does not fit on the
  row moves whole to a new row, and the input area grows upward one row at a
  time (up to five rows), pushing the status bar and the scroll area up. It
  returns to one row once the message is sent. A message can be up to 500
  characters.

Each post appears once: the caller's own line is echoed locally when the hub
accepts it, and the copy the hub sends back is dropped.

---

## Chat Commands

| Command | Description |
|---------|-------------|
| `/join <room>` | Leave the current room and join another |
| `/rooms` | List all rooms on the current network with user counts and topics |
| `/topic <text>` | Set the topic for the current room |
| `/msg <who> <text>` | Send a private message to someone in the room, named as the user list names them: `bob`, or `bob@Other BBS` for a caller on another board. `bob@<node-id>` reaches a caller elsewhere on the network |
| `/users` | Refresh the user list in the status bar |
| `/network` | Switch to a different network mid-session (only networks the caller may chat on are listed) |
| `/?` or `/help` | List these commands on your own screen |
| PgUp / PgDn | Page back through the last 500 lines of chat; Enter or PgDn at the end returns |
| `/q` or `/quit` | Exit chat |

Commands are case-insensitive. Any other line is posted to the current room.
A line that starts with `/` but is not a command is never posted: chat shows
"unknown command" instead, and `/join`, `/topic` and `/msg` without their
arguments show their usage. To post a line that starts with a slash, type two:
`//shrug` posts `/shrug`.

### Callers From Other Boards

On a network, the hub lists each member of a room with the board they are
on, and the chat screen names a caller from another board with it:
`Eve@Other BBS` in the user list, on each line they type, on a private
message from them and on join and leave notices. This board's own callers
are named by handle alone, as everyone is in local chat. Handles are not
unique across a network, so two callers named `Bob` on two boards are
`Bob` and `Bob@Other BBS`.

A hub from before this version lists handles only; against it, everyone
in the room is named by handle alone until the hub is updated.

### Private Message Delivery

The hub sends a private message only to authenticated event streams belonging
to the destination BBS on that network. Other BBSes, including a separate
sending BBS, receive no private-message event. On the destination BBS, the leaf
delivers the message once to each active chat session with the target handle;
multiple sessions with that handle each receive a copy. A message between two
callers on the same BBS follows the same recipient filtering.

Private messages are not end-to-end encrypted. The hub stores private-message
history, and the hub and destination BBS can read the text. Use HTTPS to protect
traffic in transit. Delivery is live and best effort: a successful send means
the hub accepted and stored the message, not that a recipient read it. Offline
or slow event streams can miss delivery; there is no private-history replay.

### Pipe Codes in Chat

Callers can colour what they type with the colour pipe codes (`|00`-`|15`,
`|23` and `|B0`-`|B15`). The screen codes `|CL`, `|CR`, `|DE`, `|P` and `|PP` do
not act: they show as typed, so nobody can clear or scroll another caller's
screen. `||` still shows as one `|`.

This covers message text, handles, topics, room and network names, and what
arrives from other boards, which also has any control characters blanked. The
chat strings in `strings.json` are the sysop's and keep every pipe code.

The fields of a `CHATHEADER.ANS` header are the exception: they are not expanded
at all, so a topic holding `|04` or `||` shows exactly that there.

### Choosing a Private Message Recipient

`/msg` names its recipient as the user list does, so what the caller sees
is what they type. `/msg Eve@Other BBS hello` reaches Eve on Other BBS;
the name may hold spaces, and case does not matter. `/msg eve hello`
reaches her too while nobody on this board is named Eve and she is in the
room from one board only; with an Eve here as well, the bare handle is
this board's Eve, and with Eves on several other boards the message is
refused and the refusal names them. The text is whatever follows the
name.

A recipient who is not in the room is sent to the hub as typed: the first
word is the handle, with an optional `@node-id` naming their board. The
hub looks for that exact handle, case-sensitively, among the rooms on the
network. One matching active BBS receives the message, even if the handle
is in several rooms there. If several BBSes have it, the message is refused
and the error lists `handle@node-id` choices with each board's name. An
explicit `handle@node-id` target works without a joined room, provided the
BBS is an active network subscriber; delivery to a caller remains best
effort. Local chat delivers by handle and ignores any node part. Resolving
a bare handle needs an updated hub; an older hub takes only the explicit
target.

---

## Local Chat

If no V3Net networks are configured, chat runs in local mode — messages are
only visible to users on this BBS. Local chat uses the same full-screen
interface and commands.

When V3Net networks offer chat, **Local** always appears as an option in
the network picker so you can still run a local-only session, and
`RUN:CHAT LOCAL` goes straight to it.

---

## Day-to-Day Operation

### Rate Limits on a Hub

The hub limits chat per user, not per board, so two callers on your BBS never
throttle each other:

- Each user may post one room message per second and, separately, one
  private message per second.
- Each node (BBS) may send a burst of 10 messages, then 5 per second across
  all of its users. The user's own limit is checked first, so one fast typist
  does not use up the allowance their board's other callers share.

A refused line is not delivered; the caller sees a system line reading
`Could not post:` followed by the hub's reply (`rate limit exceeded`). A
room post or topic change is credited to the handle that sent it, which must
have joined the room; the hub refuses it otherwise.

### Reconnecting to a Hub

When the event stream disconnects, chat keeps the intended room and blocks
room posts and topic changes until membership is restored. Each distinct
handle and room rejoins once, including when a caller has several active
sessions. A failed recovery reports an error and retries with the event
stream's backoff; the reconnected notice appears only after recovery succeeds.
Leaving a room or closing chat during recovery abandons that session's room
intent. Other active sessions sharing the membership remain joined.

Recovery refreshes the room's users and topic. It keeps the existing scrollback
and does not automatically append history from the gap, avoiding duplicate
messages. Messages sent while disconnected may therefore be missing from the
screen. An explicit room join still fetches recent history in the usual way.

### Troubleshooting

| Symptom | What to check |
|---------|---------------|
| `Could not post: ... rate limit exceeded` | The caller, or the whole board, is sending faster than the limits above. Wait a second and retry. |
| Lag shows `·---ms` | The hub did not answer the last round-trip check (every 15 seconds). Look for `leaf: SSE reconnecting` in the log; the field fills in again once the hub answers. |
| Lag is blank | Expected on **Local** chat, which has no hub. |
| A network is missing from the picker | It has no subscription in `v3net.json`. Add it under **6 — ViSiON/3 Networking (V3Net) → 2. Subscriptions** in `./config`. |
| `*** reconnected` appears mid-chat | The event stream returned and active room memberships were restored. Users and topic have been refreshed; missed messages are not automatically replayed. |

---

## Related Documentation

- [FelonyNet Setup](v3net/felonynet.md) — joining the public V3Net chat network
- [V3Net Configuration](v3net/configuration.md) — configuring V3Net leaf nodes
- [Menus & ACS](menus/menu-system.md) — adding the chat command to a menu
- [String Editor](advanced/string-editor.md) — editing the chat strings

---

## Technical Reference

### Limits

| Limit | Value |
|-------|-------|
| Message length | 500 characters |
| Input area | grows to 5 rows |
| Scrollback | 500 display lines per session |
| Lag measurement | every 15 seconds, a `GET` of the network's public room list |
| Hub per-user limit | 1 room post/s and 1 private message/s per handle |
| Hub per-node limit | burst of 10, then 5 messages/s |

### Strings

The help list, the usage lines and the unknown-command reply are the
`chatHelp`, `chatJoinUsage`, `chatTopicUsage`, `chatMsgUsage` and
`chatUnknownCommand` strings. `chatScrollbackHint` is the hint shown while
paged back, `chatSystemPrefix` wraps system lines such as `Could not post:`,
`chatReconnected` is the reconnect notice, and the picker and room-list rows
are `chatNetworkPickerHeader`, `chatNetworkPickerEntry`, `chatRoomListHeader`
and `chatRoomListEntry`. All are editable in the string editor.

### ANSI Art Header (CHATHEADER.ANS)

The chat header is loaded from the menu set's ANSI art directory:

```
menus/<set>/ansi/CHATHEADER.ANS
```

The file must be CP437-encoded (standard `.ANS` format, editable in tools like
PabloDraw). ViSiON/3 converts it to UTF-8 at display time.

#### Placeholders

Placeholders can be embedded anywhere in the art file. A placeholder is `@`,
the name, any number of `#` characters, and a closing `@`. Its total length is
the width of the field on screen: the value is padded with spaces or cut to
exactly that many columns, so the art keeps its shape.

| Placeholder | Replaced with | Stock art |
|-------------|---------------|-----------|
| `@NET###…@` | Current network name | `felonynet` |
| `@ROOM##…@` | Current room name | `lobby` |
| `@TOPIC#…@` | Current room topic | `Welcome to FelonyNet` |
| `@U#…@` | Users in the room | `·003` |
| `@M#…@` | Messages since you joined the room that contain your handle as a word | `·01` |
| `@L#…@` | Network round-trip time; `·local` on Local chat | `·042ms` |
| `@B#…@` | Scrollback lines in use and the scrollback size | `·012/500` |
| `@K@` | Link: `?` until the first ping answers, `■` while the hub answers, `!` while it does not; blank on Local chat | ` ■ ` |

The four counters are drawn as a `·` separator followed by the number,
zero-padded to fill the field. A number with more digits than fit shows as all
nines. `@L` ends in `ms`, and `@B` splits its digits between the two numbers.
While the hub is unreachable `@L` shows dashes in place of the digits
(`·---ms`) and `@K` shows `!`; both follow the lag ping, taken every 15
seconds. The stock art uses the narrowest fields: `@U#@` (4 columns),
`@M@` (3), `@L###@` (6), `@B#####@` (8) and `@K@` (3, with the mark in the
middle). Make a field wider for more digits.

**Example**: a 16-character room field:

```
@ROOM##########@
```

If the current room is `lobby`, the art displays `lobby` followed by 11
spaces.

Placeholders are filled after the art is converted from CP437, in one pass, so
a topic or room name that happens to contain `@U@` is shown as typed.

### Wire Details

A leaf drives chat through the hub's `/v3net/v1/{network}/chat/rooms/...`
endpoints: `join`, `leave`, `post`, `private` and `topic` are signed `POST`s,
and the room list (`chat/rooms`) and a room's `history` are unsigned `GET`s.
A private request may omit `to_node` for hub-side resolution: no joined match
returns HTTP 404 and several matching nodes return HTTP 409 with explicit
choices, without storing or sending the message. An explicit `to_node` keeps
the existing direct-target behavior.
Each post, private message and topic change names the sending `handle`; the
hub returns HTTP 403 (`not joined to room`) for a room post or topic from a
handle that has not joined, and HTTP 429 (`rate limit exceeded`) when a limit
above is hit. Events arrive on the network's SSE stream as `chat_message`,
`chat_private`, `chat_join`, `chat_leave` and `chat_topic`. The hub keeps
room history in its own database for seven days.
