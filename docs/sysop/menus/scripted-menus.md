# Scripted Menus

A scripted menu is a menu whose screen is drawn by a [VPL script](scripting/vpl-scripting.md) instead of its `.ANS` file and prompt. The menu's `.MNU` names a `v3_script` door in its `SCRIPT` field. Each time the menu is shown, the BBS runs that script, gives it the commands the caller may choose, and acts on the choice the script hands back as if the caller had typed it.

---

## Scripted Menus and Themes

| | Theme | Scripted menu |
|---|---|---|
| What it is | A folder of `.ANS`, `.MNU`, `.CFG` and `.BAR` files and colours, such as `menus/v3/` | One field, `SCRIPT`, in one menu's `.MNU` |
| Who draws the screen | The BBS, from the menu's `.ANS` (and `.BAR` for a lightbar menu) | The script |
| Who reads the caller's choice | The BBS, at the menu prompt or lightbar | The script, which answers with `v3.prompt.answer()` |
| Scope | Every menu of the board | The menus whose `.MNU` sets `SCRIPT` |

A theme can make any of its menus a scripted menu by setting `SCRIPT` in that menu's `.MNU`. The menu keeps its `.CFG` commands, so what each key does is unchanged. The stock `v3` theme sets no `SCRIPT`, so it looks and works as before.

---

## Setting Up a Scripted Menu

### 1. Write the script

Put the script under `scripts/`. It reads the menu's commands from `v3.menu.items`, draws them, waits for the caller, and calls `v3.prompt.answer()` with the key of the command the caller chose:

```javascript
// scripts/mymenu.js
v3.console.cls();
v3.menu.items.forEach(function (item) {
    v3.console.println("|08[|15" + item.key + "|08] |07" + item.label);
});
v3.console.print("|15Choice: ");
v3.prompt.answer(v3.console.getstr(10));
```

The script must clear the screen itself: the menu's `CLR` setting is not applied. A fuller example, a lightbar, ships as `scripts/examples/menu-lightbar.js` and is described [below](#worked-example-menu-lightbarjs).

### 2. Add the door to doors.json

Add a `v3_script` door for the script to `configs/doors.json`:

```json
{
    "code": "MYMENU",
    "name": "My Menu",
    "type": "v3_script",
    "script": "mymenu.js",
    "working_directory": "scripts",
    "hidden": true
}
```

