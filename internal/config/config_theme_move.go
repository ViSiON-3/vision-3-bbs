package config

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// ThemeColorMove is what moving the colours of a menu set's overlay
// theme.json into config.json's theme.colors does (#1093): the board's
// colours afterwards, and what is left of the overlay.
type ThemeColorMove struct {
	// Board is theme.colors after the move.
	Board ThemeColors
	// Moved lists, in BoardColorRoles order with toastFadeColors last, the
	// theme.colors keys the move sets.
	Moved []string
	// Removed lists, sorted, the overlay keys the move takes out: the
	// colours it moved, and keys that change nothing once they have gone.
	Removed []string
	// Kept lists, sorted, the overlay's colours left where they are because
	// theme.colors already sets them; the move never overwrites one.
	Kept []string
	// Overlay is the overlay theme.json afterwards; empty means the file
	// goes.
	Overlay map[string]json.RawMessage

	// from is the overlay the plan was made from, so a write can tell
	// that the file changed since (CheckThemeColorMove).
	from map[string]json.RawMessage
}

// Empty reports whether the move would change nothing.
func (m ThemeColorMove) Empty() bool {
	return len(m.Moved) == 0 && len(m.Removed) == 0
}

// boardColorKeys lists the theme.json keys theme.colors can hold.
func boardColorKeys() []string {
	keys := make([]string, 0, len(BoardColorRoles)+1)
	for _, role := range BoardColorRoles {
		keys = append(keys, string(role))
	}
	return append(keys, "toastFadeColors")
}

// boardSets reports whether theme.colors sets the theme.json key key.
func (c ThemeColors) boardSets(key string) bool {
	if key == "toastFadeColors" {
		_, ok := ParseToastFadeColors(c.ToastFadeColors)
		return ok
	}
	return c.Get(ColorRole(key)).Set
}

// PlanThemeColorMove works out how to move the colours of a menu set's
// overlay theme.json into board, the board's theme.colors, so the board
// draws every colour as it does now: each colour the overlay changes from
// the shipped theme becomes a theme.colors key, unless theme.colors already
// has that key, and the overlay keeps only what is not a colour
// (sysopChatScreen, keys this version does not know) and the colours
// theme.colors already overrides. Nothing is written; see
// ApplyThemeColorMove.
//
// Board colours apply whatever theme is selected, where an overlay's apply
// to its own theme only, so the move is the sysop's choice, never made on
// their behalf.
func PlanThemeColorMove(menuSetPath string, board ThemeColors) (ThemeColorMove, error) {
	move := ThemeColorMove{Board: board}
	set := menuset.FromPath(menuSetPath)
	if !set.HasOverlay() {
		return move, nil
	}
	shipped, overlay, err := readThemeLayers(set)
	if err != nil || len(overlay) == 0 {
		return move, err
	}
	load := func(o map[string]json.RawMessage) (ThemeConfig, error) {
		return decodeTheme(builtinTheme(), mergeThemeDocs(shipped, o))
	}
	before, err := load(overlay)
	if err != nil {
		return move, err
	}
	move.from = overlay

	doc := maps.Clone(overlay)
	for _, k := range boardColorKeys() {
		if _, ok := doc[k]; !ok {
			continue
		}
		if board.boardSets(k) {
			move.Kept = append(move.Kept, k)
			continue
		}
		delete(doc, k)
	}
	after, err := load(doc)
	if err != nil {
		return move, err
	}

	// The Yes/No roles come first in BoardColorRoles, so a lightbar role
	// that follows its Yes/No colour is only written when it does not
	// follow the moved Yes/No colour too.
	for _, role := range BoardColorRoles {
		want, _ := ResolveColor(role, before, board)
		if got, _ := ResolveColor(role, after, move.Board); got != want {
			// theme.json reads its older colours as plain numbers, so an
			// overlay can hold one config.json would refuse to load.
			if want < 0 || want > 255 {
				return ThemeColorMove{Board: board}, fmt.Errorf("%s in %s is %d, not a colour from 0 to 255; correct it first", role, set.Path(menuset.LayerOverlay, "theme.json"), want)
			}
			move.Board.Override(role, want)
			move.Moved = append(move.Moved, string(role))
		}
	}
	if want := before.WithBoardColors(board).FadeColors(); !slices.Equal(after.WithBoardColors(move.Board).FadeColors(), want) {
		move.Board.ToastFadeColors = FormatToastFadeColors(want)
		move.Moved = append(move.Moved, "toastFadeColors")
	}
	if !sameBoardColors(before, board, after, move.Board) {
		return ThemeColorMove{Board: board}, fmt.Errorf("moving the colours of %s would change how the board looks", set.Path(menuset.LayerOverlay, "theme.json"))
	}

	// What is left may now repeat the shipped theme. Removing it keeps the
	// theme as it loads, but a role left to fall back follows a Yes/No
	// colour theme.colors sets where one set to the same colour does not,
	// so check again with the board's colours.
	if trimmed, _, err := minimizeThemeOverlay(shipped, doc); err == nil {
		if t, err := load(trimmed); err == nil && sameBoardColors(before, board, t, move.Board) {
			doc = trimmed
		}
	}
	for k := range overlay {
		if _, ok := doc[k]; !ok {
			move.Removed = append(move.Removed, k)
		}
	}
	sort.Strings(move.Removed)
	move.Kept = slices.DeleteFunc(move.Kept, func(k string) bool { _, ok := doc[k]; return !ok })
	sort.Strings(move.Kept)
	move.Overlay = doc
	return move, nil
}

// sameBoardColors reports whether a board running theme a with overrides
// ab draws every colour theme.colors can hold as one running b with bb.
func sameBoardColors(a ThemeConfig, ab ThemeColors, b ThemeConfig, bb ThemeColors) bool {
	for _, role := range BoardColorRoles {
		x, _ := ResolveColor(role, a, ab)
		y, _ := ResolveColor(role, b, bb)
		if x != y {
			return false
		}
	}
	return slices.Equal(a.WithBoardColors(ab).FadeColors(), b.WithBoardColors(bb).FadeColors())
}

// CheckThemeColorMove reports an error when the overlay theme.json is no
// longer what move was planned from, so a value changed since is not lost.
// The caller checks before writing move.Board to config.json.
func CheckThemeColorMove(menuSetPath string, move ThemeColorMove) error {
	set := menuset.FromPath(menuSetPath)
	if !set.HasOverlay() || move.Empty() {
		return nil
	}
	path := set.Path(menuset.LayerOverlay, "theme.json")
	now, err := readThemeDoc(path)
	if err != nil {
		return err
	}
	same := len(now) == len(move.from)
	for k, v := range now {
		if old, ok := move.from[k]; !ok || !sameJSON(old, v) {
			same = false
		}
	}
	if !same {
		return fmt.Errorf("%s changed while the move was planned; run it again", path)
	}
	return nil
}

// ApplyThemeColorMove writes what is left of the overlay theme.json after
// move, or removes the file when nothing is, unless the file changed since
// the plan. The caller writes move.Board to config.json first: board
// colours win over the theme, so the board looks the same if this step
// then fails.
func ApplyThemeColorMove(menuSetPath string, move ThemeColorMove) error {
	set := menuset.FromPath(menuSetPath)
	if !set.HasOverlay() || len(move.Removed) == 0 {
		return nil
	}
	if err := CheckThemeColorMove(menuSetPath, move); err != nil {
		return err
	}
	return TrimThemeOverlay(menuSetPath, move.Removed)
}
