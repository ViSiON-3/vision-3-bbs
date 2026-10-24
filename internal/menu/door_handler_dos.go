//go:build !windows

package menu

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// dosDropfileName returns the DOS filename for the configured dropfile type.
func dosDropfileName(dropfileType string) string {
	switch strings.ToUpper(strings.TrimSpace(dropfileType)) {
	case "DOOR32.SYS":
		return "DOOR32.SYS"
	case "CHAIN.TXT":
		return "CHAIN.TXT"
	case "DORINFO1.DEF":
		return "DORINFO1.DEF"
	case dropfileIniType:
		return dropfileIniType
	default:
		return "DOOR.SYS"
	}
}

// dosSerialStart is the screen clear EXTERNAL.BAT echoes to COM1 just before
// a serial-mode door runs. DOS echo ends it with CRLF, which the output gate
// drops along with the boot text before it.
const dosSerialStart = "\x1b[2J\x1b[H"

// writeBatchFile generates EXTERNAL.BAT for dosemu2 execution.
func writeBatchFile(ctx *DoorCtx, batchPath string) error {
	slog.Info("writing batch file", "path", batchPath)
	crlf := "\r\n"

	var b strings.Builder
	b.WriteString("@echo off" + crlf)
	// Ensure dosemu commands (exitemu, etc.) are on the PATH.
	// D:\dosemu is the standard location for dosemu2-cmds.
	b.WriteString("SET PATH=%PATH%;D:\\dosemu" + crlf)
	b.WriteString("c:" + crlf)
	// Load FOSSIL driver if configured (needed by most BBS door games for serial I/O)
	if ctx.Config.FossilDriver != "" {
		b.WriteString(ctx.Config.FossilDriver + crlf)
	}

	// dosemu does not pass the host environment through, so a DROPFILE.INI
	// door gets its DROPFILE_INI variable from the batch file instead.
	if dosDropfileName(ctx.Config.DropfileType) == dropfileIniType && ctx.Subs["{DOSDROPFILE}"] != "" {
		b.WriteString("SET " + dropfileIniEnvVar + "=" + ctx.Subs["{DOSDROPFILE}"] + crlf)
	}

	// Clear screen — the PTY bridge detects this ESC[2J sequence to know
	// boot is done, discarding all prior DOS boot text. A serial-mode door's
	// screen never reaches the PTY, so it gets the clear through COM1 instead.
	b.WriteString("cls" + crlf)
	if ctx.dosSerial() {
		b.WriteString("echo " + dosSerialStart + ">COM1" + crlf)
	}

	// Auto-cd to the door's working directory if configured
	if ctx.Config.WorkingDirectory != "" {
		// Switch drive letter first (e.g. "D:"), then cd with quotes for spaces
		if len(ctx.Config.WorkingDirectory) >= 2 && ctx.Config.WorkingDirectory[1] == ':' {
			b.WriteString(ctx.Config.WorkingDirectory[:2] + crlf)
		}
		b.WriteString(fmt.Sprintf("cd \"%s\"", ctx.Config.WorkingDirectory) + crlf)
	}

	for _, cmd := range ctx.Config.Commands {
		processed := strings.ReplaceAll(cmd, "{NODE}", ctx.NodeNumStr)
		for key, val := range ctx.Subs {
			processed = strings.ReplaceAll(processed, key, val)
		}
		// In DOS, running a .BAT without CALL replaces the current batch
		// context — execution never returns, so exitemu would never run.
		// Prepend CALL when the first token (the executable) ends with .BAT,
		// so control returns to EXTERNAL.BAT after the called batch finishes.
		trimUpper := strings.ToUpper(strings.TrimSpace(processed))
		if !strings.HasPrefix(trimUpper, "CALL ") {
			firstToken := strings.Fields(trimUpper)
			if len(firstToken) > 0 && strings.HasSuffix(firstToken[0], ".BAT") {
				processed = "CALL " + processed
			}
		}
		b.WriteString(processed + crlf)
	}

	b.WriteString("exitemu" + crlf)

	return os.WriteFile(batchPath, []byte(b.String()), 0600)
}

