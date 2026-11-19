package menu

import (
	"errors"
	"io"

	"github.com/ViSiON-3/vision-3-bbs/internal/scripting"
)

// scriptedStats has YOURSTAT.MNU's SCRIPT, when the caller's theme has one
// that names a script, draw the stats screen (USERSTATS in login.json,
// SHOWSTATS on a menu) in place of YOURSTAT.ANS, so each caller gets their
// own theme's. The script draws the screen, waits for the caller and
// answers "Q". shown is false when the theme has no such menu, it names no
// script, or the script could not run or gave no answer; the built-in
// screen is then drawn. err is set when the session ended in the script.
func scriptedStats(c *cmdCtx) (shown bool, err error) {
	if exists, _ := c.e.Menus().Exists("mnu", "YOURSTAT.MNU"); !exists {
		return false, nil
	}
	rec, err := LoadMenu("YOURSTAT", c.e.Menus())
	if err != nil || rec.Script == "" {
		return false, nil
	}
	view := &scripting.MenuView{
		Title: rec.Title,
		Items: []scripting.MenuItemView{{Key: "Q", Label: "Continue"}},
	}
	_, shown, err = c.scriptedChoice("YOURSTAT", rec, view)
	if errors.Is(err, io.EOF) {
		return true, err
	}
	return shown, err
}
