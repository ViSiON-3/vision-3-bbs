//go:build !windows

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
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// --- Batch File Generator ---

// --- DOS Door Executor ---

// --- Native Door Executor ---

// --- Door Cleanup ---

// cleanupTimeout is the maximum time allowed for a cleanup command to run.
const cleanupTimeout = 30 * time.Second

// executeCleanup runs the optional post-door cleanup command with a timeout.
// Errors are logged but not returned — cleanup failure should not mask door results.
func executeCleanup(ctx *DoorCtx) {
	if ctx.Config.CleanupCommand == "" {
		return
	}

	// Substitute placeholders in cleanup args
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

	cmd := exec.CommandContext(cleanupCtx, ctx.Config.CleanupCommand, substitutedArgs...)
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

// --- Door Dispatcher ---

// executeDoor dispatches to the appropriate door executor based on config.
// DOS doors require dosemu2 on Linux x86/x86-64.
// Handles single-instance locking and post-execution cleanup.
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
		return executeDOSDoor(ctx)
	}
	return executeNativeDoor(ctx)
}

// --- Door Post-Execution ---

// doorErrorMessage sends a formatted error message to the user.
func doorErrorMessage(ctx *DoorCtx, msg string) {
	errMsg := fmt.Sprintf(ctx.Executor.LoadedStrings.DoorErrorFormat, msg)
	wErr := terminalio.WriteProcessedBytes(ctx.Session.Stderr(), ansi.ReplacePipeCodes([]byte(errMsg)), ctx.OutputMode)
	if wErr != nil {
		slog.Error("failed writing door error message", "error", wErr)
	}
}

// --- Door Menu Runnables ---

// runListDoors displays a list of all configured doors.
func runListDoors(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	terminal := c.terminal
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	outputMode := c.outputMode

	slog.Debug("running LISTDOORS", "node", nodeNumber)

	if currentUser == nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.DoorLoginRequired)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Load templates
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
	// For CP437 mode, write raw bytes directly to avoid UTF-8 false positives
	processedTop := ansi.ReplacePipeCodes(topBytes)
	if outputMode == ansi.OutputModeCP437 {
		_, _ = terminal.Write(processedTop) // best-effort display
	} else {
		terminalio.WriteProcessedBytes(terminal, processedTop, outputMode)
	}

	// Get door registry atomically
	e.configMu.RLock()
	doorRegistryCopy := make(map[string]config.DoorConfig, len(e.DoorRegistry))
	for k, v := range e.DoorRegistry {
		doorRegistryCopy[k] = v
	}
	e.configMu.RUnlock()

	// Sort door names for consistent display
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
		line := formatDoorListLine(midTemplate, displayIdx, name, doorCfg)
		terminalio.WriteProcessedBytes(terminal, []byte(line), outputMode)
	}

	if displayIdx == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.DoorNoneConfigured)), outputMode)
	}

	// Display footer
	processedBot := ansi.ReplacePipeCodes(botBytes)
	if outputMode == ansi.OutputModeCP437 {
		_, _ = terminal.Write(processedBot) // best-effort display
	} else {
		terminalio.WriteProcessedBytes(terminal, processedBot, outputMode)
	}

	return currentUser, "", nil
}

// runOpenDoor prompts the user for a door name and launches it.
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

	slog.Debug("running OPENDOOR", "node", nodeNumber)

	if currentUser == nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.DoorLoginRequired)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Prompt for door name
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
			_, _, _ = runListDoors(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, "")
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		// Look up door
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

		// Build context and execute
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
				slog.Error("door execution failed", "node", nodeNumber, "handle", currentUser.Handle, "door", upperInput, "error", cmdErr)
				doorErrorMessage(ctx, fmt.Sprintf("Error running door '%s': %v", upperInput, cmdErr))
			}
		} else {
			slog.Info("door completed", "node", nodeNumber, "handle", currentUser.Handle, "door", upperInput)
		}

		return currentUser, "", nil
	}
}

// runDoorInfo displays information about a specific door.
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

	slog.Debug("running DOORINFO", "node", nodeNumber)

	if currentUser == nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.LoadedStrings.DoorInfoLoginRequired)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	// Prompt for door name
	renderedPrompt := ansi.ReplacePipeCodes([]byte(e.LoadedStrings.DoorPrompt))
	curUpClear := "\x1b[A\r\x1b[2K"

	terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)

	for {
		inputName, err := readLineFromSessionIH(s, terminal)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, "LOGOFF", io.EOF
			}
			slog.Error("error reading DOORINFO input", "node", nodeNumber, "error", err)
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
			_, _, _ = runListDoors(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, "")
			terminalio.WriteProcessedBytes(terminal, renderedPrompt, outputMode)
			continue
		}

		// Look up door
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
		doorType := "Native Linux"
		switch {
		case doorConfig.Type == "v3_script":
			doorType = "VPL Script"
		case doorConfig.Type == "synchronet_js":
			doorType = "Synchronet JS"
		case doorConfig.IsDOS:
			doorType = "DOS (dosemu2)"
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
