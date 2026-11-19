package menu

import (
	"fmt"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/scripting"
)

// twitScriptPage is how many entries a script is given at a time.
const twitScriptPage = 12

// scriptedTwitScreen has TWITLIST.MNU's SCRIPT, when it names one, draw one
// page of the Twit List and answer with the caller's command. The body is one
// row per entry, numbered as the Delete command takes them; the script lays
// the rows out. scripted is false when there is no script or it could not
// run, or answered with a key the Twit List has no command for, and the
// built-in screen is drawn. It returns the page shown, clamped
// to the pages there are.
func scriptedTwitScreen(c *cmdCtx, page int, notice string) (shown int, choice string, scripted bool, err error) {
	rec, err := c.e.twitMenu()
	if err != nil || rec.Script == "" {
		return page, "", false, nil
	}
	entries := c.currentUser.TwitList
	pages := max(1, (len(entries)+twitScriptPage-1)/twitScriptPage)
	page = min(max(page, 0), pages-1)
	view := &scripting.MenuView{
		Title:     rec.Title,
		Shortcuts: []string{"N", "P"},
		Notice:    plainSetting(string(ansi.ReplacePipeCodes([]byte(notice)))),
		Hint:      fmt.Sprintf("%d of %d  page %d of %d", len(entries), twitPolicy(c.e).Limit(), page+1, pages),
		Items: []scripting.MenuItemView{
			{Key: "A", Label: "Add Author"}, {Key: "D", Label: "Delete Entry"},
			{Key: "C", Label: "Clear List"}, {Key: "Q", Label: "Back"},
		},
	}
	if pages > 1 {
		view.Items = append(view.Items,
			scripting.MenuItemView{Key: "[", Label: "Prev Page"}, scripting.MenuItemView{Key: "]", Label: "Next Page"})
	}
	if len(entries) == 0 {
		view.Body = []string{plainSetting(string(ansi.ReplacePipeCodes([]byte(c.e.Strings().TwitEmpty))))}
	}
	for i := page * twitScriptPage; i < min((page+1)*twitScriptPage, len(entries)); i++ {
		name := entries[i].Name
		if entries[i].Addr != "" {
			name += "@" + entries[i].Addr
		}
		view.Body = append(view.Body, plainSetting(fmt.Sprintf("%2d  %s", i+1, name)))
	}
	choice, scripted, err = c.scriptedChoice("TWITLIST", rec, view)
	if scripted && !twitCommand(choice) {
		// A script that answers without reading the caller would be run
		// again at once; the built-in screen waits for a key.
		return page, "", false, err
	}
	return page, choice, scripted, err
}

// twitCommand reports whether key is one the Twit List acts on.
func twitCommand(key string) bool {
	switch key {
	case "Q", "]", "N", "[", "P", "A", "D", "C":
		return true
	}
	return false
}
