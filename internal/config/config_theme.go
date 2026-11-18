package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// ThemeConfig holds theme-related settings, loaded per menu set.
// Colors are standard DOS color codes (0-255).
type ThemeConfig struct {
	// YesNoHighlightColor and YesNoRegularColor are the two answers of a
	// Yes/No prompt. They are also what the lightbar selected and normal
	// roles fall back to in a theme that sets neither.
	YesNoHighlightColor int `json:"yesNoHighlightColor"`
	YesNoRegularColor   int `json:"yesNoRegularColor"`
	// The lightbar roles (see ColorRole). Each is optional: read them
	// through Color, which fills in what the theme leaves out.
	LightbarSelected    OptColor `json:"lightbarSelected,omitzero"`
	LightbarNormal      OptColor `json:"lightbarNormal,omitzero"`
	LightbarCommand     OptColor `json:"lightbarCommand,omitzero"`
	LightbarHotkey      OptColor `json:"lightbarHotkey,omitzero"`
	LightbarHint        OptColor `json:"lightbarHint,omitzero"`
	LightbarBorder      OptColor `json:"lightbarBorder,omitzero"`
	LightbarDestructive OptColor `json:"lightbarDestructive,omitzero"`
	LightbarInactive    OptColor `json:"lightbarInactive,omitzero"`
	LightbarBracket     OptColor `json:"lightbarBracket,omitzero"`
	// The caller colour roles: the board's default for each of |C1 to
	// |C7 (see CallerColorRoles). Each is optional, like the lightbar
	// roles.
	CallerPrompt OptColor `json:"callerPrompt,omitzero"`
	CallerInput  OptColor `json:"callerInput,omitzero"`
	CallerText   OptColor `json:"callerText,omitzero"`
	CallerStat   OptColor `json:"callerStat,omitzero"`
	CallerText2  OptColor `json:"callerText2,omitzero"`
	CallerStat2  OptColor `json:"callerStat2,omitzero"`
	CallerBar    OptColor `json:"callerBar,omitzero"`

	ChatSysopColor int `json:"chatSysopColor"` // sysop pane in split-screen chat
	ChatUserColor  int `json:"chatUserColor"`  // caller pane in split-screen chat
	// AutocorrectHighlightColor is the colour the message editor draws a
	// word in just after autocorrect fixed it, while the fix is shown on the
	// prompt row.
	AutocorrectHighlightColor int `json:"autocorrectHighlightColor"`
	// SysopChatScreen names the chat screen split-screen chat opens with:
	// cfg/NAME.json and ansi/NAME.ANS in the menu set. Empty means the
	// built-in layout, and "random" one of the set's screens each chat. The
	// sysop can pick another from the WFC.
	SysopChatScreen string `json:"sysopChatScreen"`
	// ToastFadeColors is the colour ramp a toast fades in through, and out
	// through in reverse, when the board has toast fading on (#1002): DOS
	// foreground colours 0-15, comma-separated, in fade-in order, 1 to
	// MaxToastFadeSteps of them. Kept as text so the theme stays comparable
	// and merges as one value. Read it through FadeColors, which falls back
	// to the default ramp for a value it cannot use.
	ToastFadeColors string `json:"toastFadeColors"`
	// Add other theme elements here as needed (e.g., default menu colors)
}

// DefaultToastFadeColors is the ramp a theme without one fades through:
// dark gray, light gray, white.
const DefaultToastFadeColors = "8,7,15"

// MaxToastFadeSteps is the longest fade ramp.
const MaxToastFadeSteps = 8

// ParseToastFadeColors reads a fade ramp: 1 to MaxToastFadeSteps
// foreground colours 0-15, separated by commas or spaces. ok is false for
// anything else.
func ParseToastFadeColors(s string) (colors []int, ok bool) {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	if len(fields) == 0 || len(fields) > MaxToastFadeSteps {
		return nil, false
	}
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 || n > 15 {
			return nil, false
		}
		colors = append(colors, n)
	}
	return colors, true
}

// FormatToastFadeColors writes a ramp as ToastFadeColors stores it.
func FormatToastFadeColors(colors []int) string {
	parts := make([]string, len(colors))
	for i, c := range colors {
		parts[i] = strconv.Itoa(c)
	}
	return strings.Join(parts, ",")
}

