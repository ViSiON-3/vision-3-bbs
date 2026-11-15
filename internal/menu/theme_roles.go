package menu

import (
	"fmt"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// The theme's colour roles, under the names the lightbar code uses.
const (
	roleSelected    = config.RoleLightbarSelected
	roleNormal      = config.RoleLightbarNormal
	roleCommand     = config.RoleLightbarCommand
	roleDestructive = config.RoleLightbarDestructive
	roleBorder      = config.RoleLightbarBorder
	roleYesNoHi     = config.RoleYesNoHighlight
	roleYesNoLo     = config.RoleYesNoRegular
)

// roleColor is the DOS attribute of a theme colour role, through the
// theme's resolver (config.ThemeConfig.Color).
func (e *MenuExecutor) roleColor(role config.ColorRole) int {
	if e == nil {
		// A screen drawn without an executor gets the built-in colours.
		return config.ThemeConfig{}.Color(role)
	}
	return e.Theme().Color(role)
}

// roleAnsi is the escape sequence that selects a role's colour.
func (e *MenuExecutor) roleAnsi(role config.ColorRole) string {
	return colorCodeToAnsi(e.roleColor(role))
}

// rolePipe is a role's colour for text that goes on through the pipe-code
// renderer. A plain foreground, 0-15, is the pipe code a generated screen
// has always written for it, so the default theme's output is unchanged;
// a colour with a background is the escape sequence.
func (e *MenuExecutor) rolePipe(role config.ColorRole) string {
	c := e.roleColor(role)
	if c >= 0 && c <= 15 {
		return fmt.Sprintf("|%02d", c)
	}
	return colorCodeToAnsi(c)
}

// lightbarHint colours a generated hint row. The hint is written the way
// these rows always were, with |15 before a key name and |08 before the
// brackets and words around it, and those two codes are replaced by the
// hotkey and hint roles.
func (e *MenuExecutor) lightbarHint(hint string) string {
	return strings.NewReplacer(
		"|15", e.rolePipe(config.RoleLightbarHotkey),
		"|08", e.rolePipe(config.RoleLightbarHint),
	).Replace(hint)
}

// lightbarRule colours a generated separator line with the border role,
// and puts the colour back to light gray after it.
func (e *MenuExecutor) lightbarRule(line string) string {
	return e.rolePipe(roleBorder) + line + "|07"
}
