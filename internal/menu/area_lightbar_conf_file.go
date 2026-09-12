package menu

import (
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

// getAccessibleFileAreasInConference returns the file areas in a conference the
// caller may list, ordered by ID (file areas have no explicit position field).
func getAccessibleFileAreasInConference(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, currentUser *user.User, conferenceID int, sessionStartTime time.Time) []file.FileArea {
	if e.FileMgr == nil {
		return nil
	}
	var result []file.FileArea
	for _, area := range e.FileMgr.ListAreas() {
		if area.ConferenceID != conferenceID {
			continue
		}
		if !checkACS(area.ACSList, currentUser, s, terminal, sessionStartTime) {
			continue
		}
		result = append(result, area)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// findFirstAccessibleFileAreaInConference returns the first file area the caller
// can list in the conference, or nil when there is none.
func findFirstAccessibleFileAreaInConference(e *MenuExecutor, s ssh.Session, terminal *term.Terminal, currentUser *user.User, conferenceID int, sessionStartTime time.Time) *file.FileArea {
	areas := getAccessibleFileAreasInConference(e, s, terminal, currentUser, conferenceID, sessionStartTime)
	if len(areas) > 0 {
		a := areas[0]
		return &a
	}
	return nil
}

// joinConferenceForBoth makes a conference the caller's active conference for
// both messages and files, moving them to the first accessible area on each
// side (#304: a conference change in either the message or file menu applies to
// both). A side with no accessible area in the conference has its current area
// cleared rather than left pointing into the old conference. It does not save;
// the caller persists the user record.
func (e *MenuExecutor) joinConferenceForBoth(s ssh.Session, terminal *term.Terminal, u *user.User, conferenceID int, sessionStartTime time.Time) {
	e.setUserMsgConference(u, conferenceID)
	if area := findFirstAccessibleAreaInConference(e, s, terminal, u, conferenceID, sessionStartTime); area != nil {
		u.CurrentMessageAreaID = area.ID
		u.CurrentMessageAreaTag = area.Tag
	} else {
		u.CurrentMessageAreaID = 0
		u.CurrentMessageAreaTag = ""
	}

	e.setUserFileConference(u, conferenceID)
	if fa := findFirstAccessibleFileAreaInConference(e, s, terminal, u, conferenceID, sessionStartTime); fa != nil {
		u.CurrentFileAreaID = fa.ID
		u.CurrentFileAreaTag = fa.Tag
	} else {
		u.CurrentFileAreaID = 0
		u.CurrentFileAreaTag = ""
	}
}

// confSelection captures the conference/area fields joinConferenceForBoth
// changes, so a failed save can be rolled back rather than leaving the session
// showing a conference it did not persist (which would vanish on reconnect).
type confSelection struct {
	msgConfID, msgAreaID, fileConfID, fileAreaID     int
	msgConfTag, msgAreaTag, fileConfTag, fileAreaTag string
}

func snapshotConfSelection(u *user.User) confSelection {
	return confSelection{
		msgConfID: u.CurrentMsgConferenceID, msgConfTag: u.CurrentMsgConferenceTag,
		msgAreaID: u.CurrentMessageAreaID, msgAreaTag: u.CurrentMessageAreaTag,
		fileConfID: u.CurrentFileConferenceID, fileConfTag: u.CurrentFileConferenceTag,
		fileAreaID: u.CurrentFileAreaID, fileAreaTag: u.CurrentFileAreaTag,
	}
}

func restoreConfSelection(u *user.User, s confSelection) {
	u.CurrentMsgConferenceID, u.CurrentMsgConferenceTag = s.msgConfID, s.msgConfTag
	u.CurrentMessageAreaID, u.CurrentMessageAreaTag = s.msgAreaID, s.msgAreaTag
	u.CurrentFileConferenceID, u.CurrentFileConferenceTag = s.fileConfID, s.fileConfTag
	u.CurrentFileAreaID, u.CurrentFileAreaTag = s.fileAreaID, s.fileAreaTag
}

// commitConferenceJoin joins a conference for both menus and persists it. If the
// save fails it rolls the in-memory selection back to what it was and returns
// false, so the caller does not report a change that will not survive the
// session — the active conference stays consistent with what is on disk.
func (e *MenuExecutor) commitConferenceJoin(s ssh.Session, terminal *term.Terminal, userManager *user.UserMgr, u *user.User, conferenceID int, sessionStartTime time.Time) bool {
	prev := snapshotConfSelection(u)
	e.joinConferenceForBoth(s, terminal, u, conferenceID, sessionStartTime)
	if err := userManager.UpdateUser(u); err != nil {
		restoreConfSelection(u, prev)
		slog.Error("failed to save conference change; reverting selection", "handle", u.Handle, "error", err)
		return false
	}
	return true
}

// runChangeFileConferenceLightbar is the file-menu counterpart to
// runChangeMsgConferenceLightbar: it lets the caller pick a conference from the
// same shared conference list and joins it for both files and messages. It
// reuses the MSGCONF templates when a menu set ships no FILECONF ones — the
// conference list and layout are identical, and the shipped MSGCONF art is
// generic ("Current Conference" / "# Conference Name Description").
func runChangeFileConferenceLightbar(c *cmdCtx, args string) (*user.User, string, error) {
	e := c.e
	s := c.s
	terminal := c.terminal
	userManager := c.userManager
	currentUser := c.currentUser
	nodeNumber := c.nodeNumber
	sessionStartTime := c.sessionStartTime
	outputMode := c.outputMode

	slog.Debug("running CHANGEFILECONF (lightbar)", "node", nodeNumber)

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

	topBytes, midBytes := loadFileConfTemplates(e)
	if topBytes == nil || midBytes == nil {
		// Conferences may well exist — the templates are what's missing — so
		// report a template error rather than "no conferences".
		slog.Warn("FILECONF/MSGCONF templates unavailable for CHANGEFILECONF", "node", nodeNumber)
		_ = terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(e.Strings().ConfTemplateError)), outputMode)
		time.Sleep(1 * time.Second)
		return currentUser, "", nil
	}

	processedMidTemplate := string(ansi.ReplacePipeCodes(midBytes))

	var confs []confPickItem
	currentConfName := "None"
	for _, conf := range e.ConferenceMgr.ListConferences() {
		if !checkACS(conf.ACS, currentUser, s, terminal, sessionStartTime) {
			continue
		}
		confs = append(confs, confPickItem{id: conf.ID, name: conf.Name, description: conf.Description})
		if conf.ID == currentUser.CurrentFileConferenceID {
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

	for i, cf := range confs {
		if cf.id == currentUser.CurrentFileConferenceID {
			p.selectedIndex = i
			break
		}
	}

	p.onSelect = func(idx int) (bool, *user.User, string, error) {
		chosen := confs[idx]
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

		slog.Info("user changed file conference",
			"node", nodeNumber, "handle", currentUser.Handle, "id", chosen.id, "tag", confTag, "fileArea", currentUser.CurrentFileAreaTag)
		return true, currentUser, "", nil
	}

	return p.run(s)
}

// loadFileConfTemplates returns the top/mid templates for the file conference
// picker, preferring FILECONF.* and falling back to the shipped MSGCONF.*.
func loadFileConfTemplates(e *MenuExecutor) (top, mid []byte) {
	top, errTop := readTemplateFile(e.templateFile("FILECONF.TOP"))
	mid, errMid := readTemplateFile(e.templateFile("FILECONF.MID"))
	if errTop == nil && errMid == nil {
		return top, mid
	}
	top, errTop = readTemplateFile(e.templateFile("MSGCONF.TOP"))
	mid, errMid = readTemplateFile(e.templateFile("MSGCONF.MID"))
	if errTop != nil || errMid != nil {
		return nil, nil
	}
	return top, mid
}
