# Set Up a Door Server Connection

A door server is a separate machine that hosts doors for one or more BBSes. Rather than installing a game on every board you run, you install it once on the server and point each BBS at it over RLogin. The caller's terminal is handed across to the server and comes back to the menu when the door ends.

This walkthrough connects to a door server at `doors.example.com`. Replace the host with yours. If your doors run on the same machine as the BBS, you do not want this — use a [native](how-to-guides/door-native.md) or [DOS](how-to-guides/door-dos.md) door instead.

## 1. Get the connection details

Ask whoever runs the door server, or read its documentation, for four things:

- **Host and port.** Port 513 is the RLogin default. It is privileged on Unix, so a server running as a normal user is often on a different port.
- **What goes in the two name fields.** RLogin sends a client name and a server name. Servers disagree about which carries the caller's handle, whether it wants a system tag such as `[V3]`, and whether one of them is really a password.
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
| Port | `0` for 513, or the port the server uses |
| Client User | blank, or what the server asks for |
| Server User | blank, or e.g. `[V3]{USERHANDLE}` |
| Terminal Type | blank for the server's menu, or `xtrn=LORD` for one game |
| Single Instance | `N` — the server handles its own concurrency |

Blank name fields send the caller's handle, which is what a private door server usually wants. The placeholders from [Door Programs](doors/doors.md#placeholders) work in all three fields, so `[V3]{USERHANDLE}` sends the handle with your system tag in front.

Press **Esc**, then **Q** and **Y** to save. The same record in `configs/doors.json`:

```json
{
  "code": "DOORSRV",
  "name": "Door Server",
  "type": "rlogin",
  "host": "doors.example.com"
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

**It connects but the server does not know the caller** — the name fields are not what that server expects. Swap **Client User** and **Server User**, or add the system tag it asks for.

**It lands on a menu instead of the game** — put the door code in **Terminal Type**, with or without the `xtrn=` prefix depending on the server.

## Variations

**One menu key per game.** Add a record per door, all with the same host, differing only in **Terminal Type** and **Code**. Callers then go straight into a game rather than through the server's menu.

**A door that needs Ctrl-] itself.** Change **Disconnect Key** to another control key in `^X` notation, or to `none` to pass every key through. With `none` a caller has no way out of a wedged session except dropping carrier, so only do it if the door genuinely needs the key.

**Time limits.** Remote doors obey the caller's time limit like local ones: the connection is closed when it runs out, and a caller with no time left never reaches the server.

## See also

- [Door Servers](doors/door-servers.md) for the handshake fields, security notes and full troubleshooting
- [Door Programs](doors/doors.md) for settings shared by every door type
