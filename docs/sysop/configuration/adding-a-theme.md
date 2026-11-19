# Adding a Theme

A theme is one directory under `menus/` that holds everything callers see
after they log in: screens, menus, scripts, pictures, colours and wording.
This page covers installing one someone else made, checking it, and keeping
yours in step with the theme ViSiON/3 ships (`menus/v3`). How the board
selects a theme and how callers choose one are in
[Theme Selection](configuration/configuration.md#theme-selection) and
[Callers' Own Themes](configuration/configuration.md#callers-own-themes).

## What a theme holds

```
menus/glass/
├── theme.json          name, description, colours, menuScript, sysopChatScreen
├── strings.json        prompts and messages worded for the theme (optional)
├── mnu/                one NAME.MNU per menu; MAIN.MNU is required
├── cfg/                one NAME.CFG per menu: its keys and commands
├── ansi/               NAME.ANS screens, and the screens commands show
├── bar/                NAME.BAR lightbar layouts
├── templates/          list and header templates (X.TOP, X.MID, X.BOT)
├── scripts/            .js files the menus name (optional)
└── data/               pictures and stores of those scripts (optional)
```

- **Self-contained.** A theme is read from its own directory only. A screen,
  menu or template it doesn't have is not borrowed from `menus/v3`. A caller
  who goes to a menu whose screen is missing, and whose script does not run
  in its place, gets an error line, presses Enter and is back on the menu
  they came from. Where there is no menu to go back to, such as the first
  menu after login, the caller is logged off.
  That is why a theme has to be checked, and checked again after an upgrade.
- **The directory.** The directory name identifies the theme: letters,
  digits, `.`, `_` and `-`, not starting with a dot. A caller's choice is
  saved as the directory, so it survives any change to the theme's name.
- **Name and description.** What callers see in Konfig; see
  [Name and description](#name-and-description).
- **Menus.** `mnu/NAME.MNU` and `cfg/NAME.CFG` are the menu and its commands,
  with `ansi/NAME.ANS` as its screen (see
  [Menus & ACS](menus/menu-system.md)). A menu with a `SCRIPT` needs no screen.
- **Scripts.** A menu's `SCRIPT`, or `menuScript` in `theme.json` for every
  menu, that names a `.js` file runs `scripts/<file>`. Any other word is a
  door code from the board's `configs/doors.json`, which has to be a
  `v3_script` door. See
  [Theme scripts](scripting/vpl-scripting.md#theme-scripts).
- **Pictures.** Images and other files a theme script reads go in `data/`;
  `v3.gfx.image("logo.png")` reads `data/logo.png`. The script's `v3.data`
  store is kept there too.
- **Strings.** `strings.json` holds only the keys the theme words
  differently from the board; see
  [Theme Strings](configuration/configuration.md#theme-strings).
- **Colours.** `theme.json` holds the theme's colours. The board's own
  overrides in `config.json` `theme.colors` apply only to the board's theme;
  see [Board Colour Overrides](configuration/configuration.md#board-colour-overrides).
- **Your changes** go in `menus.d/<name>/`, which is searched first, file by
  file, so a new copy of the theme doesn't overwrite them (see
  [Customising menus](menus/menu-system.md#customising-menus-without-losing-your-changes)).
- **Not the pre-login screens.** The connection check art and the login
  matrix belong to the board, not to a theme, so no theme changes what a
  caller sees before login. A theme's `PDMATRIX.*` files and `BOTCHECK.ASC`
  are never read. See [Pre-login screens](#pre-login-screens).
- **The stats screen.** A theme may draw the logon stats screen
  (`USERSTATS` in `login.json`, `RUN:SHOWSTATS` on a menu) with a script: a
  `mnu/YOURSTAT.MNU` whose `SCRIPT` names it. Without one, `ansi/YOURSTAT.ANS`
  is shown. Each caller gets their own theme's.

## Name and description

Konfig lists each theme by its name, shows a line describing the
highlighted one, and shows the name as the **Theme** value and in the
`Saved. Theme is now ...` line. Both the built-in Konfig form and a theme's
scripted Konfig (a `KONFIG.MNU` with a `SCRIPT`) use them. A theme states
them in its `theme.json`:

```json
{
  "name": "Glass",
  "description": "Clear panes, cyan bars"
}
```

- **Name.** Up to 20 columns show in the list; longer is cut.
- **Description.** One line, up to 60 columns; longer is cut.
- **Neither.** A theme that states neither is listed by its directory name
  with no description.
- **The board's labels.** `theme.labels` in `configs/config.json` names
  and describes a theme by its directory, over what its `theme.json` says,
  field by field. Use it for a theme you keep exactly as it ships, such as
  `menus/v3`, or to reword one without editing its files:

  ```json
  "theme": {
    "name": "vision3",
    "labels": {
      "v3": { "name": "V3-Stock", "description": "Stock ViSiON/3 menus, as shipped" }
    }
  }
  ```

  Changes to either apply the next time a caller opens Konfig, without a
  restart. `./vision3 doctor` warns about a `theme.labels` entry for a
  directory that isn't under `menus/`.
- **Text only.** Control characters are dropped, and pipe codes print as
  typed rather than colouring the text.

The order of the list is by name, ignoring case.

## Pre-login screens

The connection check (`challengeGateFile`, `BOTCHECK.ASC` by default) and the
login matrix (`PDMATRIX.ANS`, `.BAR`, `.CFG`, and the optional `.PNG`, `.EFX`,
`.MOVE.WAV` and `.CHOOSE.WAV`) are read from the board's `prelogin/`
directory, beside `configs/` and `menus/`, laid out as a menu set:

```
prelogin/
├── strings.json        the matrix's wording (optional): matrix* keys, pauseString
├── ansi/               BOTCHECK.ASC, PDMATRIX.ANS, .PNG, .EFX, .MOVE.WAV, .CHOOSE.WAV
├── bar/                PDMATRIX.BAR
└── cfg/                PDMATRIX.CFG
```

- An example set ships in `templates/prelogin-example/`: the ViSiON/3 logo
  as the matrix picture with its effects file, lettering and lightbar, and
  two tones (Kenney Interface Sounds, CC0; see `ansi/PDMATRIX.SOUNDS.txt`).
  Copy its contents into the board's `prelogin/`, as in
  `mkdir -p prelogin && cp -R templates/prelogin-example/. prelogin/` run in
  a checkout that is also the BBS root. The picture needs a sixel terminal
  and the tones a terminal that answers the audio query.
- Each file `prelogin/` doesn't hold comes from the shipped stock set,
  `menus/v3`, as it ships (its `menus.d/v3` overlay is not read). A board
  with no `prelogin/` directory gets the stock connection check and matrix.
- Neither the board's theme nor a caller's own theme is consulted, so
  switching either leaves both screens as they are.
- `prelogin/strings.json` words the matrix (check access, the goodbye, the
  pause after check access) over `configs/strings.json`; no theme's
  `strings.json` applies to it.
- What follows the matrix belongs to the board's theme: the `PRELOGON` art,
  the login prompt and the new-user form.
- `./vision3 doctor` reports, as **pre-login**, where the files come from,
  and warns about a matrix file or the connection check art that is missing
  or can't be read, and an optional one that can't be read.
- A theme's `PDMATRIX.*` and `BOTCHECK.ASC` are ignored, and
  `./vision3 doctor --theme` does not list them among what a theme lacks.

## Installing a theme

1. Unpack or copy the theme somewhere outside `menus/` and check it:

   ```bash
   ./vision3 doctor --theme /tmp/glass
   ```

2. Fix what the check fails or warns about (below), then copy it in under
   a name starting with a dot, which is never offered, and rename it, so
   callers never see it half copied:

   ```bash
   cp -R /tmp/glass menus/.glass.new && mv menus/.glass.new menus/glass
   ```

3. Check it again where it is installed, which also reads any changes of
   your own in `menus.d/glass/`:

   ```bash
   ./vision3 doctor --theme glass
   ```

That is all callers need. A running board offers the theme under **V Theme**
in User Konfig the next time a caller opens the Theme box; no restart. A
directory is offered when it has a valid name, a `mnu/MAIN.MNU`, and a
`theme.json` that loads or none at all. Callers keep
the board's theme until they pick another, and everything before login
stays on the board's theme (see
[Callers' Own Themes](configuration/configuration.md#callers-own-themes)).

To make it the board's own theme instead, set **Theme → Theme Selection** in
`./config` (or `theme.name` in `config.json`) and restart the BBS.

To start a theme of your own, copy the shipped one and change it:

```bash
cp -R menus/v3 menus/mytheme
```

## Checking a theme

`./vision3 doctor --theme <name>` checks the theme `menus/<name>`;
`./vision3 doctor --theme <directory>` checks a theme anywhere. Run it from
the BBS directory: the board's `configs/` is used for doors and message
areas, and `menus/v3` for the comparison. It changes nothing.

It fails or warns when:

| Finding | Status |
|---------|--------|
| No `mnu/MAIN.MNU` | FAIL: callers can't choose the theme and the board can't run on it |
| A `.MNU`, `.CFG`, `theme.json`, `strings.json` or chat screen that doesn't load | FAIL |
| A menu with commands and neither a screen `ansi/NAME.ANS` nor a script | WARN |
| A `SCRIPT` or `menuScript` naming a `.js` that isn't in `scripts/`, a path instead of a file name, or a door that `doors.json` lacks or that isn't a `v3_script` door | WARN |
| A `GOTO`, `DOOR`, `RUN` or `MSGAREA` command whose target doesn't exist | WARN |
| A `strings.json` value whose `%` codes don't match what the BBS passes it | WARN |
| A name longer than 20 columns or a description longer than 60, with `theme.labels` applied | WARN |

It also shows the name and description callers see and where they come
from (`theme.json` or `theme.labels`), or says the theme states neither.

The exit code is doctor's: `0` clean, `1` warnings, `2` failures. `--json`
writes the report as JSON.

## Keeping a theme current

After each upgrade, run the check on your theme. Its last lines compare it
with the shipped theme as it ships (`menus/v3`, without `menus.d/v3`) and
list what `menus/v3` has that your theme lacks:

```text
  THEME
  [INFO]  theme glass: compared with v3  lacks 1 menu: NEWS
         FIX → Copy what the theme should have from menus/v3, drawn in the theme's style; leave out what it does without on purpose.
  [INFO]  theme glass: compared with v3  lacks 2 screens: ansi/NEWSHDR.ANS, templates/NEWS.MID
         FIX → Copy what the theme should have from menus/v3, drawn in the theme's style; leave out what it does without on purpose.
  [INFO]  theme glass: compared with v3  lacks 2 commands: GOTO:NEWS, RUN:LISTNEWS
         FIX → Copy what the theme should have from menus/v3, drawn in the theme's style; leave out what it does without on purpose.
```

- **Menus** are `mnu/*.MNU` files.
- **Screens** are files in `ansi/`, `bar/` and `templates/`.
- **Commands** are the `CMD` values of the shipped `cfg/*.CFG` files that no
  menu of your theme has, such as a new `RUN:` feature.

These are listed, not failed: a theme may leave things out on purpose. For
each one you want, copy it from `menus/v3` and redraw it in your theme's
style; a new command goes into the `.CFG` of the menu where callers should
find it. The board's own doctor (`./vision3 doctor`) notes under its
**theme** line how many of each the board's theme lacks.
