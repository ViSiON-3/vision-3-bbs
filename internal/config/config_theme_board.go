package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ThemeColors is config.json's theme.colors: the board's own colour
// overrides (#1078). It holds only the colours the sysop changed. A colour
// left out inherits the selected theme's, so a theme's defaults, a theme
// upgrade and a switch of theme all still reach every colour not named
// here. The keys are theme.json's.
type ThemeColors struct {
	YesNoHighlightColor       OptColor `json:"yesNoHighlightColor,omitzero"`
	YesNoRegularColor         OptColor `json:"yesNoRegularColor,omitzero"`
	LightbarSelected          OptColor `json:"lightbarSelected,omitzero"`
	LightbarNormal            OptColor `json:"lightbarNormal,omitzero"`
	LightbarCommand           OptColor `json:"lightbarCommand,omitzero"`
	LightbarHotkey            OptColor `json:"lightbarHotkey,omitzero"`
	LightbarHint              OptColor `json:"lightbarHint,omitzero"`
	LightbarBorder            OptColor `json:"lightbarBorder,omitzero"`
	LightbarDestructive       OptColor `json:"lightbarDestructive,omitzero"`
	ChatSysopColor            OptColor `json:"chatSysopColor,omitzero"`
	ChatUserColor             OptColor `json:"chatUserColor,omitzero"`
	AutocorrectHighlightColor OptColor `json:"autocorrectHighlightColor,omitzero"`
	CallerPrompt              OptColor `json:"callerPrompt,omitzero"`
	CallerInput               OptColor `json:"callerInput,omitzero"`
	CallerText                OptColor `json:"callerText,omitzero"`
	CallerStat                OptColor `json:"callerStat,omitzero"`
	CallerText2               OptColor `json:"callerText2,omitzero"`
	CallerStat2               OptColor `json:"callerStat2,omitzero"`
	CallerBar                 OptColor `json:"callerBar,omitzero"`
	// ToastFadeColors overrides the theme's fade ramp; "" inherits it.
	ToastFadeColors string `json:"toastFadeColors,omitempty"`
}

// BoardColorRoles lists every role theme.colors can override, in the order
// the config editor shows them.
var BoardColorRoles = append([]ColorRole{RoleYesNoHighlight, RoleYesNoRegular},
	append(append(append([]ColorRole(nil), LightbarRoles...), RoleChatSysop, RoleChatUser, RoleAutocorrect), CallerColorRoles...)...)

// field returns a pointer to role's override, or nil for a role
// theme.colors does not have.
func (c *ThemeColors) field(role ColorRole) *OptColor {
	switch role {
	case RoleYesNoHighlight:
		return &c.YesNoHighlightColor
	case RoleYesNoRegular:
		return &c.YesNoRegularColor
	case RoleLightbarSelected:
		return &c.LightbarSelected
	case RoleLightbarNormal:
		return &c.LightbarNormal
	case RoleLightbarCommand:
		return &c.LightbarCommand
	case RoleLightbarHotkey:
		return &c.LightbarHotkey
	case RoleLightbarHint:
		return &c.LightbarHint
	case RoleLightbarBorder:
		return &c.LightbarBorder
	case RoleLightbarDestructive:
		return &c.LightbarDestructive
	case RoleChatSysop:
		return &c.ChatSysopColor
	case RoleChatUser:
		return &c.ChatUserColor
	case RoleAutocorrect:
		return &c.AutocorrectHighlightColor
	case RoleCallerPrompt:
		return &c.CallerPrompt
	case RoleCallerInput:
		return &c.CallerInput
	case RoleCallerText:
		return &c.CallerText
	case RoleCallerStat:
		return &c.CallerStat
	case RoleCallerText2:
		return &c.CallerText2
	case RoleCallerStat2:
		return &c.CallerStat2
	case RoleCallerBar:
		return &c.CallerBar
	}
	return nil
}

