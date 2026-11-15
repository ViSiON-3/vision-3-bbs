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
//
// A pipe code sets a foreground and leaves the background alone, so text
// in a background-bearing colour must be closed with roleReset before
// anything else is drawn, and a pipe code must not follow one directly:
// lightbarHint and lightbarRule see to both.
func (e *MenuExecutor) rolePipe(role config.ColorRole) string {
	c := e.roleColor(role)
	if plainForeground(c) {
		return fmt.Sprintf("|%02d", c)
	}
	return colorCodeToAnsi(c)
}

// plainForeground reports whether a DOS attribute is a foreground on the
// default black background, which a pipe code can express.
func plainForeground(c int) bool { return c >= 0 && c <= 15 }

// roleReset closes text drawn with rolePipe(role): nothing for a plain
// foreground, as the screens have always written, and an attribute reset
// for a colour with a background, so the background ends with the text.
func (e *MenuExecutor) roleReset(role config.ColorRole) string {
	if plainForeground(e.roleColor(role)) {
		return ""
	}
	return "\x1b[0m"
}

// lightbarHint colours a generated hint row. The hint is written the way
// these rows always were, with |15 before a key name and |08 before the
// brackets and words around it, and those two codes are replaced by the
// hotkey and hint roles.
//
// With both roles plain foregrounds the result is pipe codes, as before.
// If either has a background, both are written as escape sequences, each
// of which resets the attributes first: a pipe code after a background
// colour would keep that background, so a key name in yellow on blue
// would leave the words after it on blue too. The row is then closed
// with a reset.
func (e *MenuExecutor) lightbarHint(hint string) string {
	hotkey, text := e.roleColor(config.RoleLightbarHotkey), e.roleColor(config.RoleLightbarHint)
	if plainForeground(hotkey) && plainForeground(text) {
		return strings.NewReplacer(
			"|15", fmt.Sprintf("|%02d", hotkey),
			"|08", fmt.Sprintf("|%02d", text),
		).Replace(hint)
	}
	return strings.NewReplacer(
		"|15", colorCodeToAnsi(hotkey),
		"|08", colorCodeToAnsi(text),
	).Replace(hint) + "\x1b[0m"
}

// lightbarRule colours a generated separator line with the border role,
// and puts the colour back to light gray after it.
func (e *MenuExecutor) lightbarRule(line string) string {
	return e.rolePipe(roleBorder) + line + e.roleReset(roleBorder) + "|07"
}
