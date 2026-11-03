package menu

// The |XX placeholders a caller's session fills in: the handle, the node, the
// time left, the current areas. They were expanded in menu prompts only; this
// file expands them in strings.json values too, through the shared writer in
// string_hooks.go, so the same code means the same thing in both (#622).

import (
	"strconv"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// sessionPlaceholderKeys lists every placeholder sessionPlaceholders resolves.
// It is the gate for strings: a value naming none of them is written without
// building the map. TestSessionPlaceholderKeys keeps it in step with the map.
var sessionPlaceholderKeys = []string{
	"|NODE", "|DATE", "|TIME", "|MN", "|PV", "|NEWUSERS",
	"|UH", "|ALIAS", "|HANDLE", "|LEVEL", "|NAME", "|GL", "|UN",
	"|UPLDS", "|DNLDS", "|POSTS", "|CALLS", "|LCALL", "|TL",
	"|CA", "|CAN", "|CFA", "|CFAN", "|CC", "|CCN", "|FC", "|FCN",
}

// sessionPlaceholders returns the |XX placeholders for a caller, as a menu
// prompt and a strings.json value expand them. Before login, or with no user,
// the user values are the Guest defaults the prompt has always shown.
// currentAreaName overrides the message area name when the caller gives one.
//
// |PV, the pending-validation count, scans every user, so it is counted only
// when countPending is set and the caller is at CoSysOp level on the MAIN
// menu; otherwise it reads 0.
func (e *MenuExecutor) sessionPlaceholders(s ssh.Session, currentUser *user.User, userManager *user.UserMgr, nodeNumber int, currentMenuName string, sessionStartTime time.Time, currentAreaName string, countPending bool) map[string]string {
	newUsersStatus := "NO"
	if e.GetServerConfig().AllowNewUsers {
		newUsersStatus = "YES"
	}

	now := config.NowIn(e.GetServerConfig().Timezone)
	currentAreaTag, currentAreaDisplayName := e.resolveCurrentAreaTokens(currentUser, currentAreaName)
	currentFileAreaTag, currentFileAreaDisplayName := e.resolveCurrentFileAreaTokens(currentUser)

	placeholders := map[string]string{
		"|NODE":     strconv.Itoa(nodeNumber),
		"|DATE":     now.Format("01/02/06"),
		"|TIME":     now.Format("3:04 pm"),
		"|MN":       currentMenuName,
		"|PV":       "0",
		"|UH":       "Guest",
		"|NEWUSERS": newUsersStatus,
		"|ALIAS":    "Guest",
		"|HANDLE":   "Guest",
		"|LEVEL":    "0",
		"|NAME":     "Guest User",
		"|GL":       "",
		"|UN":       "",
		"|UPLDS":    "0",
		"|DNLDS":    "0",
		"|POSTS":    "0",
		"|CALLS":    "0",
		"|LCALL":    "Never",
		"|TL":       "N/A",
		"|CA":       currentAreaTag,
		"|CAN":      currentAreaDisplayName,
		"|CFA":      currentFileAreaTag,
		"|CFAN":     currentFileAreaDisplayName,
		"|CC":       "None",
		"|CCN":      "None",
		"|FC":       "None",
		"|FCN":      "None",
	}
	if currentUser == nil {
		return placeholders
	}

	applyUserPlaceholders(placeholders, currentUser)

	if currentUser.CurrentMsgConferenceTag != "" {
		placeholders["|CC"] = currentUser.CurrentMsgConferenceTag
	}
	if e.ConferenceMgr != nil && currentUser.CurrentMsgConferenceID != 0 {
		if conf, ok := e.ConferenceMgr.GetByID(currentUser.CurrentMsgConferenceID); ok {
			placeholders["|CCN"] = conf.Name
		}
	}
	if currentUser.CurrentFileConferenceTag != "" {
		placeholders["|FC"] = currentUser.CurrentFileConferenceTag
	}
	if e.ConferenceMgr != nil && currentUser.CurrentFileConferenceID != 0 {
		if conf, ok := e.ConferenceMgr.GetByID(currentUser.CurrentFileConferenceID); ok {
			placeholders["|FCN"] = conf.Name
		}
	}

	placeholders["|TL"] = e.timeLeftPlaceholder(s, currentUser, sessionStartTime)

	if countPending && currentMenuName == "MAIN" && currentUser.AccessLevel >= 100 {
		placeholders["|PV"] = strconv.Itoa(pendingValidationCount(userManager))
	}
	return placeholders
}

// namedSessionPlaceholders returns the placeholders text names, in the order
// of sessionPlaceholderKeys, or nil when it names none.
func namedSessionPlaceholders(text string) []string {
	if !strings.Contains(text, "|") {
		return nil
	}
	var named []string
	for _, k := range sessionPlaceholderKeys {
		if strings.Contains(text, k) {
			named = append(named, k)
		}
	}
	return named
}

// expandStringPlaceholders expands the session placeholders and |{...|}
// optional groups in a strings.json value written to terminal, the way the
// menu prompt expands them (see renderPromptText). The text is the sysop's
// template, before any format verb or call-site code is filled in, so a
// value substituted later cannot be read as a placeholder.
//
// owned lists codes the call site fills in itself (|UN as the new user's
// number, say); those are left for it. When the value is formatted, a
// percent sign in a substituted value is doubled so it cannot become a verb.
//
// The caller is found from the terminal's registered session; with none (a
// terminal outside a session) the text is returned unchanged.
func (e *MenuExecutor) expandStringPlaceholders(terminal *term.Terminal, text string, owned []string, formatted bool) string {
	named := namedSessionPlaceholders(text)
	if len(named) == 0 && !strings.Contains(text, optGroupOpen) {
		return text
	}
	hs := lookupStringHookSession(terminal)
	if hs == nil || hs.bbs == nil {
		return text
	}
	hs.bbs.Mutex.RLock()
	u, menuName := hs.bbs.User, hs.bbs.CurrentMenu
	hs.bbs.Mutex.RUnlock()

	countPending := false
	for _, k := range named {
		if k == "|PV" {
			countPending = true
		}
	}
	placeholders := e.sessionPlaceholders(hs.s, u, hs.userMgr, hs.bbs.NodeID, menuName, hs.bbs.StartTime, "", countPending)
	for _, k := range owned {
		delete(placeholders, k)
	}
	if formatted {
		for k, v := range placeholders {
			placeholders[k] = strings.ReplaceAll(v, "%", "%%")
		}
	}

	text = expandOptionalGroups(text, placeholders)
	pairs := make([]string, 0, len(placeholders)*2)
	for _, k := range placeholderKeysLongestFirst(placeholders) {
		pairs = append(pairs, k, placeholders[k])
	}
	return strings.NewReplacer(pairs...).Replace(text)
}
