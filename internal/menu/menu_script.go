package menu

import (
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/scripting"
)

// menuScriptNone in a menu's SCRIPT turns the menu set's menuScript off for
// that menu.
const menuScriptNone = "NONE"

// menuScriptWarned holds the menu scripts already logged as unable to run,
// so a misconfigured menu does not flood the log.
var menuScriptWarned sync.Map

// menuScriptDoor returns the code of the v3_script door that draws menuRec,
// or the name of a theme script (isThemeScript), and the words to add to the
// script's arguments: from the menu's SCRIPT, else, when themed, from the
// menu set's menuScript in theme.json. code is "" for a menu that shows its
// screen.
func (st *runLoopState) menuScriptDoor(menuRec *MenuRecord, themed bool) (code string, args []string) {
	words := strings.Fields(menuRec.Script)
	if len(words) == 0 && themed {
		if theme := st.e.Theme(); theme != nil {
			words = strings.Fields(theme.MenuScript)
		}
	}
	if len(words) == 0 || strings.EqualFold(words[0], menuScriptNone) {
		return "", nil
	}
	if isThemeScript(words[0]) {
		return words[0], words[1:] // a file name keeps its case
	}
	return strings.ToUpper(words[0]), words[1:]
}

// menuView is the menu as its script sees it. Items are the commands the
// caller's access allows that have a key and are not hidden; when any
// command has a LABEL, only the labelled ones are listed. Shortcuts are all
// the keys the caller may use, listed or not.
func (st *runLoopState) menuView(menuRec *MenuRecord, commands []CommandRecord) *scripting.MenuView {
	view := &scripting.MenuView{Name: st.currentMenuName, Title: menuRec.Title}
	labelled := false
	for _, c := range commands {
		if c.Label != "" {
			labelled = true
			break
		}
	}
	for _, c := range commands {
		// The keys as matchCommand compares them: split on spaces, case kept.
		keys := slices.DeleteFunc(strings.Split(c.Keys, " "), func(k string) bool { return k == "" })
		if len(keys) == 0 || keys[0] == "//" || keys[0] == "~~" {
			continue
		}
		if !checkACS(c.ACS, st.currentUser, st.s, st.terminal, st.sessionStartTime) {
			continue
		}
		view.Shortcuts = append(view.Shortcuts, keys...)
		if c.Hidden || (labelled && c.Label == "") {
			continue
		}
		label := c.Label
		if label == "" {
			label = c.NodeActivity
		}
		view.Items = append(view.Items, scripting.MenuItemView{Key: keys[0], Label: label, Activity: c.NodeActivity})
	}
	return view
}

// runMenuScript has the menu's script door draw the menu and answer its
// prompt (scripting.PromptMenu), and acts on the answer as menuPromptAnswer
// does for a prompt script. handled is false when the menu has no script, the
// script could not run, or it gave no usable answer; the loop then shows the
// menu's screen and prompt. Otherwise act is loopContinue with the menu
// changed, loopReturn with "LOGOFF", or loopFallthrough with line as the
// caller's command line.
func (st *runLoopState) runMenuScript(menuRec *MenuRecord, commands []CommandRecord) (line string, act loopAction, retErr error, handled bool) {
	if st.currentUser == nil {
		return "", loopFallthrough, nil, false // a door needs a caller
	}
	view := st.menuView(menuRec, commands)
	notices := st.newMenuNotices()
	var answer promptAnswer
	var ran bool
	for {
		run := &menuScriptRun{notice: notices.start(), idleFrom: notices.idleFrom}
		answer, act, retErr, ran = st.runMenuView(menuRec, view, true, run)
		notices.stop(ran && act == loopContinue)
		if !ran || act != loopContinue {
			break
		}
		// Stopped for a toast or the time-left warning: show it, then run
		// the script again to draw the screen anew (see menuNotices). The
		// script starts over from its first statement, so whatever it did
		// before the key wait it was stopped at is done again.
		notices.idleFrom = run.idleFrom
		notices.show()
	}
	if !ran {
		return "", loopFallthrough, nil, false
	}
	if act == loopReturn {
		return "LOGOFF", loopReturn, retErr, true
	}
	line, act, handled = st.menuPromptAnswer(answer)
	if handled && act == loopFallthrough && line == "" {
		// An empty command line would run the script again at once.
		return "", loopFallthrough, nil, false
	}
	return line, act, nil, handled
}