// writeDosemurc generates a per-node dosemurc config file that points
// $_hdimage at the correct drive_c path and allows lredir to the node dir.
func writeDosemurc(ctx *DoorCtx, rcPath, driveCPath, nodePath string) error {
	slog.Info("writing dosemurc", "path", rcPath, "driveC", driveCPath)

	// Start from the user's custom config if one exists, otherwise use defaults
	baseConfig := ctx.Config.DosemuConfig
	var content string
	if baseConfig != "" {
		data, err := os.ReadFile(baseConfig)
		if err != nil {
			return fmt.Errorf("failed to read base dosemurc %s: %w", baseConfig, err)
		}
		content = string(data)
	} else {
		// Read the default ~/.dosemu/.dosemurc if it exists
		homeDir, _ := os.UserHomeDir()
		defaultRc := filepath.Join(homeDir, ".dosemu", ".dosemurc")
		if data, err := os.ReadFile(defaultRc); err == nil {
			content = string(data)
		}
	}

	// Strip settings that we manage ourselves to avoid conflicts.
	// $_hdimage is handled via --Fdrive_c CLI argument instead, because
	// $_hdimage "+0" always resolves to ~/.dosemu/drive_c.
	// $_video must not be set so the -t flag can select Video_term.
	// $_com1 is managed below depending on whether the door uses COM1. A
	// door's own config that maps COM1 to the terminal (a door that loads its
	// own FOSSIL driver, or talks to the port directly) puts the door in
	// serial mode just as a FOSSIL Driver setting does. The shared
	// ~/.dosemu/.dosemurc does not: the installed template sets a virtual
	// COM1, and terminal-mode doors share it.
	var filtered strings.Builder
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if value, ok := com1Setting(trimmed); ok {
			// The last assignment wins, as it does in dosemu.
			ctx.baseCOM1Virtual = baseConfig != "" && strings.EqualFold(value, "virtual")
			continue
		}
		if strings.HasPrefix(trimmed, "$_hdimage") ||
			strings.HasPrefix(trimmed, "$_video") ||
			strings.HasPrefix(trimmed, "$_vga") {
			continue
		}
		filtered.WriteString(line)
		filtered.WriteString("\n")
	}
	content = filtered.String()

	// Append the critical settings
	content += fmt.Sprintf("\n# Generated by ViSiON/3 for node %d\n", ctx.NodeNumber)
	content += fmt.Sprintf("$_lredir_paths = \"%s\"\n", nodePath)

	if ctx.dosSerial() {
		// FOSSIL mode: doors communicate via serial port through a FOSSIL driver.
		// $_com1 = "virtual" maps COM1 to the PTY for serial I/O.
		// $_video = "none" suppresses all screen output (no boot text on PTY).
		// The -t flag is NOT used in FOSSIL mode — only serial data reaches the PTY.
		content += "$_com1 = \"virtual\"\n"
		content += "$_video = \"none\"\n"
	} else {
		// Terminal mode: doors write directly to the DOS screen (INT 10h).
		// -t translates screen writes to ANSI on the PTY. No serial port needed.
		content += "$_term_color = (on)\n"
		content += "$_term_esc_char = (27)\n"
	}

	return os.WriteFile(rcPath, []byte(content), 0600)
}

// com1Setting reports whether a trimmed dosemurc line sets $_com1 (and not,
// say, $_com10), and returns its value with any comment, trailing semicolon
// and quotes removed, e.g. virtual for $_com1 = "virtual".
func com1Setting(line string) (string, bool) {
	name, value, _ := strings.Cut(line, "=")
	if strings.TrimSpace(name) != "$_com1" {
		return "", false
	}
	value, _, _ = strings.Cut(value, "#")
	value = strings.TrimSuffix(strings.TrimSpace(value), ";")
	return strings.Trim(strings.TrimSpace(value), `"'`), true
}

// indexDoorStart returns the offset and length of the earliest of starts in
// b, or -1 if none occurs.
func indexDoorStart(b []byte, starts [][]byte) (int, int) {
	idx, n := -1, 0
	for _, start := range starts {
		if i := bytes.Index(b, start); i >= 0 && (idx < 0 || i < idx) {
			idx, n = i, len(start)
		}
	}
	return idx, n
}

