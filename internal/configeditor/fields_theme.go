package configeditor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
	"github.com/ViSiON-3/vision-3-bbs/internal/uitext"
)

// Field definitions for the Theme inner menu: the theme the board runs,
// the board's colours over it (config.json theme.colors), the screens
// whose own BAR file overrides them, and the theme's chat screen, which is
// saved to the theme's overlay (menus.d/<theme>/theme.json) so an upgrade
// of the shipped menus keeps the sysop's choice.

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
		{Label: "Screen Overrides", Build: func(m *Model) []fieldDef {
			if m.configs.barInherit == nil {
				m.configs.barInherit = map[string]bool{}
			}
			return themeFieldsScreens(themeScreenBars(m.configs.themeSet), &m.configs.Theme, &m.configs.Server.Theme.Colors, m.configs.barInherit)
		}},
		{Label: "Split-Screen Chat", Build: func(m *Model) []fieldDef {
			return themeFieldsChat(&m.configs.Theme, &m.configs.Server.Theme.Colors, func() []themeChatScreen { return themeChatScreens(m.configs.themeSet) })
		}},
		{Label: "Toasts", Build: func(m *Model) []fieldDef {
			return themeFieldsToasts(&m.configs.Theme, &m.configs.Server.Theme.Colors)
		}},
		{Label: "Caller Colors", Build: func(m *Model) []fieldDef {
			return themeFieldsCaller(&m.configs.Theme, &m.configs.Server.Theme.Colors)
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

// themeFieldsCaller is the board's default for each caller colour slot,
// |C1 to |C7 (#426). A caller who picks their own in User Konfig sees
// theirs instead, where a screen uses the code.
func themeFieldsCaller(th *config.ThemeConfig, board *config.ThemeColors) []fieldDef {
	return []fieldDef{
		boardColorField("Prompt (|C1)", "A prompt's question", 1, config.RoleCallerPrompt, th, board),
		boardColorField("Input (|C2)", "What a caller types at a prompt", 2, config.RoleCallerInput, th, board),
		boardColorField("Text (|C3)", "Plain text", 3, config.RoleCallerText, th, board),
		boardColorField("Stat (|C4)", "A value picked out of the text, such as a name or a number", 4, config.RoleCallerStat, th, board),
		boardColorField("Text 2 (|C5)", "Secondary text, such as brackets and separators", 5, config.RoleCallerText2, th, board),
		boardColorField("Stat 2 (|C6)", "A second kind of value, such as an area name", 6, config.RoleCallerStat2, th, board),
		boardColorField("Bar (|C7)", "A highlighted choice; unset, the lightbar Selected colour", 7, config.RoleCallerBar, th, board),
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

// screenBar is a per-screen highlight override the editor found: a
// *HI.BAR in the menu set, the colour it sets, and the layer it is in.
type screenBar struct {
	menuset.ScreenBar
	Highlight int
	Overlay   bool // in menus.d rather than the shipped set
}

// themeScreenBars lists the *HI.BAR overrides present in the menu set, in
// menuset.ScreenBars order. A screen with no BAR, or with an inherit
// marker, has no override and is not listed.
func themeScreenBars(menuSetPath string) []screenBar {
	set := menuset.FromPath(menuSetPath)
	var bars []screenBar
	for _, sb := range menuset.ScreenBars {
		path, layer, ok, err := set.Locate("bar", sb.Name+".BAR")
		if err != nil || !ok {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		hi, _, ok := menuset.BarHighlight(data)
		if !ok {
			continue
		}
		bars = append(bars, screenBar{ScreenBar: sb, Highlight: hi, Overlay: layer == menuset.LayerOverlay})
	}
	return bars
}

// themeFieldsScreens is the Screen Overrides page (#1079): the screens
// whose own BAR file sets their highlight, over the shared Selected
// colour. Each is a Y/N: Y makes the screen inherit the shared colours,
// by writing an inherit marker to the overlay when the editor saves
// (inherit holds the screens chosen). The BARs themselves are edited in
// the menu editor, not here.
func themeFieldsScreens(bars []screenBar, th *config.ThemeConfig, board *config.ThemeColors, inherit map[string]bool) []fieldDef {
	if len(bars) == 0 {
		return []fieldDef{{
			Label: "Overrides", Help: "A screen's *HI.BAR file would be listed here; none is present", Type: ftDisplay, Col: 3, Row: 1, Width: 48,
			Get: func() string { return "none: every lightbar inherits the shared colours" },
		}}
	}
	var fields []fieldDef
	for i, b := range bars {
		layer := "shipped"
		if b.Overlay {
			layer = "menus.d"
		}
		fields = append(fields, fieldDef{
			Label: b.Screen,
			Help:  fmt.Sprintf("%s.BAR (%s) sets this screen's highlight, over the shared Selected colour; Y = inherit shared colours", b.Name, layer),
			Type:  ftYesNo, Col: 3, Row: i + 1, Width: 1,
			Get: func() string { return uitext.BoolToYN(inherit[b.Name]) },
			Set: func(val string) error {
				if uitext.YNToBool(val) {
					inherit[b.Name] = true
				} else {
					delete(inherit, b.Name)
				}
				return nil
			},
			Source: func() string {
				if inherit[b.Name] {
					v, _ := config.ResolveColor(config.RoleLightbarSelected, *th, *board)
					return fmt.Sprintf("will inherit colour %d when saved", v)
				}
				return fmt.Sprintf("%s, colour %d (%s)", config.SourceScreen, b.Highlight, layer)
			},
		})
	}
	return fields
}

// saveBarInherit writes an inherit marker to the overlay for each screen
// chosen on the Screen Overrides page, so the screen takes the shared
// colours even where the theme ships a BAR for it. Markers written are
// dropped from inherit.
func saveBarInherit(menuSetPath string, inherit map[string]bool) error {
	set := menuset.FromPath(menuSetPath)
	for _, sb := range menuset.ScreenBars {
		if !inherit[sb.Name] {
			continue
		}
		path := set.WritePath("bar", sb.Name+".BAR")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := atomicfile.WriteFile(path, []byte(menuset.BarInheritFile), 0o644); err != nil {
			return err
		}
		delete(inherit, sb.Name)
	}
	return nil
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