// FadeColors returns the theme's fade ramp, or the default one when the
// theme's is missing or cannot be used.
func (t ThemeConfig) FadeColors() []int {
	if colors, ok := ParseToastFadeColors(t.ToastFadeColors); ok {
		return colors
	}
	colors, _ := ParseToastFadeColors(DefaultToastFadeColors)
	return colors
}

// builtinTheme is what a menu set without a theme.json gets, and what a
// theme.json fills its missing keys from.
func builtinTheme() ThemeConfig {
	return ThemeConfig{
		YesNoHighlightColor: 31, // Bright white on blue
		YesNoRegularColor:   13, // Light magenta
		ChatSysopColor:      11, // Light cyan
		ChatUserColor:       10, // Light green

		AutocorrectHighlightColor: 14, // Yellow
		ToastFadeColors:           DefaultToastFadeColors,
	}
}

// LoadThemeConfig loads the theme settings of a menu set. The shipped
// menus/<set>/theme.json fills in the built-in defaults, and the overlay's
// menus.d/<set>/theme.json is merged over it key by key (see mergeThemeDocs).
// Either file may be missing.
func LoadThemeConfig(menuSetPath string) (ThemeConfig, error) {
	path := menuset.FromPath(menuSetPath).Path(menuset.LayerBase, "theme.json")
	slog.Info("loading theme configuration", "path", path)
	theme, err := readThemeConfig(menuSetPath)
	if err != nil {
		slog.Error("failed to load theme, using default theme settings", "error", err)
		return theme, err
	}
	slog.Info("loaded theme configuration", "path", path)
	return theme, nil
}

// readThemeConfig is LoadThemeConfig without the logging, for a tool such
// as an editor that reads the theme while its screen is up. On an error it
// returns the built-in defaults.
// CheckThemeConfig reports whether the theme.json of the menu set at
// menuSetPath, with its overlay, loads, without logging: for a check
// made on every screen, such as Konfig's list of themes.
func CheckThemeConfig(menuSetPath string) error {
	_, err := readThemeConfig(menuSetPath)
	return err
}

func readThemeConfig(menuSetPath string) (ThemeConfig, error) {
	defaults := builtinTheme()
	shipped, overlay, err := readThemeLayers(menuset.FromPath(menuSetPath))
	if err != nil {
		return defaults, err
	}
	theme, err := decodeTheme(defaults, mergeThemeDocs(shipped, overlay))
	if err != nil {
		return defaults, fmt.Errorf("parsing theme JSON: %w", err)
	}
	return theme, nil
}

// SaveThemeConfig writes theme to the menu set's overlay theme.json
// (menus.d/<set>/theme.json), so an upgrade of the shipped set leaves it
// alone. Only the keys needed to turn what the shipped theme.json (or,
// without one, the built-in defaults) gives into theme are written, so
// later changes to the shipped theme still reach the board. Keys
// ThemeConfig does not know are kept unless they repeat the shipped file.
// An overlay left with nothing to say is removed.
//
// A set with no overlay is written in place, in full.
func SaveThemeConfig(menuSetPath string, theme ThemeConfig) error {
	set := menuset.FromPath(menuSetPath)
	known, err := themeDoc(theme)
	if err != nil {
		return err
	}
	if !set.HasOverlay() {
		path := set.Path(menuset.LayerBase, "theme.json")
		doc, err := readThemeDoc(path)
		if err != nil {
			return err
		}
		if doc == nil {
			doc = map[string]json.RawMessage{}
		}
		for _, k := range themeKeys() {
			delete(doc, k) // a role left unset is written by leaving it out
		}
		maps.Copy(doc, known)
		return writeThemeDoc(path, doc)
	}

	shipped, overlay, err := readThemeLayers(set)
	if err != nil {
		return err
	}
	// Start from an overlay that says everything: every known key, null for
	// a role left unset, and the overlay's unknown keys. Then take out what
	// changes nothing.
	doc := map[string]json.RawMessage{}
	for k, v := range overlay {
		if !slices.Contains(themeKeys(), k) {
			doc[k] = v
		}
	}
	for _, k := range themeKeys() {
		doc[k] = json.RawMessage("null")
	}
	maps.Copy(doc, known)
	doc, _, err = minimizeThemeOverlay(shipped, doc)
	if err != nil {
		return err
	}
	path := set.Path(menuset.LayerOverlay, "theme.json")
	if len(doc) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeThemeDoc(path, doc)
}

