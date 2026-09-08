# ViSiON/3 String Editor

The string editor (`strings`) is a TUI tool for editing `configs/strings.json`, which contains all customizable text prompts and messages displayed by the BBS. It is a Go reimplementation of the original Vision/2 `STRINGS.EXE` Turbo Pascal utility.

## Running

```bash
./strings                           # Edit configs/strings.json (default)
./strings --config path/to/file.json  # Edit a specific strings file
./strings --help                     # Show usage
```

## Interface

The editor uses a fullscreen layout based on the DOS original. It requires at least 80×25 and grows
to fill a larger terminal:

```text
            -- ViSiON/3 String Configuration v1.0 --
░░░░ Current Topic Number: 1 │ ViSiON/3 BBS String Config │ Page: 1 ░░░░
░░░░  # Name                      Value                             ░░░░
░░░░  1[Default User's Prompt  ]██ |MN ██ |TL Left:                 ░░░░
░░░░  2[System Pause String    ]███ ► Stroke Me! ►███               ░░░░
░░░░  3[System Password String ]███ Login Password:                 ░░░░
░░░░  ...                                                           ░░░░
░░░░                                                                ░░░░
░░░░░░░░░░ This is the Default prompt for new users ░░░░░░░░░░░░░░░░░░░░
  Enter Edit  F1 Prefill  F3 Revert  F4 Default  F10 Save  Esc Quit
```

- **First row** — Title bar, shared with `./config`
- **List panel** — The DOS list, centered over the shaded background:
  - Status bar showing current topic number, title, and page
  - Column headers (Name / Value)
  - One string per row, with label and color-rendered value
  - Flash messages, the edit indicator, or the search box
- **Second-from-last row** — Description of the currently highlighted string
- **Last row** — Keyboard shortcut reference, shared with `./config`

The title bar, help bar, DOS color palette and background fill come from `internal/tuiart`, so both
editors render in the same colors and the same chrome. The list panel itself keeps the Pascal
original's flat three-column layout.

### Sizing

Six rows are reserved for the title bar, status bar, column headers, message bar, description bar
and help bar; every remaining row shows one string. An 80×25 terminal gives 19 items per page, a
100×30 terminal gives 24, and so on up to a cap of **60 items per page** — past that the description
bar sits too far from the selection to read as its caption, and the leftover rows become background
above and below the panel.

The panel is 80 columns wide at the minimum terminal size and widens on a larger one, always leaving
10 columns of background on each side, up to a maximum panel width of **120 columns**.

Resizing re-pages around the current selection, so the highlighted string stays on screen. Terminals
smaller than 80×25 are not supported: the editor draws at 80×25 and the terminal clips it.

`./config` paints its embedded ANSI art as its background. `./strings` cannot: that art is 80 columns
wide and centered, and this editor's list panel is never narrower than 80, so the panel would cover
the picture completely. It uses the shared shaded fill instead.

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `↑` / `↓` | Move cursor up/down |
| `PgUp` / `PgDn` | Previous/next page |
| `Home` / `End` | Jump to first/last item |
| `Enter` | Edit selected string (blank input field) |
| `F1` | Edit selected string (pre-filled with current value) |
| `F3` | Revert selected string to its last-saved value |
| `F4` | Restore selected string to ViSiON/3 default (from templates/) |
| `F10` | Save changes and exit |
| `Esc` | Abort — shows confirmation dialog if unsaved changes |
| `/` | Search/filter strings by name |
| `Ctrl-R` | Show or hide reserved placeholder entries |

### Edit Mode

When editing a string value:

- **Enter** — Save the new value
- **Esc** — Cancel editing without changes

Values are edited on a single line, so control characters are shown and typed as **escape
sequences**. `F1` pre-fills the input with the escaped form of the current value, and `Enter`
converts it back before storing, so opening a string and accepting it unchanged leaves it exactly as
it was.

| Sequence | Character |
|----------|-----------|
| `\r` | Carriage return |
| `\n` | Line feed |
| `\t` | Tab |
| `\e` | Escape (ESC, 0x1B) |
| `\xNN` | Any other control character, as two hex digits |
| `\\` | A literal backslash |

A backslash always starts an escape sequence, so to store a literal backslash — in a DOS path, for
example — type it twice. Anything else after a backslash is rejected: the editor keeps you in the
input with your text intact and shows the problem on the message bar, rather than guessing and
writing something you did not mean to disk.

The same escapes appear in the value column of the list, drawn in inverse video so a stored carriage
return is visible as `\r` instead of breaking the row. Every string occupies exactly one row; a
value too wide for the column ends with a magenta `»`.

Values are not length-limited. A long custom string is stored in full, and the input scrolls
horizontally rather than truncating it.

### Confirmation Dialogs

Several actions display a centered Yes/No confirmation dialog before proceeding:

