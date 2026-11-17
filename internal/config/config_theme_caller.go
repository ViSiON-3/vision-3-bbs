package config

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// The caller colour roles (#426): the board's default for each caller
// colour slot, |C1 to |C7. A caller who picks their own colours in User
// Konfig sees those instead, wherever a screen uses the code.
const (
	// RoleCallerPrompt is |C1: a prompt's question.
	RoleCallerPrompt ColorRole = "callerPrompt"
	// RoleCallerInput is |C2: what the caller types at a prompt.
	RoleCallerInput ColorRole = "callerInput"
	// RoleCallerText is |C3: plain text.
	RoleCallerText ColorRole = "callerText"
	// RoleCallerStat is |C4: a value picked out of the text, such as a name
	// or a number.
	RoleCallerStat ColorRole = "callerStat"
	// RoleCallerText2 is |C5: secondary text, such as brackets and
	// separators.
	RoleCallerText2 ColorRole = "callerText2"
	// RoleCallerStat2 is |C6: a second kind of value, such as an area name.
	RoleCallerStat2 ColorRole = "callerStat2"
	// RoleCallerBar is |C7: a highlighted choice. Unset, it is the
	// lightbarSelected role.
	RoleCallerBar ColorRole = "callerBar"
)

// CallerColorRoles lists the caller colour roles in slot order: element
// n-1 is the default of |Cn.
var CallerColorRoles = []ColorRole{
	RoleCallerPrompt, RoleCallerInput, RoleCallerText, RoleCallerStat,
	RoleCallerText2, RoleCallerStat2, RoleCallerBar,
}

// callerFieldPtr returns a pointer to the theme's own setting for a caller
// colour role, or nil for any other role.
func (t *ThemeConfig) callerFieldPtr(role ColorRole) *OptColor {
	switch role {
	case RoleCallerPrompt:
		return &t.CallerPrompt
	case RoleCallerInput:
		return &t.CallerInput
	case RoleCallerText:
		return &t.CallerText
	case RoleCallerStat:
		return &t.CallerStat
	case RoleCallerText2:
		return &t.CallerText2
	case RoleCallerStat2:
		return &t.CallerStat2
	case RoleCallerBar:
		return &t.CallerBar
	}
	return nil
}

// ColorSlotCodes returns the board's default for each caller colour slot,
// |C1 to |C7, as a pipe code. A slot the theme or the board's theme.colors
// sets is that colour; one neither sets is the slot's colour from an older
// strings.json's defColorN (legacy, from StringsConfig.LegacyColorSlots)
// when it has one, else the role's built-in colour. t is the theme as the
// board draws it, theme.colors applied (WithBoardColors).
func (t ThemeConfig) ColorSlotCodes(legacy [7]string) [7]string {
	var out [7]string
	for i, role := range CallerColorRoles {
		c, src := t.ColorWithSource(role)
		if src == SourceBuiltin && legacy[i] != "" {
			out[i] = legacy[i]
			continue
		}
		out[i] = AttrPipeCode(c)
	}
	return out
}

// ResolveCallerColor is ResolveColor for a caller colour role, with the
// legacy slot defaults from an older strings.json (as ColorSlotCodes takes
// them): a slot the theme and board leave unset is its defColorN, as the
// board draws it. Any other role resolves as ResolveColor does.
func ResolveCallerColor(role ColorRole, theme ThemeConfig, board ThemeColors, legacy [7]string) (int, ColorSource) {
	c, src := ResolveColor(role, theme, board)
	if src != SourceBuiltin {
		return c, src
	}
	for i, r := range CallerColorRoles {
		if r == role && legacy[i] != "" {
			return PipeCodeAttr(legacy[i]), SourceStrings
		}
	}
	return c, src
}

// AttrPipeCode returns the pipe code for a DOS attribute (background*16 +
// foreground): the foreground |00 to |15, and the background as a |B code
// when it isn't black. The blink bit is dropped, as the lightbars drop it.
func AttrPipeCode(attr int) string {
	if attr < 0 || attr > 255 {
		attr = 7
	}
	code := fmt.Sprintf("|%02d", attr%16)
	if bg := (attr / 16) % 8; bg != 0 {
		code += fmt.Sprintf("|B%d", ansi.ColorOrder(bg))
	}
	return code
}

// BoardColorSlotCodes returns the board's caller colour slot defaults, as
// ColorSlotCodes works them out, for a tool such as the string or menu
// editor that previews |C1-|C7 without running the board: the theme of
// menuSet, or when that is "" the one configDir's config.json selects under
// bbsRoot; config.json's theme.colors; and the legacy defColorN in the
// strings file at stringsPath, or configDir's strings.json when that is
// "". A part that can't be read adds nothing, so at
// worst the built-in colours apply. Nothing is logged, so it is safe to
// call with an editor's screen up.
func BoardColorSlotCodes(bbsRoot, configDir, stringsPath, menuSet string) [7]string {
	if stringsPath == "" {
		stringsPath = filepath.Join(configDir, "strings.json")
	}
	legacy := LegacyColorSlotsFile(stringsPath)
	sel, err := themeSelectionFromConfigDir(configDir)
	if err != nil || sel.Colors.validate() != nil {
		sel = ThemeSelection{}
	}
	if menuSet == "" {
		name := sel.Name
		if name == "" {
			name = menuset.DefaultTheme
		}
		if dir, err := menuset.CheckTheme(bbsRoot, name); err == nil {
			menuSet = dir
		}
	}
	theme := builtinTheme()
	if menuSet != "" {
		theme, _ = readThemeConfig(menuSet)
	}
	return theme.WithBoardColors(sel.Colors).ColorSlotCodes(legacy)
}

// PipeCodeAttr returns the DOS attribute a colour slot's pipe code draws,
// for a screen that colours by attribute, such as a lightbar: a foreground
// |00 to |15 and an optional background |B0 to |B15. A bright background
// (|B8 to |B15) becomes its plain one, as the lightbars draw it. Anything
// else is light gray.
func PipeCodeAttr(code string) int {
	if len(code) < 3 || code[0] != '|' {
		return 7
	}
	fg, err := strconv.Atoi(code[1:3])
	if err != nil || fg < 0 || fg > 15 {
		return 7
	}
	attr := fg
	if bg := code[3:]; len(bg) >= 3 && bg[:2] == "|B" {
		if b, err := strconv.Atoi(bg[2:]); err == nil && b >= 0 && b <= 15 {
			// ColorOrder maps either way: |B codes are in ANSI order, an
			// attribute's background in DOS order.
			attr += ansi.ColorOrder(b) * 16
		}
	}
	return attr
}
