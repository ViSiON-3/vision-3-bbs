# Set Up a DOS Door

Most classic door games are 16-bit DOS programs. ViSiON/3 runs them on Linux through dosemu2, feeding the caller's session to the game as if it were a modem on COM1. This walkthrough installs Legend of the Red Dragon (the original DOS version, not the JavaScript port that ships with the BBS). Any DOS door follows the same steps with different file names.

> **Linux x86 only.** dosemu2 runs on 32-bit and 64-bit x86 Linux. ARM boards, macOS and Windows cannot run DOS doors; on those, use the [Synchronet JS port of LORD](how-to-guides/door-synchronet-js.md) instead.

> **Hands-on steps.** Steps 3 and 4 depend on the door's own installer and switches. They are written from LORD's documentation and the BBS's own DOS launcher; check them against your copy of the game.

## 1. Install dosemu2

Install dosemu2 from your distribution or from the [dosemu2 project](https://github.com/dosemu2/dosemu2). The BBS calls the binary directly at `/usr/libexec/dosemu2/dosemu2.bin`; if your package puts it elsewhere, set the path under **System Setup → DOS Emulation** in `./config`.

Then run `./setup.sh` again from the BBS root. When it finds dosemu2 it writes a `~/.dosemu/.dosemurc` with the character set and COM1 settings the BBS needs, and creates `doors/drive_c/`, which is the virtual C: drive every DOS door lives on.

## 2. Get the door and a FOSSIL driver

You need two downloads. Neither ships with the BBS.

- **The game.** LORD is freeware and available from BBS archives such as [bbsdocumentary.com](http://software.bbsdocumentary.com/) or the [Internet Archive](https://archive.org/). Take the registered 4.x release.
- **A FOSSIL driver.** DOS doors talk to the caller through a serial-port standard called FOSSIL. `X00.EXE` is the usual choice and is also on the archives.

Unpack them onto the C: drive:

```
doors/drive_c/
├── DOORS/LORD/     everything from the LORD archive
├── UTILS/X00.EXE   the FOSSIL driver
└── NODES/          created by the BBS; one TEMPn directory per node
```

Keep DOS names short: eight characters, no spaces, upper-case is conventional.

## 3. Configure the game from a DOS prompt

The BBS ships a helper that opens a plain DOS prompt on the C: drive, without the serial-port wiring, so you can run installers and configuration programs the way you would have on a real DOS machine:

```bash
./scripts/dos-local.sh
```

At the `C:\>` prompt, change into the game's directory and run its configuration program (LORD ships one; `LORD.DOC` names it):

```
CD \DOORS\LORD
```

In the game's configuration, set the dropfile type to `DOOR.SYS` and, for each node you will run, the dropfile directory to the per-node path the BBS uses: `C:\NODES\TEMP1` for node 1, `C:\NODES\TEMP2` for node 2, and so on. Save, then type `EXITEMU` to leave dosemu2.

LORD ships its own `START.BAT`, which takes the node number as its first argument and already carries the switches the game needs. Leave it in place; the BBS passes the node number to it in the next step. If your copy has no `START.BAT`, `LORD.DOC` shows the launch line to put in one.

## 4. Define the door

Run `./config`, press **7** for Door Programs, then **I** to insert a record and fill in:

| Field | Value |
| --- | --- |
| Code | `LORD` |
| Name | `Legend of the Red Dragon` |
| Type | DOS |
| Working Dir | `C:\DOORS\LORD` |
| Commands | `START.BAT {NODE}` |
| Dropfile Type | `DOOR.SYS` |
| Drive C Path | `doors/drive_c` |
| FOSSIL Driver | `C:\UTILS\X00.EXE eliminate` |
| Single Instance | `Y` |

For a DOS door, **Commands** is a comma-separated list of batch lines, and **Working Dir** is a DOS path the BBS changes into before running them. `{NODE}` becomes the node number. A door that takes its dropfile directory on the command line can be given `{DOSNODEDIR}`, which becomes that node's directory such as `C:\NODES\TEMP1`; LORD reads it from its own configuration instead. The BBS writes all four dropfile formats into the node directory before every launch, so the game finds `DOOR.SYS` whichever way it looks.

The **FOSSIL Driver** line is run first. `eliminate` tells X00 to unload any earlier copy before loading, which keeps repeated launches clean.

Press **Esc**, then **Q** and **Y** to save. The same record in `configs/doors.json`:

```json
{
  "code": "LORD",
  "name": "Legend of the Red Dragon",
  "is_dos": true,
  "working_directory": "C:\\DOORS\\LORD",
  "commands": ["START.BAT {NODE}"],
  "dropfile_type": "DOOR.SYS",
  "drive_c_path": "doors/drive_c",
  "fossil_driver": "C:\\UTILS\\X00.EXE eliminate",
  "single_instance": true
}
```

## 5. Add it to a menu

Run `./menuedit`, open `DOORSM`, press **F5**, and set **Keys** to `L`, **Command** to `DOOR:LORD`, **ACS** to `*`. Press **Esc** to save; the editor writes to the `menus.d/v3/` overlay. To show the key, copy the shipped art into the overlay and add the key there with an ANSI editor:

```bash
mkdir -p menus.d/v3/ansi
cp menus/v3/ansi/DOORSM.ANS menus.d/v3/ansi/
```

## 6. Try it

Connect over SSH, open the doors menu and press **L**. If you also set up the JavaScript port of LORD, give one of them a different key. You should see a brief pause, then LORD's title screen with no DOS boot text before it. Create a character, quit, and call again to be sure the character was saved.

## If it does not work

**Blank screen, then back to the menu.** dosemu2 did not start. Check the path under **System Setup → DOS Emulation** and look for `dosemu_boot.log` in `doors/drive_c/nodes/temp1/`.

**The game starts and immediately hangs, or ignores keys.** It is waiting on COM1 and no FOSSIL driver answered. Make sure `X00.EXE` is at the path in **FOSSIL Driver** and that `~/.dosemu/.dosemurc` contains `$_com1 = "virtual"`. Running `./setup.sh` again restores the template.

**"Bad command or file name".** A DOS path is wrong. Remember the C: drive is `doors/drive_c/` and paths inside it are DOS paths with backslashes.

**The game asks who you are.** It did not find `DOOR.SYS`. Check that the dropfile directory configured in the game for that node is `C:\NODES\TEMPn`, with `n` the node number.

**Box-drawing characters are wrong.** `~/.dosemu/.dosemurc` must set both character sets to `cp437`. The template does; a hand-edited file may not.

## See also

- [Running DOS Doors](doors/doors.md#running-dos-doors) for how the launcher works, the generated dosemu2 config, and platform support
- [Door Programs](doors/doors.md) for every DOS field
