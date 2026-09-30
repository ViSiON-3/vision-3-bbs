# Door Servers (Outbound RLogin and Telnet)

A **door server** is a separate machine that hosts door games for one or more BBSes. Instead of installing every door on every board you run, you install them once on the door server and point each BBS at it. ViSiON/3 connects out to the server over [RLogin](https://datatracker.ietf.org/doc/html/rfc1282) or [Telnet](https://datatracker.ietf.org/doc/html/rfc854), hands the user's terminal across, and returns them to the menu when the door ends.

This is configured as a door like any other, so it appears in the door list, obeys access levels and time limits, and is opened with the same `DOOR:CODE` menu command. See [Door Programs](doors/doors.md) for settings shared by every door type.

## When to use this

- You run more than one BBS and want them to share one set of doors.
- You want to reach a door server that speaks plain RLogin, including **DoorParty** by way of [its connector](#doorparty-via-the-connector). BBSLink and Exodus need handshakes of their own and are not supported yet — see [Limitations](#limitations).
- You want to reach a door server, or another BBS, that only answers Telnet — see [Telnet](#telnet).
- You want to run doors on a different machine from the BBS.

If the doors run on the same machine as ViSiON/3, you do not need this — use a native, DOS, or script door instead.

## Which protocol

**Use RLogin when the server offers it.** It is what door servers are built around: the handshake carries the caller's identity and a door code, so the user lands in the game already logged in. Every door server built for BBS use — Synchronet's, Mystic's, DoorParty — speaks it.

**Use Telnet when RLogin is not on offer.** Telnet has no handshake, so the server sees an anonymous connection and shows whatever it shows a new caller, usually a login prompt. ViSiON/3 can type a login for the user on connect ([Send On Connect](#logging-in-automatically)), but there is no standard way to name a door, so the user usually arrives at the server's menu rather than in a game.

The settings shared by both — host, port, timeout and the hang-up key — are described under [Quick start](#quick-start). The rest of this page is about RLogin up to the [Telnet](#telnet) section.

## Quick start

In the [Configuration Editor](configuration/configuration.md#configuration-editor-tui) (`./config`, section 7 — Door Programs), add a door and set **Type** to `RLogin`. Fill in **Host**, and usually **Terminal Type**:

| Field | JSON key | Meaning |
| --- | --- | --- |
| Host | `host` | Door server hostname or IP address. Required — the config editor warns when you leave a door without one. |
| Port | `port` | TCP port. Blank or `0` uses 513. Most door servers are **not** on 513 — see [Ports](#ports). |
| Client User | `client_username` | First handshake field. **This is where a password goes** on servers that want one — see [Passwords](#passwords). Blank sends the user's handle. |
| Server User | `server_username` | Second handshake field, normally the caller's identity. Blank sends the user's handle. |
| Terminal Type | `terminal_type` | Third handshake field. Blank sends `ANSI/38400`. |
| Connect Timeout | `connect_timeout` | Seconds to wait for the server. Blank or `0` uses 10. |
| Disconnect Key | `disconnect_key` | Key that hangs up the session. Blank uses `^]`. |

A Telnet door has the same **Host**, **Port** (defaulting to 23), **Connect Timeout** and **Disconnect Key**, and its own fields in place of the handshake — see [Telnet](#telnet).

The smallest entry that will work against a server wanting no authentication:

```json
{
  "code": "DOORSRV",
  "name": "Door Server",
  "type": "rlogin",
  "host": "doors.example.com",
  "port": 3513
}
```

Most real door servers want more than that. See [Passwords](#passwords) and the [worked examples](#worked-examples).

## Ports

**513 is the RLogin default, but assume your door server is not on it.** On Unix, binding a port below 1024 requires privileges, so a door server run by an ordinary user is almost always somewhere else — 3513 and 9999 are both common. The DoorParty connector, for instance, listens on **9999** by default.

Set **Port** to whatever your server documents. Leave it blank or `0` only if you know it really is on 513.

## Passwords

**This is the field most first attempts get wrong.** Both Synchronet door servers and DoorParty authenticate the caller with a password, and RLogin has nowhere to put one — so both overload the *client-user-name* field to carry it. Put the password in **Client User** (`client_username`), not in Server User.

The DoorParty connector's own source names the field exactly that way when it reads what your BBS sent:

```go
// Slice of "", client username, server username, terminal-type
password: rloginData[1],   // the client-user-name field is the password
userName: rloginData[2],
```

So the layout those servers expect is:

| Field | Contents |
| --- | --- |
| Client User | The password |
| Server User | The caller's handle, sometimes tagged, e.g. `[V3]Neo` |
| Terminal Type | The door code, or blank for the server's menu |

The password is normally **one shared secret for your whole system**, issued by whoever runs the door server (or chosen by you if it is your own), not a per-user password. ViSiON/3 deliberately has no placeholder for a caller's own BBS password: RLogin is cleartext, and sending real user passwords to another machine is not something this should make easy. Synchronet offers a hashed variant of that for third-party servers; if you need it, say so on [#382](https://github.com/ViSiON-3/vision-3-bbs/issues/382).

> The password is stored in plain text in `configs/doors.json` and sent in the clear over the network. Keep the file's permissions tight, and see [Security](#security).

## The three handshake fields

RLogin sends exactly three strings when it connects, and door servers overload all three. **What to put in them is decided by the door server, not by ViSiON/3** — the fields are sent exactly as configured, so follow whatever your server's documentation says.

The common conventions:

| Field | Typical contents |
| --- | --- |
| Client User | A password on servers that authenticate (Synchronet, DoorParty); otherwise the caller's handle |
| Server User | The caller's handle, sometimes prefixed with a system tag, e.g. `[V3]Neo` |
| Terminal Type | The terminal, e.g. `ANSI/38400` — **or the door to launch** |

### Launching a door directly

Most door servers read the door code from the terminal-type field, so a menu entry can drop the user straight into one game. Two conventions are in use:

| Server | Terminal Type |
| --- | --- |
| Synchronet door servers | `xtrn=LORD` |
| Others (including DoorParty) | `LORD` |

Leave **Terminal Type** blank to land on the door server's own menu instead.

### Placeholders

**Client User**, **Server User**, and **Terminal Type** accept the [standard door placeholders](doors/doors.md#placeholders) — `{USERHANDLE}`, `{REALNAME}`, `{NODE}`, `{USERID}`, `{LEVEL}` and the rest. This is how per-user values reach the door server:

```json
{
  "code": "DPLORD",
  "name": "LORD (Door Server)",
  "type": "rlogin",
  "host": "doors.example.com",
  "client_username": "{USERHANDLE}",
  "server_username": "[V3]{USERHANDLE}",
  "terminal_type": "xtrn=LORD"
}
```

## Worked examples

### A Synchronet door server

Synchronet's RLogin server expects the client-user-name to be a valid password and the server-user-name to be a valid user ID or alias on that system, and reads `xtrn=CODE` from the terminal type to launch a door directly.

```json
{
  "code": "SYNCLORD",
  "name": "LORD (Door Server)",
  "type": "rlogin",
  "host": "doors.example.com",
  "port": 3513,
  "client_username": "the-shared-password",
  "server_username": "{USERHANDLE}",
  "terminal_type": "xtrn=LORD"
}
```

Leave `terminal_type` out to land on the door server's own menu instead of one game.

### DoorParty, via the connector

DoorParty itself needs an SSH tunnel, which ViSiON/3 does not yet speak ([#382](https://github.com/ViSiON-3/vision-3-bbs/issues/382)). Until it does, run [DoorParty Connector](https://github.com/echicken/dpc2) alongside the BBS: it accepts a plain RLogin connection locally and carries it to DoorParty over the tunnel, so this door type reaches DoorParty today.

Point the door at the connector, not at DoorParty:

```json
{
  "code": "DOORPRTY",
  "name": "DoorParty",
  "type": "rlogin",
  "host": "127.0.0.1",
  "port": 9999,
  "client_username": "the-password-from-your-connector-config",
  "server_username": "{USERHANDLE}"
}
```

Three things to know:

- **Port 9999** is the connector's default `local_port`. Change it here if you changed it there.
- **Client User must match** the password in the connector's own configuration — that is what it forwards to DoorParty.
- **Do not add your system tag.** The connector prefixes `server_username` with the `system_tag` from its config, and only leaves it alone if it is already bracketed. Sending the bare `{USERHANDLE}` is correct; sending `[TAG]{USERHANDLE}` works too but is redundant.

Add `"terminal_type": "lord"` to go straight into a game — DoorParty takes the door code bare, without the `xtrn=` prefix. Its [door code list](https://wiki.throwbackbbs.com/doku.php?id=doorcode) has the valid values.

## Telnet

Set **Type** to `Telnet` for a server that speaks Telnet rather than RLogin. The connection is negotiated the way any Telnet client would: the server is told the caller's terminal type and screen size when it asks, and its echo is accepted. What the caller sees is then the server's own session, from its opening screen onward.

| Field | JSON key | Meaning |
| --- | --- | --- |
| Host | `host` | Door server hostname or IP address. Required. |
| Port | `port` | TCP port. Blank or `0` uses 23. |
| Terminal Type | `terminal_type` | What to report when the server asks for the terminal type. Blank sends `ANSI`. |
| Send On Connect | `send_on_connect` | Typed into the session as soon as it connects — see [Logging in automatically](#logging-in-automatically). |
| Raw TCP | `raw_tcp` | `Yes` turns the Telnet protocol off — see [Raw TCP](#raw-tcp). |
| Connect Timeout | `connect_timeout` | Seconds to wait for the server. Blank or `0` uses 10. |
| Disconnect Key | `disconnect_key` | Key that hangs up the session. Blank uses `^]`. |

The smallest entry:

```json
{
  "code": "OTHERBBS",
  "name": "The Other BBS",
  "type": "telnet",
  "host": "bbs.example.com",
  "port": 2323
}
```

The caller's stored screen size is what the server is told, so a door server that draws to the window size gets the right one. Terminal Type accepts the same [placeholders](#placeholders) as the RLogin fields.

### Logging in automatically

Telnet carries no identity, so a server that wants a login shows its prompt to every caller. **Send On Connect** (`send_on_connect`) is typed into the session for them, before anything they press themselves. Placeholders are substituted, so a login can carry the caller's handle:

```json
{
  "code": "OTHERBBS",
  "name": "The Other BBS",
  "type": "telnet",
  "host": "bbs.example.com",
  "send_on_connect": "{USERHANDLE}\rthe-shared-password\r"
}
```

In `doors.json` the Enter key is JSON's `\r`. In the config editor type the same thing — `\r` for Enter, `\n` for a line feed, `\\` for a backslash — and the editor stores the real characters.

Three things to know:

- **It is sent blind, the moment the connection is up**, the same way Synchronet's Telnet gateway sends its strings. A server that clears its input while it detects the terminal, or that takes a while to show its prompt, may never see it. Try it against your server before relying on it; if it does not take, leave the field blank and let the caller log in by hand.
- **It is one login for everyone.** A shared password goes in the field as written; there is no placeholder for a caller's own BBS password, for the reason given under [Security](#security).
- **A bare `\r` is a real value**: it is what gets a server past a "press Enter to continue" screen.

### Raw TCP

Some servers listen on a port a sysop would expect Telnet on but speak none of it: no negotiation, and every byte is data. Set **Raw TCP** (`raw_tcp`) to `Yes` for those. Nothing is negotiated and nothing is rewritten in either direction; **Send On Connect** is still sent.

Leave it at `No` for anything that is actually a Telnet server. With the protocol off, its negotiation arrives on the caller's screen as garbage, and it never learns the terminal type or screen size.

### What Telnet does not do

- **No door selection.** There is no field a door code can travel in, as there is in RLogin. Where the user lands is up to the server.
- **No identity.** The server does not learn who is calling unless Send On Connect tells it.
- **Line endings.** Outside binary mode the Enter key is sent as the protocol requires (CR NUL), and a NUL the server sends after a CR is dropped. Servers that negotiate binary mode get the bytes as they are. A server that misreads either is one for **Raw TCP**.

## Hanging up

Pressing **Ctrl-]** disconnects from the door server and returns the user to the BBS. This is deliberately the same key Synchronet uses, so users of other boards already know it.

If a door legitimately needs Ctrl-] for itself, change **Disconnect Key** to another control key (`^X` notation, e.g. `^B`), or set it to `none` to pass every key through.

> Setting **Disconnect Key** to `none` means a user whose door server stops responding has no way back to the BBS except dropping carrier. Only disable it if the door genuinely needs the key.

## Time limits

A remote door obeys the caller's time limit like a local one. If the limit is reached during the session the connection is closed and the user is returned to the BBS. A user with no time left never reaches the door server at all.

Users with no time limit set (`0`), and CoSysOps and above whatever their limit, stay connected for as long as the door server keeps the session open.

## Security

**RLogin and Telnet both send everything in the clear, including whatever is in the handshake fields or Send On Connect.** Anyone who can watch the network between your BBS and the door server sees it.

- Only point doors at servers you trust. The fields are sent as configured, so a server can be told whatever it asks for — including a password.
- Prefer a door server on your own network, or reachable over a VPN or tunnel, over one across the public internet.
- `configs/doors.json` holds these values in plain text. Keep its permissions tight if a door server requires a password.
- The RLogin handshake fields are kept out of the default log for the same reason, and appear only at debug level. Send On Connect is never logged at all.

## Troubleshooting

**"Unable to connect"** — the server is unreachable, the port is wrong, or a firewall is in the way. Port 513 is privileged on Unix: a door server running as a normal user is often on a different port, so check what yours uses. Confirm with `telnet <host> <port>` from the BBS machine.

**Connects, then drops, or asks for a login** — almost always a missing or wrong password. On Synchronet door servers and DoorParty the password goes in **Client User**, not Server User; see [Passwords](#passwords). If you left Client User blank, the caller's handle was sent as the password and the server rejected it.

**Connects, but the door server does not recognise the user** — the identity is not in the shape that server wants. Check whether it expects a system tag (`[V3]Neo`) or a bare handle, and whether your setup adds the tag for you — the DoorParty connector does. Some servers also expect the two name fields in the opposite order.

**Lands on a menu instead of the game** — set **Terminal Type** to the door code, with or without the `xtrn=` prefix depending on the server.

**Garbled output** — the door server is sending a character set the terminal is not expecting. RLogin has no binary-mode negotiation, so file transfers through a door server are unreliable; this affects doors that try to send files.

**Telnet: the screen fills with stray characters on connect** — the server is not speaking Telnet and its bytes are being read as negotiation, or the reverse. Toggle **Raw TCP**.

**Telnet: the login is not typed, or only part of it is** — the server discarded what arrived before it was ready; see [Logging in automatically](#logging-in-automatically). Blank the field and let the caller log in by hand.

**Telnet: typed characters do not appear** — the server did not offer to echo. ViSiON/3 does not echo locally; a server that expects the client to is one this door type does not suit.

The BBS log records the address of every remote door connection. The handshake fields themselves are logged at debug level only, since they can carry a shared password on servers that authenticate that way; raise the log level to see them when diagnosing which field a server is unhappy with.

## Limitations

- **Connection establishment only.** The rest of the RLogin protocol — cooked mode, out-of-band window-size changes — is not implemented, matching what Synchronet and ENiGMA½ do. Door servers do not rely on it.
- **The window size is not sent over RLogin.** The door server uses its own default, generally 80x24. Telnet does report it.
- **No SSH.** Outbound SSH to a door server is not implemented yet ([#381](https://github.com/ViSiON-3/vision-3-bbs/issues/381)).
- **Telnet has no login script.** Send On Connect is sent once, blind; there is no waiting for a prompt.
- **No built-in provider support.** Public services that wrap RLogin in something else are not spoken directly: DoorParty needs an SSH tunnel, BBSLink an HTTP pre-authentication step, Exodus an HTTPS ticket and a key-based SSH login. DoorParty is reachable today through [its connector](#doorparty-via-the-connector), which does the tunnelling for you. Native support for all three is [#382](https://github.com/ViSiON-3/vision-3-bbs/issues/382).
- **No per-user passwords.** The password in the handshake is one value for the whole system. Synchronet can send a caller's own password, or a hash of it, to a door server; ViSiON/3 does not.