// RedundantThemeOverlayKeys lists, sorted, the keys of a menu set's overlay
// theme.json that can be removed without changing the theme: known keys
// whose removal leaves the loaded theme as it is, and unknown keys that
// repeat the shipped file. total is how many keys the overlay has.
func RedundantThemeOverlayKeys(menuSetPath string) (keys []string, total int, err error) {
	set := menuset.FromPath(menuSetPath)
	if !set.HasOverlay() {
		return nil, 0, nil
	}
	shipped, overlay, err := readThemeLayers(set)
	if err != nil || len(overlay) == 0 {
		return nil, 0, err
	}
	_, keys, err = minimizeThemeOverlay(shipped, overlay)
	return keys, len(overlay), err
}

// TrimThemeOverlay removes keys from a menu set's overlay theme.json, and
// the file itself if nothing is left. It rewrites nothing else.
func TrimThemeOverlay(menuSetPath string, keys []string) error {
	set := menuset.FromPath(menuSetPath)
	if !set.HasOverlay() || len(keys) == 0 {
		return nil
	}
	path := set.Path(menuset.LayerOverlay, "theme.json")
	doc, err := readThemeDoc(path)
	if err != nil || doc == nil {
		return err
	}
	for _, k := range keys {
		delete(doc, k)
	}
	if len(doc) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeThemeDoc(path, doc)
}

// readThemeLayers reads the shipped and overlay theme.json, either of
// which may be nil. Each is checked on its own, so an error names the file
// that holds the bad value.
func readThemeLayers(set menuset.Set) (shipped, overlay map[string]json.RawMessage, err error) {
	read := func(layer menuset.Layer) (map[string]json.RawMessage, error) {
		path := set.Path(layer, "theme.json")
		doc, err := readThemeDoc(path)
		if err != nil {
			return nil, err
		}
		if _, err := decodeTheme(builtinTheme(), doc); err != nil {
			return nil, fmt.Errorf("parse theme JSON %s: %w", path, err)
		}
		return doc, nil
	}
	if shipped, err = read(menuset.LayerBase); err != nil {
		return nil, nil, err
	}
	if set.HasOverlay() {
		if overlay, err = read(menuset.LayerOverlay); err != nil {
			return nil, nil, err
		}
	}
	return shipped, overlay, nil
}

// legacyRoleFallbacks pairs each lightbar role that falls back to a Yes/No
// colour with that colour (see ThemeConfig.ColorWithSource).
var legacyRoleFallbacks = [][2]string{
	{string(RoleLightbarSelected), string(RoleYesNoHighlight)},
	{string(RoleLightbarNormal), string(RoleYesNoRegular)},
}

// mergeThemeDocs applies the overlay's keys over the shipped ones. A key
// the overlay leaves out comes from the shipped file, with one exception
// that keeps an overlay from before the lightbar roles looking as it did:
// an overlay that sets a Yes/No colour and leaves out the role that falls
// back to it leaves that role unset, so it follows the overlay's colour
// rather than the shipped role. null in the overlay is the value null:
// it unsets a role, and leaves any other key as the shipped file has it.
func mergeThemeDocs(shipped, overlay map[string]json.RawMessage) map[string]json.RawMessage {
	doc := maps.Clone(shipped)
	if doc == nil {
		doc = map[string]json.RawMessage{}
	}
	maps.Copy(doc, overlay)
	for _, pair := range legacyRoleFallbacks {
		role, yesNo := pair[0], pair[1]
		_, hasRole := overlay[role]
		if v, hasYesNo := overlay[yesNo]; hasYesNo && !isJSONNull(v) && !hasRole {
			delete(doc, role)
		}
	}
	return doc
}

