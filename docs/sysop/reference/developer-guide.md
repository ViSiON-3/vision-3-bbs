# ViSiON/3 Developer Guide

## Setup

```bash
git clone https://github.com/ViSiON-3/vision-3-bbs.git
cd vision-3-bbs
go build ./cmd/vision3
```

`go.mod` requires Go 1.25. The other programs build the same way: `go build ./cmd/wfc`, `./cmd/config`, `./cmd/helper`, `./cmd/v3mail`, `./cmd/menuedit`, `./cmd/ue`, `./cmd/strings`.

## Architecture

```bash
cmd/vision3/main.go → SSH / Telnet Server → Session Handler → Menu Executor → Runnable Functions
```

### Key Components

- **Managers**: UserMgr, MessageManager, FileManager
- **Menu System**: Loads .MNU/.CFG/.ANS files, executes commands
- **Runnables**: Functions callable from menus via `RUN:` command

## Adding Features

### New Runnable Function

1. Write the function. Runnables take a `*cmdCtx` (executor, session, terminal, user manager, current user, node number, session start, output mode and terminal size) and the text after the command name:

```go
// RunnableFunc is defined in internal/menu/executor.go:
// type RunnableFunc func(c *cmdCtx, args string) (*user.User, string, error)

func runMyFeature(c *cmdCtx, args string) (*user.User, string, error) {
    msg := "|15Feature output|07\r\n"
    if err := terminalio.WriteProcessedBytes(c.terminal, ansi.ReplacePipeCodes([]byte(msg)), c.outputMode); err != nil {
        slog.Error("writing feature output", "node", c.nodeNumber, "error", err)
    }
    return nil, "", nil
}
```

1. Register it in `registerAppRunnables()` in `internal/menu/executor_runnables_registry.go`:

```go
registry["MYFEATURE"] = runMyFeature
```

1. Add a row for it to `docs/sysop/reference/menu-commands.md`. `TestEveryRegisteredMenuCommandIsDocumented` in `internal/menu/menu_commands_doc_test.go` fails the build when a registered command has no table row whose first cell is the backticked name, and `TestEveryDocumentedMenuCommandIsRegistered` fails when the page names a command that no longer exists.

1. Bind it in a menu's `.CFG` file (JSON, in `menus/v3/cfg/`):

```json
{ "KEYS": "M", "CMD": "RUN:MYFEATURE", "ACS": "*", "HIDDEN": false, "NODE_ACTIVITY": "My feature" }
```

### New Door

Add to `configs/doors.json` (the field list is `DoorConfig` in `internal/config/config_doors.go`):

```json
[
  {
    "code": "MYDOOR",
    "name": "My Door",
    "commands": ["/path/to/door", "-node", "{NODE}"],
    "working_directory": "/path/to/door",
    "dropfile_type": "DOOR.SYS",
    "io_mode": "STDIO",
    "requires_raw_terminal": true
  }
]
```

`type` selects the other kinds: `synchronet_js`, `v3_script`, `rlogin` or `telnet`; `is_dos` marks a DOS door run under dosemu. Menus launch a door with `DOOR:MYDOOR`.

## Code Standards

- Use `\r\n` for line endings
- Support both UTF-8 and CP437 modes
- Log through `log/slog` with the node as a field: `slog.Info("...", "node", nodeNumber)`. `internal/logging` installs the process logger; nothing uses `log.Printf`
- Handle errors properly, check all write operations
- Use pipe codes for colors: `|15` (white), `|07` (gray)

## Common Patterns

### ANSI Display

