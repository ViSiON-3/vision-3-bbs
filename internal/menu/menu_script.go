package menu

import (
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/scripting"
)

// menuScriptWarned holds the menu scripts already logged as unable to run, so
// a misconfigured menu shown on every visit does not flood the log.
var menuScriptWarned sync.Map

// runMenuScript has the v3_script door a menu's SCRIPT names draw the menu in
// place of its screen and prompt, given the commands the caller may choose
// (v3.menu.items), and acts on its v3.prompt.answer as the run loop does for
// a script in place of the menu prompt (menuPromptAnswer).
//
// handled is false when the menu has no script, the door is missing, is not
// a v3_script door, has no script file or refuses the caller, the script
// fails, or it gives no usable answer; the loop then shows the menu's screen
// and prompt. Otherwise act is loopFallthrough with line as the caller's
// command line, loopContinue to show the menu (or the one the answer named)
// again, or loopReturn with "LOGOFF".
func (st *runLoopState) runMenuScript(menuRec *MenuRecord, commands []CommandRecord) (line string, act loopAction, retErr error, handled bool) {
	u := st.currentUser
	code := strings.ToUpper(strings.TrimSpace(menuRec.Script))
	if code == "" || u == nil {
		return "", loopFallthrough, nil, false
	}
	warn := func(reason string, attrs ...any) {
		st.warnMenuScriptOnce(code, "menu script not run, menu shows its screen: "+reason, attrs...)
	}
	doorConfig, ok := st.e.GetDoorConfig(code)
	switch {
	case !ok:
		warn("no such door")
		return "", loopFallthrough, nil, false
	case doorConfig.Type != "v3_script":
		warn("door is not a v3_script door", "type", doorConfig.Type)
		return "", loopFallthrough, nil, false
	case !stringScriptExists(doorConfig):
		warn("script file not found", "script", doorConfig.Script)
		return "", loopFallthrough, nil, false
	case !doorAllowed(doorConfig, u, st.s, st.terminal, st.sessionStartTime):
		// A door's ACS can keep the scripted menu to some callers.
		return "", loopFallthrough, nil, false
	}

	// The script takes the session's input; keys typed ahead of it are
	// read first, and what it leaves goes back to the menu's handler.
	handOffSessionIH(st.s)
	defer getSessionIH(st.s)
	ctx := buildDoorCtx(st.e, borrowInputSession(st.s), st.terminal,
		u.ID, u.Handle, u.RealName,
		u.AccessLevel, u.TimeLimit, u.TimesCalled,
		u.GroupLocation,
		st.termWidth, st.termHeight,
		st.nodeNumber, st.sessionStartTime, st.outputMode,
		doorConfig, code)
	ctx.UserManager = st.userManager
	ctx.CurrentUser = u
	ctx.Prompt = scripting.PromptMenu
	ctx.Menu = st.menuItems(commands)
	// A door is not started once the caller's time is up; the menu's own
	// prompt gives the time-limit notice.
	if deadline := doorTimeLimitDeadline(ctx); !deadline.IsZero() && !time.Now().Before(deadline) {
		return "", loopFallthrough, nil, false
	}

	switch runErr := executeDoor(ctx); {
	case runErr == nil:
	case errors.Is(runErr, editor.ErrIdleTimeout):
		st.e.handleSessionTimeout(st.s, st.terminal, st.outputMode, st.nodeNumber, st.termWidth, st.termHeight)
		return "LOGOFF", loopReturn, nil, true
	case isSessionFatal(runErr):
		slog.Info("user disconnected in menu script", "node", st.nodeNumber, "menu", st.currentMenuName)
		return "LOGOFF", loopReturn, nil, true
	case errors.Is(runErr, editor.ErrTimeLimit):
		// The top of the run loop gives the notice and logs the caller off.
		return "", loopContinue, nil, true
	default:
		slog.Error("menu script failed, menu shows its screen", "menu", st.currentMenuName, "door", code, "node", st.nodeNumber, "error", runErr)
		return "", loopFallthrough, nil, false
	}

	line, act, handled = st.menuPromptAnswer(promptAnswer{set: ctx.Answered, value: ctx.Answer})
	if handled && act == loopFallthrough && line == "" {
		// An empty command line would only run the script again.
		return "", loopFallthrough, nil, false
	}
	return line, act, nil, handled
}

// unmatchedScriptAnswer handles an answer from the menu's script that
// matches no command. When the menu has no FALLBACK, or its FALLBACK is the
// menu itself, the menu would come straight back and run the script again
// with nothing from the caller in between, so a script that always answers
// that way would never stop. Such an answer is not a usable one: the next
// showing of the menu skips the script and shows the menu's own screen and
// prompt, and true is returned. A FALLBACK to another menu is left to the
// run loop (false).
func (st *runLoopState) unmatchedScriptAnswer(menuRec *MenuRecord) bool {
	fallback := strings.ToUpper(menuRec.Fallback)
	if fallback != "" && fallback != strings.ToUpper(st.currentMenuName) {
		return false
	}
	st.warnMenuScriptOnce(strings.ToUpper(strings.TrimSpace(menuRec.Script)),
		"menu script answer matched no command, menu shows its screen", "answer", st.userInput)
	st.scriptOff = st.currentMenuName
	return true
}

// warnMenuScriptOnce logs msg for the current menu and its script door code
// the first time it comes up, so a menu shown on every visit does not flood
// the log.
func (st *runLoopState) warnMenuScriptOnce(code, msg string, attrs ...any) {
	if _, seen := menuScriptWarned.LoadOrStore(st.currentMenuName+"\x00"+code+"\x00"+msg, struct{}{}); !seen {
		slog.Warn(msg, append([]any{"menu", st.currentMenuName, "door", code}, attrs...)...)
	}
}

// menuItems returns the commands of a menu its script lists: those with a
// key, the caller's ACS allows, and are neither hidden nor run on entry
// (// and ~~), in menu order. The result is never nil.
func (st *runLoopState) menuItems(commands []CommandRecord) []scripting.MenuItem {
	items := []scripting.MenuItem{}
	for _, c := range commands {
		key, _, _ := strings.Cut(strings.TrimSpace(c.Keys), " ")
		if key == "" || key == "//" || key == "~~" || c.Hidden ||
			!checkACS(c.ACS, st.currentUser, st.s, st.terminal, st.sessionStartTime) {
			continue
		}
		label := c.Label
		if label == "" {
			label = c.NodeActivity
		}
		items = append(items, scripting.MenuItem{Key: key, Label: label, Activity: c.NodeActivity})
	}
	return items
}