// menuScriptReady returns the v3_script door, or theme script, that draws
// menuRec for the current caller and the words to add to its arguments. ok
// is false when there is no caller, the menu has no script, or the door or
// theme script is missing, is not a v3_script door, has no script file or
// its ACS refuses the caller; the reason for a misconfigured door is logged
// once.
func (st *runLoopState) menuScriptReady(menuRec *MenuRecord, themed bool) (code string, doorConfig config.DoorConfig, args []string, ok bool) {
	u := st.currentUser
	if u == nil {
		return "", config.DoorConfig{}, nil, false // a door needs a caller
	}
	code, args = st.menuScriptDoor(menuRec, themed)
	if code == "" {
		return "", config.DoorConfig{}, nil, false
	}
	warn := func(reason string, attrs ...any) {
		if _, seen := menuScriptWarned.LoadOrStore(st.currentMenuName+"\x00"+code+"\x00"+reason, struct{}{}); !seen {
			slog.Warn("menu script not run, menu shows its screen: "+reason,
				append([]any{"menu", st.currentMenuName, "door", code}, attrs...)...)
		}
	}
	doorConfig, ok = st.e.GetDoorConfig(code)
	if isThemeScript(code) {
		doorConfig, ok = st.e.themeScriptDoor(code)
	}
	if !ok {
		warn("no such door or theme script")
		return "", config.DoorConfig{}, nil, false
	}
	if doorConfig.Type != "v3_script" {
		warn("door is not a v3_script door", "type", doorConfig.Type)
		return "", config.DoorConfig{}, nil, false
	}
	if !stringScriptExists(doorConfig) {
		warn("script file not found", "script", doorConfig.Script)
		return "", config.DoorConfig{}, nil, false
	}
	if !doorAllowed(doorConfig, u, st.s, st.terminal, st.sessionStartTime) {
		// A door's ACS can keep the scripted menu to some callers.
		return "", config.DoorConfig{}, nil, false
	}
	return code, doorConfig, args, true
}

// menuScriptRun is what runMenuScript gives one run of a menu's script and
// gets back from it.
type menuScriptRun struct {
	notice <-chan struct{} // scripting.ScriptConfig.Notice
	// idleFrom is when the caller's idle countdown started: given, it carries
	// on from an earlier run (zero for none); after the run, it is when the
	// countdown last started.
	idleFrom time.Time
}

