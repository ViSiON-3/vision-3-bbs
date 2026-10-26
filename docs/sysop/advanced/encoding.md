# Encoding (UTF-8 / CP437)

Classic BBS art is drawn in CP437, the IBM PC character set with its box
drawing and block characters, and retro terminals such as SyncTERM still
expect those bytes. Modern terminals speak UTF-8 (Unicode) instead, and
sending them CP437 bytes produces the familiar mess of accented letters where
a box should be. ViSiON/3 keeps everything internal in one form and converts
on the way out, per session, so the same menu set looks right in both kinds
of terminal. Nothing needs configuring: the board detects each caller's
encoding when they connect, and a caller whose terminal is unusual can pin
one in their own settings.

## How a Session's Encoding Is Chosen

1. **The terminal type is a first guess.** Terminals that announce
   themselves as `syncterm`, `sync`, `magiterm`, `netrunner`,
   `ansi-256color-rgb`, `ansi`, `ansi-bbs`, `scoansi` or `vt100*` are
   treated as CP437; anything else is treated as UTF-8. (An SSH session
   reporting `xterm` with more than 80 columns is also taken for NetRunner,
   which identified itself that way in older builds.)
2. **The terminal is then asked.** At connect the board draws a box-drawing
   character, which is three bytes in UTF-8, and sends a cursor-position
   query. A terminal that decodes UTF-8 reports the cursor one cell on; a
   single-byte terminal reports three. The answer overrides the guess. With
   no answer within 750 ms the guess stands, so a terminal that does not
   respond costs nothing more than that wait, once. Telnet and SSH share the
   probe; anything the caller typed during it is dropped.
3. **A saved preference wins.** A caller who has pinned UTF-8 or CP437 in
   **K** (see below) gets that encoding regardless of what was detected.
   Everyone else is on Auto and gets a fresh detection every call, so one
   account used from SyncTERM at home and a Linux terminal at work is right
   in both.

The chosen encoding is logged at connect (`auto mode selecting CP437 for
retro BBS terminal`, `encoding probe overrides terminal type guess`, and so
on), which is the first place to look when a caller reports garbled art.

## The Caller's Encoding Setting

The user Konfig screen (**K** on the main menu) has an **Encoding** field.
It reads `Auto (UTF-8)` or `Auto (CP437)` while unset, naming the encoding
this session detected. Each press cycles:

1. Auto → the encoding this session is using (nothing visibly changes)
2. → the other encoding
3. → back to Auto

Saving the encoding the session already uses confirms it quietly. Saving the
other one shows *Saved, but this terminal is using UTF-8. CP437 starts next
login.* (or the reverse), because switching a live session mid-screen would
garble it. A pinned encoding is the right answer for a terminal that answers
the probe wrongly; for everyone else, Auto is the setting to leave alone.

