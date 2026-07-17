//go:build windows

package menu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// executeNativeDoorWindows runs a native door program on Windows using STDIO redirection.
// PTY mode is not supported on Windows; RequiresRawTerminal is ignored with a warning.
func executeNativeDoorWindows(ctx *DoorCtx) error {
	doorConfig := ctx.Config

	// --- Dropfile Generation ---
	var dropfilePath string
	dropfileDir := "."
	if doorConfig.WorkingDirectory != "" {
		dropfileDir = doorConfig.WorkingDirectory
	}

	// Configurable dropfile location: "node" uses a unique per-node temp directory.
	// Uses os.MkdirTemp for unique names and defers os.RemoveAll unconditionally
	// so the directory is always cleaned up, even if no recognized dropfile is generated.
	dropfileLoc := strings.ToLower(doorConfig.DropfileLocation)
	if dropfileLoc == "node" {
		nodeDir, err := os.MkdirTemp("", fmt.Sprintf("vision3_node%d_", ctx.NodeNumber))
		if err != nil {
			return fmt.Errorf("failed to create node dropfile directory: %w", err)
		}
		defer func() {
			if err := os.RemoveAll(nodeDir); err != nil {
				slog.Warn("failed to remove node dropfile dir", "dir", nodeDir, "error", err)
			}
		}()
		dropfileDir = nodeDir
	}

	dropfileTypeUpper := strings.ToUpper(doorConfig.DropfileType)

	if dropfileTypeUpper == "DOOR.SYS" || dropfileTypeUpper == "CHAIN.TXT" || dropfileTypeUpper == "DOOR32.SYS" || dropfileTypeUpper == "DORINFO1.DEF" {
		dropfilePath = filepath.Join(dropfileDir, dropfileTypeUpper)
		slog.Info("generating dropfile", "type", dropfileTypeUpper, "path", dropfilePath)

		var genErr error
		switch dropfileTypeUpper {
		case "DOOR.SYS":
			genErr = generateDoorSys(ctx, dropfileDir)
		case "DOOR32.SYS":
			genErr = generateDoor32Sys(ctx, dropfileDir)
		case "CHAIN.TXT":
			genErr = generateChainTxt(ctx, dropfileDir)
		case "DORINFO1.DEF":
			genErr = generateDorInfo(ctx, dropfileDir)
		}

		if genErr != nil {
			slog.Error("failed to write dropfile", "path", dropfilePath, "error", genErr)
			errMsg := fmt.Sprintf(ctx.Executor.LoadedStrings.DoorDropfileError, ctx.DoorName)
			if wErr := terminalio.WriteProcessedBytes(ctx.Session.Stderr(), ansi.ReplacePipeCodes([]byte(errMsg)), ctx.OutputMode); wErr != nil {
				slog.Error("failed writing dropfile creation error message", "error", wErr)
			}
			return genErr
		}

		defer func() {
			slog.Debug("cleaning up dropfile", "path", dropfilePath)
			if err := os.Remove(dropfilePath); err != nil {
				slog.Warn("failed to remove dropfile", "path", dropfilePath, "error", err)
			}
		}()
	}

	// Populate placeholders
	ctx.Subs["{DROPFILE}"] = dropfilePath
	ctx.Subs["{NODEDIR}"] = dropfileDir

	// Extract command and args from Commands slice (native: [0]=executable, [1:]=args)
	if len(doorConfig.Commands) == 0 || doorConfig.Commands[0] == "" {
		return fmt.Errorf("door %q has no command configured", ctx.DoorName)
	}
	doorCommand := doorConfig.Commands[0]
	doorArgs := doorConfig.Commands[1:]

	// Substitute placeholders in command and arguments
	for key, val := range ctx.Subs {
		doorCommand = strings.ReplaceAll(doorCommand, key, val)
	}
	substitutedArgs := make([]string, len(doorArgs))
	for i, arg := range doorArgs {
		newArg := arg
		for key, val := range ctx.Subs {
			newArg = strings.ReplaceAll(newArg, key, val)
		}
		substitutedArgs[i] = newArg
	}

	// Substitute in Environment Variables
	substitutedEnv := make(map[string]string)
	if doorConfig.EnvironmentVars != nil {
		for key, val := range doorConfig.EnvironmentVars {
			newVal := val
			for subKey, subVal := range ctx.Subs {
				newVal = strings.ReplaceAll(newVal, subKey, subVal)
			}
			substitutedEnv[key] = newVal
		}
	}

	// Prepare command — optionally wrap in cmd.exe.
	// When using cmd.exe, we build the command line manually via SysProcAttr.CmdLine
	// because cmd.exe uses different parsing rules than Go's standard CommandLineToArgvW
	// quoting. This prevents special characters (&|^()%!) from being reinterpreted.
	var cmd *exec.Cmd
	if doorConfig.UseShell {
		cmd = exec.Command("cmd")
		cmdLine := "cmd /c " + syscall.EscapeArg(doorCommand)
		for _, arg := range substitutedArgs {
			cmdLine += " " + syscall.EscapeArg(arg)
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdLine}
		slog.Debug("using shell execution", "node", ctx.NodeNumber, "command", doorCommand, "argCount", len(substitutedArgs))
	} else {
		cmd = exec.Command(doorCommand, substitutedArgs...)
	}

	if doorConfig.WorkingDirectory != "" {
		cmd.Dir = doorConfig.WorkingDirectory
	}

	// Set environment variables
	cmd.Env = os.Environ()
	if len(substitutedEnv) > 0 {
		for key, val := range substitutedEnv {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, val))
		}
	}

	// Add standard BBS env vars
	envMap := make(map[string]bool)
	for _, envPair := range cmd.Env {
		envMap[strings.SplitN(envPair, "=", 2)[0]] = true
	}
	if _, exists := envMap["BBS_USERHANDLE"]; !exists {
		cmd.Env = append(cmd.Env, fmt.Sprintf("BBS_USERHANDLE=%s", ctx.User.Handle))
	}
	if _, exists := envMap["BBS_USERID"]; !exists {
		cmd.Env = append(cmd.Env, fmt.Sprintf("BBS_USERID=%s", ctx.UserIDStr))
	}
	if _, exists := envMap["BBS_NODE"]; !exists {
		cmd.Env = append(cmd.Env, fmt.Sprintf("BBS_NODE=%s", ctx.NodeNumStr))
	}
	if _, exists := envMap["BBS_TIMELEFT"]; !exists {
		cmd.Env = append(cmd.Env, fmt.Sprintf("BBS_TIMELEFT=%s", ctx.TimeLeftStr))
	}

	if strings.EqualFold(doorConfig.IOMode, "SOCKET") {
		return fmt.Errorf("socket I/O mode is not implemented on Windows yet")
	}

	if doorConfig.RequiresRawTerminal {
		slog.Warn("door requires raw terminal, but PTY is not supported on Windows, falling back to STDIO", "node", ctx.NodeNumber, "door", ctx.DoorName)
	}

	slog.Info("starting door with standard I/O redirection (Windows)", "node", ctx.NodeNumber, "door", ctx.DoorName)
	cmd.Stdout = ctx.Session
	cmd.Stderr = ctx.Session
	cmd.Stdin = ctx.Session
	cmdErr := cmd.Run()

	time.Sleep(100 * time.Millisecond)

	// Run cleanup while dropfiles/node dirs still exist (before deferred cleanup fires)
	executeCleanupWindows(ctx)

	return cmdErr
}