// runMenuView has menuRec's script door draw view and returns its answer.
// themed lets the menu set's menuScript stand in for a menu without a SCRIPT;
// a built-in screen passes false, so only its own .MNU hands it to a script.
// ran is false when there is no script or it could not run, and the caller
// then draws the screen itself. act is loopReturn, with the error, when the
// session ended in the script, and loopContinue when run's notice stopped it.
// run may be nil.
func (st *runLoopState) runMenuView(menuRec *MenuRecord, view *scripting.MenuView, themed bool, run *menuScriptRun) (answer promptAnswer, act loopAction, retErr error, ran bool) {
	u := st.currentUser
	code, doorConfig, args, ok := st.menuScriptReady(menuRec, themed)
	if !ok {
		return promptAnswer{}, loopFallthrough, nil, false
	}
	e := st.e
	doorConfig.Args = append(append([]string(nil), doorConfig.Args...), args...)

	// The script takes the session's input. Keys the caller typed ahead of
	// it, at a prompt or in an earlier run of the script, are read first.
	handOffSessionIH(st.s)
	ctx := buildDoorCtx(e, borrowInputSession(st.s), st.terminal,
		u.ID, u.Handle, u.RealName,
		u.AccessLevel, u.TimeLimit, u.TimesCalled,
		u.GroupLocation,
		st.termWidth, st.termHeight,
		st.nodeNumber, st.sessionStartTime, st.outputMode,
		doorConfig, code)
	ctx.UserManager = st.userManager
	ctx.CurrentUser = u
	ctx.Prompt = scripting.PromptMenu
	if isThemeScript(code) {
		ctx.ScriptDataDir = themeScriptDataDir(doorConfig)
	}
	if view.Expand == nil {
		view.Expand = func(text string) string {
			ph := e.sessionPlaceholders(st.s, u, st.userManager, st.nodeNumber, st.currentMenuName, st.sessionStartTime, st.currentAreaName, false)
			return replacePlaceholders(expandOptionalGroups(text, ph), ph)
		}
	}
	ctx.Menu = view
	// A door is not started once the caller's time is up; the menu's own
	// prompt gives the time-limit notice.
	if deadline := doorTimeLimitDeadline(ctx); !deadline.IsZero() && !time.Now().Before(deadline) {
		return promptAnswer{}, loopFallthrough, nil, false
	}

	if run != nil {
		ctx.Notice = run.notice
		ctx.IdleFrom = run.idleFrom
	}
	runErr := executeDoor(ctx)
	_ = getSessionIH(st.s)
	if run != nil && ctx.watch != nil {
		run.idleFrom = ctx.watch.idleSince()
	}

	switch {
	case runErr == nil:
	case errors.Is(runErr, scripting.ErrNotice):
		return promptAnswer{}, loopContinue, nil, true
	case isSessionFatal(runErr) || errors.Is(runErr, editor.ErrTimeLimit):
		return promptAnswer{}, loopReturn, runErr, true
	case errors.Is(runErr, ErrDoorBusy):
		slog.Info("menu script door busy, menu shows its screen", "menu", st.currentMenuName, "door", code, "node", st.nodeNumber)
		return promptAnswer{}, loopFallthrough, nil, false
	default:
		slog.Error("menu script failed, menu shows its screen", "menu", st.currentMenuName, "door", code, "node", st.nodeNumber, "error", runErr)
		return promptAnswer{}, loopFallthrough, nil, false
	}
	return promptAnswer{set: ctx.Answered, value: ctx.Answer}, loopFallthrough, nil, true
}

// scriptedChoice has a built-in screen's .MNU script draw view, and returns
// the key the script answered with, upper-cased. ok is false when the screen
// has no script, the script could not run, or it gave no key; the screen then
// draws itself. err is set when the session ended in the script.
func (c *cmdCtx) scriptedChoice(name string, rec *MenuRecord, view *scripting.MenuView) (choice string, ok bool, err error) {
	if rec == nil || c.currentUser == nil {
		return "", false, nil
	}
	st := c.runState(name)
	view.Name = name
	answer, act, runErr, ran := st.runMenuView(rec, view, false, nil)
	if act == loopReturn {
		if runErr == nil {
			runErr = io.EOF
		}
		return "", false, runErr
	}
	value, isText := answer.value.(string)
	if !ran || !answer.set || !isText {
		return "", false, nil
	}
	choice = strings.ToUpper(strings.TrimSpace(value))
	return choice, choice != "", nil
}

// runState is the caller's session as the menu loop holds it, on the menu
// called name.
func (c *cmdCtx) runState(name string) *runLoopState {
	return &runLoopState{
		e: c.e, s: c.s, terminal: c.terminal, userManager: c.userManager,
		currentUser: c.currentUser, nodeNumber: c.nodeNumber,
		sessionStartTime: c.sessionStartTime, outputMode: c.outputMode,
		termWidth: c.termWidth, termHeight: c.termHeight, currentMenuName: name,
	}
}

// plainSetting is s as a script may write it to the terminal: without ANSI
// sequences or control characters, so a caller's own text stays text.
func plainSetting(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.StripAnsi(s))
}
