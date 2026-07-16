# ViSiON/3 Codebase Audit Scorecard

Date: 2026-07-15 · Baseline: main after PRs #79–#84 (post Step-1 fixes) · 1678 tests passing in 55 packages

Measurement environment: Apple M1 Pro, macOS, SSD, Go 1.26. Benchmarks via `go test -benchmem` in a
discarded worktree; server measurements against a scratch BBS root built from `templates/configs`.

## Scorecard

| Category | Grade | Key metric | Worst offenders | Remediation cost |
|---|---|---|---|---|
| Loading performance | **A−** | Startup → listening: **67.6 ms**; menu render 74 µs; ANSI screen 87 µs | regex recompiled per render (executor.go `processFileIncludes`) | Low (hours) |
| Concurrency safety | **A** | 0 leaks found in audit; `-race` clean; lifecycle regression tests in place | — | Done (PR #83) |
| Memory management | **B** | Idle RSS **22.7 MB**; all network reads now bounded; file-area browse copies 64 KB/listing (500 records, no pagination) | `GetFilesForArea` full-copy per browse; no ANSI/include cache | Low–Med |
| Error handling | **C+** | **50** unchecked-error lint findings (post-fix baseline); ~350 explicit `_ =` sites (most are intentional display writes) | `internal/menu` lightbar writers; assorted I/O paths | Med (burn-down) |
| Dead code | **C+** | **26** `unused` lint findings + **76** unreachable-from-main findings (`x/tools/deadcode`) | `internal/editor` (17), `internal/menu` (7), `internal/tosser` (6), `internal/ansi` (6) | Low–Med |
| Duplication | **C** | **~800–950 LOC** consolidatable across 3 mirror families | lightbar pair ~85% duplicated (393+407 LOC); configeditor FTN↔V3Net view/update mirrors | Med |
| Test coverage | **C−** | Mean **54.8%** across 42 packages; **4 packages at 0%** | configeditor 5.5%, menu 6.7%, usereditor 14.1%, scripting 17.5%, transfer 20.6% | High (ongoing) |
| File size / organization | **D** | **106 files** exceed the project's own 300-line limit (peak 2350) | `menu/executor.go` 2350, `menu/executor_admin_users.go` 1899, `cmd/vision3/main.go` 1815; `internal/menu` alone has 44 offenders | High |

## Measured metrics

### Binary sizes (release-unoptimized `go build`)
| Binary | Size | | Binary | Size |
|---|---|---|---|---|
| vision3 | **31.1 MB** | | v3net-bootstrap | 8.6 MB |
| config | 11.7 MB | | wfc | 7.8 MB |
| helper | 7.2 MB | | ansitest | 6.9 MB |
| ue | 6.8 MB | | v3mail | 6.7 MB |
| menuedit | 5.1 MB | | strings | 5.0 MB |
| ini2ftnreg | 2.8 MB | | | |

vision3's size is dominated by goja (JS engine), bubbletea/lipgloss, and the SQLite driver — expected for
the feature set; `-ldflags="-s -w"` would cut ~30% for release builds.

### Loading / latency (measured)
| Path | Cost | Notes |
|---|---|---|
| Cold start → telnet listening | **67.6 ms** (42 log records) | template config, empty data |
| Startup config load (Server+Strings+Theme+Doors+FTN) | 0.61 ms | strings.json (26 KB) dominates |
| Menu render with 3 × 4 KB `%%file%%` includes | 74 µs, 106 KB, 104 allocs | 3 uncached `os.ReadFile` + **2 regex compiles per render**; nested includes multiply reads (depth ≤ 5) |
| Full ANSI screen display (8 KB CP437) | 87 µs, 61 KB, 16 allocs | disk read is minor; pipeline ~60 µs |
| Message-area init (50 areas) | 199 µs | once per startup — negligible |
| File-area init (1 area, 500 records) | 1.08 ms | JSON unmarshal + MkdirAll |
| File-area browse `GetFilesForArea` (500) | 5 µs, **64 KB copy**/call | full-load, no pagination (executor_files.go:772 TODO) |
| Idle server RSS | 22.7 MB | 1 telnet listener, no sessions |

**Verdict: there is no loading-performance problem today.** Per-render disk work is µs-scale on SSD.
The original "cache all ANSI/menu assets" hypothesis is *optional* polish, not a bottleneck — the cheapest
real wins are hoisting the include-regex to a package var and (optionally) an mtime-keyed include cache
for spinning-disk deployments.

### Lint / dead code (post Step-1 baseline)
- golangci-lint: **121 issues** (50 errcheck, 45 staticcheck, 26 unused) — down from 132 pre-audit. CI
  gates *new* issues on every PR (`only-new-issues`); this baseline is the burn-down target.
- `deadcode ./cmd/...` (reachability): 76 candidate findings. Treat as candidates — some are
  API-surface kept for planned features; verify per-symbol before deleting.

### Test coverage (mean 54.8%)
Zero tests: `internal/conference`, `internal/menueditor`, `internal/stringeditor`, `internal/types`,
`internal/util`, `internal/version` (scripting/syncjs got their first tests in PR #83).
Weakest with tests: configeditor 5.5%, **menu 6.7%** (the largest package), usereditor 14.1%.
Strongest: uitext 100%, archiver 94.4%, qwk 92.6%, session 90.3%.

### Duplication (quantified)
| Family | Size | Duplication | Consolidation approach |
|---|---|---|---|
| `menu/area_lightbar_conf.go` ↔ `area_lightbar_select.go` | 393/407 LOC | **~85%** | Generic lightbar picker: `[]T` + `buildItemLine(T)` + `onSelect(T)` callback (~320 LOC saved) |
| configeditor FTN ↔ V3Net browsers/wizards (5 pairs) | ~1,100 LOC | 25–75% per pair | Shared bordered-list renderer + cursor/scroll nav helper (~350–450 LOC saved) |
| ftn ↔ tosser/jam address & SEEN-BY | ~300 LOC | partial | Two independent 4D address parsers (`ftn.Address` vs `jam.FidoAddress`, ~80 LOC) + SEEN-BY overlap (~60 LOC). **Highest risk** — FTN wire behavior must not change |

## Step-by-step refactoring plan (proposed — nothing executed)

Ordered by value ÷ risk. Each step is one PR, gated by the now-in-place CI (tests + race + lint-on-new-code).

1. **Generic lightbar picker** (`internal/menu`) — merge the two ~400-line lightbar handlers into one
   generic component. Biggest single dedup win (~320 LOC), self-contained, pure refactor.
   *Cost: S · Risk: low.*
2. **Hoist `processFileIncludes` regex to a package-level var** and skip the re-scan pass when no include
   matched. Trivial, measurable (2 regex compiles per render → 0). *Cost: XS · Risk: none.*
3. **Split `internal/menu/executor.go` (2350) and `executor_admin_users.go` (1899)** along existing
   command-family seams (display/includes; navigation; admin subcommands). No behavior change; brings the
   worst offenders under ~600 each and unlocks testability of the render path. *Cost: M · Risk: low-med.*
4. **Dead-code burn-down**: delete the 26 `unused` lint findings package-by-package (editor first: 17
   reachability findings too), verifying each against planned-feature docs before removal. Shrinks the lint
   baseline toward zero so CI can eventually run in full-strict mode. *Cost: S–M · Risk: low.*
5. **configeditor shared list-box renderer + nav helper** — consolidate the FTN↔V3Net view/update mirror
   pairs (~400 LOC). View-layer only; both TUI paths must stay visually identical (snapshot the rendered
   strings in tests first). *Cost: M · Risk: med.*
6. **Paginated file-area listing** (`executor_files.go:772` TODO + tracked issue): add
   `GetFilesForAreaPaginated`/`GetFileCountForArea` to FileManager. Removes the 64 KB-per-browse copy and
   the unbounded growth path as areas scale past ~10k records. *Cost: M · Risk: low.*
7. **Coverage floor for the zero/near-zero packages**: characterization tests for `internal/menu`'s
   executor seams exposed by step 3, plus first tests for conference/menueditor/stringeditor/util.
   Target: no package below 30%, menu ≥ 25%. *Cost: L · Risk: none (tests only).*
8. **errcheck burn-down** (50 findings): fix or explicitly `_ =`-annotate per package, tightening the CI
   baseline as each package reaches zero. *Cost: M · Risk: low.*
9. **(Deferred / needs discussion) ftn↔tosser address unification** — smallest savings, touches FTN wire
   behavior on two live stacks; only worth doing with golden-packet fixtures in place first. *Cost: M ·
   Risk: high. Recommend: don't do it until third-party interop fixtures exist (see QWK deferred list).*
10. **(Optional) mtime-keyed ANSI/include cache** — measured benefit is µs-scale on SSD; implement only if
    a spinning-disk or network-FS deployment is a real target. *Cost: S · Risk: low.*

Explicitly **not** proposed: caching overhaul, startup optimization, binary-size work (all measured
healthy), and any V3Net/FTN behavior changes.

## Suggested sequencing

Steps 1–4 are independent and could proceed immediately after this report is approved (each is a small,
verifiable PR). Step 5 depends on none but benefits from snapshot tests. Steps 6–8 are ongoing-quality
work suitable for interleaving with feature development. Steps 9–10 are parked pending fixtures/need.
