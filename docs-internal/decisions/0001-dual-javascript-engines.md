# Decision: Two JavaScript engines (syncjs + scripting)

*Status: accepted · Recorded: 2026-06-28 (documenting existing state)*

## Context

The codebase contains **two** JavaScript runtimes, both built on
[`github.com/dop251/goja`](https://github.com/dop251/goja):

| Package | Purpose | Wired in |
| ------- | ------- | -------- |
| `internal/syncjs` | **Synchronet-compatible** JS runtime for running existing Synchronet-style JS doors | `internal/menu/syncjs_door.go` |
| `internal/scripting` | **Native ViSiON/3** scripting runtime for V3-authored scripts | `internal/menu/v3script_door.go` (+ `_windows.go`) |

Each gives a connected user their own `goja.Runtime` instance per session.

This was flagged in the [2026-06-27 technical audit](../specs/2026-06-27-technical-audit.md)
as ~5K lines of parallel scripting infrastructure on the same engine — worth a
recorded rationale so it isn't mistaken for accidental duplication.

## Decision

Keep both engines. They expose **deliberately different APIs** and serve
different audiences:

- **`syncjs`** mirrors the Synchronet global JS API so that doors written for
  Synchronet run with little or no change. It exposes Synchronet's globals and
  objects — e.g. `bbs`, `console`, `client`, `system`, `File`, `Queue`, and the
  Synchronet top-level helper functions (`alert`, `ascii`, `center`, `clear`,
  `cleartoeol`, …). Its config type is `SyncJSDoorConfig`.
- **`scripting`** is the native V3 runtime with an idiomatic, sandboxed API —
  e.g. `bbs`, `console`, `ansi`, `area`/`areas`, `color`, `broadcast`,
  `activity`, with sandboxed file access via a providers layer. Its config type
  is `ScriptConfig`.

They are **not** interchangeable: a Synchronet door expects the Synchronet API
shape, and a V3 script expects the native API. Collapsing them into one engine
would mean either breaking Synchronet-door compatibility or bolting the
Synchronet API onto the native runtime (and vice-versa).

## Consequences

- Two scripting surfaces to maintain. Changes to one do not automatically apply
  to the other.
- When adding a capability, decide which audience it serves: Synchronet-door
  compatibility (`syncjs`) or native V3 scripting (`scripting`). Prefer adding
  to `scripting` for new V3-native features; only extend `syncjs` to improve
  Synchronet compatibility.
- If Synchronet-door support is ever dropped, `internal/syncjs` and
  `internal/menu/syncjs_door.go` can be removed wholesale without touching the
  native scripting path.
