//go:build windows

package menu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/stlalpha/vision3/internal/ansi"
	"github.com/stlalpha/vision3/internal/config"
	"github.com/stlalpha/vision3/internal/terminalio"
	"github.com/stlalpha/vision3/internal/user"
	"golang.org/x/term"
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
				log.Printf("WARN: Failed to remove node dropfile dir %s: %v", nodeDir, err)
			}
		}()
		dropfileDir = nodeDir
	}

	dropfileTypeUpper := strings.ToUpper(doorConfig.DropfileType)

	if dropfileTypeUpper == "DOOR.SYS" || dropfileTypeUpper == "CHAIN.TXT" || dropfileTypeUpper == "DOOR32.SYS" || dropfileTypeUpper == "DORINFO1.DEF" {
		dropfilePath = filepath.Join(dropfileDir, dropfileTypeUpper)
		log.Printf("INFO: Generating %s dropfile at: %s", dropfileTypeUpper, dropfilePath)

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
			log.Printf("ERROR: Failed to write dropfile %s: %v", dropfilePath, genErr)
			errMsg := fmt.Sprintf(ctx.Executor.LoadedStrings.DoorDropfileError, ctx.DoorName)
			if wErr := terminalio.WriteProcessedBytes(ctx.Session.Stderr(), ansi.ReplacePipeCodes([]byte(errMsg)), ctx.OutputMode); wErr != nil {
				log.Printf("ERROR: Failed writing dropfile creation error message: %v", wErr)
			}
			return genErr
		}

		defer func() {
			log.Printf("DEBUG: Cleaning up dropfile: %s", dropfilePath)
			if err := os.Remove(dropfilePath); err != nil {
				log.Printf("WARN: Failed to remove dropfile %s: %v", dropfilePath, err)
			}
		}()
	}

	// Populate placeholders
	ctx.Subs["{DROPFILE}"] = dropfilePath
	ctx.Subs["{NODEDIR}"] = dropfileDir

	// Substitute in Arguments
	substitutedArgs := make([]string, len(doorConfig.Args))
	for i, arg := range doorConfig.Args {
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

	// Prepare command — optionally wrap in cmd.exe
	var cmd *exec.Cmd
	if doorConfig.UseShell {
		// Pass command and args directly to cmd /c with proper quoting to prevent injection
		cmdParts := append([]string{doorConfig.Command}, substitutedArgs...)
		cmd = exec.Command("cmd", append([]string{"/c"}, cmdParts...)...)
		log.Printf("DEBUG: Node %d: Using shell execution for %q with %d arg(s)", ctx.NodeNumber, doorConfig.Command, len(substitutedArgs))
	} else {
		cmd = exec.Command(doorConfig.Command, substitutedArgs...)
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
		log.Printf("WARN: Node %d: Door '%s' requires raw terminal, but PTY is not supported on Windows. Falling back to STDIO.", ctx.NodeNumber, ctx.DoorName)
	}

	log.Printf("INFO: Node %d: Starting door '%s' with standard I/O redirection (Windows)", ctx.NodeNumber, ctx.DoorName)
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
		if !acquireDoorLock(ctx.DoorName, ctx.NodeNumber) {
			log.Printf("WARN: Node %d: Door '%s' is already in use by another node", ctx.NodeNumber, ctx.DoorName)
			return fmt.Errorf("%w: %s", ErrDoorBusy, ctx.DoorName)
		}
		defer releaseDoorLock(ctx.DoorName, ctx.NodeNumber)
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

	log.Printf("INFO: Node %d: Running cleanup command for door '%s': %s %v",
		ctx.NodeNumber, ctx.DoorName, ctx.Config.CleanupCommand, substitutedArgs)

	cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()

	cmd := exec.CommandContext(cleanupCtx, ctx.Config.CleanupCommand, substitutedArgs...)
	if ctx.Config.WorkingDirectory != "" {
		cmd.Dir = ctx.Config.WorkingDirectory
	}
	cmd.Env = os.Environ()

	if output, err := cmd.CombinedOutput(); err != nil {
		if cleanupCtx.Err() == context.DeadlineExceeded {
			log.Printf("WARN: Node %d: Cleanup command for door '%s' timed out after %v",
				ctx.NodeNumber, ctx.DoorName, cleanupTimeout)
		} else {
			log.Printf("WARN: Node %d: Cleanup command for door '%s' failed: %v (output: %s)",
				ctx.NodeNumber, ctx.DoorName, err, string(output))
		}
	} else {
		log.Printf("DEBUG: Node %d: Cleanup command for door '%s' completed successfully", ctx.NodeNumber, ctx.DoorName)
	}
}

