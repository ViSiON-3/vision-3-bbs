# Set Up a VPL Script Door

VPL scripts are JavaScript files that run inside the BBS with access to the caller, the user database, messages and files. They need no external program, no dropfile and no emulator, which makes them the quickest door to set up and a good first one.

Seven ship with every install under `scripts/examples/`, already defined in `configs/doors.json` and wired to the doors menu: a hello-world, one-liners, a voting booth, system stats, an auto-message board, user stats and the last ten callers. This walkthrough adds an eighth.

## 1. Write the script

Create `scripts/greeting.js`:

```javascript
v3.console.clear();
v3.console.println("|11Welcome, |15" + v3.user.handle + "|11!");
v3.console.println("|07You have called |15" + v3.user.timesCalled + "|07 times.");
v3.console.println("|07There are |15" + v3.users.count() + "|07 users registered.");
v3.console.pause();
```

Pipe codes such as `|11` set colours, the same as in menu strings. The full API is in [VPL Scripting](scripting/vpl-scripting.md#programming-reference).

## 2. Define the door

Run `./config`, press **6** for Door Programs, then **I** to insert a record and fill in:

| Field | Value |
| --- | --- |
| Code | `GREETING` |
| Name | `Greeting` |
| Type | VPL Script |
| Working Dir | `scripts` |
| Script | `greeting.js` |

Leave **Script Args** blank unless the script reads `v3.args`. Press **Esc** to leave the record, then **Q** and answer **Y** to save.

If you prefer to edit JSON, the same record in `configs/doors.json` is:

```json
{
    "code": "GREETING",
    "name": "Greeting",
    "type": "v3_script",
    "script": "greeting.js",
    "working_directory": "scripts"
}
```

## 3. Add it to a menu

Run `./menuedit`, open `DOORSM`, press **F5** to add a command, and set:

| Field | Value |
| --- | --- |
| Keys | `G` |
| Command | `DOOR:GREETING` |
| ACS | `*` |

Press **Esc** to save. The key works straight away; to show it on the doors screen, add a `[G] Greeting` line to `menus/v3/ansi/DOORSM.ANS` with an ANSI editor.

## 4. Try it

Connect to the BBS, open the doors menu and press **G**. No restart is needed, and the script is read fresh each time it runs, so you can edit `greeting.js` and press **G** again to see the change.

## Passing arguments

The shipped voting booth shows how a script takes settings from its record. Its **Script Args** are a poll ID, a question and the choices, which the script reads as `v3.args[0]`, `v3.args[1]` and so on. Reuse the same script under a second code with different arguments to run two polls.

## See also

- [VPL Scripting](scripting/vpl-scripting.md) for the API and the shipped examples
- [Door Programs](doors/doors.md) for the remaining record fields such as **Single Instance** and **Min Access Level**
