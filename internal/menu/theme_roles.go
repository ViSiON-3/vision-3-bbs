package menu

import (
	"fmt"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
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

// barColor is the DOS attribute of the highlighted row or command on a
// lightbar u is looking at: what |C7 draws for u (colorSlot), so u's own Bar
// colour when they picked one, else the board's, which unset is the
// lightbarSelected role. A screen's own BAR file still colours that screen
// over this. u may be nil.
func (e *MenuExecutor) barColor(u *user.User) int {
	return config.PipeCodeAttr(e.colorSlot(u, user.ColorSlots))
}

// barAnsi is barColor's escape sequence.
func (e *MenuExecutor) barAnsi(u *user.User) string {
	return colorCodeToAnsi(e.barColor(u))
}

// roleAnsi is the escape sequence that selects a role's colour.
func (e *MenuExecutor) roleAnsi(role config.ColorRole) string {
	return colorCodeToAnsi(e.roleColor(role))
}

// roleText is text in a role's colour, for output that goes on through the
// pipe-code renderer. For a plain foreground, 0-15, it is the pipe code a
// generated screen has always written and then the text, so the default
// theme's output is unchanged. For a colour with a background it is the
// escape sequence, the text, and an attribute reset: a pipe code sets a
// foreground and leaves the background alone, so without the reset the
// background would stay behind whatever is drawn next.
//
// The role is read once. Read for the colour and again for the reset, a
// theme reload between the two could open a background and never close it.
func (e *MenuExecutor) roleText(role config.ColorRole, text string) string {
	c := e.roleColor(role)
	if plainForeground(c) {
		return fmt.Sprintf("|%02d", c) + text
	}
	return colorCodeToAnsi(c) + text + "\x1b[0m"
}

// plainForeground reports whether a DOS attribute is a foreground on the
// default black background, which a pipe code can express.
func plainForeground(c int) bool { return c >= 0 && c <= 15 }

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
	return e.roleText(roleBorder, line) + "|07"
}
