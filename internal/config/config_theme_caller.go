package config

import (
	"fmt"
	"path/filepath"
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

// callerField is callerFieldPtr for a theme read by value.
func (t ThemeConfig) callerField(role ColorRole) *OptColor {
	return t.callerFieldPtr(role)
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

// pipeBackground maps a DOS attribute's background (its high nibble, in DOS
// colour order: 1 blue, 4 red) to the |B code that draws it (|B codes
// follow ANSI order: |B1 red, |B4 blue).
var pipeBackground = [8]int{0, 4, 2, 6, 1, 5, 3, 7}

// AttrPipeCode returns the pipe code for a DOS attribute (background*16 +
// foreground): the foreground |00 to |15, and the background as a |B code
// when it isn't black. The blink bit is dropped, as the lightbars drop it.
func AttrPipeCode(attr int) string {
	if attr < 0 || attr > 255 {
		attr = 7
	}
	code := fmt.Sprintf("|%02d", attr%16)
	if bg := (attr / 16) % 8; bg != 0 {
		code += fmt.Sprintf("|B%d", pipeBackground[bg])
	}
	return code
}

// BoardColorSlotCodes returns the board's caller colour slot defaults, as
// ColorSlotCodes works them out, for a tool such as the string or menu
// editor that previews |C1-|C7 without running the board: the theme that
// configDir's config.json selects under bbsRoot, its theme.colors, and the
// legacy defColorN in configDir's strings.json. A part that can't be read
// adds nothing, so at worst the built-in colours apply.
func BoardColorSlotCodes(bbsRoot, configDir string) [7]string {
	legacy := LegacyColorSlotsFile(filepath.Join(configDir, "strings.json"))
	theme := builtinTheme()
	if dir, _, err := SelectedMenuSet(bbsRoot, configDir); err == nil {
		theme, _ = LoadThemeConfig(dir)
	}
	board, _ := BoardColorsFromConfigDir(configDir)
	return theme.WithBoardColors(board).ColorSlotCodes(legacy)
}