The fields are those of any [VPL door](scripting/vpl-scripting.md#door-config-fields). For a menu script:

- Set `"hidden": true`. Without it the door is listed in the [generated door menu](doors/generated-menu.md), where a caller could run it as a door.
- Leave out `single_instance`. With it, a second node showing the menu is refused the door and gets the menu's own screen. The log records `door already in use by another node`, then `menu script failed, menu shows its screen` with the error `door is currently in use by another node: MYMENU`.
- `acs` and `min_access_level` decide which callers get the script. A caller they refuse gets the menu's own screen and prompt.
- `max_run_minutes` limits one showing of the menu (30 minutes unless set). When it runs out the menu shows its own screen and prompt.

### 3. Set SCRIPT in the menu's .MNU

Add `SCRIPT` with the door's code to the menu's `.MNU`. To keep the change out of the shipped files, copy the `.MNU` to `menus.d/` first (see [Customising menus without losing your changes](menus/menu-system.md#customising-menus-without-losing-your-changes)):

```json
{
    "TITLE": "Main Menu",
    "CLR": true,
    "USEPROMPT": true,
    "PROMPT1": "|C4|UH|{|C3 from |C6|GL|}|{ |C5(|C6|UN|C5)|}|C1",
    "PROMPT2": "|C5[|C3|TIME|C5] |C5[|C4TL:|C3|TL|C5]|C4 |C5-|C4>|C2 ",
    "FALLBACK": "MAIN",
    "ACS": "",
    "PASS": "",
    "HELPMENU": "",
    "FORCEHELPLEVEL": 0,
    "MES_CONF": 0,
    "FILE_CONF": 0,
    "FORCEHOTKEY": false,
    "SCRIPT": "MYMENU"
}
```

`SCRIPT` is not case-sensitive and surrounding spaces are ignored. Keep the menu's `.ANS`: the BBS reads it before running the script, ends the call if it cannot (`failed to read ANSI file`), and shows it whenever the script cannot run.

`menuedit` has no field for `SCRIPT`. Edit the `.MNU` by hand; `menuedit` keeps the field when it saves the menu.

### 4. Optionally, add LABEL to commands

The script gets each command's `NODE_ACTIVITY` as its label, which is written for Who's Online ("Reading Messages"). To show other text, add `LABEL` to the command in the `.CFG`:

```json
{
    "KEYS": "M",
    "CMD": "GOTO:MSGMENU",
    "ACS": "*",
    "HIDDEN": false,
    "NODE_ACTIVITY": "Message Menu",
    "LABEL": "Messages"
}
```

`LABEL` is used only by scripted menus. `menuedit` keeps it when it saves the menu but cannot edit it.

### 5. Reload

No restart is needed:

| Change | Takes effect |
|---|---|
| The script file | The next time the menu is shown |
| `configs/doors.json` | When the BBS reloads it on its own; the log records `doors.json reloaded`. The next showing of the menu uses the new settings |
| The `.MNU` or `.CFG` | The next time the menu is shown, as both are read each time |

---

## What the Script Gets and Answers

### What the script gets

| Name | Value |
|---|---|
| `v3.prompt.kind` | `"menu"` |
| `v3.menu.items` | The commands the caller may choose, in `.CFG` order. Never `null`; an empty array when there are none |
| `v3.menu.items[i].key` | The first key of the command's `KEYS`: `"V"` for `"V VOTE"`. Keys keep their case and can be longer than one character (`"/SYSOP"`) |
| `v3.menu.items[i].label` | The command's `LABEL`, or its `NODE_ACTIVITY` when it has no `LABEL`. `""` when it has neither |
| `v3.menu.items[i].activity` | The command's `NODE_ACTIVITY`, or `""` |
| `v3.args` | The door's `args` from `doors.json` |

Every other `v3` call works as in any [VPL script](scripting/vpl-scripting.md#programming-reference).

Commands left out of `v3.menu.items`:

| Command | Why |
|---|---|
| `KEYS` empty | There is no key to answer with |
| `KEYS` of `//` or `~~` | These run on their own when the menu is entered, before the script ([Auto-Run Commands](menus/menu-system.md#auto-run-commands)) |
| `HIDDEN: true` | Hidden commands are not listed on a menu |
| An `ACS` the caller does not pass | The caller cannot run the command |

Two commands with the same first key are both listed when the caller passes both ACS strings. Both rows answer the same key. The BBS runs the first command in `.CFG` order that has that key anywhere in its `KEYS` and whose ACS the caller passes, as it does when the caller types the key.

### What the script answers

The script answers by calling `v3.prompt.answer(value)` and then ending. A later call replaces an earlier one.

| Answer | What the BBS does |
|---|---|
| A command line, such as `"M"` or `"/SYSOP"` | Runs it as if the caller had typed it at the prompt, so `;` [chains commands](menus/menu-system.md#chaining-commands) and `^P` goes back to the previous menu. The answer is changed to upper case and spaces around it are removed. When the command returns, the menu is shown again and the script runs again |
| `"GOTO:NAME"` | Goes to menu `NAME`, checking that menu's ACS and password on the way in |
| `"LOGOFF"` | Ends the call. The log records `menu prompt script logged the caller off` |
| `""`, or only spaces | The menu shows its own screen and prompt |
| Anything but a string | The menu shows its own screen and prompt. The log records `string script answered the menu prompt with something other than text; reading input` |
| No answer | The menu shows its own screen and prompt |

An answer counts only when the script runs to its end or calls `exit()`.

---

## When the Menu Shows Its Own Screen

In each case below the menu is drawn from its `.ANS` and reads the caller's command at its usual prompt. The full log lines are copied from `data/logs/vision3.log` on a test board, with the `time` field left out; the others give the message only.

| Case | Log line |
|---|---|
| No door with that code in `doors.json` | `{"level":"WARN","msg":"menu script not run, menu shows its screen: no such door","menu":"MSGMENU","door":"LIGHTBAR"}` |
| The door's `type` is not `v3_script` | `{"level":"WARN","msg":"menu script not run, menu shows its screen: door is not a v3_script door","menu":"MAIN","door":"LIGHTBAR","type":"synchronet_js"}` |
| The script file does not exist | `{"level":"WARN","msg":"menu script not run, menu shows its screen: script file not found","menu":"MSGMENU","door":"LIGHTBAR","script":"missing.js"}` |
| The door's `acs` or `min_access_level` refuses the caller | None |
| The script throws an error it does not catch | `{"level":"ERROR","msg":"V3 script error","node":1,"door":"LIGHTBAR","error":"script error: Error: boom at /dev/shm/smenu-board/scripts/smenu-throw.js:1:44(8)"}` then `{"level":"ERROR","msg":"menu script failed, menu shows its screen","menu":"MSGMENU","door":"LIGHTBAR","node":1,"error":"script error: Error: boom at /dev/shm/smenu-board/scripts/smenu-throw.js:1:44(8)"}` |
| The script ends without an answer | None beyond `V3 script completed normally` |
| The script answers `""` | None beyond `V3 script completed normally` |
| The script answers `GOTO:` a menu that does not exist | `{"level":"WARN","msg":"menu prompt script named a menu that cannot be loaded; reading input","node":1,"menu":"MSGMENU","target":"NOPE","error":"menu file not found: /dev/shm/smenu-board/menus/v3/mnu/NOPE.MNU"}` |
| The script answers `GOTO:` a menu whose `.ANS` cannot be read | `menu prompt script named a menu whose screen cannot be read; reading input` |
| The script answers `GOTO:` a menu whose `ACS` the caller does not pass | The access-denied string is shown first. `menu prompt script named a menu the caller may not enter; reading input` |
| The script reaches its `max_run_minutes` | `V3 script reached its run time limit` |

The three `menu script not run` warnings are written once for each menu, door and reason until the BBS is restarted, so a menu shown on every visit does not fill the log.

These cases end the call or the script instead:

| Case | What happens | Log lines |
|---|---|---|
| The caller hangs up while the script runs | The call ends | `{"level":"INFO","msg":"user disconnected from V3 script","node":1,"door":"LIGHTBAR"}`, `{"level":"INFO","msg":"door ended by the BBS","node":1,"door":"LIGHTBAR","reason":"caller disconnected","doorError":null}`, `{"level":"INFO","msg":"user disconnected in menu script","node":1,"menu":"MAIN"}` |
| The caller is idle for their idle timeout while the script runs | The idle notice is shown and the call ends | `door ended by the BBS` with `"reason":"caller idle"`, then `idle timeout, disconnecting` |
| The caller's time runs out while the script runs | The time-limit notice is shown and the call ends | `door ended by the BBS` with `"reason":"time limit reached"`, then `time limit reached, disconnecting` |
| The caller's time ran out before the menu was shown | The call ends before the script would run | `time limit reached, disconnecting` |

A script that answers a command line matching no command gets one of two results. When the menu's `FALLBACK` names another menu, the caller goes there, as for a typed command that matches nothing, and the log records `no command match, using fallback menu`. When `FALLBACK` is empty or names the menu itself, as it does for the stock `MAIN`, the answer is not used: the menu shows its own screen and prompt, and the script runs again only when the menu is next shown, after the caller has entered something at that prompt. The log records this once for each menu and door until the BBS is restarted:

```text
{"level":"WARN","msg":"menu script answer matched no command, menu shows its screen","menu":"MAIN","door":"NOMATCH","answer":"ZZZ"}
```

---

## One Script for Many Menus

Any number of menus can name the same door in `SCRIPT`. `v3.menu.items` holds the commands of the menu being shown, so one script draws each of them with its own commands.

The script is not told which menu it is drawing. To give each menu a title, define one door per menu, each with the same `script` and its own `args`, and read the title from `v3.args`:

```json
{ "code": "LB_MAIN", "name": "Main Menu Lightbar", "type": "v3_script",
  "script": "menu-lightbar.js", "working_directory": "scripts/examples",
  "args": ["Main Menu"], "hidden": true },
{ "code": "LB_MSG", "name": "Message Menu Lightbar", "type": "v3_script",
  "script": "menu-lightbar.js", "working_directory": "scripts/examples",
  "args": ["Message Menu"], "hidden": true }
```

---

## Scripted and Plain Menus in One Theme

`SCRIPT` belongs to one `.MNU`, so a theme can script some menus and leave the rest to their `.ANS` files. A `GOTO:` between the two kinds works as between any two menus. To try a scripted menu on a running board, copy one `.MNU` into `menus.d/`, add `SCRIPT` there, and delete the copy to go back.

---

## Worked Example: menu-lightbar.js

`scripts/examples/menu-lightbar.js` draws any menu as a lightbar list. Up and Down move the bar, Home and End jump to the first and last command, Enter picks the command under the bar, and pressing a command's key picks it at once.

To use it, add the door:

```json
{
    "code": "LIGHTBAR",
    "name": "Lightbar Menu",
    "type": "v3_script",
    "script": "menu-lightbar.js",
    "working_directory": "scripts/examples",
    "hidden": true
}
```

and set `"SCRIPT": "LIGHTBAR"` in the `.MNU` of each menu to draw with it. On the stock `MAIN` menu the first rows look like this:

```text
 Menu  Up/Down, Enter or a key

   B  Browsing BBS List
   J  Reading News
   C  Chatting
   E  Email Menu
```

The script, in order:

```javascript
// v3.menu exists only for a menu's SCRIPT, so check the kind first.
if (v3.prompt.kind !== "menu" || v3.menu.items.length === 0) {
    exit(); // no answer: the menu shows its own screen and prompt
}
var items = v3.menu.items;
```

Run as an ordinary door, `v3.prompt.kind` is `""` and the script has no `v3.menu`, so the kind is checked first. A menu with no commands the caller may choose has nothing to list. In both cases the script ends without an answer; on a menu, the menu then shows its own screen and prompt.

```javascript
var title = v3.args.length > 0 ? v3.args[0] : "Menu";
var rows = Math.max(1, v3.console.height - 4); // list rows that fit
var sel = 0;
var top = 0;
```

The title comes from the door's `args`. `rows` is how many commands fit on the caller's screen below the title. `sel` is the command under the bar and `top` the first command on screen.

```javascript
function drawRow(i) {
    var it = items[i];
    var text = v3.util.padRight((" " + it.key + "  " + it.label).substring(0, 34), 34);
    v3.console.gotoxy(3, 3 + i - top);
    v3.console.print(i === sel ? "|B4|15" + text : "|B0|07" + text);
    v3.console.print("|B0|07");
}
```

`drawRow` draws one command on its row, cut or padded to 34 columns: white on blue under the bar, grey on black otherwise.

```javascript
function drawAll() {
    v3.console.clear();
    v3.console.println("|11 " + title + "  |08Up/Down, Enter or a key");
    for (var i = top; i < items.length && i < top + rows; i++) {
        drawRow(i);
    }
}
```

`drawAll` clears the screen and draws the title and one screenful of commands from `top`.

```javascript
function choose(i) {
    v3.console.gotoxy(1, 3 + Math.min(rows, items.length - top));
    v3.prompt.answer(items[i].key);
}
```

`choose` moves the cursor to the line below the list, so whatever the command prints starts there, and answers with the command's key.

```javascript
drawAll();
for (;;) {
    var k = v3.console.readkey();
    var old = sel;
    if (k === "UP" && sel > 0) sel--;
    else if (k === "DOWN" && sel < items.length - 1) sel++;
    else if (k === "HOME") sel = 0;
    else if (k === "END") sel = items.length - 1;
    else if (k === "\r") { choose(sel); break; }
    else if (k.length === 1) {
        var hit = -1;
        for (var j = 0; j < items.length; j++) {
            if (items[j].key.toUpperCase() === k.toUpperCase()) { hit = j; break; }
        }
        if (hit >= 0) { choose(hit); break; }
    }
```

`readkey()` returns arrow, Home and End keys by name ([Reading special keys](scripting/vpl-scripting.md#reading-special-keys)). A typed character picks the command whose key it is, in either case. Only one-character keys can be picked this way; a key such as `/SYSOP` is picked with the bar. Other keys are ignored. A `##` command, which takes a number typed after it, is listed with the key `##`; this script answers `##` for it with no number.

```javascript
    if (sel === old) continue;
    if (sel < top || sel >= top + rows) { // scrolled off: redraw the page
        top = sel < top ? sel : sel - rows + 1;
        drawAll();
    } else {
        drawRow(old);
        drawRow(sel);
    }
}
```

When the bar moves within the screen, only the two rows that changed are redrawn. When it moves past the top or bottom, the list scrolls and the screen is redrawn.

After the command runs, the BBS shows the menu again and the script starts from the top with the bar on the first command.

---

## Limits

- The menu's prompt, its `.BAR` lightbar and its `CLR`, `USEPROMPT`, `PROMPT1`, `PROMPT2` and `FORCEHOTKEY` settings are not used while the script runs. They apply whenever the menu shows its own screen.
- The time-left warning the menu prompt shows in a caller's last five minutes is not drawn. A script can show `v3.session.timeLeft` itself.
- The script is not told the menu's name or title. Use `args`, as in [One Script for Many Menus](#one-script-for-many-menus).
- The script runs again from the start each time the menu is shown. Keep anything it must remember between showings in [`v3.data`](scripting/vpl-scripting.md#v3data).
- Menus shown before login do not run a script: `SCRIPT` is used only once the caller has logged in.

Scripts kept in a theme's own folder, pictures drawn with `v3.gfx`, and toasts drawn inside a scripted menu are not part of this feature; they are separate work.
