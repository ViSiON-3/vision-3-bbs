# Door Servers (Outbound RLogin)

A **door server** is a separate machine that hosts door games for one or more BBSes. Instead of installing every door on every board you run, you install them once on the door server and point each BBS at it. ViSiON/3 connects out to the server over [RLogin](https://datatracker.ietf.org/doc/html/rfc1282), hands the user's terminal across, and returns them to the menu when the door ends.

This is configured as a door like any other, so it appears in the door list, obeys access levels and time limits, and is opened with the same `DOOR:CODE` menu command. See [Door Programs](doors/doors.md) for settings shared by every door type.

## When to use this

- You run more than one BBS and want them to share one set of doors.
- You want to offer doors from a public door server.
- You want to run doors on a different machine from the BBS.

If the doors run on the same machine as ViSiON/3, you do not need this — use a native, DOS, or script door instead.

## Quick start

In the [Configuration Editor](configuration/configuration.md#configuration-editor-tui) (`./config`, section 6 — Door Programs), add a door and set **Type** to `RLogin`. Fill in **Host**, and usually **Terminal Type**:

| Field | JSON key | Meaning |
| --- | --- | --- |
| Host | `host` | Door server hostname or IP address. Required. |
| Port | `port` | TCP port. Blank or `0` uses 513, the RLogin default. |
| Client User | `client_username` | First handshake field. Blank sends the user's handle. |
| Server User | `server_username` | Second handshake field. Blank sends the user's handle. |
| Terminal Type | `terminal_type` | Third handshake field. Blank sends `ANSI/38400`. |
| Connect Timeout | `connect_timeout` | Seconds to wait for the server. Blank or `0` uses 10. |
| Disconnect Key | `disconnect_key` | Key that hangs up the session. Blank uses `^]`. |

A minimal entry in `configs/doors.json`:

```json
{
  "code": "DOORSRV",
  "name": "Door Server",
  "type": "rlogin",
  "host": "doors.example.com"
}
```

## The three handshake fields

RLogin sends exactly three strings when it connects, and door servers overload all three. **What to put in them is decided by the door server, not by ViSiON/3** — the fields are sent exactly as configured, so follow whatever your server's documentation says.

The common conventions:

| Field | Typical contents |
| --- | --- |
| Client User | The user's handle, or a shared password on servers that authenticate this way |
| Server User | The user's handle, sometimes prefixed with a system tag, e.g. `[V3]Neo` |
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

## Hanging up

Pressing **Ctrl-]** disconnects from the door server and returns the user to the BBS. This is deliberately the same key Synchronet uses, so users of other boards already know it.

If a door legitimately needs Ctrl-] for itself, change **Disconnect Key** to another control key (`^X` notation, e.g. `^B`), or set it to `none` to pass every key through.

> Setting **Disconnect Key** to `none` means a user whose door server stops responding has no way back to the BBS except dropping carrier. Only disable it if the door genuinely needs the key.

## Time limits

A remote door obeys the caller's time limit like a local one. If the limit is reached during the session the connection is closed and the user is returned to the BBS. A user with no time left never reaches the door server at all.

Users with no time limit set (`0`) stay connected for as long as the door server keeps the session open.

## Security

**RLogin sends everything in the clear, including whatever is in the handshake fields.** Anyone who can watch the network between your BBS and the door server sees it.

- Only point doors at servers you trust. The fields are sent as configured, so a server can be told whatever it asks for — including a password.
- Prefer a door server on your own network, or reachable over a VPN or tunnel, over one across the public internet.
- `configs/doors.json` holds these values in plain text. Keep its permissions tight if a door server requires a password.

## Troubleshooting

**"Unable to connect"** — the server is unreachable, the port is wrong, or a firewall is in the way. Port 513 is privileged on Unix: a door server running as a normal user is often on a different port, so check what yours uses. Confirm with `telnet <host> <port>` from the BBS machine.

**Connects, but the door server does not recognise the user** — the handshake fields are not what that server expects. Check its documentation for which field carries the handle and whether it wants a system tag or password, then adjust **Client User** and **Server User**. Some servers expect the two in the opposite order from others.

**Lands on a menu instead of the game** — set **Terminal Type** to the door code, with or without the `xtrn=` prefix depending on the server.

**Garbled output** — the door server is sending a character set the terminal is not expecting. RLogin has no binary-mode negotiation, so file transfers through a door server are unreliable; this affects doors that try to send files.

The BBS log records the address, the server-user name, and the terminal type for every remote door connection, which is usually enough to see which field is wrong.

## Limitations

- **Connection establishment only.** The rest of the RLogin protocol — cooked mode, out-of-band window-size changes — is not implemented, matching what Synchronet and ENiGMA½ do. Door servers do not rely on it.
- **The window size is not sent.** The door server uses its own default, generally 80x24.
- **RLogin only.** Outbound Telnet and SSH, and the public door servers that need more than a plain connection (DoorParty, BBSLink, Exodus), are not supported yet.
