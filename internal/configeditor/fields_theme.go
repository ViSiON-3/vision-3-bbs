package configeditor

import (
	"encoding/json"
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
		{Label: "Prompt Colors", Build: func(m *Model) []fieldDef { return themeFieldsPrompts(&m.configs.Theme) }},
		{Label: "Split-Screen Chat", Build: func(m *Model) []fieldDef {
			return themeFieldsChat(&m.configs.Theme, func() []themeChatScreen { return themeChatScreens(m.configPath) })
		}},
	}
}

// themeColorField edits one DOS colour code.
func themeColorField(label, help string, row int, v *int) fieldDef {
	return fieldDef{
		Label: label, Help: help + " (DOS colour 0-255)", Type: ftInteger, Col: 3, Row: row, Width: 3, Min: 0, Max: 255,
		Get: func() string { return strconv.Itoa(*v) },
		Set: func(val string) error {
			n, err := strconv.Atoi(val)
			if err != nil {
				return err
			}
			*v = n
			return nil
		},
	}
}

func themeFieldsPrompts(th *config.ThemeConfig) []fieldDef {
	return []fieldDef{
		themeColorField("Yes/No Highlight", "The selected answer in Yes/No prompts", 1, &th.YesNoHighlightColor),
		themeColorField("Yes/No Regular", "The other answer in Yes/No prompts", 2, &th.YesNoRegularColor),
	}
}

// The Chat Screen picker's entries for the built-in layout and for random.
// theme.json stores the built-in layout as "".
const (
	themeChatBuiltin = "(built-in)"
	themeChatRandom  = "random"
)

func themeFieldsChat(th *config.ThemeConfig, screens func() []themeChatScreen) []fieldDef {
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
		themeColorField("Sysop Color", "Sysop's text where the chat screen sets no colour", 3, &th.ChatSysopColor),
		themeColorField("Caller Color", "Caller's text where the chat screen sets no colour", 4, &th.ChatUserColor),
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
func themeChatScreens(configPath string) []themeChatScreen {
	set := menuset.FromPath(menuSetPath(configPath))
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
