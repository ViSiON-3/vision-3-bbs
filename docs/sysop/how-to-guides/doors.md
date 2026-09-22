# Setting Up Doors

Doors are programs that run outside the BBS but inside a caller's session: games, utilities, anything that talks to a terminal. ViSiON/3 starts the program, hands it the caller's terminal, tells it who is calling, and takes the session back when the program exits.

This guide gets a door from "I have the files" to "a caller can pick it from a menu." The field-by-field details live in the [Door Programs](doors/doors.md) reference; you should not need them for a first door.

## Which kind of door do you have?

ViSiON/3 runs four kinds of door. Work out which one you have first, then follow that walkthrough.

| You have | Door type | Walkthrough |
| --- | --- | --- |
| A 16-bit DOS program (`.EXE` or `.BAT`, usually a game from the 1990s such as LORD, TradeWars or Usurper) | DOS door, run under dosemu2 | [Set up a DOS door](how-to-guides/door-dos.md) |
| A program built for the machine the BBS runs on (a Linux or macOS binary, or a Windows program on a Windows BBS) | Native door | [Set up a native door](how-to-guides/door-native.md) |
| A Synchronet JavaScript game (the `xtrn/` folder from Synchronet, such as the LORD and LORD II ports) | Synchronet JS door | [Set up a Synchronet JS door](how-to-guides/door-synchronet-js.md) |
| A script you wrote, or one of the examples that ship with the BBS | VPL script | [Set up a VPL script door](how-to-guides/door-vpl.md) |

Not sure? A `.EXE` that will not run on your Linux box is a DOS door. A folder of `.js` files that came from Synchronet is a Synchronet JS door. Anything else is native.

Every ViSiON/3 install already has seven VPL script doors wired into the doors menu, so if you want to see a door run before setting one up, connect and press **H** on the doors menu for the "Hello World" example.

## Before you start

Three things are the same for every door type.

**Where the files go.** Keep doors under the `doors/` directory in the BBS root. DOS doors go on the virtual C: drive at `doors/drive_c/`. Synchronet JS games go under `doors/sbbs/xtrn/`. Native doors and VPL scripts can go anywhere, but `doors/<name>/` and `scripts/` keep them together and inside your backups.

**How a door is defined.** Each door is one record in the config editor. Run `./config`, press **6** for Door Programs, and press **I** to insert a record. Every record has a **Code**, which is the name you use in menus, a **Name** that callers see, and a **Type**. The walkthroughs list the other fields each type needs.

**How a caller reaches it.** A menu command `DOOR:CODE` launches the door. Run `./menuedit`, open the `DOORSM` menu, press **F5** to add a command, set **Keys** to the letter callers will press and **Command** to `DOOR:CODE`. The stock doors screen is ANSI art, so a new key works immediately but is not drawn on screen until you add it to `menus/v3/ansi/DOORSM.ANS` with an ANSI editor. See [Menus & ACS](menus/menu-system.md) for the menu editor and art files.

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
- [VPL Scripting](scripting/vpl-scripting.md) for the script API
- [Menu Commands Reference](reference/menu-commands.md) for `DOOR:`, `LISTDOORS`, `OPENDOOR` and `DOORINFO`
