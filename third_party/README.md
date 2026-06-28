# Vendored & patched third-party packages

This directory holds local, **patched** copies of upstream packages, wired in via
`replace` directives in the repo-root `go.mod`:

```
replace (
	github.com/charmbracelet/bubbletea v1.3.10 => ./third_party/charmbracelet-bubbletea
	github.com/charmbracelet/x/term     => ./third_party/charmbracelet-x-term
)
```

| Local copy | Upstream module | Pinned upstream version |
| ---------- | --------------- | ----------------------- |
| `charmbracelet-bubbletea/` | `github.com/charmbracelet/bubbletea` | `v1.3.10` |
| `charmbracelet-x-term/` | `github.com/charmbracelet/x/term` | `v0.2.2` (indirect) |

## Why these are patched

On **Windows 10 32-bit / pre-1709** builds (and some other older consoles), the
console does not support `ENABLE_VIRTUAL_TERMINAL_INPUT`. Upstream's
`MakeRaw`/`makeRaw` calls `SetConsoleMode` with that flag and **returns the error**
when the console rejects it, which makes the BBS client fail to start in raw
input mode on those platforms.

BubbleTea reads Windows console input via
[`coninput`](https://github.com/erikgeiser/coninput) (raw console event records),
so VT **input** mode is not actually required for input to work. The patch makes
raw-mode setup degrade gracefully instead of failing.

## The patch

**`charmbracelet-x-term/term_windows.go` — `makeRaw`:** when
`SetConsoleMode` with `ENABLE_VIRTUAL_TERMINAL_INPUT` fails, clear that bit and
retry raw mode without it (only return an error if the retry also fails):

```go
raw |= windows.ENABLE_VIRTUAL_TERMINAL_INPUT
if err := windows.SetConsoleMode(windows.Handle(fd), raw); err != nil {
	// ENABLE_VIRTUAL_TERMINAL_INPUT is unsupported on Windows pre-1709 and some
	// 32-bit builds. Fall back to raw mode without it; BubbleTea uses coninput
	// to read Windows console events directly and does not require VT input mode.
	raw &^= windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	if err2 := windows.SetConsoleMode(windows.Handle(fd), raw); err2 != nil {
		return nil, err2
	}
}
```

The `charmbracelet-bubbletea` copy is the matching fork at the same upstream tag;
its Windows input path (`inputreader_windows.go`, `key_windows.go`) relies on
`coninput` so it pairs with the x/term fallback above.

## Upgrading upstream

These `replace` copies pin specific upstream versions, so `go get -u` will **not**
move them. To upgrade:

1. Re-vendor the new upstream version into the corresponding `third_party/`
   directory.
2. Re-apply the `makeRaw` fallback shown above (diff against the previous local
   copy to recover the exact change), plus any other local deltas surfaced by:
   ```
   git diff <old-upstream-tag> -- term_windows.go      # in an upstream checkout
   ```
3. Bump the version in the root `go.mod` `replace`/`require` lines.
4. Verify on a pre-1709 / 32-bit Windows console (or a VM) that raw mode starts
   without error, then run `go build ./...` and `go test ./...`.

If upstream adopts an equivalent graceful fallback, drop the `replace` directives
and these copies entirely.
