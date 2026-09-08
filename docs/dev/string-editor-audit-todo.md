# String editor and configuration TUI audit

Created: 2026-09-08. Status: in progress. Investigation complete; section 2
all four sections implemented.

Related issues:

- [#234: Investigate ./strings odd behavior](https://github.com/ViSiON-3/vision-3-bbs/issues/234)
- [#237: A sysop editing a format string can silently break it](https://github.com/ViSiON-3/vision-3-bbs/issues/237)

## Objective

Make `./strings` safe to view and edit, give it consistent terminal behavior with
`./config`, and detect incompatible format strings before they reach BBS users.
The work explicitly includes an audit of **both binaries**: sizing, positioning,
backgrounds, colors, scrolling, dialogs, and terminal resizing. Treat `./config`
as a reference to inspect, not an assumption that its behavior is already correct.

## Confirmed findings

- String entry 199, `pageOnlineNodesHeader`, contains actual `\r\n` characters.
  The preview emits them directly instead of keeping each item on one row.
  With shipped defaults, page 11 produces 43 newline-delimited rows in an 80×25
  terminal, and carriage returns overwrite parts of the displayed rows.
- `./strings` always displays 20 entries per page. Larger terminals change its
  width but do not increase the number of entries or center the content.
- Dialog positioning uses terminal height while the normal view assumes 25 rows.
  This mismatch needs explicit coverage at larger sizes.
- Pressing F1 and then Enter without modifying entry 199 replaces its line breaks
  with spaces. The single-line input sanitizes the stored value during prefill.
- The input has a 200-character limit, creating a truncation risk for longer
  custom values, including their format placeholders.
- The inspected metadata has 427 entries: 7 reserved placeholders, 41 editable
  entries absent from the shipped template, and 2 explicitly empty template values.
  These counts describe the inspected revision, not a permanent schema guarantee.
- Editor metadata, shipped defaults, and runtime defaults are separate sources.
  For example, `matrixAccountCannotLogon` exists in the runtime and template but
  is absent from the editor catalog. The editor's `DefaultStrings()` supplies
  empty values, while factory restore depends on finding a template file.
- Neither edit acceptance, file saving, nor runtime string loading validates
  format arguments. Runtime loading is also used during hot reload.
- `formatVerbCounts` only examines the character immediately after `%` and is
  insufficient as a general parser. Existing strings include `%02d`, `%-20s`,
  and `%3d`.

Investigation used the shipped template and temporary Go test overlays. It did
not modify the BBS's live configuration or repository implementation files.

## 1. Audit ./strings and ./config together

- [x] Run both binaries against disposable copies of configuration, including
  a copy of `~/bbs-dev/` configuration when useful. Avoid saving test changes
  to the live development BBS.
- [x] Capture before/after screens for the same terminal sizes and interaction
  states. Record each discrepancy and the intended shared behavior.
- [x] Review the following surfaces in both binaries:

| Surface | Audit and acceptance criteria |
| --- | --- |
| Window sizing | Respect actual terminal dimensions; document minimum supported dimensions and behavior below them. |
| Content sizing | Define content width and maximum visible row count. `./strings` must use additional height for more entries, up to the chosen cap. |
| Positioning | Use consistent horizontal and vertical centering, outer margins, and placement of headers, messages, and help bars. |
| Backgrounds | Inspect backdrop art, scaling/cropping or centering rules, blank-area fill, and foreground/background contrast. No stale or unpainted areas after navigation or resizing. |
| Colors and borders | Align the roles of selection, input, help, warnings, and dialogs; preserve intentional application-specific styling. |
| Lists and fields | Keep selection visible and navigation predictable when resizing, changing pages, or opening an editor. Use terminal cell width for truncation. |
| Dialogs | Center within the current layout; keep prompts and buttons visible; restore the underlying view cleanly when dismissed. |
| Terminal lifecycle | Verify alternate-screen entry/exit, cursor visibility, and restoration of the terminal after save, cancel, or error. |

- [x] Exercise at least 80×25, 100×30, 120×45, and 160×60, plus a terminal below
  the supported minimum. Resize repeatedly between small and large dimensions.
- [x] Exercise list navigation, the last page, search, editing, confirmation
  dialogs, long descriptions, and error/status messages during resizing.
- [ ] Verify display widths with box-drawing characters, wide Unicode characters,
  combining characters, and color-coded values.
- [x] Decide whether shared layout helpers are warranted. Extract only behavior
  both editors need; avoid a broad TUI rewrite to fix a localized problem.
- [x] Fix directly related `./config` inconsistencies discovered by the audit,
  or record larger findings as separate follow-ups with reproduction steps.

### Cross-binary audit results

`./config` was probed with the same row/column exactness test as `./strings`
(`view_geometry_test.go`), across the top menu, the system menu, all three
confirm dialogs and the help overlay, at 80×25, 100×30, 120×45, 160×60, 200×100
and an undersized 60×15. It fills its terminal exactly at every size, so it is
sound as a reference. `./strings` did not, and the defects are recorded in
section 2.

Both binaries already agreed on terminal lifecycle (alt screen plus input TTY)
and on the 80×25 minimum with clamping below it.

They disagreed on everything visual, so the shared parts were extracted into
**`internal/tuiart`**: the DOS/VGA truecolor palette and its `Color`/`Style`
constructors, the title-bar and help-bar styles, `CenterText`/`PadRight`, and
the backdrop rasterizer with its embedded ANSI art. `./config` now aliases these
and its golden tests still pass byte-identically.

`./strings` gained the shared title bar, help bar, palette and background fill,
and its DOS list is now a centered panel with a background margin. Its list
keeps the flat three-column layout rather than becoming a bordered box: the
decision was to share chrome, not to rewrite the editor.

One thing deliberately not shared: `./config` paints the embedded ANSI art,
which is 80 columns wide and centered. The string list panel is never narrower
than 80 columns, so the art would be completely hidden behind it; `./strings`
uses `tuiart.Shaded` instead.

Still open in this section: the record-list and field-edit screens of `./config`
are not yet covered by the geometry probe, and no before/after screen captures
have been archived.

## 2. Fix string preview and preserve editing data (#234)

- [x] Make every list item occupy exactly one terminal row. Display control
  characters safely without executing raw CR, LF, tab, or escape sequences.
- [x] Preserve supported pipe/dollar color previews while enforcing the row's
  terminal cell budget, including its overflow marker.
- [x] Choose and document a lossless editing representation, such as escaped
  `\r`, `\n`, and `\t`. Define literal backslash handling so ordinary text does
  not unexpectedly become a control sequence.
- [x] Keep preview conversion separate from stored content. Do not fix rendering
  by deleting legitimate control characters from the BBS strings themselves.
- [x] Ensure F1 → Enter without changes preserves the exact original value.
- [x] Remove silent truncation during prefill and editing. If a length limit is
  required, report it explicitly and retain the user's original content.
- [x] Derive pagination from available height, with a documented maximum. Keep
  the current selection visible when the page size changes.
- [x] Use a coherent full-screen layout for the list, background, footer, and
  overlays, following the decisions from the cross-binary audit.
- [x] Add regressions using actual multiline shipped values around entries
  199–220, long custom values, and resize transitions.

### Implemented so far

`internal/stringeditor/escape.go` defines the lossless editing representation:
control characters become `\r`, `\n`, `\t`, `\e` or `\xNN`, and a literal
backslash is doubled. `EscapeForEdit` and `UnescapeFromEdit` are exact inverses,
verified against every value in the shipped template. A malformed escape is
reported on the message bar and leaves the sysop in the input with their text,
rather than being guessed at and written to disk.

The preview draws those same escapes in inverse video, so no control character
reaches the terminal and every entry stays on one row. Width budgeting now uses
terminal cells (`go-runewidth`) instead of rune counts.

Pagination derives from terminal height: `chromeRows` (5) are reserved and the
rest of the screen is list, clamped between 20 and 60 entries per page. A resize
re-pages around the cursor so the selection stays visible.

Two layout defects surfaced while adding the row-exactness regression and were
fixed with it: the column header was three cells too wide while item rows were
three cells too short (the header, label and value arithmetic disagreed), and
the status bar measured itself with a hand-maintained parallel plain-text copy
that drifted, overflowing the last column at three-digit topic numbers. Every
chrome row is now clipped and padded to the terminal width.

The full-screen layout follows the section 1 decisions: a shared title bar, the
DOS list as a centered panel over the shared background fill, the description
caption below it, and the shared help bar on the last row. Chrome now costs six
rows, so the minimum page is 19 entries.

## 3. Clarify reserved, missing, and empty values

- [x] Hide reserved placeholders by default, with an optional way to inspect
  them if useful. Preserve stable identifiers/original numbering when filtering.
- [x] Distinguish an absent key, an explicitly empty value, a runtime fallback,
  and a custom value. Do not treat all visually blank rows as unused.
- [x] Audit catalog coverage against runtime string fields and shipped defaults;
  classify legacy entries rather than deleting them based only on absence from
  the template.
- [x] Add missing active entries, including `matrixAccountCannotLogon`, with
  meaningful descriptions of their arguments.
- [x] Make factory defaults available reliably in installed binaries. Avoid
  depending solely on a template path relative to the current directory.
- [x] Preserve unrelated/custom keys when saving, and document what restoring
  a default or clearing a value means for each supported state.
- [x] Check these states against runtime fallback behavior before changing how
  blank or missing strings are saved.

### Implemented so far

Classification of the 446 catalog entries and 399 template keys against the
runtime `StringsConfig`:

| Class | Count | Handling |
| --- | --- | --- |
| Live, catalogued, in template | 358 | unchanged |
| Live, catalogued, template-less | 40 | runtime fallback, marked `~` |
| Live but **missing from the catalog** | 19 | entries added |
| Catalogued but no runtime field | 1 | `uploadMsgStr`, recorded as legacy |
| Template-only Vision/2 leftovers | 5 | recorded as legacy, preserved on save |
| Reserved placeholders | 7 | hidden by default, `Ctrl-R` reveals |

The 19 additions include `matrixAccountCannotLogon`, both door access-control
strings, all nine chat network/room strings, the two new-user outcome strings,
the newscan network prompt and two conference strings. `defColor1`-`defColor7`
turned out to be `uint8`, not editable text, so they are correctly absent.

`applyStringDefaults` was a hand-written list of 36 assignments. It is now the
exported `config.StringFallbacks` table applied by reflection over the struct's
json tags, so the loader and the editor read the same source rather than two
that drift.

`valueState` distinguishes custom, default, runtime fallback, explicitly empty,
not set and reserved. A fallback row previews what the BBS actually prints,
dimmed, instead of appearing blank and unused.

Factory defaults are now reliable in an installed binary: `templates/configs`
gained an `embed.go`, so the Go file sits beside the JSON and there is still
exactly one canonical copy of each template. `F4` falls back to the runtime
default where the template has no entry, and a missing `strings.json` is created
from the shipped defaults rather than from 420 empty values.

Four coverage tests now fail the build if a new runtime string has no catalog
entry, if a catalog entry names no runtime field, if a template key is neither
editable nor recorded as legacy, or if a recorded legacy key becomes live.

## 4. Validate format contracts (#237)

- [x] Inventory actual formatted-string call sites, including aliases and helper
  wrappers. Distinguish strings passed to `fmt` from plain text and other BBS
  placeholder syntaxes.
- [x] Establish shared expected arguments/defaults for formatted keys, available
  to the runtime, editor, and tests. Avoid importing the menu package into the
  config loader or creating another independently maintained source of defaults.
- [x] Implement parsing that understands `%%`, flags, width, precision, argument
  indexes, and width/precision arguments supplied through `*`. Detect malformed
  directives, including a trailing `%`.
- [x] Define compatibility rules: count and argument types must agree; cosmetic
  padding must not cause false warnings. Explicit indexed reordering should be
  evaluated by argument binding, not just textual verb order.
- [x] Keep argument descriptions: type validation cannot detect swapping the
  meanings of two arguments that both use `%s`.
- [x] Validate on runtime load and hot reload. Log the key, expected arguments,
  and specific mismatch without preventing BBS startup or silently rewriting
  custom strings.
- [x] Add editor feedback at edit acceptance and save. Agree on warning versus
  rejection behavior, and avoid blocking unrelated edits because an older file
  already contains a mismatch.
- [x] Ensure restoring a default provides a clear recovery path for an invalid
  template without losing the user's current edit unexpectedly.
- [x] Add CI checks tying shipped defaults to actual call-site arguments. A
  comparison between two copies of a default does not prove the call site is
  correct. Ensure newly introduced formatted keys cannot bypass coverage.
- [x] Retain the specific version-string compatibility behavior from PR #236.
  Any reuse of the new parser must preserve its supported legacy cases.
- [x] Avoid changing every render call site as part of this work unless a
  demonstrated mismatch requires a targeted fix.

### Implemented

`internal/formatspec` parses what fmt parses: escaped `%%`, the flags `+-# 0`,
width and precision given literally or as `*` arguments, and explicit argument
indexes in `[n]` form. It returns an argument signature keyed by binding
position, not textual order, so `%[2]s to %[1]s` compares equal to `%s from %s`.
Malformed directives are reported: a trailing bare `%`, an unterminated or
non-numeric index, an unknown verb, and a single argument used as two
incompatible types. A property test fills every parsed signature through Sprintf
and asserts no `%!` marker appears in the output.

Validation is scoped by `internal/stringformat`. Not every BBS string is a
format string, and some legitimately contain a bare percent sign -- `badUDRatio`
ends `(|15|RA%|09)`, a literal `%` followed by a pipe colour code -- so running
every value through the parser would report working prompts as broken.
`FormattedKeys` lists the 89 keys a call site actually hands to fmt.

That list is derived from the source, not maintained by hand. An AST test walks
the repository for `fmt.Sprintf`/`Printf`/`Errorf`/`Fprintf` calls whose format
argument is a `LoadedStrings` field, maps the field to its json key by reflection
over the struct tags, and fails if the committed list and the discovered set
differ in either direction. A second test compares each call site's argument
count against the arity of that key's shipped default -- the check the audit
asked for, since comparing two copies of a default proves nothing about the call
site. All 118 call sites were checked with none skipped; a site spreading a
slice would be logged rather than silently passed over.

The runtime validates in `config.LoadStrings`, which is the single entry point
for both startup and hot reload. Each mismatch is logged with the key, the
expected signature and the specific problem. It never blocks startup and never
rewrites a sysop's string.

The editor warns at edit acceptance and keeps the text, since the sysop may be
mid-rewrite and discarding what they just typed would be worse than the warning.
F10 reports remaining mismatches once and saves on a second press, so an older
file's mistake cannot trap unrelated edits. The description bar shows the
expected signature while editing, and argument descriptions are retained in the
catalog because type validation cannot detect two `%s` arguments being swapped.

`renderVersionString` now uses the shared parser instead of `formatVerbCounts`.
Every behavior from PR #236 is preserved and still tested, including the
`100% Go` case where the verb is `%<space>G` rather than `%s`, the superseded
template upgrade, and the exact-match rule that treats a whitespace difference
as a sysop edit. No other render call site was changed.

## Delivery and verification

- [ ] Prefer two coordinated PRs: #234 for safe editing and visual consistency,
  followed by #237 for shared validation and call-site/default checks. Place any
  shared metadata/default groundwork deliberately and document the dependency.
- [x] Update the string-editor guide with escape editing, blank/default states,
  adaptive sizing, and format-validation behavior.
- [x] Verify preview safety, no-op edit round trips, save/reload round trips,
  missing-template installations, and legacy/custom configurations.
- [x] Verify runtime load and hot reload warnings, malformed format strings,
  escaped percentages, padded verbs, indexed arguments, and argument order.
- [x] Run relevant package tests, the repository test suite and race checks,
  `go vet`, formatting checks, and `git diff --check`.
- [ ] Perform final visual checks of **both** `./strings` and `./config`; automated
  non-empty-view smoke tests alone do not establish layout correctness.

## Starting points in the code

- [`internal/stringeditor/model.go`](../../internal/stringeditor/model.go):
  navigation, fixed page size, resize handling, edit acceptance, and input limit.
- [`internal/stringeditor/view.go`](../../internal/stringeditor/view.go) and
  [`colors.go`](../../internal/stringeditor/colors.go): list rendering, color
  previews, backgrounds, footers, and dialog positioning.
- [`internal/stringeditor/metadata.go`](../../internal/stringeditor/metadata.go),
  [`fileio.go`](../../internal/stringeditor/fileio.go), and
  [`cmd/strings/main.go`](../../cmd/strings/main.go): catalog, persistence, and
  factory-default discovery.
- [`internal/configeditor/view.go`](../../internal/configeditor/view.go),
  [`view_list_box.go`](../../internal/configeditor/view_list_box.go),
  [`backdrop.go`](../../internal/configeditor/backdrop.go), and
  [`model.go`](../../internal/configeditor/model.go): configuration TUI reference.
- [`internal/config/config_strings.go`](../../internal/config/config_strings.go)
  and [`cmd/vision3/config_watcher.go`](../../cmd/vision3/config_watcher.go):
  runtime defaults, loading, and hot reload.
- [`internal/menu/version_string.go`](../../internal/menu/version_string.go):
  existing narrow validation and legacy version-template handling.
- [`templates/configs/strings.json`](../../templates/configs/strings.json):
  shipped values used to reproduce the rendering failures.