UTF-8 sessions also have the VGA palette loaded into their terminal so colours
match what the art was drawn for. See `disableVgaPalette` under
[Default Settings](configuration/configuration.md#field-reference) to turn that
off.

## Forcing an Encoding for the Whole Board

`./vision3 --output-mode=utf8` or `--output-mode=cp437` starts every session
in that encoding and skips the connect-time probe. It exists for testing and
for a board that is only ever reached from one kind of client. The default,
`auto`, is described above.

A caller's saved **Encoding** preference still wins after they log in, flag or
no flag: a user pinned to CP437 switches to CP437 at the login screen even on
a board started with `--output-mode=utf8`. To hold one encoding for a whole
session, the caller's preference must be Auto.

## Troubleshooting

| Symptom | Cause | What to do |
|---------|-------|------------|
| Box drawing shows as accented letters (`ÚÄÄ¿`) in a modern terminal | The session is on CP437 | Have the caller open **K** and check **Encoding**; a saved `CP437` overrides detection. Set it back to Auto |
| Art shows `â”€â”€` or question marks in SyncTERM or NetRunner | The session is on UTF-8 | Same check; a saved `UTF-8` is the usual cause. SyncTERM should be in its CP437 mode |
| Right on one client, wrong on another, same account | A pinned encoding | Set **Encoding** to Auto so each call is detected |
| Everything is wrong for every caller | `--output-mode` is forced | Start the BBS without the flag |
| Right until login, then wrong | A pinned encoding overriding `--output-mode` | Set that caller's **Encoding** to Auto |

An unanswered probe is logged as `encoding probe got no answer, keeping
terminal type guess`; a terminal that never answers keeps working on its
terminal-type guess.

## Technical Reference

The rest of this page is for people changing the code. It lists the
invariants the output path keeps so that both encodings stay correct.

### Invariants

1. **All terminal/session output must go through `terminalio` helpers**
   - Preferred: `terminalio.WriteProcessedBytes(...)`
   - Use `terminalio.WriteStringCP437(...)` for known UTF-8 source strings when CP437 conversion is required.

2. **Do not call `terminal.Write(...)` directly for user-visible output**
   - Exception: commented code or legacy debug snippets not used at runtime.

3. **Always process pipe codes before display**
   - Use `ansi.ReplacePipeCodes(...)` for strings containing `|xx` codes.

4. **Never hard-force `ansi.OutputModeCP437` on user-facing paths**
   - Use the active session output mode, propagated through function parameters/context.

5. **ANSI/control sequences must preserve semantics across modes**
   - Cursor movement/clear sequences should still be written via `terminalio` so mode handling remains centralized.

### Canonical Path

- `internal/terminalio/writer.go`
  - `WriteProcessedBytes(...)` is the central mode-aware write path.
  - CP437 mode routes through conversion logic.
  - UTF-8 mode converts raw CP437 high bytes to UTF-8 when needed while preserving ANSI escapes.
  - **Rune-by-rune decoding**: Preserves valid UTF-8 while mapping invalid bytes to CP437.
  - Handles mixed encoding content correctly without corruption.
- `internal/menu/encoding_probe.go` — `ProbeSessionEncoding` draws the glyph,
  reads the cursor report (`ansi.ParseCPR`) and maps the column to an
  encoding (`ansi.EncodingFromProbeColumn`). `cmd/vision3/encoding.go` holds
  the 750 ms timeout and the connect/login precedence.
- `internal/menu/user_konfig.go` — `toggleEncoding` is the **K** Encoding
  cycle; its text comes from the `konfigEncoding*` strings.

### Audit Commands

Run from repo root:

```bash
grep -RInE 'terminal\.Write\(|fmt\.Fprint\(|session\.Write\(|s\.Write\(' internal
```

```bash
grep -RInE 'WriteProcessedBytes\(.*OutputModeCP437|OutputModeCP437\)' internal/menu
```

```bash
grep -RInE 'WriteProcessedBytes\(.*\[]byte\("\|' internal/menu
```

### Mixed UTF-8 + CP437 Handling

`terminalio.WriteProcessedBytes` decodes rune by rune: a span that is valid
UTF-8 is kept, and only the bytes that are not valid UTF-8 are mapped from
CP437. Before this the whole span was treated as CP437 as soon as one byte
was invalid, which corrupted mixed content such as UTF-8 text with CP437 box
drawing.

### ANSI Renderer Styled Spaces

The ANSI renderer (`internal/menu/ansi_renderer.go`) keeps trailing spaces
that carry a non-default style, so a background colour drawn with spaces is
not trimmed off the end of a row.

### Change Checklist (for future PRs)

- [ ] New output code uses `terminalio` helpers only.
- [ ] Any `|xx` string is wrapped with `ansi.ReplacePipeCodes(...)` before write.
- [ ] No forced `OutputModeCP437` in user-facing menu/session flows.
- [ ] Build passes: `go build ./cmd/vision3`.
- [ ] Grep audit commands above show no new bypasses.
