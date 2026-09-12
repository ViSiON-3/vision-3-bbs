package menu

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// confPickItem is one selectable conference row in the lightbar picker.
type confPickItem struct {
	id          int
	name        string
	description string
}

// runChangeMsgConferenceLightbar is the lightbar version of runChangeMsgConference.
func runChangeMsgConferenceLightbar(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode

	slog.Debug("running CHANGEMSGCONF (lightbar)", "node", nodeNumber)

	termWidth, termHeight := resolveTermDims(currentUser, c.termWidth, c.termHeight)

	if currentUser == nil {
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.Strings().ConfLoginRequired)), outputMode)
		time.Sleep(1 * time.Second)
		return nil, "", nil
	}

	if e.ConferenceMgr == nil {
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.Strings().ConfNoConferences)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	topBytes, errTop := readTemplateFile(e.templateFile("MSGCONF.TOP"))
	midBytes, errMid := readTemplateFile(e.templateFile("MSGCONF.MID"))

	if errTop != nil || errMid != nil {
		slog.Warn("MSGCONF templates unavailable, using text mode", "node", nodeNumber, "topError", errTop, "midError", errMid)
		return runChangeMsgConference(&cmdCtx{e: e, s: s, terminal: terminal, userManager: userManager, currentUser: currentUser, nodeNumber: nodeNumber, sessionStartTime: sessionStartTime, outputMode: outputMode, termWidth: termWidth, termHeight: termHeight}, args)
	}

	processedMidTemplate := string(ansi.ReplacePipeCodes(midBytes))

	// Build the accessible conference list.
	var confs []confPickItem
	currentConfName := "None"
	for _, conf := range e.ConferenceMgr.ListConferences() {
		if !checkACS(conf.ACS, currentUser, s, terminal, sessionStartTime) {
			continue
		}
		confs = append(confs, confPickItem{id: conf.ID, name: conf.Name, description: conf.Description})
		if conf.ID == currentUser.CurrentMsgConferenceID {
			currentConfName = conf.Name
		}
	}

	if len(confs) == 0 {
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.Strings().ConfNoConferences)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	p := &areaLightbarPicker[confPickItem]{
		e:           e,
		terminal:    terminal,
		outputMode:  outputMode,
		currentUser: currentUser,
		nodeNumber:  nodeNumber,
		items:       confs,
		buildItemLine: func(item confPickItem, displayIdx int) string {
			line := processedMidTemplate
			line = strings.ReplaceAll(line, "^CI", padRight(strconv.Itoa(displayIdx), 3))
			line = strings.ReplaceAll(line, "^CN", padRight(truncateStr(item.name, 20), 20))
			line = strings.ReplaceAll(line, "^CD", truncateStr(item.description, 53))
			return strings.TrimRight(line, "\r\n")
		},
		hint:       "|08[ |15Up|08/|15Dn|08 ] Navigate  [ |15PgUp|08/|15PgDn|08 ] Page  [ |15Enter|08 ] Select  [ |15Q|08 ] Quit",
		headerName: currentConfName,
		topBytes:   topBytes,
		hiColorSeq: resolveAreaHiColor(e, "MSGCONFHI", nodeNumber),
		termWidth:  termWidth,
		termHeight: termHeight,
	}
	p.computeLayout(measureAreaHeaderRows(e, topBytes, currentUser, nodeNumber))

	// Pre-select the current conference.
	for i, cf := range confs {
		if cf.id == currentUser.CurrentMsgConferenceID {
			p.selectedIndex = i
			break
		}
	}

	p.onSelect = func(idx int) (bool, *user.User, string, error) {
		chosen := confs[idx]
		// Joins the conference for both messages and files (#304), so the active
		// conference stays in step across the two menus. Reverts if the save
		// fails rather than showing a change that won't survive the session.
		if !e.commitConferenceJoin(s, terminal, userManager, currentUser, chosen.id, sessionStartTime) {
			p.showConfirm("|12Could not save the conference change.|07")
			return true, currentUser, "", nil
		}

		confName := chosen.name
		confTag := ""
		if conf, ok := e.ConferenceMgr.GetByID(chosen.id); ok {
			confName = conf.Name
			confTag = conf.Tag
		}

		p.showConfirm("|08[ |15" + confName + " |08] |15Conference Joined!|07")

		slog.Info("user changed conference",
			"node", nodeNumber, "handle", currentUser.Handle, "id", chosen.id, "tag", confTag, "area", currentUser.CurrentMessageAreaTag)
		return true, currentUser, "", nil
	}

	return p.run(s)
}
