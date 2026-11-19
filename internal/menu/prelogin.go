package menu

import (
	"log/slog"
	"path/filepath"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// The connection check and the login matrix belong to the board, not to a
// theme: no theme, the board's own or a caller's, changes what a
// caller sees before login. Their files come from the board's prelogin/
// directory, else the shipped stock set (menuset.PreloginFiles), and the
// matrix's wording from the board's strings with prelogin/strings.json
// over them. Screens that follow the matrix (the PRELOGON art, the login
// prompt, the new-user form) stay with the board's theme.

// bbsRoot is the BBS root of the board e belongs to: the directory that
// holds its menus/ directory.
func (e *MenuExecutor) bbsRoot() string {
	return filepath.Dir(filepath.Dir(e.shared().MenuSetPath))
}

// preloginView is the executor the pre-login screens are drawn with: the
// board's, reading its menu files from menuset.PreloginFiles and its
// strings from the board's strings.json with prelogin/strings.json over
// them. A caller's view gives the same as the board.
func (e *MenuExecutor) preloginView() *MenuExecutor {
	b := e.shared()
	root := b.bbsRoot()
	set := menuset.PreloginFiles(root)
	v := b.newView("", set.Base, &config.ThemeConfig{})
	v.menuSet = &set
	path := filepath.Join(set.Overlay, config.ThemeStringsFile)
	strs, err := config.LoadStringsLayers(path)
	if err != nil {
		slog.Error("pre-login strings not loaded; using the board's", "path", path, "error", err)
		strs, _ = config.LoadStringsLayers()
	}
	v.themeStrings.Store(strs)
	return v
}