// executeDoor dispatches to the appropriate executor.
// DOS doors require dosemu2 (Linux) or NTVDM (32-bit Windows, not yet implemented).
// Native doors are supported on Windows via STDIO redirection.
func executeDoor(ctx *DoorCtx) error {
	// Single-instance locking
	if ctx.Config.SingleInstance {
		if err := acquireDoorLock(ctx.DoorName, ctx.NodeNumber); err != nil {
			if errors.Is(err, ErrDoorBusy) {
				slog.Warn("door already in use by another node", "node", ctx.NodeNumber, "door", ctx.DoorName)
				return fmt.Errorf("%w: %s", ErrDoorBusy, ctx.DoorName)
			}
			slog.Error("failed to acquire lock for door", "node", ctx.NodeNumber, "door", ctx.DoorName, "error", err)
			return fmt.Errorf("failed to acquire door lock: %w", err)
		}
		defer releaseDoorLock(ctx.DoorName, ctx.NodeNumber)
	}

	if ctx.Config.Type == "synchronet_js" {
		return executeSyncJSDoor(ctx)
	}
	if ctx.Config.Type == "v3_script" {
		return executeV3ScriptDoor(ctx)
	}

	if ctx.Config.IsDOS {
		return fmt.Errorf("DOS doors are not yet supported on Windows; use dosemu2 on Linux (NTVDM support is planned)")
	}

	return executeNativeDoorWindows(ctx)
}

