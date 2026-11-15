package config

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// OptColor is a DOS colour attribute (0-255) that a theme may leave unset.
// The zero value is unset, which is not the same as colour 0, black on
// black's foreground: a role left out of theme.json inherits, and one set
// to 0 is 0. In JSON it is a number, and an unset one is left out
// (omitzero).
type OptColor struct {
	Set   bool
	Value int
}

// SetColor returns the colour v, set.
func SetColor(v int) OptColor { return OptColor{Set: true, Value: v} }

// MarshalJSON writes a set colour as its number. An unset one is null, for
// a caller that doesn't use omitzero.
func (c OptColor) MarshalJSON() ([]byte, error) {
	if !c.Set {
		return []byte("null"), nil
	}
	return []byte(strconv.Itoa(c.Value)), nil
}

// UnmarshalJSON reads a number 0-255 as a set colour and null as unset.
func (c *OptColor) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*c = OptColor{}
		return nil
	}
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("colour must be a number from 0 to 255, not %s", data)
	}
	if n < 0 || n > 255 {
		return fmt.Errorf("colour %d is outside 0 to 255", n)
	}
	*c = SetColor(n)
	return nil
}

// ColorRole names a colour by what it is for, as theme.json keys it.
type ColorRole string

// The lightbar roles (#1077). Every lightbar the BBS generates takes its
// colours from these, so one change to a theme reaches all of them.
const (
	// RoleLightbarSelected is the highlighted row or command: the file
	// list's current file, the selected area, conference, BBS or door, and
	// the selected command on a command bar.
	RoleLightbarSelected ColorRole = "lightbarSelected"
	// RoleLightbarNormal is an unselected command on the file list's
	// command bar and the message scan's.
	RoleLightbarNormal ColorRole = "lightbarNormal"
	// RoleLightbarCommand is an unselected command on the message reader's
	// command bar.
	RoleLightbarCommand ColorRole = "lightbarCommand"
	// RoleLightbarHotkey is a key name in a hint row ("Up", "Enter", "Q").
	RoleLightbarHotkey ColorRole = "lightbarHotkey"
	// RoleLightbarHint is the rest of a hint row: its brackets and words.
	RoleLightbarHint ColorRole = "lightbarHint"
	// RoleLightbarBorder is the separator line a lightbar screen draws
	// between its list and its footer.
	RoleLightbarBorder ColorRole = "lightbarBorder"
	// RoleLightbarDestructive is an unselected command that destroys
	// something: Del on the message reader's command bar.
	RoleLightbarDestructive ColorRole = "lightbarDestructive"
)

// The colours a theme has always had, as roles, so every colour is read
// through the one resolver. theme.json always holds these (LoadThemeConfig
// fills in a default for one it leaves out).
const (
	// RoleYesNoHighlight and RoleYesNoRegular are the selected and the
	// other answer of a Yes/No prompt.
	RoleYesNoHighlight ColorRole = "yesNoHighlightColor"
	RoleYesNoRegular   ColorRole = "yesNoRegularColor"
	// RoleChatSysop and RoleChatUser are the sysop's and the caller's text
	// in split-screen chat, where the chat screen sets no colour.
	RoleChatSysop ColorRole = "chatSysopColor"
	RoleChatUser  ColorRole = "chatUserColor"
	// RoleAutocorrect is a word the message editor has just autocorrected.
	RoleAutocorrect ColorRole = "autocorrectHighlightColor"
)

// LightbarRoles lists the lightbar roles in the order they are documented
// and edited.
var LightbarRoles = []ColorRole{
	RoleLightbarSelected, RoleLightbarNormal, RoleLightbarCommand,
	RoleLightbarHotkey, RoleLightbarHint, RoleLightbarBorder, RoleLightbarDestructive,
}

// builtinRoleColors is the lowest layer of the colour precedence: what a
// role is when no theme sets it. Each is the colour the screens that now
// take the role used to hard-code, so a theme written before the roles
// looks as it did. The selected and normal roles are not here: they fall
// back to the theme's Yes/No colours, which is what every lightbar used
// before it had roles of its own.
var builtinRoleColors = map[ColorRole]int{
	RoleLightbarCommand:     9,  // bright blue
	RoleLightbarHotkey:      15, // bright white
	RoleLightbarHint:        8,  // dark gray
	RoleLightbarBorder:      8,  // dark gray
	RoleLightbarDestructive: 4,  // red
}

// ColorSource says which layer of the precedence a colour came from.
type ColorSource int

const (
	// SourceBuiltin is the built-in fallback.
	SourceBuiltin ColorSource = iota
	// SourceTheme is the selected theme's theme.json, its overlay included.
	SourceTheme
)

// roleField returns the theme's own setting for role.
func (t ThemeConfig) roleField(role ColorRole) OptColor {
	switch role {
	case RoleLightbarSelected:
		return t.LightbarSelected
	case RoleLightbarNormal:
		return t.LightbarNormal
	case RoleLightbarCommand:
		return t.LightbarCommand
	case RoleLightbarHotkey:
		return t.LightbarHotkey
	case RoleLightbarHint:
		return t.LightbarHint
	case RoleLightbarBorder:
		return t.LightbarBorder
	case RoleLightbarDestructive:
		return t.LightbarDestructive
	}
	return OptColor{}
}

// Color returns the colour of role in this theme. It is the resolver for
// the colour precedence, lowest to highest:
//
//  1. the built-in fallback;
//  2. the theme's theme.json, with its overlay.
//
// A role the theme sets, 0 included, is that colour. One it leaves out is
// the built-in fallback; for the selected and normal roles that is the
// theme's Yes/No highlight and regular colours, so a theme.json from
// before the roles draws every lightbar as it always did.
func (t ThemeConfig) Color(role ColorRole) int {
	c, _ := t.ColorWithSource(role)
	return c
}

// ColorWithSource is Color, and the layer the colour came from.
func (t ThemeConfig) ColorWithSource(role ColorRole) (int, ColorSource) {
	if c := t.roleField(role); c.Set {
		return c.Value, SourceTheme
	}
	switch role {
	case RoleYesNoHighlight:
		return t.YesNoHighlightColor, SourceTheme
	case RoleYesNoRegular:
		return t.YesNoRegularColor, SourceTheme
	case RoleChatSysop:
		return t.ChatSysopColor, SourceTheme
	case RoleChatUser:
		return t.ChatUserColor, SourceTheme
	case RoleAutocorrect:
		return t.AutocorrectHighlightColor, SourceTheme
	case RoleLightbarSelected:
		return t.YesNoHighlightColor, SourceBuiltin
	case RoleLightbarNormal:
		return t.YesNoRegularColor, SourceBuiltin
	}
	return builtinRoleColors[role], SourceBuiltin
}
