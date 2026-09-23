# Set Up a Door Server Connection

A door server is a separate machine that hosts doors for one or more BBSes. Rather than installing a game on every board you run, you install it once on the server and point each BBS at it over RLogin. The caller's terminal is handed across to the server and comes back to the menu when the door ends.

This walkthrough connects to a door server at `doors.example.com`. Replace the host with yours. If your doors run on the same machine as the BBS, you do not want this — use a [native](how-to-guides/door-native.md) or [DOS](how-to-guides/door-dos.md) door instead.

## 1. Get the connection details

Ask whoever runs the door server, or read its documentation, for four things:

- **Host and port.** Assume it is *not* 513 — that port is privileged on Unix, so a server run by an ordinary user is almost always elsewhere. 3513 and 9999 are both common.
- **The password.** Synchronet door servers and DoorParty both want one, and RLogin has nowhere to put a password, so both read it from the *client-user-name* field. It is normally one shared secret for your whole system, not a per-user password.
- **What goes in the name fields.** With a password in the client field, the caller's handle goes in the server field, sometimes prefixed with a system tag such as `[V3]`. Servers disagree, so check.
- **How a door is chosen.** Most servers read the door code from the terminal-type field, either as `xtrn=LORD` (Synchronet-style) or bare as `LORD`.
- **Whether you need to register.** Public door servers issue credentials; your own does not.

If the server was set up for Synchronet or Mystic, its documentation is written in those terms. The three values it names map straight onto the three fields below.

## 2. Check you can reach it

From the BBS machine, before touching any config:

```bash
telnet doors.example.com 513
```

A connection that opens and sits there is what you want — RLogin says nothing until the BBS sends its handshake. "Connection refused" means the port is wrong or a firewall is in the way; fix that first, because the BBS has no more access than your shell does.

## 3. Define the door

Run `./config`, press **6** for Door Programs, then **I** to insert a record and fill in:

| Field | Value |
| --- | --- |
| Code | `DOORSRV` |
| Name | `Door Server` |
| Type | RLogin |
| Host | `doors.example.com` |
| Port | the port the server uses — `0` means 513, which it probably is not |
| Client User | **the password**, if the server wants one |
| Server User | `{USERHANDLE}`, or `[V3]{USERHANDLE}` if the server wants a tag |
| Terminal Type | blank for the server's menu, or `xtrn=LORD` for one game |
| Single Instance | `N` — the server handles its own concurrency |

**Client User is the password field.** RLogin has nowhere else to put one, so Synchronet door servers and DoorParty both read it from there. Leave it blank only for a server that asks for no authentication; blank sends the caller's handle, which such a server would reject as a bad password.

The placeholders from [Door Programs](doors/doors.md#placeholders) work in all three fields, so `[V3]{USERHANDLE}` sends the handle with your system tag in front.

Press **Esc**, then **Q** and **Y** to save. If Host is blank, or the port or disconnect key is out of range, the editor refuses the save and says which door is at fault rather than writing a record that cannot run.

The same record in `configs/doors.json`:

```json
{
  "code": "DOORSRV",
  "name": "Door Server",
  "type": "rlogin",
  "host": "doors.example.com",
  "port": 3513,
  "client_username": "the-shared-password",
  "server_username": "{USERHANDLE}"
}
```

## 4. Add it to a menu

Run `./menuedit`, open `DOORSM`, press **F5**, and set **Keys** to `S`, **Command** to `DOOR:DOORSRV`, **ACS** to `*`. Press **Esc** to save; the editor writes to the `menus.d/v3/` overlay. To show the key, copy the shipped art into the overlay and add it there with an ANSI editor:

```bash
mkdir -p menus.d/v3/ansi
cp menus/v3/ansi/DOORSM.ANS menus.d/v3/ansi/
```

## 5. Try it

Connect, open the doors menu and press **S**. You should see a "Connecting" line and then the door server. **Ctrl-]** hangs up and returns you to the BBS.

If it does not work, `data/logs/vision3.log` records the address of every attempt. The handshake fields are logged at debug level only, because they can carry a password; raise the log level when you need to see them.

**"Unable to connect"** — wrong host or port, or a firewall. Step 2 catches this.

**It connects, then drops or asks you to log in** — the password is missing or wrong. It belongs in **Client User**. If you left that blank, the caller's handle went across as the password.

**It connects but the server does not know the caller** — the name fields are not the shape that server expects. Add or remove the system tag, or try the two swapped.

**It lands on a menu instead of the game** — put the door code in **Terminal Type**, with or without the `xtrn=` prefix depending on the server.

## Variations

**One menu key per game.** Add a record per door, all with the same host, differing only in **Terminal Type** and **Code**. Callers then go straight into a game rather than through the server's menu.

**DoorParty.** Run [DoorParty Connector](https://github.com/echicken/dpc2) alongside the BBS and point this door at it — `127.0.0.1`, port `9999` by default — with **Client User** set to the password from the connector's own config. The connector adds your system tag and carries the session to DoorParty over its SSH tunnel. Full example in [Door Servers](doors/door-servers.md#doorparty-via-the-connector).

**A door that needs Ctrl-] itself.** Change **Disconnect Key** to another control key in `^X` notation, or to `none` to pass every key through. With `none` a caller has no way out of a wedged session except dropping carrier, so only do it if the door genuinely needs the key.

**Time limits.** Remote doors obey the caller's time limit like local ones: the connection is closed when it runs out, and a caller with no time left never reaches the server.

## See also

- [Door Servers](doors/door-servers.md) for the handshake fields, security notes and full troubleshooting
- [Door Programs](doors/doors.md) for settings shared by every door type
