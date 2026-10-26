# Generated door menus

The generated door menu draws a door list straight from `doors.json`: every
door the caller may run, grouped into the categories you define, numbered,
paged to fit their terminal and picked with a lightbar or a typed number.
Adding a door means adding the record, not redrawing a screen. It is opt-in:
the stock hand-drawn `DOORSM` menu and its `DOOR:CODE` entries keep working,
and a board with a handful of doors may never need this. A board with twenty
does.

## Turning it on

Bind the `DOORMENU` command to a key on any menu. Run `./menuedit`, open the
menu (`MAIN` or `DOORSM`), press **F5** to add a command, and set **Keys** to
the letter, **Command** to `RUN:DOORMENU` and **ACS** to `*`. The editor saves
into the `menus.d/v3/` overlay. That is all a flat menu needs: with no
categories defined, every door the caller may see is listed in one page set.

`RUN:DOORMENU GAMES` opens the `GAMES` category directly instead of the
category picker; `RUN:DOORMENU:GAMES` and `DOORMENU:GAMES` are the same
command. Direct entry checks the category's access. **Q** or **Esc** exits a
direct category menu; when entered through the category picker, it returns to
the category list. Returning from a door keeps that door selected and
recalculates the page for the current terminal size.

## Setting it up in the config editor

Run `./config`. Three places matter:

- **System Setup → Default Settings** has **Door Menu Mode** (`lightbar` or
  `list`), **Door Menu Sort** (the default ordering) and **Door Menu Cols**
  (1 to 4 columns).
- **C — Door Categories** adds, edits and deletes categories. Each has a
  **Code**, **Name**, **Description**, **Min Access Level**, **ACS**, **Sort
  Order**, a **Door Sort** that overrides the default inside that category,
  and a **Columns** count that overrides the global one for its door list.
  Renaming a category updates the doors that reference it; deleting one moves
  those doors to Other.
- **7 — Door Programs** has, on every door, a **Category** picker, a
  **Description** (shown by the `^DS` row placeholder), a **Hidden** flag and a
  manual **Sort Order**. Saving preserves the original order of `doors.json`.

A door has one category. Doors without a category, or referencing an unknown
one, appear under **Other**. Inaccessible doors and categories are omitted, as
are empty categories. **Hidden** hides a door only from the generated picker;
explicit `DOOR:CODE` commands still work. Category access governs this picker,
while the normal per-door minimum access still governs direct launches.

No restart is needed. The board checks `config.json` and `doors.json` every
couple of seconds and reloads them, even with callers online, and the menu
reads its settings, `DOORMENU.MNU` and its templates each time it draws.

## Sorting and columns

The default sort is `name`; the alternatives are `code`, `config` (the order
in `doors.json`) and `manual` (ascending **Sort Order**, then code). A
category's own **Door Sort** overrides the default. Categories themselves sort
by **Sort Order**, then name and code. Category codes use the same 1–16
character uppercase slug rules as door codes; `OTHER` is reserved.

**Door Menu Cols** lays the list out in 1 to 4 columns (default 1), and a
category's **Columns** overrides it for that category's door list; the
category picker uses the global setting. Entries are numbered down each
column, and a page that is not full is split evenly across the columns. A
terminal too narrow for the setting gets as many columns as fit, at least 19
characters each, and a row template that spans more than one line always gets
one column.

## Input

Both modes accept the displayed number or a door/category code followed by
Enter. Numbers are global across pages. `[` / `]` or PgUp/PgDn change pages;
Home/End jump to the beginning/end. Q or Esc goes back. Q is reserved when the
input is empty; select a code starting with Q using its displayed number.

Lightbar mode also accepts Up/Down and Enter to launch the highlighted entry,
and Left/Right to move between columns. Moving within a page repaints only the
entries involved when the MNU clears the screen (`CLR`); with `CLR` off, or
with multi-line rows, each move redraws the page.
List mode waits for a typed selection followed by Enter. There is no per-user
mode override in this version.

## Menu art and prompts

Customize files in `menus.d/v3/` to preserve changes across upgrades. Each
file resolves through the usual menu-set overlay before shipped files.

- `mnu/DOORMENU.MNU`: `TITLE`, `PROMPT1`, `PROMPT2`, `USEPROMPT`, `CLR`/`CLS`,
  `ACS`, and access-denied `FALLBACK`. Prompts use the normal MCI pipeline.
  `menuedit` lists it with the other menus and edits these fields, saving to
  the overlay. It has no `.CFG`: the entries come from `doors.json`, so
  commands added under F10 are never read.
- `bar/DOORMENUHI.BAR` (optional, not shipped): the first record supplies
  highlight and regular colors for this menu only; coordinates are ignored.
  Without it the highlight is the theme's `yesNoHighlightColor`, as in the
  other lightbars, and rows keep their template colors.
- `templates/DOORMENU.TOP`, `.MID`, `.BOT`: door header, repeated row, footer,
  used for any list without its own per-category art, including **Other** and
  a flat menu.
- `templates/DOORMENU_GAMES.TOP`, `.MID`, `.BOT`: optional per-category art.
  Each missing part independently falls back to the generic file.
- `templates/DOORCAT.TOP`, `.MID`, `.BOT`: category picker art. The shipped
  row shows the number and name; a category's code is not shown but can
  still be typed.
