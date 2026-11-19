# ViSiON/3 theme

The name is always styled `ViSiON/3`: lowercase i's, the other letters
uppercase.

This theme is warm gold and steel, built on the ViSiON/3 logo. It is a complete
menu set: everything the stock `v3` theme has, plus its own scripts and
pictures, so it needs nothing from any other directory.

## Install

Copy the directory into the board's `menus/`. Run this from the root of this
checkout, with `BOARD` set to the board's root. An installed copy is moved to
`data/backups/` first, outside `menus/` so callers are not offered it as a
theme; if that fails, or `BOARD` is this checkout, nothing is moved or
copied:

```bash
BOARD=/path/to/board
[ "$(cd "$BOARD/menus" && pwd -P)" != "$(cd menus && pwd -P)" ] &&
{ [ ! -e "$BOARD/menus/vision3" ] || { mkdir -p "$BOARD/data/backups" &&
  mv "$BOARD/menus/vision3" "$BOARD/data/backups/menus-vision3.$(date +%Y%m%d-%H%M%S)"; }; } &&
  cp -R menus/vision3 "$BOARD/menus/"
```

A board run from this checkout already has it. `./vision3 doctor --theme
vision3`, run from the board root, checks the copy. Callers can then pick it
under **Theme** in User Konfig (`K`), or the sysop makes it the board's
theme with **Theme → Theme Selection** in `./config` (`"theme": { "name":
"vision3" }` in `config.json`) and restarts the BBS. Make local changes in
`menus.d/vision3/`, file by file, not in `menus/vision3`: installing a newer
copy leaves `menus.d/` alone.

## What it changes

- **Main menu** (`mnu/MAIN.MNU`, `"SCRIPT": "mainmenu.js"`): the logo, the
  labelled commands of `cfg/MAIN.CFG` in three columns in a frame, a small
  turning cube beside the selected command and embers and welding sparks on
  the logo. Commands without a `LABEL` stay as typed shortcuts.
- **Every menu reachable from Main** (Admin, BBS list, Doors, Email, Files,
  InfoForms, Messages, QWK, Rumors, V3Net; `"SCRIPT": "submenu.js"`): the
  menu's name in large lettering in the same colours, its labelled commands
  in a frame, and on Messages and Files the caller's conference and area.
- Every other file is the stock `v3` theme's, unchanged except that the
  command lists of those menus give each command a `LABEL`, and the
  pre-login files are left out (see below).

## Terminals

- **Sixel.** The main menu's logo and cube are sixel pictures, drawn only
  when `v3.gfx.sixel` is true: the terminal said at connect that it draws
  sixel, or the caller turned **Sixel Graphics** on in Konfig. Otherwise the
  logo is text and there is no cube. The submenus are text on every terminal.
- **Colour.** The scripts use 24-bit colour. Add `classic` after the script
  name in a `.MNU` (`"SCRIPT": "submenu.js classic"`) for 16 colours.
- **Screen size.** On a screen smaller than 80x24, or a menu with more
  commands than fit, the script draws nothing and the menu shows its `.ANS`
  screen and prompt.

## Files

| Path | What it is |
|------|------------|
| `scripts/mainmenu.js` | The main menu |
| `scripts/submenu.js` | The section menus |
| `data/vision3-logo.png` | The logo as the main menu draws it, 480x160 |
| `data/vision3-palette.png` | The sixel colours shared by the logo, embers, welds and cube |
| `art/logo-original.png`, `art/logo-riveted-original.png` | The source images the logo was made from; the board does not read them |

## Not in this theme

The connection check and the login matrix belong to the board, which reads
them from its `prelogin/` directory, else from stock's, so this theme holds
none of them. A board that wants the ViSiON/3 matrix puts
its files in `prelogin/`: the logo as `prelogin/ansi/PDMATRIX.PNG` (`data/vision3-logo.png`
scaled to 320x107 on its own palette, so it fits beside the cube), with
`prelogin/ansi/PDMATRIX.ANS`, `prelogin/ansi/PDMATRIX.EFX`,
`prelogin/bar/PDMATRIX.BAR` and `prelogin/cfg/PDMATRIX.CFG`.
`templates/prelogin-example/` holds that set, with two tones. From the board
root, with `SRC` set to this checkout, copy it in with
`mkdir -p prelogin && cp -R "$SRC/templates/prelogin-example/." prelogin/`. Without
it the board shows the stock matrix from `menus/v3`.