// cleanupTimeout is the maximum time allowed for a cleanup command to run.
const cleanupTimeout = 30 * time.Second

// executeCleanupWindows runs the optional post-door cleanup command on Windows with a timeout.
func executeCleanupWindows(ctx *DoorCtx) {
	if ctx.Config.CleanupCommand == "" {
		return
	}

	substitutedArgs := make([]string, len(ctx.Config.CleanupArgs))
	for i, arg := range ctx.Config.CleanupArgs {
		newArg := arg
		for key, val := range ctx.Subs {
			newArg = strings.ReplaceAll(newArg, key, val)
		}
		substitutedArgs[i] = newArg
	}

	slog.Info("running cleanup command for door",
		"node", ctx.NodeNumber, "door", ctx.DoorName, "command", ctx.Config.CleanupCommand, "args", substitutedArgs)

	cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()

	cmd := exec.CommandContext(cleanupCtx, ctx.Config.CleanupCommand)
	// Build command line with proper escaping so .bat/.cmd cleanup scripts
	// don't reinterpret special characters in substituted arguments.
	cmdLine := syscall.EscapeArg(ctx.Config.CleanupCommand)
	for _, arg := range substitutedArgs {
		cmdLine += " " + syscall.EscapeArg(arg)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdLine}
	if ctx.Config.WorkingDirectory != "" {
		cmd.Dir = ctx.Config.WorkingDirectory
	}
	cmd.Env = os.Environ()

	if output, err := cmd.CombinedOutput(); err != nil {
		if cleanupCtx.Err() == context.DeadlineExceeded {
			slog.Warn("cleanup command for door timed out",
				"node", ctx.NodeNumber, "door", ctx.DoorName, "timeout", cleanupTimeout)
		} else {
			slog.Warn("cleanup command for door failed",
				"node", ctx.NodeNumber, "door", ctx.DoorName, "error", err, "output", string(output))
		}
	} else {
		slog.Debug("cleanup command for door completed successfully", "node", ctx.NodeNumber, "door", ctx.DoorName)
	}
}

// doorErrorMessage writes a formatted error message to the session.
func doorErrorMessage(ctx *DoorCtx, msg string) {
	errMsg := fmt.Sprintf(ctx.Executor.LoadedStrings.DoorErrorFormat, msg)
	wErr := terminalio.WriteProcessedBytes(ctx.Session.Stderr(), ansi.ReplacePipeCodes([]byte(errMsg)), ctx.OutputMode)
	if wErr != nil {
		slog.Error("failed writing door error message", "error", wErr)
	}
}

