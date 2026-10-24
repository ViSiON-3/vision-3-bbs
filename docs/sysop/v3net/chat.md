# V3Net Chat

V3Net Chat provides real-time inter-BBS chat across all nodes on a V3Net
network. Users can also chat locally (this BBS only) without any V3Net
configuration.

> **Experimental — Development Only.** V3Net is under active development and
> is not yet ready for production use. APIs, configuration, and wire formats
> may change without notice.

---

## How It Works

Chat is room-based. Each network has one or more named rooms; users join a room
and see messages from everyone in that room across all connected BBSes in
real time.

When a user enters chat from the main menu, they are presented with:

1. **Network picker** — choose a configured V3Net network or local-only chat
2. **Room picker** — choose a room on that network (or type a name to create one)

After selecting, the full-screen chat interface opens.

---

## Menu Command

Add the chat command to a menu `.CFG` file:

```
TYPE    = CHAT
HOTKEY  = C
DISPLAY = Enter Chat
```

No arguments are needed. The network and room pickers run automatically at
launch.

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

- **Header** (rows 1–5): ANSI art showing network, room, and topic
- **Scroll area**: incoming messages; scrolls automatically. The last 500
  lines are kept, and PgUp and PgDn page through them; the hint bar says when
  the view is paged back, and new messages wait below until you return
- **Status bar**: current room members, hint line
- **Input row**: typed text, sent with Enter. A word that does not fit on the
  row moves whole to a new row, and the input area grows upward one row at a
  time (up to five rows), pushing the status bar and the scroll area up. It
  returns to one row once the message is sent. A message can be up to 500
  characters.

---

## Chat Commands

| Command | Description |
|---------|-------------|
| `/join <room>` | Leave the current room and join another |
| `/rooms` | List all rooms on the current network with user counts and topics |
| `/topic <text>` | Set the topic for the current room |
| `/msg <handle> <text>` | Send a private message to a user |
| `/users` | Refresh the user list in the status bar |
| `/network` | Switch to a different network mid-session |
| `/?` or `/help` | List these commands on your own screen |
| PgUp / PgDn | Page back through the last 500 lines of chat; Enter or PgDn at the end returns |
| `/q` or `/quit` | Exit chat |

Commands are case-insensitive. Any other line is posted to the current room.
A line that starts with `/` but is not a command is never posted: chat shows
"unknown command" instead, and `/join`, `/topic` and `/msg` without their
arguments show their usage. To post a line that starts with a slash, type two:
`//shrug` posts `/shrug`.

The help list, the usage lines and the unknown-command reply are the
`chatHelp`, `chatJoinUsage`, `chatTopicUsage`, `chatMsgUsage` and
`chatUnknownCommand` strings, editable in the string editor.

---

## Local Chat

If no V3Net networks are configured, chat runs in local mode — messages are
only visible to users on this BBS. Local chat uses the same full-screen
interface and commands.

When V3Net networks are configured, **Local** always appears as an option in
the network picker so you can still run a local-only session.

---

## ANSI Art Header (CHATHEADER.ANS)

The chat header is loaded from the menu set's ANSI art directory:

```
menus/<set>/ansi/CHATHEADER.ANS
```

The file must be CP437-encoded (standard `.ANS` format, editable in tools like
PabloDraw). ViSiON/3 converts it to UTF-8 at display time.

### Placeholders

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
| `@L#…@` | Network round-trip time; blank on Local chat | `·042ms` |
| `@B#…@` | Scrollback lines in use and the scrollback size | `·012/500` |

The four counters are drawn as a `·` separator followed by the number,
zero-padded to fill the field. A number with more digits than fit shows as all
nines. `@L` ends in `ms`, and `@B` splits its digits between the two numbers.
The stock art uses the narrowest fields: `@U#@` (4 columns), `@M@` (3),
`@L###@` (6) and `@B#####@` (8). Make a field wider for more digits.

**Example**: a 16-character room field:

```
@ROOM##########@
```

If the current room is `lobby`, the art displays `lobby` followed by 11
spaces.

Placeholders are filled after the art is converted from CP437, in one pass, so
a topic or room name that happens to contain `@U@` is shown as typed.

---

## Related Documentation

- [FelonyNet Setup](v3net/felonynet.md) — joining the public V3Net chat network
- [V3Net Configuration](v3net/configuration.md) — configuring V3Net leaf nodes
- [Menus & ACS](menus/menu-system.md) — adding the chat command to a menu