- **Esc** (with unsaved changes) — "Abort Without Saving?"
- **F3** — "Revert to Last Saved?"
- **F4** — "Restore ViSiON/3 Default?"

In any confirmation dialog:

- **←/→** — Toggle between Yes/No
- **Enter** — Confirm selection
- **Y** / **N** — Quick accept/reject
- **Esc** — Cancel and return to navigation

## BBS Color Codes

String values support BBS pipe codes that are rendered with color in the editor. These same codes produce colored output when displayed to connected users at runtime.

### Foreground Colors (`|00` – `|15`)

| Code | Color |
|------|-------|
| `\|00` | Black |
| `\|01` | Dark Blue |
| `\|02` | Dark Green |
| `\|03` | Dark Cyan |
| `\|04` | Dark Red |
| `\|05` | Dark Magenta |
| `\|06` | Brown/Dark Yellow |
| `\|07` | Light Gray |
| `\|08` | Dark Gray |
| `\|09` | Light Blue |
| `\|10` | Light Green |
| `\|11` | Light Cyan |
| `\|12` | Light Red |
| `\|13` | Light Magenta |
| `\|14` | Yellow |
| `\|15` | White |

### Background Colors (`|B0` – `|B7`)

| Code | Color |
|------|-------|
| `\|B0` | Black background |
| `\|B1` | Red background |
| `\|B2` | Green background |
| `\|B3` | Brown/Yellow background |
| `\|B4` | Blue background |
| `\|B5` | Magenta background |
| `\|B6` | Cyan background |
| `\|B7` | Light Gray background |

### Special Codes