// minimizeThemeOverlay removes, one at a time in key order, each overlay
// key whose removal leaves the theme it loads as unchanged; an unknown key
// goes only when the shipped file has the same value. It returns what is
// left and the keys removed.
func minimizeThemeOverlay(shipped, overlay map[string]json.RawMessage) (map[string]json.RawMessage, []string, error) {
	load := func(o map[string]json.RawMessage) (ThemeConfig, error) {
		return decodeTheme(builtinTheme(), mergeThemeDocs(shipped, o))
	}
	want, err := load(overlay)
	if err != nil {
		return nil, nil, err
	}
	doc := maps.Clone(overlay)
	var removed []string
	// Removing one key can make another removable (a Yes/No colour that
	// held a role to its fallback, say), so go round until a pass removes
	// nothing.
	for again := true; again; {
		again = false
		for _, k := range slices.Sorted(maps.Keys(doc)) {
			if !slices.Contains(themeKeys(), k) {
				if base, ok := shipped[k]; ok && sameJSON(base, doc[k]) {
					delete(doc, k)
					removed = append(removed, k)
					again = true
				}
				continue
			}
			trial := maps.Clone(doc)
			delete(trial, k)
			if got, err := load(trial); err == nil && drawnTheme(got) == drawnTheme(want) {
				doc = trial
				removed = append(removed, k)
				again = true
			}
		}
	}
	sort.Strings(removed)
	return doc, removed, nil
}

// readThemeDoc reads a theme.json as its top-level keys, each key
// ThemeConfig knows spelt as its json tag (encoding/json matches field
// names without regard to case, so "ChatUserColor" sets chatUserColor; the
// layers must agree on the spelling to merge). A missing file is nil with
// no error; a link to a missing file is an error, never an absent file.
func readThemeDoc(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if _, linkErr := os.Lstat(path); linkErr == nil {
			return nil, fmt.Errorf("read theme file %s: %w", path, err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read theme file %s: %w", path, err)
	}
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse theme JSON %s: %w", path, err)
	}
	doc := make(map[string]json.RawMessage, len(raw))
	for _, k := range slices.Sorted(maps.Keys(raw)) {
		key := k
		for _, known := range themeKeys() {
			if strings.EqualFold(k, known) {
				key = known
				break
			}
		}
		if _, dup := doc[key]; dup && key != k {
			continue // the exact spelling wins over another case of it
		}
		doc[key] = raw[k]
	}
	return doc, nil
}

func writeThemeDoc(path string, doc map[string]json.RawMessage) error {
	out, err := json.MarshalIndent(doc, "", "    ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.WriteFile(path, append(out, '\n'), 0o644)
}

// decodeTheme applies doc's keys over base.
func decodeTheme(base ThemeConfig, doc map[string]json.RawMessage) (ThemeConfig, error) {
	if len(doc) == 0 {
		return base, nil
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return base, err
	}
	theme := base
	if err := json.Unmarshal(data, &theme); err != nil {
		return base, err
	}
	return theme, nil
}

// themeDoc is theme as top-level keys. Unset roles are left out.
func themeDoc(theme ThemeConfig) (map[string]json.RawMessage, error) {
	data, err := json.Marshal(theme)
	if err != nil {
		return nil, err
	}
	doc := map[string]json.RawMessage{}
	return doc, json.Unmarshal(data, &doc)
}

// drawnTheme is theme with each lightbar role set to the colour it draws
// in, so a role left to fall back and one set to the same colour compare
// equal.
func drawnTheme(theme ThemeConfig) ThemeConfig {
	drawn := func(role ColorRole) OptColor { return OptColor{Set: true, Value: theme.Color(role)} }
	theme.LightbarSelected = drawn(RoleLightbarSelected)
	theme.LightbarNormal = drawn(RoleLightbarNormal)
	theme.LightbarCommand = drawn(RoleLightbarCommand)
	theme.LightbarHotkey = drawn(RoleLightbarHotkey)
	theme.LightbarHint = drawn(RoleLightbarHint)
	theme.LightbarBorder = drawn(RoleLightbarBorder)
	theme.LightbarDestructive = drawn(RoleLightbarDestructive)
	theme.LightbarInactive = drawn(RoleLightbarInactive)
	theme.LightbarBracket = drawn(RoleLightbarBracket)
	return theme
}

// themeKeys lists the JSON keys ThemeConfig reads.
func themeKeys() []string {
	t := reflect.TypeFor[ThemeConfig]()
	keys := make([]string, 0, t.NumField())
	for f := range t.Fields() {
		if name, _, _ := strings.Cut(f.Tag.Get("json"), ","); name != "" && name != "-" {
			keys = append(keys, name)
		}
	}
	return keys
}

func sameJSON(a, b json.RawMessage) bool {
	var ca, cb bytes.Buffer
	if json.Compact(&ca, a) != nil || json.Compact(&cb, b) != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(ca.Bytes(), cb.Bytes())
}

func isJSONNull(v json.RawMessage) bool {
	return string(bytes.TrimSpace(v)) == "null"
}
