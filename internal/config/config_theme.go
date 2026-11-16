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
// menus.d/<set>/theme.json is merged over it key by key: a key the overlay
// leaves out, or sets to null, keeps the shipped value. Either file may be
// missing.
func LoadThemeConfig(menuSetPath string) (ThemeConfig, error) {
	set := menuset.FromPath(menuSetPath)
	defaults := builtinTheme()
	slog.Info("loading theme configuration", "path", set.Path(menuset.LayerBase, "theme.json"))

	doc, err := themeLayers(set)
	if err != nil {
		slog.Error("failed to load theme, using default theme settings", "error", err)
		return defaults, err
	}
	theme, err := decodeTheme(defaults, doc)
	if err != nil {
		slog.Error("failed to parse theme JSON, using default theme settings", "error", err)
		return defaults, err
	}
	slog.Info("loaded theme configuration", "path", set.Path(menuset.LayerBase, "theme.json"))
	return theme, nil
}

// SaveThemeConfig writes theme to the menu set's overlay theme.json
// (menus.d/<set>/theme.json), so an upgrade of the shipped set leaves it
// alone. Only the keys whose value differs from what the shipped theme.json
// (or, without one, the built-in defaults) gives are written, so later
// changes to the shipped theme still reach the board. Keys ThemeConfig does
// not know are kept. An overlay left with nothing to say is removed.
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
		maps.Copy(doc, known)
		return writeThemeDoc(path, doc)
	}

	shipped, err := readThemeDoc(set.Path(menuset.LayerBase, "theme.json"))
	if err != nil {
		return err
	}
	inheritedTheme, err := decodeTheme(builtinTheme(), shipped)
	if err != nil {
		return err
	}
	inherited, err := themeDoc(inheritedTheme)
	if err != nil {
		return err
	}
	// Unknown keys are compared with the shipped file as it is.
	for k, v := range shipped {
		if _, ok := inherited[k]; !ok {
			inherited[k] = v
		}
	}

	path := set.Path(menuset.LayerOverlay, "theme.json")
	doc, err := readThemeDoc(path)
	if err != nil {
		return err
	}
	if doc == nil {
		doc = map[string]json.RawMessage{}
	}
	for _, key := range themeKeys() {
		delete(doc, key) // a role left unset clears its override
	}
	maps.Copy(doc, known)
	for k, v := range doc {
		if base, ok := inherited[k]; ok && sameJSON(base, v) {
			delete(doc, k)
		}
	}
	if len(doc) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeThemeDoc(path, doc)
}

// RedundantThemeOverlayKeys lists, sorted, the keys of a menu set's overlay
// theme.json whose value only repeats what the shipped theme gives, and so
// can be removed without changing anything. null values count, since they
// already inherit.
func RedundantThemeOverlayKeys(menuSetPath string) ([]string, error) {
	set := menuset.FromPath(menuSetPath)
	if !set.HasOverlay() {
		return nil, nil
	}
	overlay, err := readThemeDoc(set.Path(menuset.LayerOverlay, "theme.json"))
	if err != nil || len(overlay) == 0 {
		return nil, err
	}
	shipped, err := readThemeDoc(set.Path(menuset.LayerBase, "theme.json"))
	if err != nil {
		return nil, err
	}
	inheritedTheme, err := decodeTheme(builtinTheme(), shipped)
	if err != nil {
		return nil, err
	}
	inherited, err := themeDoc(inheritedTheme)
	if err != nil {
		return nil, err
	}
	for k, v := range shipped {
		if _, ok := inherited[k]; !ok {
			inherited[k] = v
		}
	}
	var keys []string
	for k, v := range overlay {
		if isJSONNull(v) {
			keys = append(keys, k)
			continue
		}
		if base, ok := inherited[k]; ok && sameJSON(base, v) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
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
		return os.Remove(path)
	}
	return writeThemeDoc(path, doc)
}

// themeLayers reads the shipped theme.json and merges the overlay's over
// it. Overlay keys set to null are dropped, so they inherit.
func themeLayers(set menuset.Set) (map[string]json.RawMessage, error) {
	doc, err := readThemeDoc(set.Path(menuset.LayerBase, "theme.json"))
	if err != nil {
		return nil, err
	}
	if doc == nil {
		doc = map[string]json.RawMessage{}
	}
	if !set.HasOverlay() {
		return doc, nil
	}
	overlay, err := readThemeDoc(set.Path(menuset.LayerOverlay, "theme.json"))
	if err != nil {
		return nil, err
	}
	for k, v := range overlay {
		if !isJSONNull(v) {
			doc[k] = v
		}
	}
	return doc, nil
}

// readThemeDoc reads a theme.json as its top-level keys. A missing file is
// nil with no error.
func readThemeDoc(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read theme file %s: %w", path, err)
	}
	doc := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse theme JSON %s: %w", path, err)
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