```go
// Load ANSI file
rawContent, err := ansi.GetAnsiFileContent(filepath.Join(e.MenuSetPath, "ansi", "file.ans"))
if err != nil {
    log.Printf("ERROR: Node %d: Failed loading ANSI: %v", nodeNumber, err)
    return nil, "", err
}

// Process for display and extract field coordinates
result, err := ansi.ProcessAnsiAndExtractCoords(rawContent, outputMode)
if err != nil {
    log.Printf("ERROR: Node %d: Failed processing ANSI: %v", nodeNumber, err)
    return nil, "", err
}

// result contains:
// - result.DisplayBytes: Processed ANSI content ready for display
// - result.FieldCoords: Map of field codes (|P, |O, etc.) to their (X, Y) coordinates
// - result.FieldColors: Map of field codes to their ANSI color sequences (cumulative SGR state)

// Write to terminal
wErr := terminalio.WriteProcessedBytes(terminal, result.DisplayBytes, outputMode)
if wErr != nil {
    log.Printf("ERROR: Node %d: Failed writing display: %v", nodeNumber, wErr)
}

// Use field coordinates and colors for positioned input
if coord, ok := result.FieldCoords["P"]; ok {
    // Move cursor to field position
    terminalio.WriteProcessedBytes(terminal, []byte(ansi.MoveCursor(coord.Y, coord.X)), outputMode)
    // Apply the color that was active at |P position (respects foreground, background, bold, etc.)
    if color, ok := result.FieldColors["P"]; ok && color != "" {
        terminalio.WriteProcessedBytes(terminal, []byte(color), outputMode)
    }
    input, _ := terminal.ReadLine()
    // Reset color attributes after input (required for bright colors and backgrounds)
    terminalio.WriteProcessedBytes(terminal, []byte("\x1b[0m"), outputMode)
}
```

### User Input

```go
// Read line input
input, err := terminal.ReadLine()
if err != nil {
    if errors.Is(err, io.EOF) {
        return nil, "LOGOFF", io.EOF  // User disconnected
    }
    return nil, "", err
}

// Read password (masked)
password, err := terminal.ReadPassword("Password: ")
```

### Path Management

```go
ansPath := filepath.Join(e.MenuSetPath, "ansi", "file.ans")
configPath := filepath.Join(e.RootConfigPath, "config.json")
dataPath := filepath.Join("data", "users", "users.json")
```

## Testing

- Test with both `--output-mode=utf8` and `--output-mode=cp437`
- Test with multiple concurrent users
- Verify menu navigation and error cases
- Test disconnection handling (Ctrl+C, network drops)

### What CI checks

`.github/workflows/ci.yml` gates every pull request and every push to `main`:

| Gate | What it runs |
|------|--------------|
| Format and vet | `gofmt -l` (outside `third_party/`) and `go vet ./...` |
| Lint | `golangci-lint` with `.golangci.yml`: govet, errcheck, staticcheck, unused, and revive's `exported` and `package-comments` rules, so every exported identifier and every package needs a doc comment (see CONTRIBUTING.md). It runs again for linux/386, windows/386 and linux/arm with `.golangci-32bit.yml` to catch unaligned 64-bit atomics |
| Tests | `go test ./...` on Linux and Windows, `go test -race ./...` on both, plus the vendored modules under `third_party/`; a macOS job runs the script-storage tests natively |
| Coverage floor | `scripts/coverage-check.sh` compares statement coverage of `internal/` with `scripts/coverage-floor` (87.0% at the time of writing). The floor is a ratchet: raise it in the same PR when coverage rises, never lower it |
| Docs | `internal/menu/menu_commands_doc_test.go` fails when a `RUN:` command or `login.json` step is missing from `docs/sysop/reference/menu-commands.md`, or that page names one that does not exist |

Run the same locally before opening a PR:

```bash
gofmt -l . && go vet ./... && golangci-lint run ./... && go test -race ./...
```

## Contributing

1. Fork and create feature branch
2. Make changes following code standards and `CONTRIBUTING.md` (doc-comment style)
3. Run the checks above
4. Submit PR with clear description

## Common Issues

- **Import errors**: Make sure all imports use `github.com/ViSiON-3/vision-3-bbs/internal/...`
- **ANSI display issues**: Always use `terminalio.WriteProcessedBytes()` for output
- **Path issues**: Use `e.MenuSetPath` for menu files, `e.RootConfigPath` for configs
