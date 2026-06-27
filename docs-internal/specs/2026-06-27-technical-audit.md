# ViSiON/3 BBS — Technical Audit Report

*Date: 2026-06-27 · Branch: `main` (clean) · Go 1.25.0 · ~117K LOC (92.6K src / 24.7K test)*

## Executive Summary

The codebase is **healthy at the build/dependency level** — it compiles clean
(`go build ./...` exit 0), `go mod tidy` produces no changes, and every direct
dependency is actually imported. The debt is **structural and concentrated**,
not systemic. Two issues dominate: a single 10,607-line monolith
(`internal/menu/executor.go`) and an overall test ratio of **0.26** (test:src)
with several non-trivial packages at **zero** coverage.

---

## 1. Monolithic Files (highest priority)

| File | Lines | Assessment |
|------|------:|------------|
| `internal/menu/executor.go` | **10,607** | 🔴 Critical. 139 functions, a 127-case command-dispatch switch, single 33K-line `menu` package. |
| `cmd/vision3/main.go` | 2,269 | 🟠 Large entry point — wiring/bootstrap should be extractable. |
| `internal/menu/message_reader.go` | 1,604 | 🟠 |
| `internal/config/config.go` | 1,237 | 🟠 |
| `internal/usereditor/model.go` | 1,228 | 🟠 (0 package tests) |
| `internal/menu/file_lightbar.go` | 1,297 | 🟠 |

All exceed the project's own **300-line guideline** in CLAUDE.md by 4–35×.

**`executor.go` is the clear refactor target.** It is structured as free
functions taking the *same wide signature* —
`(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth, termHeight int)`
— repeated **94 times** across the `menu` package. The biggest functions
(`runValidateUser` 870 lines, `Run` 837, `runAdminListUsers` 831,
`runListFiles` 609) are themselves over-large.

**Recommended decomposition:**

1. Introduce a **command-context struct** (e.g. `CmdCtx`) bundling
   session/terminal/user/node/dimensions. This alone eliminates the 94-arg-list
   repetition and shrinks every signature.
2. Split the 127-case dispatch by domain into separate files already implied by
   the package layout (`executor_files.go`, `executor_message.go`,
   `executor_admin.go`, `executor_user.go`). The package already practices this
   pattern — `executor.go` is the leftover that never got split.
3. Extract the four 600+ line functions into sub-steps.

---

## 2. Test Coverage Gaps

Overall ratio 0.26 is low for a system handling auth, file transfer, and message
networking. Packages with **production code and zero tests**:

| Package | Src lines | Risk |
|---------|----------:|------|
| `internal/configeditor` | 12,120 | 🔴 134 test lines only — **~1% coverage** on the second-largest subsystem |
| `internal/usereditor` | 2,950 | 🔴 user data mutation, untested |
| `internal/scripting` | 2,001 | 🔴 JS engine (goja), untested |
| `internal/menueditor` | 2,543 | 🟠 |
| `internal/stringeditor` | 1,766 | 🟠 |
| `internal/telnetserver` | 1,040 | 🟠 network-facing, untested |
| `internal/sshserver` | 353 | 🟠 network-facing/auth, untested |
| `internal/conference` | 195 | 🟡 |

Conversely, `jam`, `user`, `ansi`, `ziplab`, `file`, and `scheduler` have
test:src ratios ≥ 1.0 — coverage is bimodal. **Prioritize the network-facing
(`sshserver`, `telnetserver`) and data-mutating (`usereditor`, `configeditor`)
untested packages** over the editors.

---

## 3. Dependencies

**No unused or outdated-by-tidy dependencies.** `go mod tidy` is a no-op; all 13
direct deps are imported. Usage spread:

- Heavily used: `gliderlabs/ssh` (57), `golang.org/x/term` (49),
  `bubbletea` (33), `goja` (24).
- Single-file deps (fine, just noting): `robfig/cron/v3` (1), `creack/pty` (2),
  `fsnotify` (2).

**Two things to verify, not necessarily fix:**

1. **Two JavaScript engines, both on goja.** `internal/syncjs` (2,937 src —
   Synchronet-compatible API: `bbs`, `console`, `system`, `user` objects) and
   `internal/scripting` (2,001 src — native V3 API). Each is wired into exactly
   one door handler (`syncjs_door.go` vs `v3script_door.go`). This is plausibly
   intentional (Synchronet-script compatibility *and* native scripting), but
   it's **~5K lines of parallel scripting infrastructure** on the same engine.
   Confirm both are actively required; if `syncjs` is a migration target rather
   than a maintained surface, document that.

2. **Vendored `replace` directives** for `charmbracelet-bubbletea` and
   `charmbracelet-x/term` in `third_party/` — patched for Windows 10 32-bit
   VT-input. Legitimate but a maintenance liability: these forks must be
   re-patched on every upstream bump. Recommend a `THIRD_PARTY_PATCHES.md`
   recording the exact upstream commit and the diff rationale.

---

## 4. Other Observations

- **Tech-debt markers are low**: 21 `TODO/FIXME/HACK/XXX` across all non-test
  source — good discipline. Concentrated in `file/`, `menu/`, `session/`, and
  `cmd/vision3/main.go`.
- **`internal/types` (5 lines) and `internal/version` (6 lines)** are fine as
  shared leaf packages; no action.
- The `menu` package as a whole (33K src) is the gravity well of the project.
  Beyond `executor.go`, watch `message_reader.go`, `file_lightbar.go`,
  `door_handler.go`, and `message_scan.go` — all 1K+ and likely to keep growing.

---

## Prioritized Action List

1. 🔴 **Decompose `executor.go`** via a command-context struct + domain file
   splits (kills the 94× signature repetition as a side effect).
2. 🔴 **Add tests to network-facing & data-mutating untested packages**:
   `sshserver`, `telnetserver`, `usereditor`, then `configeditor`.
3. 🟠 **Document the dual-scripting-engine decision** and the `third_party` patch
   provenance.
4. 🟠 **Split the remaining 1K+ `menu` files** as they're touched (opportunistic,
   not a campaign).
