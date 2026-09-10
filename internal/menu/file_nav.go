package menu

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// File-menu area/conference navigation, mirroring the message menu's
// NEXT/PREVMSGAREA and NEXT/PREVMSGCONF so the two menus behave the same way.
// Conference moves join for both menus (#304), keeping the active conference in
// step across messages and files.

func runNextFileArea(c *cmdCtx, args string) (*user.User, string, error) {
	return navigateFileArea(c, true)
}

func runPrevFileArea(c *cmdCtx, args string) (*user.User, string, error) {
	return navigateFileArea(c, false)
}

func navigateFileArea(c *cmdCtx, forward bool) (*user.User, string, error) {
	e, s, terminal := c.e, c.s, c.terminal
	currentUser, nodeNumber := c.currentUser, c.nodeNumber
	sessionStartTime, outputMode := c.sessionStartTime, c.outputMode

	if currentUser == nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.Strings().ConfNavLoginRequired)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	areas := getAccessibleFileAreasInConference(e, s, terminal, currentUser, currentUser.CurrentFileConferenceID, sessionStartTime)
	if len(areas) == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.Strings().ConfNoAccessibleAreas)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	currentIdx := -1
	for i, area := range areas {
		if area.ID == currentUser.CurrentFileAreaID {
			currentIdx = i
			break
		}
	}
	newIdx := stepIndex(currentIdx, len(areas), forward)

	newArea := areas[newIdx]
	currentUser.CurrentFileAreaID = newArea.ID
	currentUser.CurrentFileAreaTag = newArea.Tag
	if err := c.userManager.UpdateUser(currentUser); err != nil {
		slog.Error("failed to save user after file area change", "node", nodeNumber, "error", err)
	}

	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes(
		[]byte(fmt.Sprintf(e.Strings().ConfCurrentAreaFormat, newArea.Name, newArea.Tag))), outputMode)
	slog.Info("user navigated to file area", "node", nodeNumber, "handle", currentUser.Handle, "id", newArea.ID, "tag", newArea.Tag)
	return currentUser, "", nil
}

func runNextFileConf(c *cmdCtx, args string) (*user.User, string, error) {
	return navigateFileConf(c, true)
}

func runPrevFileConf(c *cmdCtx, args string) (*user.User, string, error) {
	return navigateFileConf(c, false)
}

func navigateFileConf(c *cmdCtx, forward bool) (*user.User, string, error) {
	e, s, terminal := c.e, c.s, c.terminal
	currentUser, nodeNumber := c.currentUser, c.nodeNumber
	sessionStartTime, outputMode := c.sessionStartTime, c.outputMode

	if currentUser == nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.Strings().ConfNavLoginRequired)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}
	if e.ConferenceMgr == nil {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.Strings().ConfNoConferences)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	confs := getAccessibleConferences(e, s, terminal, currentUser, sessionStartTime)
	if len(confs) == 0 {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.Strings().ConfNoAccessibleConfs)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	currentIdx := -1
	for i, conf := range confs {
		if conf.ID == currentUser.CurrentFileConferenceID {
			currentIdx = i
			break
		}
	}
	newConf := confs[stepIndex(currentIdx, len(confs), forward)]

	// Join for both menus (#304); revert on a save failure rather than showing a
	// move that will not survive the session.
	if !e.commitConferenceJoin(s, terminal, c.userManager, currentUser, newConf.ID, sessionStartTime) {
		terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte("\r\n|12Could not save the conference change.|07\r\n")), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	msg := e.Strings().ConfCurrentConfFormat
	if msg == "" {
		msg = "\r\n|07(|15%s|07) [|14%s|07]\r\n"
	}
	terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(fmt.Sprintf(msg, newConf.Name, newConf.Tag))), outputMode)
	slog.Info("user navigated to file conference", "node", nodeNumber, "handle", currentUser.Handle, "id", newConf.ID, "tag", newConf.Tag)
	return currentUser, "", nil
}

// stepIndex advances (or retreats) an index within [0,n) with wraparound. A
// starting index of -1 (current item not in the list) lands on the first item.
func stepIndex(current, n int, forward bool) int {
	if current == -1 {
		return 0
	}
	if forward {
		return (current + 1) % n
	}
	return (current - 1 + n) % n
}
