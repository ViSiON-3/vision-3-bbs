# Menu editor and user editor TUI audit

Created: 2026-09-08. Status: in progress.

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
the mode leaves `textInput.Width` unset and produces false width failures that
are artifacts of the probe, not defects in the editors.

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

- [ ] Replace both local `dosColors` / `dosBgColors` tables and their
  `dosStyle` / `dosColor` constructors with aliases onto `internal/tuiart`,
  following the pattern in `internal/configeditor/colors.go:13-20`. Keep every
  screen-specific style variable and its `MENUEDIT.PAS` / `UE.PAS` provenance
  comment; only the palette underneath changes.
- [ ] Alias the backdrop the way `internal/configeditor/backdrop.go` does, load
  it in each model's constructor and resize handler, and source the background
  fill from `backdrop.Segment(row, col, width)` instead of
  `strings.Repeat("░", …)`.
- [ ] Judge, per screen, whether the art or `tuiart.Shaded` is right, because
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
- [ ] Point the title and help bars at `tuiart.HeaderBarStyle` and
  `tuiart.HelpBarStyle`; collapse `./ue`'s `editTitleStyle` onto the same style.
- [ ] Delete the local `centerText` / `padRight` and delegate to
  `tuiart.CenterText` / `tuiart.PadRight`. `internal/usereditor/view_test.go`
  already pins the current behavior of `centerText`, including the rune-safe
  truncation case — keep those assertions passing.

## 2. Fix ./menuedit geometry (#252)

- [ ] Rework the three broken screens to derive top and bottom padding from one
  declared fixed-row count, as `newListBox` does, rather than from independent
  magic constants. Do not floor `bottomPad` at 1; that floor is what turns a
  miscount into an overflow instead of a visible gap.
- [ ] Decide whether the menu editor's list screens should use
  `configeditor.listBox` directly. It currently lives in `internal/configeditor`
  and is documented as keeping each screen's exact byte output. Extract it to a
  shared package **only** if both editors genuinely need it; the prior audit's
  standing instruction is to share chrome, not to rewrite editors.

## 3. Two-dimensional field navigation in ./ue (#242)

- [ ] Move Up and Down to walk within the current column by `Row`, and map Left
  and Right to move to the nearest editable field in the adjacent column,
  preserving the row where one exists there.
- [ ] Decide and document the edge behavior: what Up does on the top row, what
  Right does from the right column, and whether the read-only footer block
  (rows 18-21, `Type: ftDisplay`) participates. `nextEditableField` currently
  wraps and skips `ftDisplay`; keep skipping, and state the wrap rule.
- [ ] Keep Tab, Enter, `ctrl+home`, and `ctrl+end` behaving as they do now. Tab
  and Enter advance in field order and are the existing muscle memory; the
  arrow keys are additive.
- [ ] Update the help bar only if a key's meaning changes.

## 4. Verification

- [ ] Add `view_geometry_test.go` to both packages, covering every reachable
  mode at 80×25, 100×30, 120×45, 160×60, 200×100 and 60×15. Drive real
  keystrokes into each mode; assigning `m.mode` directly leaves `textInput`
  unconfigured and reports width failures that do not exist.
- [ ] Add golden captures for both editors under `testdata`, following
  `internal/stringeditor/view_golden_test.go`.
- [ ] Add coverage for the new navigation: each arrow from each column, the
  edges, and that `ftDisplay` fields stay unreachable.
- [ ] Run the package tests, the repository suite with race checks, `go vet`,
  formatting checks, and `git diff --check`.
- [ ] Perform a manual visual pass over `./menuedit` and `./ue` in a real
  terminal, at small and large sizes, resizing repeatedly. **The automated
  suite does not retire this.** The prior audit's manual pass found two defects
  that every geometry and golden check passed cleanly, because row and column
  arithmetic cannot tell a bar that stops in the right place from one that stops
  a cell late. See
  [`string-editor-audit-todo.md`](string-editor-audit-todo.md#why-the-manual-pass-was-not-redundant).
- [ ] Run both editors against disposable copies of configuration. Do not save
  test changes to the live development BBS.

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
