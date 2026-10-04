# Generated door menus

`DOORMENU` builds an interactive menu from `doors.json`. It is opt-in: existing
`DOORSM` art and CFG entries using `DOOR:CODE` continue to work unchanged.

Add a command to any menu's CFG:

```json
{"KEYS":"D", "CMD":"RUN:DOORMENU", "ACS":""}
```

Use `RUN:DOORMENU GAMES` to open a category directly. `RUN:DOORMENU:GAMES` and
`DOORMENU:GAMES` also work as CFG commands. Direct entry checks category access.
Q/Esc exits a direct category menu; when entered through the category picker,
it returns to the category list. Returning from a door keeps that door selected
and recalculates the page for the current terminal size.

## Configuration

In `config.json`:

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

`doorMenuMode` is `lightbar` (also the default when absent) or `list`.
`doorMenuSort` is `name` (default), `code`, `config` (original order in
`doors.json`), or `manual` (ascending `sort_order`, then code). A category's
`sort` overrides that default. Categories sort by `sort_order`, then name and
code. Category codes use the same 1–16 character uppercase slug rules as door
codes; `OTHER` is reserved.

`doorMenuColumns` lays the list out in 1 to 4 columns (default 1), and a
category's `columns` overrides it for that category's door list; the category
picker uses the global setting. Entries are numbered down each column, and a
page that is not full is split evenly across the columns. A terminal too
narrow for the setting gets as many columns as fit, at least 20 characters
each, and a row template that spans more than one line always gets one
column.

A door can have these additional fields:

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

These fields supplement the door's existing launch configuration. A door has
one category. Doors without a category, or referencing an unknown category,
appear under **Other**. With no category definitions the menu is flat.
Inaccessible doors and categories are omitted; empty categories are omitted.
`hidden` hides a door only from the generated picker; explicit `DOOR:CODE`
commands still work. Category access governs this picker, while the normal
per-door minimum access still governs direct launches.

No restart is needed. The board checks `config.json` and `doors.json` every
couple of seconds and reloads them, even with callers online, and the menu
reads its settings, `DOORMENU.MNU` and its templates each time it draws. No
per-door CFG entry or art edit is needed.

In the config editor:

- **System Setup → Default Settings** sets the menu mode, default sort and
  column count.
- **C — Door Categories** adds, edits, and deletes categories, including a
  per-category column count. Renaming updates
  door references; deleting moves those doors to Other.
- **7 — Door Programs** includes a category picker, description, hidden flag,
  and manual sort order. Saving preserves the original configuration order.

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

Row placeholders are `^ID` (number), `^CO` (code), `^NA` (name), `^TY` (door
runtime type), `^DS` (description), and `^CN` (current category name).
`^TI` is the MNU title; `^PG` and `^PT` are current/total pages. Page and title
placeholders work in TOP/MID/BOT; common menu template tokens also work.
When every entry fits on one page, any TOP or BOT line containing `^PG` or
`^PT` is left out, so paging help appears only when there is more than one
page.
Category rows use `^ID`, `^CO`, `^NA`, and `^DS`.

The string table supplies `doorMenuEmpty`, `doorMenuDenied`, and
`doorMenuOther`. Existing installations get defaults for missing entries.

The shipped menu set includes generic art and an `EXAMPLES` category header.
The default configuration puts the Hello World door in that category. The
stock hand-drawn Doors menu is unchanged; bind `RUN:DOORMENU` to use the picker.

Per-door custom hotkeys, dimmed locked entries, favourites,
search, and launch statistics are not part of this version.
