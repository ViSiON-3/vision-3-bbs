# PMS theme

The theme of the Pirate Mind Station board, shipped as a second example of a
scripted theme beside `menus/vision3`. Magenta and grey graffiti: a graffiti
picture on the main menu, every menu drawn by a script in a dashed frame,
and an animated pause prompt. The board's name is part of its art and
footers.

## Select it

A board run from this checkout already has `menus/pms`. Make it the board's
theme with **Theme → Theme Selection** in `./config` (`"theme": { "name":
"pms" }` in `configs/config.json`) and restart the BBS, or let callers pick
it under **Theme** in User Konfig (`K`). Check it from the board root with:

```bash
./vision3 doctor --theme pms
```

Make local changes in `menus.d/pms/`, file by file, not in `menus/pms`.

## What it changes

- **Main menu** (`mnu/MAIN.MNU`, `"SCRIPT": "mainmenu.js"`): the labelled
  commands of `cfg/MAIN.CFG` under section titles in three columns, with
  the logo on the left and a star mascot in the corner.
- **Section menus** (`"SCRIPT": "submenu.js <shape>"`): the menu's name in
  large lettering beside a turning wireframe shape (`cube`, `pyramid`,
  `sphere` or `morph`), its labelled commands below.
- **User list** (`userlist.js`) and **your stats** (`mystats.js`) are
  scripted screens.
- **Pause prompt**: `strings.json` sets `pauseString` to `!PAUSEBAR`, which
  runs `scripts/pausebar.js` as a door (see below).
- `strings.json` and `templates/` restyle the board's prompts, lists and
  message headers in the same colours.

## Terminals

The main menu's logo is `data/pms_graffiti.png` (248x192), sent as sixel
only when the terminal said at connect that it draws sixel. Any other
terminal gets the logo as dotted text. Everything else is text on every
terminal.

## Login matrix

The login matrix is the board's, not the theme's, so selecting this theme
does not change it. The PMS matrix (graffiti picture, pink cube, glitter,
two tones) ships in `templates/prelogin-example-pms/`. Install it from the
board root with:

```bash
mkdir -p prelogin && cp -R templates/prelogin-example-pms/. prelogin/
```

See **Pre-login screens** in `docs/sysop/configuration/adding-a-theme.md`.

## What it expects of a board

The stock `configs/doors.json` defines the two doors the theme needs whose
scripts ship: `PAUSEBAR`, the pause prompt, and `AMILC`, Callers on the
main menu. Both are hidden, so a generated door menu does not list them.
A board set up before they were added to the template needs them copied
from `templates/configs/doors.json`; without them a pause prints
`!PAUSEBAR` as its text and Callers shows "Door 'AMILC' is not
configured".

The Doors menu also names seven games that do not ship. Each shows "Door
'...' is not configured" and returns to the menu until the sysop installs
the game and adds its door. `./vision3 doctor --theme pms` lists the
missing menu doors; it does not check `PAUSEBAR`.

| Code | Named by | Its script | In stock `doors.json` |
|------|----------|------------|-----------------------|
| `PAUSEBAR` | `strings.json` `pauseString` | `menus/pms/scripts/pausebar.js` | yes, hidden |
| `AMILC` | Main, `W` Callers | `scripts/examples/amilc.js` | yes, hidden |
| `ONELINERS`, `VOTING`, `STATS`, `USERSTATS`, `LAST10`, `AUTOMSG` | Doors; `ONELINERS` also Main | `scripts/examples/` | yes |
| `DANK`, `USURPER`, `LORD`, `LORD2`, `SYNCDOOM`, `OOII`, `CARRIER` | Doors | none; the games the sysop installs | no |

## Files

| Path | What it is |
|------|------------|
| `scripts/mainmenu.js` | The main menu |
| `scripts/submenu.js` | The section menus and User Konfig |
| `scripts/userlist.js`, `scripts/mystats.js` | The user list and the caller's stats |
| `scripts/pausebar.js` | The pause prompt |
| `scripts/matrix.js` | An animated entrance, run as a door; no menu names it |
| `scripts/lib/pms-ui.js` | The shared drawing code each script carries a copy of |
| `data/pms_graffiti.png` | The main menu's sixel logo |

A script cannot load another file, so each script holds a copy of
`scripts/lib/pms-ui.js` between its `pms-ui begin` and `pms-ui end` lines.
The comments there name a sync script and a test that keep the copies in
step; neither is in this repository, so edit every copy by hand.
