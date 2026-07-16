package menu

import (
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/message"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runSelectMessageAreaLightbar is the lightbar version of runSelectMessageArea.
// It uses arrow-key navigation and paging for large area lists, with left/right
// arrows switching the conference filter.
func runSelectMessageAreaLightbar(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode

	slog.Debug("running SELECTMSGAREA (lightbar)", "node", nodeNumber)

	termWidth, termHeight := resolveTermDims(currentUser, c.termWidth, c.termHeight)

	if currentUser == nil {
		msg := "\r\n|01Error: You must be logged in to select a message area.|07\r\n"
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(msg)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	templateDir := filepath.Join(e.MenuSetPath, "templates")
	topBytes, errTop := readTemplateFile(filepath.Join(templateDir, "MSGAREA.TOP"))
	midBytes, errMid := readTemplateFile(filepath.Join(templateDir, "MSGAREA.MID"))

	if errTop != nil || errMid != nil {
		slog.Warn("MSGAREA templates unavailable, using text mode", "node", nodeNumber, "topError", errTop, "midError", errMid)
		return runSelectMessageArea(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, args)
	}

	processedMidTemplate := string(ansi.ReplacePipeCodes(midBytes))

	// Build accessible conference list for left/right navigation.
	var accessibleConfs []accessibleConf
	if e.ConferenceMgr != nil {
		for _, conf := range e.ConferenceMgr.ListConferences() {
			if checkACS(conf.ACS, currentUser, s, terminal, sessionStartTime) {
				accessibleConfs = append(accessibleConfs, accessibleConf{id: conf.ID, name: conf.Name})
			}
		}
	}

	filterConfID := currentUser.CurrentMsgConferenceID

	buildAreaList := func(confID int) []*message.MessageArea {
		var areas []*message.MessageArea
		for _, area := range e.MessageMgr.ListAreas() {
			if area.ConferenceID != confID {
				continue
			}
			if !checkACS(area.ACSRead, currentUser, s, terminal, sessionStartTime) {
				continue
			}
			areas = append(areas, area)
		}
		sort.Slice(areas, func(i, j int) bool {
			return areas[i].Position < areas[j].Position
		})
		return areas
	}

	confNameFor := func(confID int) string {
		if e.ConferenceMgr != nil {
			if conf, ok := e.ConferenceMgr.GetByID(confID); ok {
				return conf.Name
			}
		}
		return "None"
	}

	p := &areaLightbarPicker[*message.MessageArea]{
		e:           e,
		terminal:    terminal,
		outputMode:  outputMode,
		currentUser: currentUser,
		nodeNumber:  nodeNumber,
		items:       buildAreaList(filterConfID),
		buildItemLine: func(area *message.MessageArea, displayIdx int) string {
			line := processedMidTemplate
			line = strings.ReplaceAll(line, "^ID", padRight(strconv.Itoa(displayIdx), 3))
			line = strings.ReplaceAll(line, "^TAG", padRight(truncateStr(area.Tag, 16), 16))
			line = strings.ReplaceAll(line, "^NA", padRight(truncateStr(area.Name, 14), 14))
			line = strings.ReplaceAll(line, "^DE", padRight(truncateStr(area.Description, 32), 32))
			line = strings.ReplaceAll(line, "^DS", truncateStr(area.AreaType, 8))
			return strings.TrimRight(line, "\r\n")
		},
		hint:       "|08[ |15Up|08/|15Dn|08 ] Nav  [ |15Lt|08/|15Rt|08 ] Conf  [ |15PgUp|08/|15PgDn|08 ] Page  [ |15Enter|08 ] Select  [ |15Q|08 ] Quit",
		headerName: confNameFor(filterConfID),
		topBytes:   topBytes,
		hiColorSeq: resolveAreaHiColor(e, "MSGAREAHI", nodeNumber),
		termWidth:  termWidth,
		termHeight: termHeight,
	}
	p.computeLayout(measureAreaHeaderRows(e, topBytes, currentUser, nodeNumber))

	p.onSideNav = func(left bool) {
		if left {
			filterConfID = prevConf(accessibleConfs, filterConfID)
		} else {
			filterConfID = nextConf(accessibleConfs, filterConfID)
		}
		p.items = buildAreaList(filterConfID)
		p.headerName = confNameFor(filterConfID)
		p.selectedIndex, p.topIndex = 0, 0
		p.needFullRedraw = true
	}

	p.onSelect = func(idx int) (bool, *user.User, string, error) {
		area := p.items[idx]
		if !checkACS(area.ACSRead, currentUser, s, terminal, sessionStartTime) {
			return false, nil, "", nil
		}
		currentUser.CurrentMessageAreaID = area.ID
		currentUser.CurrentMessageAreaTag = area.Tag
		e.setUserMsgConference(currentUser, area.ConferenceID)
		if err := userManager.UpdateUser(currentUser); err != nil {
			slog.Error("failed to save user after area change", "node", nodeNumber, "error", err)
		}

		p.showConfirm("|08[ |15" + area.Name + " |08] |15Area Joined!|07")

		slog.Info("user changed message area",
			"node", nodeNumber, "handle", currentUser.Handle, "id", area.ID, "tag", area.Tag)
		return true, currentUser, "", nil
	}

	return p.run(s)
}
