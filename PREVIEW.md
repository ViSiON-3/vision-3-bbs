# Preview: scripted menus, sixel pictures and two example themes

## What this branch is

A preview to try. It is cut from `main` at `59dd669d`. It is not a pull
request and is not meant to be merged as it is. The pieces you want will
come as small pull requests, in the order under
[How it would be split](#how-it-would-be-split).

## What is in it

| Capability | Docs |
|------------|------|
| A `.MNU` names a script that draws the menu and answers its prompt; `menuScript` in `theme.json` does it for every menu; `LABEL` on commands | [Menus drawn by a script](docs/sysop/scripting/vpl-scripting.md#menus-drawn-by-a-script), [Scripted Menus and Themes](docs/sysop/scripting/scripted-themes.md) |
| A `SCRIPT` ending in `.js` runs from the theme's own `scripts/`, with no `doors.json` entry | [Theme scripts](docs/sysop/scripting/vpl-scripting.md#theme-scripts) |
| Built-in screens drawn by a theme's script: User Konfig, Twit List, User List, Fast Logon, Your Stats | [Menus drawn by a script](docs/sysop/scripting/vpl-scripting.md#menus-drawn-by-a-script) |
| Pictures (`v3.gfx.image`) and a pixel canvas (`v3.gfx.canvas`), sent as sixel | [v3.gfx](docs/sysop/scripting/vpl-scripting.md#v3gfx) |
| Sixel, audio and encoding read from the terminal's answers at connect, never its name. A sixel or audio answer that comes after the 750 ms wait still counts, and so does a late encoding answer while the login matrix is up | [v3.gfx](docs/sysop/scripting/vpl-scripting.md#v3gfx), [Encoding](docs/sysop/advanced/encoding.md), [Pre-Login Matrix](docs/sysop/menus/menu-system.md#pre-login-matrix-screen) |
| Two shipped scripted themes: ViSiON3-Sixel (`menus/vision3`) and PMS (`menus/pms`) | [menus/vision3/README.md](menus/vision3/README.md), [menus/pms/README.md](menus/pms/README.md) |
| Pre-login screens (connection check, login matrix) owned by the board in `prelogin/`, with a picture, a turning cube and two tones; two example sets in `templates/` | [Pre-login screens](docs/sysop/configuration/adding-a-theme.md#pre-login-screens) |
| Per-theme `strings.json`, a theme name and description in Konfig, `./vision3 doctor --theme` | [Theme Strings](docs/sysop/configuration/configuration.md#theme-strings), [Name and description](docs/sysop/configuration/adding-a-theme.md#name-and-description), [Checking a theme](docs/sysop/configuration/adding-a-theme.md#checking-a-theme) |
| A test-account flag: its logins leave no caller history and are not announced | [Test Accounts](docs/sysop/users/user-management.md#test-accounts) |
| Panic recovery in session goroutines (`internal/safego`) | none |
| A source engine that copies Reddit, Hacker News, NASA GCN and NNTP groups into message areas. The template's one Reddit feed and three NNTP groups are disabled, and so is its sync event | [Source Gateway](docs/sysop/messages/source-gateway.md), [NNTP](docs/sysop/messages/nntp.md) |

## What it looks like

Captured from a board built from this branch, with the commands under
[Try it](#try-it), through a relay on port 48700. SyncTERM 1.9 draws sixel;
xterm 390 does not.

### ViSiON3-Sixel

![ViSiON3-Sixel login matrix on SyncTERM](docs/preview/vision3-matrix-syncterm.png)

The login matrix from `templates/prelogin-example/`, on SyncTERM: the logo picture beside the turning cube with the four choices.

![ViSiON3-Sixel main menu on SyncTERM](docs/preview/vision3-main-menu-syncterm.png)

The main menu on SyncTERM: the sixel logo over the framed menu, with the small cube beside the selected command.

![ViSiON3-Sixel main menu on xterm](docs/preview/vision3-main-menu-xterm.png)

The same main menu on xterm, which does not draw sixel: the text logo, no cube.

### PMS

![PMS login matrix on SyncTERM](docs/preview/pms-matrix-syncterm.png)

The login matrix from `templates/prelogin-example-pms/`, on SyncTERM: the graffiti picture beside the pink cube with glitter.

![PMS main menu on SyncTERM](docs/preview/pms-main-menu-syncterm.png)

The PMS main menu on SyncTERM: the graffiti picture, three columns of commands and the star mascot.

## Try it

These are the commands I ran on a fresh clone on Linux, with Go and `jq`
installed. They clone into your home directory, which `SRC` relies on. The `jq` line selects the ViSiON3-Sixel theme and makes the
board listen on 127.0.0.1 only.

```bash
cd ~
git clone -b feat/scripted-menus-sixel-preview <URL you were given> vision-3-preview
cd vision-3-preview
./dev-setup.sh ~/v3-preview
cd ~/v3-preview
SRC=~/vision-3-preview
jq '.theme = {"name": "vision3"} | .sshHost = "127.0.0.1" | .telnetHost = "127.0.0.1"' configs/config.json > configs/config.json.new && mv configs/config.json.new configs/config.json
mkdir -p prelogin && cp -R "$SRC/templates/prelogin-example/." prelogin/
./vision3 doctor --theme vision3
./vision3
```

- `doctor --theme vision3` ends with
  `ALL CHECKS PASSED  36 passed  ·  0 warning(s)  ·  0 failure(s)`.
- A plain `./vision3 doctor` passes `pre-login` (`7 from prelogin/, the rest
  from menus/v3`) and warns 5 times about the fresh install: the default
  sysop password, `binkd.conf` placeholders, and the permissions of
  `ftn.json`, `doors.json` and `qwknet.json`.
- Connect with telnet to 127.0.0.1 port 2323 (SyncTERM:
  `telnet://127.0.0.1:2323`) and log in as `felonius`, password `password`.

To switch the board to the PMS theme and its pre-login set, stop the board
(Ctrl-C) and run, from `~/v3-preview`:

```bash
jq '.theme.name = "pms"' configs/config.json > configs/config.json.new && mv configs/config.json.new configs/config.json
cp -R "$SRC/templates/prelogin-example-pms/." prelogin/
./vision3 doctor --theme pms
./vision3
```

- `doctor --theme pms` warns once for each of the seven game doors (see
  [below](#what-a-stock-board-lacks-for-the-pms-theme)) and passes the rest:
  `CHECK WARNINGS  40 passed  ·  7 warning(s)  ·  0 failure(s)`.
- Going back to the first pre-login set, delete `prelogin/strings.json` as
  well; only the PMS set has one.
- A caller can also pick either theme in Konfig without a restart; the
  pre-login set is the board's and does not change with it.

## What to look at, in order

| Step | Sixel terminal (SyncTERM) | Plain terminal (xterm) |
|------|---------------------------|------------------------|
| 1. The login matrix, on connecting | The ViSiON/3 logo picture beside a turning amber cube carrying the four choices | FIGlet lettering and a four-line lightbar |
| 2. The main menu: log in, then `2` at Fast Logon | The logo picture over a framed three-column menu, a small turning cube beside the selected command | A FIGlet text logo over the same menu, no cube |
| 3. `K` for Konfig, then `V` for Theme | A list of themes, with a line describing the highlighted one; Enter applies it at once | The same |
| 4. Pick PMS, then `Q` | Konfig is redrawn by the PMS script; `Q` gives the PMS main menu: a graffiti picture, three columns of commands, a star mascot | Konfig the same; the main menu with a dotted text logo in place of the picture |
| 5. A scripted built-in screen | In PMS, Konfig (step 4) and, at the next login, Fast Logon are drawn by the theme's `submenu.js`; both are text | The same |

With the board on the PMS pre-login set, the matrix shows the graffiti
picture beside a pink cube on SyncTERM, and a text matrix with keys `L`,
`A`, `C` and `D` on xterm.

The ViSiON3-Sixel main menu animates while it waits. Measured at a relay
in front of the board, it sent about 212,000 bytes a second to SyncTERM and
80,000 to xterm.

## Terminals

| Terminal | Its answer at connect | What the board did |
|----------|-----------------------|--------------------|
| SyncTERM 1.9, Linux, telnet | `ESC[<0;1;2;3;4;5;6;7c` and `ESC[=67;84;101;114;109;1;326c`; no answer to the audio queries | sixel on, audio off, CP437 |
| xterm 390, telnet | `ESC[?64;1;2;6;9;15;16;17;18;21;22;28c`; no answer to the audio queries | sixel off, audio off, UTF-8 |

The matrix tones are sent only to a terminal that answers yes to both audio
queries (libsndfile present, 8-bit WAV playable). Neither terminal I ran
answers yes, so I have not heard the tones.

## What a stock board lacks for the PMS theme

Its Doors menu names seven games that do not ship: `DANK`, `USURPER`,
`LORD`, `LORD2`, `SYNCDOOM`, `OOII` and `CARRIER`. Choosing one sends
`Error: Door 'LORD' is not configured.` and `Press Enter to continue...`,
then redraws the Doors menu at once, so the caller sees only a flash. The
theme's two other doors, `PAUSEBAR` and `AMILC`, are in the stock
`doors.json`.

## Known defects

- A missing door's message is drawn over at once instead of waiting for Enter, on every theme.
- `doctor --theme` checks that a `RUN:` command exists, not that the theme has the screens it reads: a theme without `ansi/YOURSTAT.ANS` passes, and `RUN:SHOWSTATS` then fails.
- `doctor --theme` does not check that a string's `!CODE` names a defined door.
- `v3.session.timeLeft` reads 3600 for a caller with no time limit, so the PMS footer shows `60 min left` to the sysop.
- PMS `submenu.js`: a label that is not selected is not cut to its column width, so a long one runs into the next column (found by reading the code).
- PMS `userlist.js` draws an 80x24 layout on a smaller terminal (found by reading the code).
- On SyncTERM, the unselected choices on the PMS matrix cube are hard to read at the board's real size.
- `setup.sh` runs `ssh-keygen` before it creates `configs/`, so on a fresh clone the SSH host key is not written and the board will not start. This is also on `main`; `dev-setup.sh`, used above, is not affected.

## What is not in the branch

- The board's own configuration and site files.
- Changes to CI: `.github/` is identical to `main`'s.

## Size

Measured at the branch head against `59dd669d`:

| | Files | Lines |
|-|-------|-------|
| Everything | 738 changed, 575 of them new | +38,585, -1,034 |
| Go code | 142 | +12,410, -740 |
| Go tests | 101 | +13,288, -94 |
| Theme files (`menus/vision3`, `menus/pms`) | 409 (199 and 210) | |
| Scripts (`.js`) | 12 | +5,589 |

The tests came over with the code. They are not proposed as they stand;
each pull request will carry only the tests for what it changes.

## How it would be split

In this order, each a small pull request:

1. Scripted menus: a `.MNU`'s `SCRIPT` names a `v3_script` door that draws the menu; `v3.menu.items`, `LABEL`, `v3.prompt.answer`, and the fallback to the `.ANS`.
2. Theme script folders: a `SCRIPT` ending in `.js` runs from the theme's `scripts/`, and `menuScript` in `theme.json`.
3. More `v3.menu` fields (`name`, `title`, `shortcuts`, `expand`) and `v3.util.fitColumns`.
4. The ViSiON3-Sixel example theme, which draws its text version until step 5 lands.
5. `v3.gfx`, the pixel canvas and sixel detection from the terminal's answer.
6. Toasts and the time-left warning inside a scripted menu.
7. Konfig and the other built-in screens drawn by scripts.
8. The login matrix picture, cube and tones: last, and only if you want it, pitched on its own first.

Two parts of step 2 overlap issue #623 and need a conversation there before
they are sent: a theme script's own data directory (script storage, its
section 4), and running theme scripts with no run limit (its section 3).
The file lookup can go without them. Step 5's picture reads would follow
the per-call read limits #623 is adding.

The PMS theme, the pre-login example sets, per-theme strings and names,
`doctor --theme`, the test-account flag, panic recovery and the source
engine are not yet sized or placed in this order.

Tell us whatever you dislike in it.
