# Generated door menus

`DOORMENU` builds an interactive menu from `doors.json`. It is opt-in: existing
`DOORSM` art and CFG entries using `DOOR:CODE` continue to work unchanged.

Add a command to any menu's CFG:

```json
{"KEYS":"D", "COMMAND":"RUN:DOORMENU", "ACS":""}
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
  "doorCategories": [
    {
      "code": "GAMES",
      "name": "Games",
      "description": "Games for callers",
      "min_access_level": 10,
      "acs": "",
      "sort_order": 0,
      "sort": "name"
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

Reload configuration through the board's existing configuration reload flow
(or restart) after editing JSON. No per-door CFG entry or art edit is needed.

In the config editor:

- **System Setup → Default Settings** sets the menu mode and default sort.
- **C — Door Categories** adds, edits, and deletes categories. Renaming updates
  door references; deleting moves those doors to Other.
- **7 — Door Programs** includes a category picker, description, hidden flag,
  and manual sort order. Saving preserves the original configuration order.

## Input

Both modes accept the displayed number or a door/category code followed by
Enter. Numbers are global across pages. `[` / `]` or PgUp/PgDn change pages;
Home/End jump to the beginning/end. Q or Esc goes back. Q is reserved when the
input is empty; select a code starting with Q using its displayed number.

Lightbar mode also accepts Up/Down and Enter to launch the highlighted entry.
List mode waits for a typed selection followed by Enter. There is no per-user
mode override in this version.

## Menu art and prompts

Customize files in `menus.d/v3/` to preserve changes across upgrades. Each
file resolves through the usual menu-set overlay before shipped files.

- `mnu/DOORMENU.MNU`: `TITLE`, `PROMPT1`, `PROMPT2`, `USEPROMPT`, `CLR`/`CLS`,
  `ACS`, and access-denied `FALLBACK`. Prompts use the normal MCI pipeline.
- `bar/DOORMENUHI.BAR`: the first record supplies highlight and regular colors;
  coordinates are ignored. Without this file, theme highlight and row colors
  are used.
- `templates/DOORMENU.TOP`, `.MID`, `.BOT`: door header, repeated row, footer.
- `templates/DOORMENU_GAMES.TOP`, `.MID`, `.BOT`: optional per-category art.
  Each missing part independently falls back to the generic file.
- `templates/DOORCAT.TOP`, `.MID`, `.BOT`: category picker art.

Template filenames may also have `.ANS` or `.ans` suffixes. SAUCE metadata is
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

Multi-column layout, per-door custom hotkeys, dimmed locked entries, favourites,
search, and launch statistics are not part of this version.
