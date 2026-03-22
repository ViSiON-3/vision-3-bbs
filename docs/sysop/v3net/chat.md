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
- **Scroll area**: incoming messages; scrolls automatically
- **Status bar**: current room members, hint line
- **Input row**: typed text, sent with Enter

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
| `/q` or `/quit` | Exit chat |

Any text that does not begin with `/` is posted to the current room.

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

Three placeholders can be embedded anywhere in the art file. Each placeholder
must be padded with `#` characters to a fixed width, matching the space
reserved in the layout:

| Placeholder | Replaced with | Example |
|-------------|---------------|---------|
| `@NET####...####@` | Current network name | `felonynet` |
| `@ROOM###...###@` | Current room name | `lobby` |
| `@TOPIC#...#@` | Current room topic | `Welcome to FelonyNet` |

The placeholder syntax is `@NAME` + enough `#` characters to fill the field +
`@`. The value is left-justified and padded with spaces to the same total byte
length as the placeholder.

**Example** — a 16-character room field in the raw ANS bytes:

```
@ROOM###########@
```

If the current room is `lobby`, the art displays:

```
lobby
```

> Keep placeholders inside ASCII-safe regions of the art. They are replaced
> before CP437→UTF-8 conversion, so they must not overlap any high-byte
> CP437 characters.

---

## Related Documentation

- [FelonyNet Setup](felonynet.md) — joining the public V3Net chat network
- [V3Net Configuration](configuration.md) — configuring V3Net leaf nodes
- [Menus & ACS](../menus/menu-system.md) — adding the chat command to a menu
