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

On Linux or macOS make the program executable (`chmod +x doors/mydoor/mydoor`). On Windows use the `.exe` as shipped. Then run it once by hand from that directory to be sure it starts and that any first-run setup is done.

## 2. Find out what the door expects

Read the door's documentation for three facts:

- **Which dropfile it reads.** `DOOR.SYS` is the most common, then `DOOR32.SYS`. Some doors want the path to the file, some want the directory it is in.
- **How it is told the node number and dropfile.** Usually command-line switches such as `-n 1 -d /path/DOOR.SYS`, sometimes environment variables.
- **Whether it draws full-screen.** A door that uses cursor positioning and colour needs a real terminal, which the BBS provides when **Raw Terminal** is on.

## 3. Define the door

Run `./config`, press **7** for Door Programs, then **I** to insert a record and fill in:

| Field | Value |
| --- | --- |
| Code | `MYDOOR` |
| Name | `My Door` |
| Type | Native |
| Working Dir | `doors/mydoor` |
| Commands | `./mydoor -n {NODE} -d {DROPFILE}` |
| Dropfile Type | `DOOR.SYS` |
| Dropfile Location | `node` |
| Raw Terminal | `Y` |
| Single Instance | `Y` if the door keeps shared save files |

**Commands** is the program, then its arguments, written as you would at a shell prompt: separated by spaces, with each word becoming one argument. A switch and its value are two words (`-n {NODE}`) unless the door wants them joined (`-n{NODE}`). An argument that must contain a space needs commas instead: once there is a comma after the program, the arguments are split on commas and may contain spaces (`./mydoor -title, My Door`). The placeholders are replaced when the door starts: `{NODE}` with the node number and `{DROPFILE}` with the full path of the dropfile the BBS just wrote. A door that wants the directory instead takes `{NODEDIR}`. The full list is in [Door Command Line](doors/doors.md#door-command-line).

**Dropfile Location** `node` writes the dropfile to a private temporary directory for the calling node, so two nodes running the door at once do not overwrite each other's file. Use `startup` only for a door that insists on finding the dropfile in its own directory.

Press **Esc**, then **Q** and **Y** to save. The record the editor writes is shown under [Manual Configuration](#manual-configuration).

## 4. Add it to a menu

Run `./menuedit`, open `DOORSM`, press **F5**, and set **Keys** to `M`, **Command** to `DOOR:MYDOOR`, **ACS** to `*`. Press **Esc** to save; the editor writes to the `menus.d/v3/` overlay. To show the key, copy the shipped art into the overlay and add the key there with an ANSI editor:

```bash
mkdir -p menus.d/v3/ansi
cp menus/v3/ansi/DOORSM.ANS menus.d/v3/ansi/
```

## 5. Try it

Connect, open the doors menu and press **M**. If the door starts but does not know who you are, it did not find the dropfile: check the switch names against the door's documentation and whether it wants the file or its directory. If it starts but draws garbage, turn **Raw Terminal** on. If it goes straight back to the menu with no message, it started and then exited with an error status; the BBS treats that as a normal exit and logs it as a warning rather than showing the caller an error. `data/logs/vision3.log` records each launch, the dropfile it wrote, the exit status and any error starting the program.

## Variations

**Shell scripts.** A `.sh` script, or a program that must be started through the shell, needs **Use Shell** set to Yes. The BBS then runs it through `/bin/sh` (`cmd` on Windows) with the arguments passed through unchanged. The field does not interpret pipes, redirects or globs; put those inside a wrapper script and launch the script.

**Environment variables instead of switches.** Some doors read their settings from the environment. Every native door gets `BBS_NODE`, `BBS_USERHANDLE`, `BBS_USERID`, `BBS_TIMELEFT` and `BBS_USERIP`; on Linux and macOS it also gets `LINES` and `COLUMNS`. Add more in **Env Vars** as `KEY=VALUE, KEY2=VALUE2`; placeholders work there too.

**Doors that expect a socket.** A few doors written for Synchronet or Mystic want a socket handle rather than a terminal. On Linux and macOS, set **I/O Mode** to `SOCKET`; the BBS passes the socket as file descriptor 3 and sets `DOOR_SOCKET_FD=3`. Socket mode is not available on Windows.

**Cleanup after exit.** Set **Cleanup Command** to a program and its arguments, separated like **Commands**, to run after the door exits, for example to process a score file. Placeholders are replaced in the arguments; the program path is used as written.

**Time and idle limits.** The caller's time limit and the session idle timeout apply inside the door, and the BBS hangs the door up (`SIGHUP`, then `SIGKILL` five seconds later) when either runs out or the caller drops. CoSysOps and above are exempt from both. A door that reads `DROPFILE.INI` is told the idle limit as `IDLE_LIMIT`, so it can warn the caller first.

## See also

- [Door Programs](doors/doors.md) for every field and placeholder
- [Supported Dropfile Types](doors/doors.md#supported-dropfile-types) for what each format carries

## Manual Configuration

The record from step 3, as the config editor writes it to `configs/doors.json`:

```json
{
  "code": "MYDOOR",
  "name": "My Door",
  "working_directory": "doors/mydoor",
  "commands": ["./mydoor", "-n", "{NODE}", "-d", "{DROPFILE}"],
  "dropfile_type": "DOOR.SYS",
  "dropfile_location": "node",
  "requires_raw_terminal": true,
  "single_instance": true
}
```

A native door has no `type`. `commands` is an array with the executable first and one argument per entry, however you typed it in the editor. The variations above map onto `use_shell`, `environment_variables`, `io_mode` and `cleanup_command` / `cleanup_args`. Every key is in the [Common Fields](doors/doors.md#common-fields) and [Native Door Fields](doors/doors.md#native-door-fields) tables, the placeholders under [Placeholders](doors/doors.md#placeholders) and the variables under [Environment Variables](doors/doors.md#environment-variables).

