# Set Up a Synchronet JS Door

ViSiON/3 includes a JavaScript engine that runs Synchronet BBS door games written for Synchronet's `jsexec` and DORKit framework. The runtime and two games, Legend of the Red Dragon and LORD II, ship in every release under `doors/sbbs/`. Nothing needs to be downloaded for those two.

This walkthrough enables LORD. LORD II is the same with `lord2` in place of `lord`.

## 1. Check the files are there

```
doors/sbbs/
├── exec/load/     JS libraries the games load
├── exec/dorkit/   DORKit terminal framework
└── xtrn/lord/     Legend of the Red Dragon, with lord.js
```

If `doors/sbbs/` is missing you installed from source rather than a release archive. Copy the directory from a release archive; see [Installation](getting-started/installation.md).

## 2. Define the door

Run `./config`, press **6** for Door Programs, then **I** to insert a record and fill in:

| Field | Value |
| --- | --- |
| Code | `LORDJS` |
| Name | `Legend of the Red Dragon` |
| Type | Synchronet JS |
| Working Dir | `doors/sbbs/xtrn/lord` |
| Script | `lord.js` |
| Exec Dir | `doors/sbbs/exec/` |
| Library Paths | `doors/sbbs/exec/load, doors/sbbs/exec/dorkit` |
| Single Instance | `Y` |

**Library Paths** is comma-separated in the editor. Both entries are needed: the games `load()` utility libraries from `exec/load` and the terminal framework from `exec/dorkit`.

**Single Instance** matters for LORD. The game keeps its player data in flat files and two nodes writing at once will corrupt them.

Press **Esc**, then **Q** and **Y** to save. The same record in `configs/doors.json`:

```json
{
  "code": "LORDJS",
  "name": "Legend of the Red Dragon",
  "type": "synchronet_js",
  "script": "lord.js",
  "working_directory": "doors/sbbs/xtrn/lord",
  "exec_dir": "doors/sbbs/exec/",
  "library_paths": ["doors/sbbs/exec/load", "doors/sbbs/exec/dorkit"],
  "single_instance": true
}
```

## 3. Add it to a menu

Run `./menuedit`, open `DOORSM`, press **F5**, and set **Keys** to `L`, **Command** to `DOOR:LORDJS`, **ACS** to `*`. Press **Esc** to save; the editor writes to the `menus.d/v3/` overlay. To show the key, copy the shipped art into the overlay and add the key there with an ANSI editor:

```bash
mkdir -p menus.d/v3/ansi
cp menus/v3/ansi/DOORSM.ANS menus.d/v3/ansi/
```

## 4. Try it

Connect, open the doors menu and press **L**. The first run creates the game's data files inside `doors/sbbs/xtrn/lord/`. Play a turn, quit, and check that a second call finds the same character.

## Adding a different Synchronet game

1. Get the game's `xtrn/<game>/` directory from the [Synchronet repository](https://gitlab.synchro.net/main/sbbs) or an existing Synchronet install.
2. Put it at `doors/sbbs/xtrn/<game>/`.
3. Define a door as above, with **Working Dir** pointing at the new directory and **Script** set to the game's main file.

Not every Synchronet game runs. The engine implements the parts of the Synchronet API that DORKit-based games use; [Synchronet JS Doors](doors/synchronet-js-doors.md#implemented-api-surface) lists what is covered and how to read the log when a game stops early.

## See also

- [Synchronet JS Doors](doors/synchronet-js-doors.md) for the runtime, module resolution, encoding notes and troubleshooting
- [Door Programs](doors/doors.md) for the remaining record fields
