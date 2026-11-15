package configeditor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// Field definitions for the Theme inner menu, which edits the menu set's
// theme.json. It is saved to the set's overlay (menus.d/v3/theme.json) so an
// upgrade of the shipped menus keeps the sysop's choices.

// themeMenuItems returns the Theme inner menu.
func themeMenuItems() []sysConfigMenuItem {
	return []sysConfigMenuItem{
		{Label: "Theme Selection", Build: func(m *Model) []fieldDef {
			return themeFieldsSelection(&m.configs.Server, func() []string { return menuset.ListThemes(bbsRootFor(m.configPath)) })
		}},
		{Label: "Prompt Colors", Build: func(m *Model) []fieldDef {
			return themeFieldsPrompts(&m.configs.Theme, &m.configs.Server.Theme.Colors)
		}},
		{Label: "Lightbar Colors", Build: func(m *Model) []fieldDef {
			return themeFieldsLightbar(&m.configs.Theme, &m.configs.Server.Theme.Colors)
		}},
		{Label: "Split-Screen Chat", Build: func(m *Model) []fieldDef {
			return themeFieldsChat(&m.configs.Theme, &m.configs.Server.Theme.Colors, func() []themeChatScreen { return themeChatScreens(m.configs.themeSet) })
		}},
		{Label: "Toasts", Build: func(m *Model) []fieldDef {
			return themeFieldsToasts(&m.configs.Theme, &m.configs.Server.Theme.Colors)
		}},
	}
}

// boardColorField edits one colour as a board override (#1078). It shows
// the colour the board draws with, the board's override if it has one and
// otherwise the theme's, and says which. Setting it stores an override in
// config.json's theme.colors, never in the theme's theme.json; D on the
// field ("Use theme default") removes the override again. th is the theme
// as loaded, without the board's colours.
func boardColorField(label, help string, row int, role config.ColorRole, th *config.ThemeConfig, board *config.ThemeColors) fieldDef {
	resolve := func() (int, config.ColorSource) { return config.ResolveColor(role, *th, *board) }
	return fieldDef{
		Label: label, Help: help + "; Enter picks, T types a number, D uses the theme's",
		Type: ftColorAttr, Col: 3, Row: row, Width: 3, Min: 0, Max: 255,
		Get: func() string { v, _ := resolve(); return strconv.Itoa(v) },
		Set: func(val string) error {
			n, err := strconv.Atoi(val)
			if err != nil {
				return err
			}
			if n < 0 || n > 255 {
				return fmt.Errorf("a colour is 0 to 255")
			}
			// Choosing the colour already shown is not an override.
			if cur, src := resolve(); n == cur && src != config.SourceBoard {
				return nil
			}
			board.Override(role, n)
			return nil
		},
		Source:  func() string { _, src := resolve(); return src.String() },
		Inherit: func() bool { return board.Inherit(role) },
	}
}

// themeFieldsLightbar is the lightbar colour roles (#1077), which every
// lightbar the BBS draws inherits.
func themeFieldsLightbar(th *config.ThemeConfig, board *config.ThemeColors) []fieldDef {
	return []fieldDef{
		boardColorField("Selected", "The highlighted row or command on every lightbar", 1, config.RoleLightbarSelected, th, board),
		boardColorField("Normal", "Unselected commands on the file list and message scan bars", 2, config.RoleLightbarNormal, th, board),
		boardColorField("Command", "Unselected commands on the message reader bar", 3, config.RoleLightbarCommand, th, board),
		boardColorField("Hotkey", "Key names in a lightbar's hint row", 4, config.RoleLightbarHotkey, th, board),
		boardColorField("Hint", "The rest of a lightbar's hint row", 5, config.RoleLightbarHint, th, board),
		boardColorField("Border", "The line between a list and its footer", 6, config.RoleLightbarBorder, th, board),
		boardColorField("Destructive", "Del on the message reader bar", 7, config.RoleLightbarDestructive, th, board),
	}
}

// themeFieldsSelection is the theme the board runs: config.json's
// theme.name, picked from the directories under menus/. The default theme
// is stored as an omitted name, as a config written before the setting has
// it. The other Theme screens go on editing the theme that was selected
// when the editor opened (allConfigs.themeSet).
func themeFieldsSelection(cfg *config.ServerConfig, themes func() []string) []fieldDef {
	return []fieldDef{
		{
			Label: "Theme", Help: "Menu set under menus/ the board runs; restart the BBS to switch", Type: ftLookup, Col: 3, Row: 1, Width: 30,
			Get: func() string { return cfg.ThemeName() },
			Set: func(val string) error {
				if err := menuset.ValidThemeName(val); err != nil {
					return err
				}
				if val == menuset.DefaultTheme {
					val = ""
				}
				cfg.Theme.Name = val
				return nil
			},
			LookupItems: func() []LookupItem {
				var items []LookupItem
				for _, name := range themes() {
					display := name
					if name == menuset.DefaultTheme {
						display += " - the default"
					}
					items = append(items, LookupItem{Value: name, Display: display})
				}
				return items
			},
		},
	}
}

func themeFieldsPrompts(th *config.ThemeConfig, board *config.ThemeColors) []fieldDef {
	return []fieldDef{
		boardColorField("Yes/No Highlight", "The selected answer in Yes/No prompts", 1, config.RoleYesNoHighlight, th, board),
		boardColorField("Yes/No Regular", "The other answer in Yes/No prompts", 2, config.RoleYesNoRegular, th, board),
		boardColorField("Autocorrect Fix", "A word the message editor just autocorrected, while the fix is shown", 3, config.RoleAutocorrect, th, board),
	}
}