| Code | Meaning |
|------|---------|
| `\|CR` | Carriage return / newline (rendered as a space in the editor's preview) |
| `\|CL` | Clear screen |
| `\|DE` | Clear to end of line |
| `@` | Yes/No selection bar |

### Dollar Codes (`$x`)

Legacy Vision/2 color codes using `$` followed by a letter. These map to the same 16 DOS colors (e.g., `$a` = dark blue, `$W` = white).

### MCI Codes

Runtime placeholder codes like `|MN` (menu name), `|TL` (time left), `|CB` (current board), `|UN` (user number), etc. These are displayed as literal text in the editor but expanded by the BBS server when shown to users.

## File Format

`configs/strings.json` is a flat JSON object mapping string keys to their values:

```json
{
    "applyAsNewStr": "|08A|07p|15ply |08F|07o|15r |08A|07c|15cess? @",
    "connectionStr": "|08► |15Connect |08(|03|BR|08)",
    "defPrompt": "|08██ |15|MN |08██ |13|TL |05Left|08: ",
    "pauseString": "|15█|07█|08█|B1|09► |15Stroke Me! |09►|B0|08█|07█|15█",
    ...
}
```

Keys prefixed with `_` (e.g., `_extra3`) are reserved placeholders and cannot be edited. They are
hidden from the list by default; `Ctrl-R` shows them. Entry numbers come from the full catalog, so
they do not shift when reserved entries are hidden — "string 199" always means the same string.

### Format verbs

Some strings carry `%s` / `%d` **format verbs**, which the BBS fills in at runtime. These are
different from `|XX` placeholder codes: a placeholder can be moved or removed freely, but a format
verb must stay, and the **number and order of verbs must not change**. The editor's description line
names the verbs a string expects, for example:

```text
Exec: Version String    Version string format (%s=version)
```

Removing or adding a verb does not fail — it prints `%!d(MISSING)` or `%!(EXTRA int=3)` into the
middle of the message the user sees. If you want a literal percent sign in one of these strings,
write it as `%%`.

`execVersionString` is the exception that repairs itself: with no `%s` it prints unchanged and logs
why, rather than showing a mangled banner.

When saving, the editor writes keys in sorted order and omits internal `_`-prefixed keys, producing clean deterministic output.

## Value States

A blank value column does not mean the string is unused. The marker between the label and the value
says which of five states an entry is in, and the message row above the description spells it out
for the selected entry.

| Marker | State | Meaning |
|--------|-------|---------|
| (none) | ViSiON/3 default | The value matches the shipped default |
| `*` | Custom | You have changed it from the shipped default |
| `~` | Runtime fallback | Blank in `strings.json`, but the BBS prints a built-in default. The list shows that default, dimmed |
| `0` | Explicitly empty | Present in `strings.json` and set to nothing. The BBS prints nothing |
| `-` | Not set | Absent from `strings.json` with no built-in default. The BBS prints nothing |

The `~` state is the common one after an upgrade: a release adds a string, your existing
`strings.json` does not have it, and the BBS falls back to the value compiled into the binary.
Nothing is broken — pressing `F4` writes that default into your file if you want it there
explicitly.

Keys in `strings.json` that the editor has no entry for — a leftover from Vision/2, or something you
added by hand — are written back unchanged when you save. Nothing is dropped.

## String Descriptions

Each string entry has a built-in description explaining its purpose. These descriptions are compiled into the editor binary in `internal/stringeditor/metadata.go` and are displayed on row 24 as you navigate. They match the original Vision/2 `things[].descrip` help text.

## String Categories

The editor contains approximately 250 string entries organized by function:

| Range | Category |
|-------|----------|
| 1–20 | Core prompts (login, pause, chat, quoting) |
| 21–40 | Message base prompts (posting, boards, scanning) |
| 41–60 | User system (registration, passwords) |
| 61–80 | Mail and feedback prompts |
| 81–100 | New user voting, rumors, BBS list |
| 101–120 | File system prompts (upload, download, batch) |
| 121–140 | File operations (ratios, listings, newscan) |
| 141–160 | QWK mail, chat, announcements |
| 161–178 | Advanced file/message operations |
| 179–200 | ViSiON/3: SSH/ANSI, Door, Matrix/Login |
| 201–220 | ViSiON/3: Conference, Executor |
| 221–240 | ViSiON/3: Terminal/Session, File Search, File Info |
| 241–250+ | ViSiON/3: File Newscan, Sysop File Review, Column Config, Want List |

Reserved entries (keys prefixed with `_`) appear in the list as non-editable rows and are skipped when saving.

## Building

The string editor is built automatically by `build.sh`:

```bash
./build.sh    # Builds all binaries including ./strings
```

Or build it standalone:

```bash
go build -o strings ./cmd/strings
```

## Origin

This tool is a faithful recreation of the Vision/2 BBS `STRINGS.EXE` (Turbo Pascal, ~1400 lines in `SRC/STRINGS.PAS`). The Go version preserves the original's paginated three-column layout, DOS color scheme, and editing workflow while adding search, escape-safe editing of control characters, terminal-adaptive sizing, and chrome shared with `./config`.

## Developer Reference

### How Defaults Are Loaded

The editor supports two layers of value restoration:

1. **Last-Saved Values (F3 "Revert")** — When the editor starts, it snapshots all values from `configs/strings.json` into `origValues`. F3 restores the currently selected string to the value it had when the editor was opened (i.e., whatever is on disk). This snapshot lives in memory only and is set in `stringeditor.New()`.

2. **ViSiON/3 Defaults (F4 "Restore")** — A separate set of shipped default values loaded from `templates/configs/strings.json`, with the copy embedded in the binary as a fallback. This represents the canonical out-of-the-box string values that ship with ViSiON/3. F4 restores the selected string to this shipped value; where the template has no entry for a key, F4 offers the runtime fallback from `config.StringFallbacks` instead.

### Default Loading Flow

```text
cmd/strings/main.go
  ├─ loadShippedDefaults()
       ├─ Try:  ./templates/configs/strings.json  (relative to CWD)
       ├─ Try:  <exe-dir>/templates/configs/strings.json  (relative to binary)
       ├─ Try:  <exe-dir>/../templates/configs/strings.json  (bin/ layout)
       └─ Else: the copy embedded in the binary (templates/configs/embed.go)

  └─ stringeditor.New(configPath, shippedDefaults)
       ├─ loads configs/strings.json into values
       ├─ copies values → origValues  (F3 revert source)
       └─ stores shippedDefaults      (F4 restore source)
```

- `loadShippedDefaults()` in `cmd/strings/main.go` attempts to read the template file from two locations: first relative to the working directory, then relative to the executable path. This allows the editor to work both during development (`cd /opt/vision3 && ./strings`) and from an installed location.
- An on-disk template wins so a distribution can ship adjusted defaults, but the templates directory is not present in every installation, so `templates/configs/embed.go` embeds the same file into the binary as a guaranteed fallback. There is one canonical copy: the Go file sits alongside the JSON rather than the JSON being duplicated into a package.
- The template file (`templates/configs/strings.json`) must be kept in sync with `configs/strings.json` when new string keys are added to the system.

### Adding a New String

1. Add the field to `StringsConfig` in `internal/config/config_strings_types.go`
2. Add a `StringEntry` struct to the appropriate position in `internal/stringeditor/metadata.go` (Label, Key, Description — name the format arguments in the description)
3. Add the key with its default value to `configs/strings.json`
4. Add the same key and value to `templates/configs/strings.json` (the ViSiON/3 defaults template)
5. If existing installations must keep working without the key, add it to `config.StringFallbacks`
6. The editor will pick up the new entry automatically on next run

`TestCatalogCoversRuntimeStrings` fails if step 2 is skipped, so a new string cannot ship without
being editable. `TestCatalogHasNoDeadEntries` fails in the other direction if an entry names no
runtime field.
