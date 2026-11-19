package menu

import "github.com/ViSiON-3/vision-3-bbs/internal/scripting"

// scriptedUserList has USERLIST.MNU's SCRIPT, when it names one, draw the
// user list. The script reads the users itself (v3.users.publicList, the
// handles, locations, levels and validation the built-in list shows), pages
// and searches them, and answers "Q" when the caller leaves. scripted is
// false when the menu set has no USERLIST.MNU, it names no script, the
// screen is smaller than 80x24, or the script could not run or answered with
// anything but "Q"; the built-in list is then drawn. err is set when the
// session ended in the script.
func scriptedUserList(c *cmdCtx) (scripted bool, err error) {
	if c.termWidth < 80 || c.termHeight < 24 {
		return false, nil
	}
	if exists, _ := c.e.Menus().Exists("mnu", "USERLIST.MNU"); !exists {
		return false, nil
	}
	rec, err := LoadMenu("USERLIST", c.e.Menus())
	if err != nil || rec.Script == "" {
		return false, nil
	}
	view := &scripting.MenuView{
		Title: rec.Title,
		Items: []scripting.MenuItemView{{Key: "Q", Label: "Back"}},
	}
	choice, scripted, err := c.scriptedChoice("USERLIST", rec, view)
	if scripted && choice != "Q" {
		// The list has no other command. A script that answers without
		// reading the caller is not run again; the built-in list waits for
		// a key.
		return false, err
	}
	return scripted, err
}
