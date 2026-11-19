# Scripted Menus and Themes

A theme can hand its menus to scripts, show pictures on terminals that draw
sixel, animate them, and word the board's prompts its own way. This page
lists each of those with the smallest example that works, walks through
building a small theme from an empty directory, and shows where the two
shipped scripted themes use each one. The full rules are in
[VPL Scripting](scripting/vpl-scripting.md),
[Adding a Theme](configuration/adding-a-theme.md) and
[Menus & ACS](menus/menu-system.md).

## What a theme can do

| To | Put this in the theme | Reference |
|----|-----------------------|-----------|
| Draw one menu with a script | `"SCRIPT": "mainmenu.js"` in `mnu/MAIN.MNU`, and the script in `scripts/` | [Menus drawn by a script](scripting/vpl-scripting.md#menus-drawn-by-a-script) |
| Draw every menu with one script | `"menuScript": "menu.js"` in `theme.json` | [Menus drawn by a script](scripting/vpl-scripting.md#menus-drawn-by-a-script) |
| Keep one menu on its `.ANS` when `menuScript` is set | `"SCRIPT": "NONE"` in that `.MNU` | [Menu files](menus/menu-system.md#menu-configuration-files-mnu) |
| Pass words to the script | `"SCRIPT": "submenu.js cube"`; the script reads them in `v3.args` | [v3.args](scripting/vpl-scripting.md#v3args) |
| Choose the text a script shows for a command | `"LABEL": "Settings"` on the command in the `.CFG` | [Command files](menus/menu-system.md#command-definition-files-cfg) |
| Draw User Konfig, the Twit List, the User List, Fast Logon or Your Stats with a script | `mnu/KONFIG.MNU`, `TWITLIST.MNU`, `USERLIST.MNU`, `FASTLOGN.MNU` or `YOURSTAT.MNU` with a `SCRIPT` | [Built-in screens](scripting/vpl-scripting.md#menus-drawn-by-a-script) |
| Show a picture | `v3.gfx.image("logo.png")`, with the file in `data/` | [v3.gfx](scripting/vpl-scripting.md#v3gfx) |
| Animate a picture | `v3.gfx.canvas(480, 160, "logo.png")` and its `heat`, `wave`, `present` and `presentDelta` | [v3.gfx](scripting/vpl-scripting.md#v3gfx) |
| Word prompts and messages | `strings.json` with only the keys the theme changes | [Theme Strings](configuration/configuration.md#theme-strings) |
| Show a name and a line about the theme in Konfig | `"name"` and `"description"` in `theme.json` | [Name and description](configuration/adding-a-theme.md#name-and-description) |
| Check it | `./vision3 doctor --theme <name>` | [Checking a theme](configuration/adding-a-theme.md#checking-a-theme) |

The login matrix is the board's, not a theme's. Its picture, effects and
tones go in the board's `prelogin/` directory; see
[Pre-login screens](#pre-login-screens) below.

## What the board knows about the terminal

At connect the board asks the terminal what it can do and logs the answer.
It never decides from the terminal's name.

```bash
grep '"terminal probe"' data/logs/vision3.log
```

SyncTERM 1.9 on telnet:

```text
"msg":"terminal probe","node":1,"remote":"127.0.0.1:43128","cursor":"\\x1b[2;4R","encoding":"cp437","extended":"\\x1b[<0;1;2;3;4;5;6;7c","primary":"\\x1b[=67;84;101;114;109;1;326c","sixel":true,"libsndfile":"none","wav_u8":"none","audio":false}
```

xterm 390 on telnet:

```text
"msg":"terminal probe","node":1,"remote":"127.0.0.1:39242","cursor":"\\x1b[2;2R","encoding":"utf8","extended":"none","primary":"\\x1b[?64;1;2;6;9;15;16;17;18;21;22;28c","sixel":false,"libsndfile":"none","wav_u8":"none","audio":false}
```

| Field | Means | Where it is used |
|-------|-------|------------------|
| `sixel` | The terminal draws sixel pictures | `v3.gfx.sixel` in every script; the login matrix picture |
| `audio` | The terminal said it has libsndfile and plays 8-bit WAV | The login matrix tones only; scripts have no sound call |

- A caller can override `sixel` with **Sixel Graphics** (`W`) in User Konfig:
  Auto, On or Off.
- An answer that arrives after the board's 750 ms wait, but within 30
  seconds of connecting, still turns sixel or audio on. A script reads
  `v3.gfx.sixel` once, when it starts.
- Every script needs a text version for terminals without sixel.
  `v3.gfx.image()` returns `false`, and draws nothing, when it can't draw.

## Walk-through: a theme with a scripted main menu

This builds a theme called Demo, offered to callers in Konfig. It has a main
menu drawn by a script with the ViSiON/3 logo, a scripted Your Stats screen,
and one string of its own. Run each step from the BBS directory.

### 1. Make the directory

```bash
mkdir -p menus/demo/mnu menus/demo/cfg menus/demo/ansi menus/demo/scripts menus/demo/data
```

`menus/demo/theme.json`:

```json
{
  "name": "Demo",
  "description": "A logo and one scripted menu"
}
```

### 2. Add the main menu

`menus/demo/mnu/MAIN.MNU`. `SCRIPT` names a file in the theme's
`scripts/` because it ends in `.js`; any other word would be a door code
from `configs/doors.json`.

```json
{
  "TITLE": "Main Menu",
  "CLR": true,
  "USEPROMPT": true,
  "PROMPT1": "|08Command|07: |15",
  "SCRIPT": "mainmenu.js"
}
```

`menus/demo/cfg/MAIN.CFG`. The script is given the commands that have a
`LABEL`; `/G` has none, so it is not shown but still works when typed.

```json
[
  { "KEYS": "K", "CMD": "RUN:USERCONFIG", "ACS": "*", "NODE_ACTIVITY": "User Settings", "LABEL": "Settings" },
  { "KEYS": "Y", "CMD": "RUN:SHOWSTATS", "ACS": "*", "NODE_ACTIVITY": "Viewing User Stats", "LABEL": "Your stats" },
  { "KEYS": "G", "CMD": "RUN:MAINLOGOFF", "ACS": "*", "NODE_ACTIVITY": "Logging Off", "LABEL": "Goodbye" },
  { "KEYS": "/G", "CMD": "RUN:IMMEDIATELOGOFF", "ACS": "*", "NODE_ACTIVITY": "Logging Off" }
]
```

A screen for when the script can't run. The menu shows it, with
`PROMPT1`, if the script is missing, fails, or ends without an answer:

```bash
printf '|15Main Menu|07\r\n\r\n|15K|07 Settings   |15Y|07 Your stats   |15G|07 Goodbye\r\n' > menus/demo/ansi/MAIN.ANS
```

### 3. Add the logo

Any PNG, GIF or JPEG works for step 4. Step 9 also uses the picture as the
palette of a canvas, which needs at most 256 colours, ignoring pixels less
than half opaque. This uses the shipped theme's logo, 480x160 pixels and 163
colours: 60 columns by 10 rows on SyncTERM's 80x25 screen, whose cells are 8
pixels wide and 16 high.

```bash
cp menus/vision3/data/vision3-logo.png menus/demo/data/logo.png
```

### 4. Write the script

`menus/demo/scripts/mainmenu.js`:

```javascript
// mainmenu.js: the Demo theme's main menu.
v3.console.clear();
v3.console.gotoxy(11, 1);
var top = 3;
if (v3.gfx.image("logo.png")) {
    top = 12; // the logo is 160 pixels high: 10 rows on SyncTERM
} else {
    v3.console.print("|14" + v3.menu.title);
}
v3.menu.items.forEach(function (item, i) {
    v3.console.gotoxy(11, top + i);
    v3.console.print("|15" + item.key + "  |07");
    v3.console.write(item.label);
});
v3.console.gotoxy(11, top + v3.menu.items.length + 1);
v3.console.print(v3.menu.expand("|08Time left: |07|TL |15> "));

// Wait for a key the caller may use. "/" starts a longer command, such as /G.
for (;;) {
    var key = v3.console.getkey().toUpperCase();
    if (key === "/") {
        v3.console.print("/");
        key += v3.console.getstr(4).toUpperCase();
    }
    if (v3.menu.shortcuts.indexOf(key) >= 0) {
        v3.console.print("\r\n");
        v3.prompt.answer(key);
        break;
    }
}
```

- `v3.gfx.image("logo.png")` reads `menus/demo/data/logo.png` and draws it
  with its top-left corner at the cursor.
- `v3.menu.items` holds `key`, `label` and `activity` for each command shown.
  `v3.menu.shortcuts` holds every key the caller may use, hidden ones too.
- `v3.menu.expand` fills in placeholder codes such as `|TL` (time left).
- `v3.prompt.answer(key)` hands the board the caller's command. The board
  then runs it as if typed at the menu prompt.
- The loop goes round on any other key. An empty answer would send the menu
  to its `.ANS` screen.
- `v3.console.write` prints a label as plain text, so pipe codes in it are
  not read as colours.

### 5. Check it

```bash
./vision3 doctor --theme demo
```

The checks part of the output (it ends with three `lacks` lines comparing
the theme with `menus/v3`):

```text
  MENUS & DOORS
  [PASS]  menus/demo/cfg/MAIN.CFG  parses
  [PASS]  menus/demo/mnu/MAIN.MNU  parses

  THEME
  [PASS]  theme demo: menu references  GOTO, DOOR, RUN, and MSGAREA targets resolve
  [PASS]  theme demo: screens  every menu with commands has a screen or a script
  [PASS]  theme demo: scripts  the scripts its menus name are there
  [PASS]  theme demo: name  shown as "Demo": A logo and one scripted menu (theme.json)
```

Before step 4, the scripts line read:

```text
  [WARN]  theme demo: scripts  mnu/MAIN.MNU: SCRIPT names theme script "mainmenu.js", which is not in scripts/
         FIX → Add the script or door, or correct the name; until then the menu shows its screen.
```

### 6. Pick it

Log in, press `K` for User Konfig, then `V` for **Theme**. The box lists
**Demo** with the line *A logo and one scripted menu* under the list when it
is highlighted. Enter picks it; no restart is needed. `Q` leaves Konfig and
the main menu is drawn by `mainmenu.js`:

- On SyncTERM the logo is drawn at the top and the three commands under it.
- On xterm, which does not answer that it draws sixel, the menu title is
  printed in its place.

The theme's Konfig has no `KONFIG.ANS`, so the form is drawn with a plain
**User Konfig** header.

### 7. Script a built-in screen

`Y` runs `RUN:SHOWSTATS`, which shows `ansi/YOURSTAT.ANS`. Demo has no such
file, so pressing `Y` only redraws the menu and logs
`failed to read file for showstats`. A script can draw the screen instead.

`menus/demo/mnu/YOURSTAT.MNU`:

```json
{
  "TITLE": "Your Stats",
  "SCRIPT": "mystats.js"
}
```

`menus/demo/scripts/mystats.js`:

```javascript
// mystats.js: the Demo theme's Your Stats screen.
var u = v3.user;
v3.console.clear();
v3.console.print("|15");
v3.console.write(u.handle);
v3.console.println("|07");
v3.console.println("Calls      " + u.timesCalled);
v3.console.println("Messages   " + u.messagesPosted);
v3.console.println("Uploads    " + u.uploads);
v3.console.println("Downloads  " + u.downloads);
v3.console.print("\r\n|08Press a key |15> ");
v3.console.getkey();
v3.prompt.answer("Q");
```

The same script draws the stats screen of the login sequence (`USERSTATS` in
`configs/login.json`) for callers on this theme.

### 8. Word a prompt

`menus/demo/strings.json` holds only the keys Demo words differently from
the board. `logOffStr` is the Goodbye question; `@` is where the Yes/No
choice goes.

```json
{
  "logOffStr": "|15Leave the Demo board? @"
}
```

A caller on the theme sees it from their next login. `G` then asks
*Leave the Demo board?*.

### 9. Animate the logo

A canvas is a picture the script draws into and sends as sixel. This version
of `mainmenu.js` redraws the logo with a heat haze every 100 ms while it
waits for a key:

```javascript
// mainmenu.js: the Demo theme's main menu, with a heat haze on the logo.
var canvas = null;
var top = 3;
var started = Date.now();
v3.console.clear();
if (v3.gfx.sixel) {
    canvas = v3.gfx.canvas(480, 160, "logo.png");
    top = 12;
} else {
    v3.console.gotoxy(11, 1);
    v3.console.print("|14" + v3.menu.title);
}

// One frame: the logo, warped by the haze at this moment.
function drawLogo() {
    canvas.clear(0, 0, 0);
    canvas.drawImage("logo.png", 0, 0);
    canvas.heat((Date.now() - started) / 1000, 0.02, true);
    v3.console.gotoxy(11, 1);
    canvas.presentDelta();
}

v3.menu.items.forEach(function (item, i) {
    v3.console.gotoxy(11, top + i);
    v3.console.print("|15" + item.key + "  |07");
    v3.console.write(item.label);
});
var promptY = top + v3.menu.items.length + 1;
var prompt = v3.menu.expand("|08Time left: |07|TL |15> ");
v3.console.gotoxy(11, promptY);
v3.console.print(prompt);
var promptX = 11 + v3.util.displayLen(prompt);

// Wait for a key the caller may use, drawing a frame every 100 ms.
for (;;) {
    if (canvas) {
        drawLogo();
        v3.console.gotoxy(promptX, promptY);
    }
    var key = v3.console.getkey(canvas ? 100 : 0).toUpperCase();
    if (key === "") {
        continue;
    }
    if (key === "/") {
        v3.console.print("/");
        key += v3.console.getstr(4).toUpperCase();
    }
    if (v3.menu.shortcuts.indexOf(key) >= 0) {
        v3.console.print("\r\n");
        v3.prompt.answer(key);
        break;
    }
}
```

- The third argument of `canvas()` is the palette: the canvas is drawn with
  the colours of `logo.png`. A palette picture with more than 256 colours,
  or none at least half opaque, stops the script.
- `presentDelta()` sends only the pixels that changed since the last frame.
  The first call sends the whole picture.
- `heat(time, strength, hotOnly)` takes the time in seconds, up to
  1,000,000, and a strength from 0 to 0.1. `Date.now() / 1000` is too large
  and stops the script with `invalid heat parameters`.
- On the scratch board this sent about 67,000 bytes a second to SyncTERM
  while the menu waited, and nothing to xterm. Choose the frame rate and
  picture size for your callers' connections.

## When a script can't run

The menu shows its `.ANS` and prompt instead, and the board logs why. The
heat call above, with `Date.now() / 1000`, logged:

```text
"level":"ERROR","msg":"menu script failed, menu shows its screen","menu":"MAIN","door":"mainmenu.js","node":1,"error":"script error: GoError: invalid heat parameters at github.com/ViSiON-3/vision-3-bbs/internal/scripting.registerCanvas.func2.10 (native)"
```

The other cases are listed under
[Menus drawn by a script](scripting/vpl-scripting.md#menus-drawn-by-a-script).

## A theme stands alone

A theme is read only from its own directory, so Demo has none of the stock
screens. `doctor --theme` lists what it lacks compared with `menus/v3` as
`[INFO]` lines. It does not check the files a `RUN:` command reads, which
the board finds missing only when the command runs:

| Missing from Demo | What the caller sees | Log |
|-------------------|----------------------|-----|
| `mnu/FASTLOGN.MNU` | The login sequence goes on without Fast Logon | `FASTLOGN.CFG is missing or has no commands; continuing login` |
| `templates/LASTCALL.*`, `templates/ONELINER.*` | The login sequence skips last callers and oneliners | `error during login item` with `failed loading LASTCALL templates` and `failed loading ONELINER templates` |
| `ansi/YOURSTAT.ANS`, before step 7 | `Y` redraws the menu | `failed to read file for showstats` |
| `ansi/GOODBYE.ANS` | `Goodbye!` and `Logging off...` as plain text | none |
| `ansi/KONFIG.ANS` | Konfig with a plain **User Konfig** header | none |

To give the theme a screen it lacks, copy it from `menus/v3` and redraw it
in the theme's style.

## Pre-login screens

The connection check and the login matrix come from the board's `prelogin/`
directory, else from `menus/v3`, whatever theme the board or caller uses. The
matrix can show a PNG beside or behind a turning cube on sixel terminals,
and play two tones on terminals that answer the audio query. Two example sets
ship with the source:

| Set | Picture | Cube | Strings |
|-----|---------|------|---------|
| `templates/prelogin-example/` | The ViSiON/3 logo, 320x107 | Amber, beside the picture | none |
| `templates/prelogin-example-pms/` | The PMS theme's graffiti picture | Pink, with glitter, beside the picture | `strings.json` colouring the check-access answers and running the `PAUSEBAR` door for the pause after them |

Both have the same two tones and their licence note,
`ansi/PDMATRIX.SOUNDS.txt`. Copy one set into `prelogin/`, not both. The
sets hold the same files except `strings.json`, so to switch from the PMS
set to the other, delete `prelogin/strings.json` as well. Run this
from the BBS directory, with `SRC` set to the source checkout;
`dev-setup.sh` does not copy the example sets into the BBS directory:

```bash
SRC=/path/to/vision-3-bbs
mkdir -p prelogin && cp -R "$SRC/templates/prelogin-example/." prelogin/
./vision3 doctor
```

Doctor's pre-login line then reads:

```text
  [PASS]  pre-login  login matrix and connection check: 7 from prelogin/, the rest from menus/v3
```

The board reads `prelogin/` on each connection; no restart. Terminals
without sixel get `PDMATRIX.ANS` and the lightbar. The files and the effect
settings are described under [Pre-login screens](configuration/adding-a-theme.md#pre-login-screens)
and [Pre-Login Matrix](menus/menu-system.md#pre-login-matrix-screen).

## How the shipped themes use this

Two scripted themes ship in `menus/`. Open their files to see each feature
at full size.

| Feature | ViSiON3-Sixel (`menus/vision3`) | PMS (`menus/pms`) |
|---------|---------------------------------|-------------------|
| Scripted menus | `MAIN.MNU`: `mainmenu.js`; the ten menus reached from Main: `submenu.js` | `MAIN.MNU`: `mainmenu.js`; section menus: `submenu.js` with a shape word (`cube`, `pyramid`, `sphere`, `morph`) |
| `menuScript` | not used | not used |
| Words after the script name | `classic` for 16 colours (`v3.args`) | the shape, read from `v3.args` |
| `LABEL` | every command the scripts show | every command the scripts show |
| Built-in screens | none | `KONFIG.MNU` (`submenu.js morph`, with `v3.menu.field` and `v3.menu.promptRow`), `TWITLIST.MNU`, `FASTLOGN.MNU`, `USERLIST.MNU` (`userlist.js`), `YOURSTAT.MNU` (`mystats.js`) |
| Pictures | `data/vision3-logo.png` with `v3.gfx.image`, or on a canvas with `data/vision3-palette.png` as palette | `data/pms_graffiti.png` with `v3.gfx.image` |
| Canvas | the logo with `heat` and `presentDelta`, and a turning cube on a second canvas | not used |
| Text version | FIGlet logo | dotted text logo |
| `strings.json` | none | 114 keys in the theme's colours; `pauseString` is `!PAUSEBAR`, a door |
| Name and description | `ViSiON3-Sixel`: *ViSiON/3 look with sixel logos* | `PMS`: *Pirate Mind Station: graffiti, scripted menus* |
| Login matrix | `templates/prelogin-example/` | `templates/prelogin-example-pms/` |

Each theme's `README.md` lists its files. The PMS theme is the theme of the
Pirate Mind Station board; its `README.md` lists the doors its menus and
strings name, and which of them a stock `configs/doors.json` defines.

A string's `!CODE` runs a door from `configs/doors.json`; it can't name a
file in the theme's `scripts/`. That is why PMS's animated pause prompt is
a `PAUSEBAR` entry in `doors.json`, which the stock file defines. A board
set up before that entry was added prints `!PAUSEBAR` at every pause until
it is copied from `templates/configs/doors.json`.
