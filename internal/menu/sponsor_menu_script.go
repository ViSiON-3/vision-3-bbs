package menu

import "github.com/ViSiON-3/vision-3-bbs/internal/scripting"

// scriptedSponsorChoice has SPONSORM.MNU's SCRIPT, when it names one, draw
// the sponsor menu for the area tagged tag and answer with the caller's
// command. scripted is false when there is no script, it could not run, or it
// answered with a key the sponsor menu has no command for; the built-in
// screen is then drawn.
func scriptedSponsorChoice(c *cmdCtx, rec *MenuRecord, tag string) (choice string, scripted bool, err error) {
	if rec == nil || rec.Script == "" {
		return "", false, nil
	}
	view := &scripting.MenuView{
		Title: rec.Title,
		Items: []scripting.MenuItemView{
			{Key: "E", Label: "Edit " + plainSetting(tag)}, {Key: "P", Label: "Positions"},
			{Key: "[", Label: "Prev Area"}, {Key: "]", Label: "Next Area"},
			{Key: "Q", Label: "Back"},
		},
	}
	choice, scripted, err = c.scriptedChoice("SPONSORM", rec, view)
	if scripted {
		switch choice {
		case "E", "P", "[", "]", "Q":
		default:
			// A script that answers without reading the caller would be run
			// again at once; the built-in screen waits for a key.
			return "", false, err
		}
	}
	return choice, scripted, err
}