// runListDoors lists configured doors from the door registry on Windows.
func runListDoors(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	slog.Debug("running LISTDOORS (Windows)", "node", nodeNumber)

	if currentUser == nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.DoorLoginRequired)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Load templates (same as non-Windows)
	topPath := filepath.Join(e.MenuSetPath, "templates", "DOORLIST.TOP")
	midPath := filepath.Join(e.MenuSetPath, "templates", "DOORLIST.MID")
	botPath := filepath.Join(e.MenuSetPath, "templates", "DOORLIST.BOT")

	topBytes, errTop := readTemplateFile(topPath)
	midBytes, errMid := readTemplateFile(midPath)
	botBytes, errBot := readTemplateFile(botPath)

	if errTop != nil || errMid != nil || errBot != nil {
		slog.Error("failed to load DOORLIST templates", "node", nodeNumber, "topError", errTop, "midError", errMid, "botError", errBot)
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.DoorTemplateError)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	// Display header
	processedTop := ansi.ReplacePipeCodes(topBytes)
	if outputMode == ansi.OutputModeCP437 {
		terminal.Write(processedTop)
	} else {
		terminalio.WriteProcessedBytes(terminal, processedTop, outputMode)
	}

	// Get door registry
	e.configMu.RLock()
	doorRegistryCopy := make(map[string]config.DoorConfig, len(e.DoorRegistry))
	for k, v := range e.DoorRegistry {
		doorRegistryCopy[k] = v
	}
	e.configMu.RUnlock()

	doorCodes := make([]string, 0, len(doorRegistryCopy))
	for code := range doorRegistryCopy {
		doorCodes = append(doorCodes, code)
	}
	sort.Strings(doorCodes)

	// Display each door (skip doors the user lacks access to)
	midTemplate := string(ansi.ReplacePipeCodes(midBytes))
	displayIdx := 0
	for _, code := range doorCodes {
		doorCfg := doorRegistryCopy[code]

		// Filter out doors the user doesn't have access to
		if doorCfg.MinAccessLevel > 0 && currentUser.AccessLevel < doorCfg.MinAccessLevel {
			continue
		}

		displayIdx++
		line := formatDoorListLine(midTemplate, displayIdx, code, doorCfg)
		terminalio.WriteProcessedBytes(terminal, []byte(line), outputMode)
	}

	if displayIdx == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.DoorNoneConfigured)), outputMode)
	}

	// Display footer
	processedBot := ansi.ReplacePipeCodes(botBytes)
	if outputMode == ansi.OutputModeCP437 {
		terminal.Write(processedBot)
	} else {
		terminalio.WriteProcessedBytes(terminal, processedBot, outputMode)
	}

	return currentUser, "", nil
}