- `templates/DOORMENU.HDR`, `.CHD` (and `DOORMENU_GAMES.*`, `DOORCAT.*`): the
  column heading drawn between the header and the rows, so it matches the
  layout. `.HDR` is used above a one-column list; the first line of `.CHD` is
  repeated over each column of a column layout and cut to the column width.
  Both are optional, and are left out when the list is empty.
- `templates/DOORMENU.COL`, `DOORMENU_GAMES.COL`, `DOORCAT.COL`: the row used
  in a column layout, falling back to `.MID`. Each cell is cut and padded to
  its column, so keep it narrow; the shipped one shows the number and name.
  Keep column titles out of TOP, which is the same in every layout, and put
  them in `.HDR` and `.CHD` instead.

Template filenames may also have `.ANS` or `.ans` suffixes. To override a
shipped template, give the overlay copy the same name: an overlay
`DOORMENU.TOP` replaces the shipped `DOORMENU.TOP`, but an overlay
`DOORMENU.TOP.ans` does not (issue #612). Per-category templates are the
exception and prefer the overlay under any suffix. SAUCE metadata is
removed through the shared template reader. Keep these templates as flowing
rows; absolute cursor positioning is unsuitable for a generated list. Avoid
filling the final terminal column to prevent terminal-dependent autowrap.
Header/footer and expanded row heights determine how many entries fit. Very
small screens or art taller than the screen still show at least one entry.

The shipped menu set includes generic art and an `EXAMPLES` category header.
The default configuration puts the Hello World door in that category. The
stock hand-drawn Doors menu is unchanged; bind `RUN:DOORMENU` to use the picker.

Per-door custom hotkeys, dimmed locked entries, favourites,
search, and launch statistics are not part of this version.

## Technical Reference

### config.json

The menu's global settings and the category list live in `config.json`:

```json
{
  "doorMenuMode": "lightbar",
  "doorMenuSort": "name",
  "doorMenuColumns": 1,
  "doorCategories": [
    {
      "code": "GAMES",
      "name": "Games",
      "description": "Games for callers",
      "min_access_level": 10,
      "acs": "",
      "sort_order": 0,
      "sort": "name",
      "columns": 2
    }
  ]
}
```

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `doorMenuMode` | string | `lightbar` | `lightbar` or `list` |
| `doorMenuSort` | string | `name` | Default door ordering: `name`, `code`, `config` (order in `doors.json`) or `manual` (ascending `sort_order`, then code) |
| `doorMenuColumns` | int | `1` | Columns in a door list and the category picker, 1 to 4 |
| `doorCategories` | array | none | The categories, in the shape below. With none defined the menu is flat |

Each entry of `doorCategories`:

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `code` | string | required | 1–16 characters, A-Z, 0-9, `_`, `-`; stored upper case. `OTHER` is reserved and codes must be unique |
| `name` | string | required | Shown by `^NA` in the category picker |
| `description` | string | blank | Shown by `^DS` |
| `min_access_level` | int | `0` | Minimum access level to see the category |
| `acs` | string | blank | ACS string the caller must pass to see the category |
| `sort_order` | int | `0` | Category position in the picker (lower comes first), then name and code |
| `sort` | string | blank (global) | Door ordering inside this category; same values as `doorMenuSort` |
| `columns` | int | `0` (global) | Columns for this category's door list, 1 to 4 |

An invalid value in a hand-edited file does not stop the BBS from starting.
At load, a bad mode, sort or column count falls back to its default; a
category with a bad, reserved or repeated code is dropped; and a door whose
`category` is not a valid code is listed under Other. Each repair is logged
as a warning. The config editor still refuses to save invalid values.

### doors.json

A door can have these additional fields, which supplement its launch
configuration and are documented with the other keys in the
[Door Programs reference](doors/doors.md#generated-menu-fields):

```json
{
  "code": "LORD",
  "name": "Legend of the Red Dragon",
  "category": "GAMES",
  "description": "A fantasy adventure",
  "sort_order": 10,
  "hidden": false
}
```

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `category` | string | blank (Other) | Code of one of `doorCategories` |
| `description` | string | blank | Shown by `^DS` |
| `sort_order` | int | `0` | Position under the `manual` sort |
| `hidden` | bool | `false` | Left out of the generated menu; `DOOR:CODE` still works |

### Menu commands

| CFG command | Effect |
| --- | --- |
| `RUN:DOORMENU` | Opens the category picker, or the flat list when no categories are defined |
| `RUN:DOORMENU GAMES`, `RUN:DOORMENU:GAMES`, `DOORMENU:GAMES` | Opens the `GAMES` category directly; the caller must pass its access |

A hand-written entry in a menu's CFG file looks like:

```json
{"KEYS":"D", "CMD":"RUN:DOORMENU", "ACS":""}
```

### Template placeholders

Row placeholders are `^ID` (number), `^CO` (code), `^NA` (name), `^TY` (door
runtime type), `^DS` (description), and `^CN` (current category name).
`^TI` is the MNU title; `^PG` and `^PT` are current/total pages. Page and title
placeholders work in TOP/MID/BOT; common menu template tokens also work.
When every entry fits on one page, any TOP or BOT line containing `^PG` or
`^PT` is left out, so paging help appears only when there is more than one
page.
Category rows use `^ID`, `^CO`, `^NA`, and `^DS`.

### Strings

The string table supplies `doorMenuEmpty` (no doors to show), `doorMenuDenied`
(the caller may not see the menu or category) and `doorMenuOther` (the name of
the **Other** category). Existing installations get defaults for missing
entries.
