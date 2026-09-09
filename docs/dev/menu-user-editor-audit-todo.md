# Menu editor and user editor TUI audit

Created: 2026-09-08. Status: complete. All four sections shipped in
[#254](https://github.com/ViSiON-3/vision-3-bbs/pull/254) and
[#255](https://github.com/ViSiON-3/vision-3-bbs/pull/255), with the manual
visual pass and the golden captures that followed it closing the record.

Related issues:

- [#252: ./menuedit TUI should follow same base layout/behavior as ./config TUI](https://github.com/ViSiON-3/vision-3-bbs/issues/252)
- [#242: ./ue TUI should align with base UI settings with ./config and ./strings, and use arrow keys to move between fields](https://github.com/ViSiON-3/vision-3-bbs/issues/242)

Prior art: the `./strings` and `./config` audit,
[`string-editor-audit-todo.md`](string-editor-audit-todo.md), shipped in
[#241](https://github.com/ViSiON-3/vision-3-bbs/pull/241) and
[#243](https://github.com/ViSiON-3/vision-3-bbs/pull/243). That audit extracted
the shared chrome into **`internal/tuiart`** and established `./config` as the
reference. This audit finishes the job for the two editors it did not cover.

## Objective

Bring `./menuedit` and `./ue` onto the same visual baseline as `./config` and
`./strings`, and give `./ue`'s two-column edit screen the arrow-key navigation
its layout already implies.

The scope is deliberately narrow: adopt the existing `internal/tuiart` chrome
and fix the geometry defects that adopting it exposes. This is not a licence to
restyle either editor. Both are faithful recreations of `MENUEDIT.PAS` and
`UE.PAS`, and their screen-specific colors, box characters, and column layouts
are intentional and stay as they are.

`./config` is the reference to inspect, not an assumption of correctness — the
same standard the prior audit applied to it, which is how its own geometry came
to be probed.

## Shipping plan

Two PRs, one per issue, in this order. The `tuiart` adoption is nearly identical
in both editors, so the second rebases cleanly on the first.

1. **#252 — `./menuedit`**: adopt `internal/tuiart`, fix the vertical geometry
   defects, add the geometry probe and golden captures.
2. **#242 — `./ue`**: the same chrome adoption, plus two-dimensional field
   navigation on the edit screen.

## Confirmed findings

Established by static reading plus a throwaway geometry probe modelled on
`internal/configeditor/view_geometry_test.go`, run over both editors at 80×25,
100×30, 120×45, 160×60, 200×100 and an undersized 60×15. The probe drove real
keystrokes into each screen rather than assigning `m.mode` directly; assigning
the mode leaves `textInput.Width` unset and produces width failures that are
artifacts of the probe. Two of the first pass's hits were exactly that. Note the
inverse trap as well: the first probe never entered the field-edit modes at all,
and so missed a real one-column overflow there. See the section 2 results.

### Shared chrome not adopted (both editors)

- Both editors define their own `dosColors` as **ANSI 256-color indices**
  (`internal/menueditor/colors.go:8`, `internal/usereditor/colors.go:10`) —
  `"4"` for blue, `"6"` for cyan, and so on. `internal/tuiart/colors.go` exists
  specifically to replace that: it pins the canonical VGA hex values because
  most terminal themes map ANSI blue to a bright, low-contrast shade that
  washes out the white-and-yellow-on-blue UI. `./config` and `./strings` render
  the authentic navy `#0000AA`; these two do not, so the four binaries do not
  match on a stock terminal.
- Neither editor paints backdrop art. `./config` composites a randomly chosen
  embedded `.ANS` screen, centered, behind everything via `tuiart.Backdrop`;
  both editors instead fill flat `░` (`internal/menueditor/view.go:23`,
  `internal/usereditor/view.go:38`). `internal/tuiart/assets/TUIUSEREDIT.ANS`
  already ships — art named for the user editor that only `./config` displays.
- Title and help bars are rebuilt by hand rather than taken from
  `tuiart.HeaderBarStyle` / `tuiart.HelpBarStyle`. `./ue` carries a third
  near-duplicate in `editTitleStyle`.
- `centerText` and `padRight` are duplicated locally in both
  (`internal/menueditor/fields.go:173,178`; `internal/usereditor/fields.go:314`
  and `view.go:343`) instead of `tuiart.CenterText` / `tuiart.PadRight`. The
  shared versions truncate before centering, so an over-long title is cut to
  the column budget rather than pushing a border out.
- Neither package has a geometry test or a `testdata` directory. `./config` and
  `./strings` both prove every row is exactly terminal-width at six sizes, and
  `./strings` archives golden captures.

### `./menuedit` geometry defects

Each screen hand-rolls its own vertical arithmetic from magic constants, and
three of them are wrong. `./config` derives padding from a single declared
`fixedRows` passed to `newListBox` (`internal/configeditor/view_list_box.go:24`,
`view_list.go:24`), which is why it has no equivalent defects.

- **The menu edit screen emits one row too many at every size.** 26 rows in an
  80×25 terminal, 101 in a 200×100 one. The last row is pushed off-screen and
  the terminal scrolls. `internal/menueditor/view_menu_edit.go:34-36` computes
  `extraV` against a fixed-content count of 26 and then floors `bottomPad` at 1.
- **The command edit screen has the same defect**, from the same shape of
  arithmetic against a different constant
  (`internal/menueditor/view_command_edit.go:42-44`).
- **The command list emits one row too few on any terminal taller than 25.** 29
  rows at 100×30, 44 at 120×45, 99 at 200×100, leaving an unpainted row at the
  bottom. It is correct only at 80×25 and below.
  `internal/menueditor/view_command_list.go:35` computes `extraV` from
  `m.height-24` while the screen's fixed content is 25 rows.

### `./ue` findings

- **Geometry is sound.** Every row of every screen is exactly the terminal width
  and every screen is exactly the terminal height, across all six sizes and all
  nine states probed: list, edit, field edit, delete confirm, validate, save-on-
  leave, key list, password entry, and info alert. No changes needed here beyond
  what the backdrop adoption itself requires.
- **Field navigation ignores the column layout.** `fieldDef` already carries an
  explicit `Col` (3 for the left column, 50 for the right) and `Row`
  (`internal/usereditor/fields.go:31-32`), and `editFields()` assigns both for
  every field. But `internal/usereditor/model.go:451-455` maps Down and Up to
  `nextEditableField(±1)`, a linear walk of the field slice, and Left and Right
  are unhandled in edit mode. Reaching `Validated` (Col 50, Row 4) from `Handle`
  (Col 3, Row 4) takes eleven presses of Down through the whole left column.
  This is #242's second item. The geometry needed to fix it is already in the
  data; no layout change is required.
- **Two different approaches to measuring `textinput.View()` coexist.**
  `renderField` assumes the widget renders `Width+1` cells
  (`internal/usereditor/view_edit.go:222-228`), while `overlayPasswordDialog`
  measures the actual visible width because "textinput.View() width varies"
  (`view_edit.go:273-276`). Both are correct today. Worth reconciling on the
  measured approach while the file is open, since the assumption is the kind
  that breaks silently on a dependency bump.

### Already consistent across all four binaries

Recorded so a later reader does not re-audit them:

- Terminal lifecycle: all four construct with
  `tea.NewProgram(model, tea.WithAltScreen(), tea.WithInputTTY())`.
- An 80×25 minimum with clamping below it, applied identically in every
  `tea.WindowSizeMsg` handler.
- Fixed list heights. `./config`'s `recordListVisible()` returns a hard 13
  (`internal/configeditor/update_save.go:155`). Growing a list to fill the
  terminal was a `./strings`-specific fix, **not** part of the baseline, so
  neither editor should adopt it here.

## 1. Adopt the shared chrome

Done for both editors.

- [x] Replace both local `dosColors` / `dosBgColors` tables and their
  `dosStyle` / `dosColor` constructors with aliases onto `internal/tuiart`,
  following the pattern in `internal/configeditor/colors.go:13-20`. Keep every
  screen-specific style variable and its `MENUEDIT.PAS` / `UE.PAS` provenance
  comment; only the palette underneath changes.
- [x] Alias the backdrop the way `internal/configeditor/backdrop.go` does, load
  it in each model's constructor and resize handler, and source the background
  fill from `backdrop.Segment(row, col, width)` instead of
  `strings.Repeat("░", …)`.
- [x] Judge, per screen, whether the art or `tuiart.Shaded` is right, because
  the art is 80 columns wide and centered, so a centered box occludes all but a
  sliver of it. This is not a defect: `./config` itself caps every box at 70
  columns (72 with borders) and therefore never shows more than 4 columns of art
  on each side, plus the rows above and below the box — where most of the
  picture actually reads. The two editors' boxes are wider, so the vertical
  sliver is thinner: 2 columns each side for `./menuedit`'s 74-column edit boxes
  and 1 column for `./ue`'s 76-column edit box. Their list screens (50 and 60
  columns) are well clear. Check on the manual pass whether a 1-column sliver
  frames or just reads as a stray line; if it does not read as framing, use
  `tuiart.Shaded` for that screen alone, as `./strings` does.
- [x] Point the title and help bars at `tuiart.HeaderBarStyle` and
  `tuiart.HelpBarStyle`; collapse `./ue`'s `editTitleStyle` onto the same style.
- [x] Delete the local `centerText` / `padRight` and delegate to
  `tuiart.CenterText` / `tuiart.PadRight`. `internal/usereditor/view_test.go`
  already pins the current behavior of `centerText`, including the rune-safe
  truncation case — keep those assertions passing.

## 2. Fix ./menuedit geometry (#252)

Done.

- [x] Rework the three broken screens to derive top and bottom padding from one
  declared fixed-row count, as `newListBox` does, rather than from independent
  magic constants. Do not floor `bottomPad` at 1; that floor is what turns a
  miscount into an overflow instead of a visible gap.
- [x] Decide whether the menu editor's list screens should use
  `configeditor.listBox` directly. It currently lives in `internal/configeditor`
  and is documented as keeping each screen's exact byte output. Extract it to a
  shared package **only** if both editors genuinely need it; the prior audit's
  standing instruction is to share chrome, not to rewrite editors.

### Results

All three row-count defects are fixed, and the cause of the first two turned out
to be more specific than "magic constants". `menuFields()` returns **13** fields,
but `model.go` declared `menuEditFields = 7` and the menu edit screen's box
height was written for that smaller list. The field list grew; the hardcoded
geometry did not follow. Both `menuEditFields` and `cmdEditFields` were dead
code — nothing read either one — so they are deleted and both edit screens now
size their box from `len(m.menuFields)` / `len(m.cmdFields)`.

The shared scaffold landed as **`tuiart.Screen`** rather than by extracting
`configeditor.listBox`: `listBox` also owns that editor's box styling, and
moving it would put its byte-identical golden captures at risk for no gain.
`tuiart.Screen` carries only the parts both remaining editors need — row-tracked
background fill and `Split`, which derives both paddings from one declared
fixed-row count and deliberately does **not** floor either at 1. That floor is
what turned a miscount into an off-screen overflow instead of a visible gap.

A fourth defect surfaced only once the geometry test entered the field-edit
modes, which the initial probe never did: the actively-edited field row was one
cell too wide on both edit screens, because the code padded against
`m.textInput.Width` while `textinput.View()` renders a cursor cell after the
text. Both sites now measure `uitext.ApproximateVisibleLen(m.textInput.View())`
instead — the approach `./ue`'s password dialog already used, and the one the
`./ue` finding above recommends adopting there too.

Adopting the backdrop also exposed that the two confirm/input dialog overlays
rebuilt the row to the right of the dialog as flat `░` fill, which erases the
art from those rows. They now preserve both sides with the `padToCol` /
`skipToCol` pair, which `./config` and this editor's own help overlay already
used; `skipToCol` was defined in `view.go` but had no callers.

## 3. Two-dimensional field navigation in ./ue (#242)

- [x] Move Up and Down to walk within the current column by `Row`, and map Left
  and Right to move to the nearest editable field in the adjacent column,
  preserving the row where one exists there.
- [x] Decide and document the edge behavior: what Up does on the top row, what
  Right does from the right column, and whether the read-only footer block
  (rows 18-21, `Type: ftDisplay`) participates. `nextEditableField` currently
  wraps and skips `ftDisplay`; keep skipping, and state the wrap rule.
- [x] Keep Tab, Enter, `ctrl+home`, and `ctrl+end` behaving as they do now. Tab
  and Enter advance in field order and are the existing muscle memory; the
  arrow keys are additive.
- [x] Update the help bar only if a key's meaning changes. No key changed
  meaning, so the bar is untouched.

### Results

Both items shipped. Up and Down now walk `Row` within the field's own column
and wrap at that column's ends; Left and Right move to the other column, landing
on the editable field whose row is nearest. With only two columns, Left in the
left column and Right in the right hold position rather than wrapping around —
arrow keys read as spatial movement, and teleporting across the screen from an
edge would not. `ftDisplay` fields stay unreachable, as before.

Tab, Enter, `ctrl+home` and `ctrl+end` are untouched: they still advance in
field order, which is the existing muscle memory. The arrow keys are additive,
so no help-bar text changed.

`columnOf` treats anything that is not `Col == 50` as the left column, so a
field added with an unexpected `Col` still navigates rather than silently
becoming unreachable.

Two incidental findings while in the file:

- `renderField` now measures `textinput.View()` rather than assuming `Width+1`,
  reconciling it with `overlayPasswordDialog` as this audit recommended. The
  same assumption was a live one-column overflow in `./menuedit`.
- **`modeSearch` was unreachable, and is now finished.** Nothing assigned it, so
  the search prompt row in `view.go` was dead code even though `updateSearch`,
  the configured `searchInput`, and the prompt row itself were all complete. The
  only missing piece was an entry point. `/` now opens it — the same key
  `./strings` binds, for the same forward-and-wrap search — so this was six
  lines to finish rather than a feature to remove.

  Three things came with it. A miss now says so: `updateSearch` returned to the
  list silently when nothing matched, which reads as the key not having worked.
  The view tests `modeSearch` before `m.message` because both draw on the same
  row, and a flash message left from a previous action would otherwise hide the
  prompt the user just opened. And the help overlay's `dialogH` is now
  `len(helpLines)` rather than a hand-maintained `19`, because adding the line
  advertising `/` would otherwise have left the box off-centre — the same defect
  that had `./menuedit`'s menu edit screen sized for seven fields after the list
  grew to thirteen.

  `./strings` shared the silent-miss gap — its `updateSearch` is near-identical,
  including the absent not-found branch — and is fixed alongside. It needed
  nothing else: it already switches on `m.mode` before `m.message`, so its
  prompt was never maskable. Its sysop guide described `/` as "search/filter
  strings by name", which was wrong twice over: it searches name, key **and**
  description, and it jumps the cursor rather than filtering the list.

All of `./ue`'s dialog overlays already preserved the screen behind them with
the `padToCol` / `skipToCol` pair, so adopting the backdrop needed no change
there. `./menuedit`'s two dialogs did not, and were fixed in the first PR.

## 4. Verification

- [x] Add `view_geometry_test.go` to both packages, covering every reachable
  mode at 80×25, 100×30, 120×45, 160×60, 200×100 and 60×15. Drive real
  keystrokes into each mode; assigning `m.mode` directly leaves `textInput`
  unconfigured and reports width failures that do not exist.
- [x] Add golden captures for both editors under `testdata`, following
  `internal/stringeditor/view_golden_test.go`. Deferred until the manual pass
  confirmed the visual result, so they archive a rendering known to be right
  rather than recording churn. 16 captures each, at 80×25 and 120×45.
- [x] Add coverage for the new navigation: each arrow from each column, the
  edges, and that `ftDisplay` fields stay unreachable.
- [x] Run the package tests, the repository suite with race checks, `go vet`,
  formatting checks, and `git diff --check`.
- [x] Perform a manual visual pass over `./menuedit` and `./ue` in a real
  terminal, at small and large sizes, resizing repeatedly. **The automated suite
  does not retire this.** Done by the maintainer; both editors pass. The prior audit's manual pass found two defects
  that every geometry and golden check passed cleanly, because row and column
  arithmetic cannot tell a bar that stops in the right place from one that stops
  a cell late. See
  [`string-editor-audit-todo.md`](string-editor-audit-todo.md#why-the-manual-pass-was-not-redundant).
- [x] Run both editors against disposable copies of configuration. Do not save
  test changes to the live development BBS. The test suites build every model
  over a `t.TempDir()` fixture; nothing reads or writes the live BBS.

### What the captures pin, and what they cannot

Two problems had to be solved that `./strings` did not face.

The backdrop art is **chosen at random on startup**, so an unpinned capture
would record which of the three embedded screens the run happened to draw. The
golden tests pin `tuiart.Arts()[0]`, which is stable because `Arts` sorts by
filename.

The user editor's records carry timestamps. The fixtures set none, so the date
fields render as `Never` and a capture taken today matches one taken next year.
Verified by regenerating and diffing, not by assumption.

Both suites were run with `-count=3` and regenerated twice to confirm the output
is byte-identical each time.

The captures earned their place immediately. `./menuedit`'s help overlay was 50
columns wide and centred, one column inside the 52-column menu list box, so the
list's side borders showed through the dialog and rendered `┌╔` and `║│` pairs —
a broken frame rather than a dialog on a panel. **Every geometry test passed
over it**: the rows were exactly the right width, with the wrong characters in
them. The dialog is now 54 columns, covering the list box outright while still
sitting comfortably inside the wider command list (72) and edit (76) boxes,
where a visible surround is the intended look. `TestNoMixedBorderPairs` in both
packages scans every capture for adjacent single/double box characters so the
class cannot return.

What the captures still cannot tell you is whether the rendering is *good*. They
freeze what is drawn, so an unintended change shows up as a reviewable diff —
but a bar that stops one cell late looks exactly as correct in a golden file as
in a terminal. That is what the manual pass is for, and why it stays in this
list rather than being retired by the automation. See
[`string-editor-audit-todo.md`](string-editor-audit-todo.md#why-the-manual-pass-was-not-redundant).

## Known dead code, deliberately left

- `internal/stringeditor/view.go:56`, `markerHighlightStyle`: unused since #243
  trimmed the selection bar. Predates this audit and sits in a file neither PR
  touches, so it is out of scope here; `golangci-lint` reports it on a full run
  but CI's `only-new-issues` does not. Worth a one-line cleanup whenever that
  file is next opened.

## Starting points in the code

- [`internal/tuiart/`](../../internal/tuiart/): the shared palette, bar styles,
  text helpers, and backdrop rasterizer this audit adopts.
- [`internal/configeditor/colors.go`](../../internal/configeditor/colors.go) and
  [`backdrop.go`](../../internal/configeditor/backdrop.go): the aliasing pattern
  to copy, which kept `./config`'s call sites and golden output unchanged.
- [`internal/configeditor/view_list_box.go`](../../internal/configeditor/view_list_box.go):
  the `fixedRows` padding derivation the menu editor's screens should follow.
- [`internal/configeditor/view_geometry_test.go`](../../internal/configeditor/view_geometry_test.go):
  the probe to port to both packages.
- [`internal/menueditor/view_menu_edit.go`](../../internal/menueditor/view_menu_edit.go),
  [`view_command_edit.go`](../../internal/menueditor/view_command_edit.go), and
  [`view_command_list.go`](../../internal/menueditor/view_command_list.go): the
  three screens with vertical geometry defects.
- [`internal/usereditor/fields.go`](../../internal/usereditor/fields.go): the
  `Col` / `Row` field geometry the new navigation reads.
- [`internal/usereditor/model.go`](../../internal/usereditor/model.go):
  `nextEditableField`, `firstEditableField`, `lastEditableField`, and the edit
  mode key handler.