// Get returns the board's override of role, unset when it has none.
func (c ThemeColors) Get(role ColorRole) OptColor {
	if f := c.field(role); f != nil {
		return *f
	}
	return OptColor{}
}

// Override sets the board's override of role to v.
func (c *ThemeColors) Override(role ColorRole, v int) {
	if f := c.field(role); f != nil {
		*f = SetColor(v)
	}
}

// Inherit removes the board's override of role, so the theme's colour
// shows again ("Use theme default"). It reports whether there was one.
func (c *ThemeColors) Inherit(role ColorRole) bool {
	f := c.field(role)
	if f == nil || !f.Set {
		return false
	}
	*f = OptColor{}
	return true
}

// validate reports a value theme.colors can't hold. The colours check
// themselves when they are read; the ramp is text, checked here.
func (c ThemeColors) validate() error {
	if c.ToastFadeColors == "" {
		return nil
	}
	if _, ok := ParseToastFadeColors(c.ToastFadeColors); !ok {
		return fmt.Errorf("theme.colors.toastFadeColors %q is not 1 to %d colours from 0 to 15", c.ToastFadeColors, MaxToastFadeSteps)
	}
	return nil
}

// WithBoardColors returns the theme as the board draws it: t with each
// colour theme.colors names put in place of the theme's own. It is layer 3
// of the colour precedence, over the theme (layer 2) and the built-in
// fallbacks (layer 1); Color on the result resolves all three.
func (t ThemeConfig) WithBoardColors(c ThemeColors) ThemeConfig {
	for _, role := range BoardColorRoles {
		o := c.Get(role)
		if !o.Set {
			continue
		}
		switch role {
		case RoleYesNoHighlight:
			t.YesNoHighlightColor = o.Value
		case RoleYesNoRegular:
			t.YesNoRegularColor = o.Value
		case RoleChatSysop:
			t.ChatSysopColor = o.Value
		case RoleChatUser:
			t.ChatUserColor = o.Value
		case RoleAutocorrect:
			t.AutocorrectHighlightColor = o.Value
		case RoleLightbarSelected:
			t.LightbarSelected = o
		case RoleLightbarNormal:
			t.LightbarNormal = o
		case RoleLightbarCommand:
			t.LightbarCommand = o
		case RoleLightbarHotkey:
			t.LightbarHotkey = o
		case RoleLightbarHint:
			t.LightbarHint = o
		case RoleLightbarBorder:
			t.LightbarBorder = o
		case RoleLightbarDestructive:
			t.LightbarDestructive = o
		default:
			if f := t.callerFieldPtr(role); f != nil {
				*f = o
			}
		}
	}
	if _, ok := ParseToastFadeColors(c.ToastFadeColors); ok {
		t.ToastFadeColors = c.ToastFadeColors
	}
	return t
}

// ResolveColor returns role's colour for a board running theme with the
// overrides in board, and the layer it came from: the board's override,
// else the theme's value, else the built-in fallback.
func ResolveColor(role ColorRole, theme ThemeConfig, board ThemeColors) (int, ColorSource) {
	if o := board.Get(role); o.Set {
		return o.Value, SourceBoard
	}
	return theme.WithBoardColors(board).ColorWithSource(role)
}

// BoardColorsFromConfigDir reads only theme.colors from configDir's
// config.json, for a program that needs the board's colours and nothing
// else. A missing file or section gives no overrides.
func BoardColorsFromConfigDir(configDir string) (ThemeColors, error) {
	path := filepath.Join(configDir, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ThemeColors{}, nil
		}
		return ThemeColors{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var raw struct {
		Theme ThemeSelection `json:"theme"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return ThemeColors{}, fmt.Errorf("reading theme.colors from %s: %w", path, err)
	}
	if err := raw.Theme.Colors.validate(); err != nil {
		return ThemeColors{}, err
	}
	return raw.Theme.Colors, nil
}