// doorErrorMessage writes a formatted error message to the session.
func doorErrorMessage(ctx *DoorCtx, msg string) {
	errMsg := fmt.Sprintf(ctx.Executor.LoadedStrings.DoorErrorFormat, msg)
	wErr := terminalio.WriteProcessedBytes(ctx.Session.Stderr(), ansi.ReplacePipeCodes([]byte(errMsg)), ctx.OutputMode)
	if wErr != nil {
		log.Printf("ERROR: Failed writing door error message: %v", wErr)
	}
}

// runListDoors lists configured doors from the door registry on Windows.
func runListDoors(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	log.Printf("DEBUG: Node %d: runListDoors (Windows)", nodeNumber)

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
		log.Printf("ERROR: Node %d: Failed to load DOORLIST templates: TOP(%v), MID(%v), BOT(%v)", nodeNumber, errTop, errMid, errBot)
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

	doorNames := make([]string, 0, len(doorRegistryCopy))
	for name := range doorRegistryCopy {
		doorNames = append(doorNames, name)
	}
	sort.Strings(doorNames)

	// Display each door (skip doors the user lacks access to)
	midTemplate := string(ansi.ReplacePipeCodes(midBytes))
	displayIdx := 0
	for _, name := range doorNames {
		doorCfg := doorRegistryCopy[name]

		// Filter out doors the user doesn't have access to
		if doorCfg.MinAccessLevel > 0 && currentUser.AccessLevel < doorCfg.MinAccessLevel {
			continue
		}

		displayIdx++
		doorType := "Native"
		if doorCfg.IsDOS {
			doorType = "DOS"
		}
		line := midTemplate
		line = strings.ReplaceAll(line, "^ID", fmt.Sprintf("%-3d", displayIdx))
		line = strings.ReplaceAll(line, "^NA", fmt.Sprintf("%-30s", name))
		line = strings.ReplaceAll(line, "^TY", doorType)
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
func runOpenDoor(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	log.Printf("DEBUG: Node %d: runOpenDoor (Windows)", nodeNumber)

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
			log.Printf("ERROR: Node %d: Error reading OPENDOOR input: %v", nodeNumber, err)
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
			runListDoors(e, s, terminal, userManager, currentUser, nodeNumber, sessionStartTime, "", outputMode, termWidth, termHeight)
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
			log.Printf("WARN: Node %d: User %s (level %d) denied access to door %s (requires %d)",
				nodeNumber, currentUser.Handle, currentUser.AccessLevel, upperInput, doorConfig.MinAccessLevel)
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
				log.Printf("INFO: Node %d: Door %s is busy for user %s", nodeNumber, upperInput, currentUser.Handle)
				busyFmt := e.LoadedStrings.DoorBusyFormat
				if strings.TrimSpace(busyFmt) == "" {
					busyFmt = "\r\n|14Door is currently in use: |11%s|07\r\n"
				}
				busyMsg := fmt.Sprintf(busyFmt, upperInput)
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(busyMsg)), outputMode)
				time.Sleep(1 * time.Second)
			} else {
				log.Printf("ERROR: Node %d: Door execution failed for user %s, door %s: %v", nodeNumber, currentUser.Handle, upperInput, cmdErr)
				doorErrorMessage(ctx, fmt.Sprintf("Error running door '%s': %v", upperInput, cmdErr))
			}
		} else {
			log.Printf("INFO: Node %d: Door completed for user %s, door %s", nodeNumber, currentUser.Handle, upperInput)
		}

		return currentUser, "", nil
	}
}

// runDoorInfo shows door configuration details on Windows.
func runDoorInfo(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	log.Printf("DEBUG: Node %d: runDoorInfo (Windows)", nodeNumber)

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
			runListDoors(e, s, terminal, userManager, currentUser, nodeNumber, sessionStartTime, "", outputMode, termWidth, termHeight)
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
			log.Printf("WARN: Node %d: User %s (level %d) denied access to door info %s (requires %d)",
				nodeNumber, currentUser.Handle, currentUser.AccessLevel, upperInput, doorConfig.MinAccessLevel)
			msg := fmt.Sprintf(e.LoadedStrings.DoorAccessDenied, upperInput)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
			time.Sleep(1 * time.Second)
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		// Display door info
		terminalio.WriteProcessedBytes(terminal, []byte("\r\n"), outputMode)
		doorType := "Native Windows"
		if doorConfig.IsDOS {
			doorType = "DOS (not supported on Windows)"
		}

		info := fmt.Sprintf("|15Door: |07%s\r\n|15Type: |07%s\r\n", upperInput, doorType)
		if doorConfig.Command != "" {
			info += fmt.Sprintf("|15Command: |07%s\r\n", doorConfig.Command)
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
