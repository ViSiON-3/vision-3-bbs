# Set Up a Native Door

A native door is any program built for the operating system the BBS runs on: a Linux binary on a Linux BBS, a Windows program on a Windows BBS, or a script the system can run. Modern door games and utilities written in Go, C, Python or similar all fall here.

The walkthrough uses a door installed at `doors/mydoor/` with a binary called `mydoor`. Replace the names, and take the command-line switches from your door's own documentation. Most native doors want two things from the BBS: which node is calling, and where the dropfile is.

## 1. Install the door

Unpack it under the BBS root:

```
doors/mydoor/
├── mydoor          the program
└── ...             its data files
```

Make the program executable (`chmod +x doors/mydoor/mydoor`) and run it once by hand from that directory to be sure it starts and that any first-run setup is done.

## 2. Find out what the door expects

Read the door's documentation for three facts:

- **Which dropfile it reads.** `DOOR.SYS` is the most common, then `DOOR32.SYS`. Some doors want the path to the file, some want the directory it is in.
- **How it is told the node number and dropfile.** Usually command-line switches such as `-n 1 -d /path/DOOR.SYS`, sometimes environment variables.
- **Whether it draws full-screen.** A door that uses cursor positioning and colour needs a real terminal, which the BBS provides when **Raw Terminal** is on.

## 3. Define the door

Run `./config`, press **6** for Door Programs, then **I** to insert a record and fill in:

| Field | Value |
| --- | --- |
| Code | `MYDOOR` |
| Name | `My Door` |
| Type | Native |
| Working Dir | `doors/mydoor` |
| Commands | `./mydoor -n {NODE}, -d {DROPFILE}` |
| Dropfile Type | `DOOR.SYS` |
| Dropfile Location | `node` |
| Raw Terminal | `Y` |
| Single Instance | `Y` if the door keeps shared save files |

**Commands** is the program followed by its arguments, separated by commas. Each comma-separated entry becomes one argument. The placeholders are replaced when the door starts: `{NODE}` with the node number and `{DROPFILE}` with the full path of the dropfile the BBS just wrote. A door that wants the directory instead takes `{NODEDIR}`. The full list is in [Door Command Line](doors/doors.md#door-command-line).

**Dropfile Location** `node` writes the dropfile to a private temporary directory for the calling node, so two nodes running the door at once do not overwrite each other's file. Use `startup` only for a door that insists on finding the dropfile in its own directory.

Press **Esc**, then **Q** and **Y** to save. The same record in `configs/doors.json`:

```json
{
  "code": "MYDOOR",
  "name": "My Door",
  "working_directory": "doors/mydoor",
  "commands": ["./mydoor", "-n {NODE}", "-d {DROPFILE}"],
  "dropfile_type": "DOOR.SYS",
  "dropfile_location": "node",
  "requires_raw_terminal": true,
  "single_instance": true
}
```

## 4. Add it to a menu

Run `./menuedit`, open `DOORSM`, press **F5**, and set **Keys** to `M`, **Command** to `DOOR:MYDOOR`, **ACS** to `*`. Press **Esc** to save, and add the key to `menus/v3/ansi/DOORSM.ANS` so callers can see it.

## 5. Try it

Connect, open the doors menu and press **M**. If the door starts but does not know who you are, it did not find the dropfile: check the switch names against the door's documentation and whether it wants the file or its directory. If it starts but draws garbage, turn **Raw Terminal** on. `data/logs/vision3.log` records the exact command line and dropfile path used for every launch.

## Variations

**Shell scripts and pipelines.** A `.sh` script, or a command line with pipes or redirects, needs **Use Shell** set to Yes. The command then runs through `/bin/sh -c`.

**Environment variables instead of switches.** Some doors read their settings from the environment. Every native door already gets `BBS_NODE`, `BBS_USERHANDLE`, `BBS_USERID`, `BBS_TIMELEFT`, `LINES` and `COLUMNS`. Add more in **Env Vars** as `KEY=VALUE, KEY2=VALUE2`; placeholders work there too.

**Doors that expect a socket.** A few doors written for Synchronet or Mystic want a socket handle rather than a terminal. Set **I/O Mode** to `SOCKET`; the BBS passes the socket as file descriptor 3 and sets `DOOR_SOCKET_FD=3`.

**Cleanup after exit.** Set **Cleanup Command** to a script that runs after the door exits, for example to process a score file. It takes the same placeholders.

## See also

- [Door Programs](doors/doors.md) for every field and placeholder
- [Supported Dropfile Types](doors/doors.md#supported-dropfile-types) for what each format carries