// The Chat Screen picker's entries for the built-in layout and for random.
// theme.json stores the built-in layout as "".
const (
	themeChatBuiltin = "(built-in)"
	themeChatRandom  = "random"
)

func themeFieldsChat(th *config.ThemeConfig, board *config.ThemeColors, screens func() []themeChatScreen) []fieldDef {
	return []fieldDef{
		{
			Label: "Chat Screen", Help: "Screen chats open on; Alt-S on the WFC overrides it", Type: ftLookup, Col: 3, Row: 1, Width: 20,
			Get: func() string {
				if th.SysopChatScreen == "" || strings.EqualFold(th.SysopChatScreen, "builtin") {
					return themeChatBuiltin
				}
				return th.SysopChatScreen
			},
			Set: func(val string) error {
				if val == themeChatBuiltin {
					val = ""
				}
				th.SysopChatScreen = val
				return nil
			},
			LookupItems: func() []LookupItem {
				items := []LookupItem{
					{Value: themeChatBuiltin, Display: themeChatBuiltin + " - sysop above, caller below"},
					{Value: themeChatRandom, Display: themeChatRandom + " - a different screen each chat"},
				}
				for _, sc := range screens() {
					items = append(items, LookupItem{Value: sc.Name, Display: sc.label()})
				}
				return items
			},
		},
		boardColorField("Sysop Color", "Sysop's text where the chat screen sets no colour", 3, config.RoleChatSysop, th, board),
		boardColorField("Caller Color", "Caller's text where the chat screen sets no colour", 4, config.RoleChatUser, th, board),
	}
}

// themeChatScreen is one of the menu set's chat screens: its name, and the
// SAUCE title and author of its art, if any.
type themeChatScreen struct {
	Name, Title, Author string
}

// label is how the picker lists the screen: "NAME - Title by Author", as
// much of it as the screen's art says.
func (sc themeChatScreen) label() string {
	s := sc.Name
	if sc.Title != "" {
		s += " - " + sc.Title
	}
	if sc.Author != "" {
		s += " by " + sc.Author
	}
	return s
}

// themeChatScreens lists the menu set's chat screens: the stems of the .json
// files in its cfg directory, overlay included, as menu.ChatScreens does for
// the BBS. Each carries the SAUCE of its art, ansi/NAME.ANS or the file the
// layout's "art" names. A screen whose files cannot be read is still listed,
// by name; the doctor reports it.
func themeChatScreens(menuSetPath string) []themeChatScreen {
	set := menuset.FromPath(menuSetPath)
	entries, err := set.ReadDir("cfg")
	if err != nil {
		return nil
	}
	var screens []themeChatScreen
	for _, e := range entries {
		if !strings.EqualFold(filepath.Ext(e.Name), ".json") {
			continue
		}
		sc := themeChatScreen{Name: strings.TrimSuffix(e.Name, filepath.Ext(e.Name))}
		var layout struct {
			Art string `json:"art"`
		}
		if data, err := os.ReadFile(e.Path); err == nil {
			_ = json.Unmarshal(data, &layout) // a broken layout keeps the default art name
		}
		var artPath string
		if layout.Art != "" {
			artPath, err = set.Resolve("ansi", layout.Art)
		} else {
			artPath, err = set.ResolveFirst("ansi", sc.Name+".ANS", sc.Name+".ans")
		}
		if err == nil {
			if art, err := os.ReadFile(artPath); err == nil {
				if sauce, ok := ansi.ReadSAUCE(art); ok {
					sc.Title, sc.Author = sauce.Title, sauce.Author
				}
			}
		}
		screens = append(screens, sc)
	}
	return screens
}

// themeFieldsToasts edits the theme's toast settings: the colours a toast
// fades through when the board has fading on (System Setup -> Toasts).
func themeFieldsToasts(th *config.ThemeConfig, board *config.ThemeColors) []fieldDef {
	// The ramp is a board override like the colours: config.json's
	// theme.colors.toastFadeColors, over the theme's.
	current := func() string { return config.FormatToastFadeColors(th.WithBoardColors(*board).FadeColors()) }
	return []fieldDef{
		{
			Label: "Fade Colors", Help: "Colours a toast fades in through, and out through in reverse, when Toast Fade is on; Enter edits them, D uses the theme's",
			Type: ftColorRamp, Col: 3, Row: 1, Width: 4 * config.MaxToastFadeSteps,
			Get: current,
			Set: func(val string) error {
				colors, ok := config.ParseToastFadeColors(val)
				if !ok {
					return fmt.Errorf("1 to %d colours 0-15", config.MaxToastFadeSteps)
				}
				val = config.FormatToastFadeColors(colors)
				if val == current() && board.ToastFadeColors == "" {
					return nil // the ramp already shown is not an override
				}
				board.ToastFadeColors = val
				return nil
			},
			Source: func() string {
				switch {
				case board.ToastFadeColors != "":
					return config.SourceBoard.String()
				case th.ToastFadeColors != "":
					return config.SourceTheme.String()
				}
				return config.SourceBuiltin.String()
			},
			Inherit: func() bool {
				had := board.ToastFadeColors != ""
				board.ToastFadeColors = ""
				return had
			},
		},
	}
}