// executeDOSDoor launches a DOS door program via dosemu2.
// Uses PTY bridging: dosemu's terminal translator converts INT 10h screen
// output to ANSI on stdout, which flows through the PTY to the SSH session.
// Boot text is suppressed by gating PTY output until the screen clear that
// EXTERNAL.BAT issues just before the door runs.
func executeDOSDoor(ctx *DoorCtx) error {
	// Determine drive_c path (supports relative paths resolved against BBS root)
	driveCPath := ctx.Config.DriveCPath
	if driveCPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		driveCPath = filepath.Join(homeDir, ".dosemu", "drive_c")
	} else if !filepath.IsAbs(driveCPath) {
		// Resolve relative paths against BBS root (parent of configs dir)
		bbsRoot := filepath.Dir(ctx.Executor.RootConfigPath)
		driveCPath = filepath.Join(bbsRoot, driveCPath)
	}

	// Determine dosemu binary path — use dosemu2.bin directly to bypass
	// the bash wrapper script which mangles backslashes via eval.
	dosemuPath := ctx.Executor.GetServerConfig().DosemuPath
	if dosemuPath == "" {
		dosemuPath = "/usr/libexec/dosemu2/dosemu2.bin"
	}

	// Create per-node temp directory inside drive_c
	nodeDir := fmt.Sprintf("temp%d", ctx.NodeNumber)
	nodePath := filepath.Join(driveCPath, "nodes", nodeDir)
	if err := os.MkdirAll(nodePath, 0700); err != nil {
		return fmt.Errorf("failed to create node directory %s: %w", nodePath, err)
	}

	dosNodeDir := fmt.Sprintf("C:\\NODES\\%s", strings.ToUpper(nodeDir))

	// Generate per-node dosemurc with correct drive_c path. This comes first
	// because it settles whether the door uses COM1, which the dropfiles report.
	nodeRcPath := filepath.Join(nodePath, "dosemurc")
	if err := writeDosemurc(ctx, nodeRcPath, driveCPath, nodePath); err != nil {
		return fmt.Errorf("failed to write dosemurc: %w", err)
	}

	// Generate all dropfiles
	if err := generateAllDropfiles(ctx, nodePath, dosNodeDir); err != nil {
		return fmt.Errorf("failed to generate dropfiles: %w", err)
	}
	defer cleanupDropfiles(nodePath)

	// Populate placeholders for batch file substitution
	dropfileName := dosDropfileName(ctx.Config.DropfileType)
	ctx.Subs["{NODEDIR}"] = nodePath
	ctx.Subs["{DROPFILE}"] = filepath.Join(nodePath, dropfileName)
	ctx.Subs["{DOSNODEDIR}"] = dosNodeDir
	ctx.Subs["{DOSDROPFILE}"] = dosNodeDir + "\\" + dropfileName

	// Write batch file
	batchPath := filepath.Join(nodePath, "EXTERNAL.BAT")
	if err := writeBatchFile(ctx, batchPath); err != nil {
		return fmt.Errorf("failed to write batch file: %w", err)
	}

	// Build dosemu command
	dosBatchPath := fmt.Sprintf("C:\\NODES\\%s\\EXTERNAL.BAT", strings.ToUpper(nodeDir))
	logPath := filepath.Join(nodePath, "dosemu_boot.log")

	// Always use the generated per-node config (writeDosemurc already merges
	// ctx.Config.DosemuConfig as a base template when set)
	rcPath := nodeRcPath

	// Build dosemu args: FOSSIL mode uses serial I/O (no -t needed),
	// terminal mode uses -t for screen-to-ANSI translation.
	args := []string{}
	if !ctx.dosSerial() {
		args = append(args, "-t")
	}
	args = append(args,
		"-f", rcPath,
		"--Fdrive_c", driveCPath,
		"-E", dosBatchPath,
		"-o", logPath,
	)

	slog.Info("launching DOS door via dosemu2", "node", ctx.NodeNumber, "door", ctx.DoorName, "path", dosemuPath, "args", args)

	cmd := exec.Command(dosemuPath, args...)
	cmd.Dir = driveCPath
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "DOSEMU_QUIET=1")

	// Start dosemu with a PTY for terminal-mode I/O. Dosemu translates DOS
	// screen output (INT 10h) to ANSI via $_term_color and writes to stdout.
	// The PTY bridges this to the SSH session.
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 25, Cols: 80})
	if err != nil {
		return fmt.Errorf("failed to start dosemu2 with pty: %w", err)
	}
	stopDoorWatch := watchDoorProcess(ctx, cmd.Process)

	ptmx = pollableDoorFile(ptmx)

	// Set PTY master to raw mode for clean passthrough of CP437 bytes. The
	// original state is restored after the output drain, before the PTY closes.
	var oldState *term.State
	_ = withRawFD(ptmx, func(fd int) { // best-effort: the door runs either way
		if st, err := term.MakeRaw(fd); err == nil {
			oldState = st
		}
	})

	// Set up a read interrupt so we can cleanly stop the input goroutine
	// when the door exits, preventing it from consuming the next keypress.
	// Sessions that support SetReadInterrupt (SSH) get clean cancellation;
	// others (telnet) fall back to the old behavior where the input goroutine
	// exits on its own when ptmx is closed and the next write fails.
	readInterrupt := make(chan struct{})
	hasInterrupt := false
	if ri, ok := ctx.Session.(interface{ SetReadInterrupt(<-chan struct{}) }); ok {
		ri.SetReadInterrupt(readInterrupt)
		defer ri.SetReadInterrupt(nil)
		hasInterrupt = true
	}

	// Bidirectional I/O: SSH session ↔ PTY master
	//
	// Two modes depending on whether a FOSSIL driver is configured:
	//
	// FOSSIL mode: $_com1="virtual" maps COM1 to the PTY. The door's output
	// flows through the FOSSIL driver → COM1 → PTY. $_video="none" suppresses
	// DOS screen output, but dosemu still writes plain-ASCII boot text directly
	// to the PTY. The output goroutine buffers until the screen clear that
	// EXTERNAL.BAT echoes to COM1 (dosSerialStart and its CRLF), so a door
	// whose first output is plain text is shown at once.
	//
	// Terminal mode: -t translates DOS screen writes (INT 10h) to ANSI on
	// the PTY. The output goroutine buffers until it detects ESC[2J (clear
	// screen from `cls` in EXTERNAL.BAT), discarding DOS boot text before it.
	//
	// Either way, starts are the forms of the marker that ends boot and keep
	// is how much of it the caller gets. The serial marker's CRLF arrives as
	// CR CR LF if dosemu writes it before the PTY is made raw below.
	starts, keep := [][]byte{[]byte("\x1b[2J")}, len("\x1b[2J")
	if ctx.dosSerial() {
		starts = [][]byte{[]byte(dosSerialStart + "\r\n"), []byte(dosSerialStart + "\r\r\n")}
		keep = len(dosSerialStart)
	}

	inputDone := make(chan struct{})
	outputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		_, err := io.Copy(ptmx, ctx.Session)
		if err != nil && err != io.EOF && !errors.Is(err, os.ErrClosed) {
			slog.Warn("error copying session stdin to dosemu PTY", "node", ctx.NodeNumber, "error", err)
		}
	}()
	go func() {
		defer close(outputDone)
		buf := make([]byte, 4096)
		var accum []byte
		gated := true

		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if gated {
					accum = append(accum, buf[:n]...)
					if idx, n := indexDoorStart(accum, starts); idx >= 0 {
						// Found the marker — forward it and everything after
						gated = false
						_, _ = ctx.Session.Write(accum[idx : idx+keep]) // best-effort door output relay
						_, _ = ctx.Session.Write(accum[idx+n:])         // best-effort door output relay
						accum = nil
						slog.Debug("DOS boot text gate opened", "node", ctx.NodeNumber)
					}
					// Safety: if we've accumulated too much without finding the marker, flush it
					if gated && len(accum) > 32768 {
						slog.Warn("DOS boot text gate: no marker after 32KB, flushing", "node", ctx.NodeNumber)
						gated = false
						_, _ = ctx.Session.Write(accum) // best-effort door output relay
						accum = nil
					}
				} else {
					_, _ = ctx.Session.Write(buf[:n]) // best-effort door output relay
				}
			}
			if err != nil {
				if gated && len(accum) > 0 {
					slog.Warn("DOS boot text gate never opened", "node", ctx.NodeNumber, "bytes", len(accum), "first200", accum[:min(200, len(accum))])
				}
				if !isDoorOutputEnd(err) {
					slog.Warn("error copying dosemu PTY to session", "node", ctx.NodeNumber, "error", err)
				}
				return
			}
		}
	}()

	// Wait for dosemu to exit, then cleanly shut down I/O goroutines
	cmdErr := cmd.Wait()
	stopDoorWatch()
	slog.Debug("dosemu2 process exited", "node", ctx.NodeNumber, "door", ctx.DoorName)

	// Interrupt the input goroutine's blocked Read() so it exits without
	// consuming the user's next keypress, then close the PTY.
	close(readInterrupt)
	if hasInterrupt {
		<-inputDone
	}
	// Relay what the door wrote on its way out before closing the PTY.
	drainDoorOutput(ptmx, outputDone, ctx.NodeNumber, ctx.DoorName)
	if oldState != nil {
		_ = withRawFD(ptmx, func(fd int) { _ = term.Restore(fd, oldState) }) // best-effort terminal restore
	}
	_ = ptmx.Close() // best-effort PTY teardown

	if cmdErr != nil {
		slog.Error("DOS door failed", "node", ctx.NodeNumber, "door", ctx.DoorName, "error", cmdErr)
	} else {
		slog.Info("DOS door completed successfully", "node", ctx.NodeNumber, "door", ctx.DoorName)
	}

	// Run cleanup while dropfiles/node dirs still exist (before deferred cleanup fires)
	executeCleanup(ctx)

	return cmdErr
}
