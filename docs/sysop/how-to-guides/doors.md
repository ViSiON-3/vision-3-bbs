# Setting Up Doors

Doors are programs that run outside the BBS but inside a caller's session: games, utilities, anything that talks to a terminal. ViSiON/3 starts the program, hands it the caller's terminal, tells it who is calling, and takes the session back when the program exits.

This guide gets a door from "I have the files" to "a caller can pick it from a menu." The field-by-field details live in the [Door Programs](doors/doors.md) reference; you should not need them for a first door.

## Which kind of door do you have?

ViSiON/3 runs five kinds of door. Work out which one you have first, then follow that walkthrough.

| You have | Door type | Walkthrough |
| --- | --- | --- |
| A 16-bit DOS program (`.EXE` or `.BAT`, usually a game from the 1990s such as LORD, TradeWars or Usurper) | DOS door, run under dosemu2 | [Set up a DOS door](how-to-guides/door-dos.md) |
| A program built for the machine the BBS runs on (a Linux or macOS binary, or a Windows program on a Windows BBS) | Native door | [Set up a native door](how-to-guides/door-native.md) |
| A Synchronet JavaScript game (the `xtrn/` folder from Synchronet, such as the LORD and LORD II ports) | Synchronet JS door | [Set up a Synchronet JS door](how-to-guides/door-synchronet-js.md) |
| A script you wrote, or one of the examples that ship with the BBS | VPL script | [Set up a VPL script door](how-to-guides/door-vpl.md) |
| No files at all — the doors live on another machine you connect to | Door server link | [Set up a door server connection](how-to-guides/door-rlogin.md) |

Not sure? A 16-bit `.EXE` from the DOS era, usually shipped with a `.DOC` that talks about FOSSIL drivers and COM ports, is a DOS door. A Windows `.EXE` is a native door and needs a Windows BBS; it will not run under dosemu2. A folder of `.js` files that came from Synchronet is a Synchronet JS door. A `.js` file written for ViSiON/3, or copied from `scripts/examples/`, is a VPL script. Anything that runs from your shell is native. If you have no door files at all, only an address someone gave you, that is a door server.

Every ViSiON/3 install already has seven VPL script doors wired into the doors menu, so if you want to see a door run before setting one up, connect and press **H** on the doors menu for the "Hello World" example.

## Before you start

Three things are the same for every door type.

These three apply to doors that run on this machine. A door server connection has no local files, no dropfile and no working directory — only an address — so it skips straight to defining the record.

**Where the files go.** Keep doors under the `doors/` directory in the BBS root. DOS doors go on the virtual C: drive at `doors/drive_c/`. Synchronet JS games go under `doors/sbbs/xtrn/`. Native doors and VPL scripts can go anywhere, but `doors/<name>/` and `scripts/` keep them together and inside your backups.

**How a door is defined.** Each door is one record in the config editor. Run `./config`, press **6** for Door Programs, and press **I** to insert a record. Every record has a **Code**, which is the name you use in menus, a **Name** that callers see, and a **Type**. The walkthroughs list the other fields each type needs.

**How a caller reaches it.** A menu command `DOOR:CODE` launches the door. Run `./menuedit`, open the `DOORSM` menu, press **F5** to add a command, set **Keys** to a letter no other command on that menu uses and **Command** to `DOOR:CODE`. The editor saves into `menus.d/v3/`, an overlay that upgrades leave alone. The stock doors screen is ANSI art, so a new key works immediately but is not drawn until you copy the shipped art into the overlay and add the key there with an ANSI editor:

```bash
mkdir -p menus.d/v3/ansi
cp menus/v3/ansi/DOORSM.ANS menus.d/v3/ansi/
```

See [Menus & ACS](menus/menu-system.md) for the menu editor and the overlay.

## Troubleshooting

**The door starts but does not know who is calling, or asks for a name.** It cannot find its dropfile. Check that **Dropfile Type** matches the format the door's documentation names, and that the door is told where the file is. Native doors get the path through the `{DROPFILE}` or `{NODEDIR}` placeholder on the command line. DOS doors get it through `{DOSDROPFILE}` or `{DOSNODEDIR}`. See [Supported Dropfile Types](doors/doors.md#supported-dropfile-types).

**"Door not configured" when a caller presses the key.** The menu command's code does not match a record. Codes are upper-case; `DOOR:lord` and `DOOR:LORD` both work, but the record must exist.

**The door cannot find its own files.** Set **Working Dir** to the door's directory. Native doors run from that directory; DOS doors `cd` into it before running the commands.

**Output looks wrong: no colour, broken boxes.** DOS doors need `cp437` character sets in the dosemu2 config, which the shipped template sets. Native doors that draw with ANSI need **Raw Terminal** set to Yes so they get a real terminal.

**A DOS door shows DOS boot text, or hangs waiting.** See the DOS walkthrough's [troubleshooting](how-to-guides/door-dos.md#if-it-does-not-work) section. Usually the FOSSIL driver is missing or the batch file never clears the screen.

**Two callers cannot play at once, or save files get corrupted.** Set **Single Instance** to Yes for the door. The second caller sees a "door is in use" message instead.

**The door works for you but not for callers.** Check the menu command's **ACS** and the door's **Min Access Level**. Doors above a caller's level are hidden from door lists and refuse to start.

## See also

- [Door Programs](doors/doors.md) for every field, placeholder and dropfile format
- [Synchronet JS Doors](doors/synchronet-js-doors.md) for the JavaScript runtime, its API surface and module resolution
- [Door Servers](doors/door-servers.md) for connecting out to a shared door server over RLogin
- [VPL Scripting](scripting/vpl-scripting.md) for the script API
- [Menu Commands Reference](reference/menu-commands.md) for `DOOR:`, `LISTDOORS`, `OPENDOOR` and `DOORINFO`