// runOpenDoor prompts for a door name and launches it on Windows.
func runOpenDoor(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	slog.Debug("running OPENDOOR (Windows)", "node", nodeNumber)

	if currentUser == nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.DoorLoginRequired)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	renderedPrompt := ansi.ReplacePipeCodes([]byte(e.LoadedStrings.DoorPrompt))
	curUpClear := "\x1b[A\r\x1b[2K"

	terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)

	for {
		inputName, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			slog.Error("error reading OPENDOOR input", "node", nodeNumber, "error", err)
			return currentUser, "", err
		}

		inputClean := strings.TrimSpace(inputName)
		upperInput := strings.ToUpper(inputClean)

		if upperInput == "Q" {
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			return currentUser, "", nil
		}
		if upperInput == "" {
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		if upperInput == "?" {
			runListDoors(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, "")
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		doorConfig, exists := e.GetDoorConfig(upperInput)
		if !exists {
			terminalio.WriteProcessedBytes(terminal, []byte(curUpClear), outputMode)
			msg := fmt.Sprintf(e.LoadedStrings.DoorNotFoundFormat, inputClean)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\x1b[2K"), outputMode)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		// Check per-door access level
		if doorConfig.MinAccessLevel > 0 && currentUser.AccessLevel < doorConfig.MinAccessLevel {
			slog.Warn("user denied access to door",
				"node", nodeNumber, "handle", currentUser.Handle, "level", currentUser.AccessLevel, "door", upperInput, "required", doorConfig.MinAccessLevel)
			msg := fmt.Sprintf(e.LoadedStrings.DoorAccessDenied, upperInput)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		ctx := buildDoorCtx(e, s, terminal,
			currentUser.ID, currentUser.Handle, currentUser.RealName,
			currentUser.AccessLevel, currentUser.TimeLimit, currentUser.TimesCalled,
			currentUser.GroupLocation,
			termWidth, termHeight,
			nodeNumber, sessionStartTime, outputMode,
			doorConfig, upperInput)

		resetSessionIH(s)
		cmdErr := executeDoor(ctx)
		_ = getSessionIH(s)

		if cmdErr != nil {
			if errors.Is(cmdErr, ErrDoorBusy) {
				slog.Info("door is busy for user", "node", nodeNumber, "door", upperInput, "handle", currentUser.Handle)
				busyFmt := e.LoadedStrings.DoorBusyFormat
				if strings.TrimSpace(busyFmt) == "" {
					busyFmt = "\r\n|14Door is currently in use: |11%s|07\r\n"
				}
				busyMsg := fmt.Sprintf(busyFmt, upperInput)
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(busyMsg)), outputMode)
				time.Sleep(1 * time.Second)
			} else {
				slog.Error("door execution failed", "node", nodeNumber, "user", currentUser.Handle, "door", upperInput, "error", cmdErr)
				doorErrorMessage(ctx, fmt.Sprintf("Error running door '%s': %v", upperInput, cmdErr))
			}
		} else {
			slog.Info("door completed", "node", nodeNumber, "user", currentUser.Handle, "door", upperInput)
		}

		return currentUser, "", nil
	}
}

// runDoorInfo shows door configuration details on Windows.
func runDoorInfo(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode
	termWidth := c.termWidth
	termHeight := c.termHeight

	slog.Debug("running DOORINFO (Windows)", "node", nodeNumber)

	if currentUser == nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.DoorInfoLoginRequired)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	renderedPrompt := ansi.ReplacePipeCodes([]byte(e.LoadedStrings.DoorPrompt))
	curUpClear := "\x1b[A\r\x1b[2K"

	terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)

	for {
		inputName, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			return currentUser, "", err
		}

		inputClean := strings.TrimSpace(inputName)
		upperInput := strings.ToUpper(inputClean)

		if upperInput == "Q" {
			terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
			return currentUser, "", nil
		}
		if upperInput == "" {
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		if upperInput == "?" {
			runListDoors(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, "")
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		doorConfig, exists := e.GetDoorConfig(upperInput)
		if !exists {
			terminalio.WriteProcessedBytes(terminal, []byte(curUpClear), outputMode)
			msg := fmt.Sprintf(e.LoadedStrings.DoorNotFoundFormat, inputClean)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			terminalio.WriteProcessedBytes(terminal, []byte("\r\x1b[2K"), outputMode)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		// Check per-door access level
		if doorConfig.MinAccessLevel > 0 && currentUser.AccessLevel < doorConfig.MinAccessLevel {
			slog.Warn("user denied access to door info",
				"node", nodeNumber, "handle", currentUser.Handle, "level", currentUser.AccessLevel, "door", upperInput, "required", doorConfig.MinAccessLevel)
			msg := fmt.Sprintf(e.LoadedStrings.DoorAccessDenied, upperInput)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		// Display door info
		terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
		doorType := "Native Windows"
		switch {
		case doorConfig.Type == "v3_script":
			doorType = "VPL Script"
		case doorConfig.Type == "synchronet_js":
			doorType = "Synchronet JS"
		case doorConfig.IsDOS:
			doorType = "DOS (not supported on Windows)"
		}

		info := fmt.Sprintf("|15Door: |07%s\r\n|15Type: |07%s\r\n", upperInput, doorType)
		if len(doorConfig.Commands) > 0 {
			info += fmt.Sprintf("|15Commands: |07%s\r\n", strings.Join(doorConfig.Commands, ", "))
		}
		if doorConfig.WorkingDirectory != "" {
			info += fmt.Sprintf("|15Directory: |07%s\r\n", doorConfig.WorkingDirectory)
		}
		if doorConfig.DropfileType != "" {
			dropLoc := doorConfig.DropfileLocation
			if dropLoc == "" {
				dropLoc = "startup"
			}
			info += fmt.Sprintf("|15Dropfile: |07%s |08(%s)|07\r\n", doorConfig.DropfileType, dropLoc)
		}
		if doorConfig.IOMode != "" {
			info += fmt.Sprintf("|15I/O Mode: |07%s\r\n", doorConfig.IOMode)
		}
		if doorConfig.MinAccessLevel > 0 {
			info += fmt.Sprintf("|15Min Access: |07%d\r\n", doorConfig.MinAccessLevel)
		}
		if doorConfig.SingleInstance {
			info += "|15Single Instance: |07Yes\r\n"
		}
		if doorConfig.UseShell {
			info += "|15Use Shell: |07Yes\r\n"
		}
		if doorConfig.CleanupCommand != "" {
			info += fmt.Sprintf("|15Cleanup: |07%s\r\n", doorConfig.CleanupCommand)
		}

		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(info)), outputMode)
		terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)

		return currentUser, "", nil
	}
}
