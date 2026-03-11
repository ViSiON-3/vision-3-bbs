//go:build windows

package menu

import (
	"errors"
	"fmt"
	"io"
	"log"
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

// executeDoor dispatches to the appropriate executor.
// DOS doors require dosemu2 (Linux only) or a 32-bit Windows build with native 16-bit support.
// 64-bit Windows does not support DOS doors. Native POSIX doors are not supported on Windows.
func executeDoor(ctx *DoorCtx) error {
	if ctx.Config.IsDOS {
		return fmt.Errorf("DOS doors are not supported on 64-bit Windows; use dosemu2 on Linux or a 32-bit Windows build")
	}
	return fmt.Errorf("native doors are not supported on Windows")
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

	// Get door registry (DOS doors are not supported on 64-bit Windows)
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

	// Display each DOS door
	midTemplate := string(ansi.ReplacePipeCodes(midBytes))
	for i, name := range doorNames {
		line := midTemplate
		line = strings.ReplaceAll(line, "^ID", fmt.Sprintf("%-3d", i+1))
		line = strings.ReplaceAll(line, "^NA", fmt.Sprintf("%-30s", name))
		line = strings.ReplaceAll(line, "^TY", "DOS")
		terminalio.WriteProcessedBytes(terminal, []byte(line), outputMode)
	}

	if len(doorNames) == 0 {
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
			log.Printf("ERROR: Node %d: Door execution failed for user %s, door %s: %v", nodeNumber, currentUser.Handle, upperInput, cmdErr)
			doorErrorMessage(ctx, fmt.Sprintf("Error running door '%s': %v", upperInput, cmdErr))
		} else {
			log.Printf("INFO: Node %d: Door completed for user %s, door %s", nodeNumber, currentUser.Handle, upperInput)
		}

		return currentUser, "", nil
	}
}

// runDoorInfo shows door configuration details on Windows.
func runDoorInfo(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, currentUser *user.User, nodeNumber int, sessionStartTime time.Time, args string, outputMode ansi.OutputMode, termWidth int, termHeight int) (*user.User, string, error) {
	log.Printf("DEBUG: Node %d: runDoorInfo (Windows)", nodeNumber)
	terminalio.WriteProcessedBytes(terminal,
		ansi.ReplacePipeCodes([]byte("|12DOS doors require dosemu2 (Linux) or a 32-bit Windows build. Native doors are not supported on Windows.|07\r\n")),
		outputMode)
	time.Sleep(1 * time.Second)
	return currentUser, "", nil
}
