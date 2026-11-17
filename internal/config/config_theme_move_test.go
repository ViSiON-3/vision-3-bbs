package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// moveBoard writes a shipped theme.json and, unless overlay is "", an
// overlay one, and returns the menu set path.
func moveBoard(t *testing.T, shipped, overlay string) string {
	t.Helper()
	root := t.TempDir()
	set := filepath.Join(root, "menus", "v3")
	if err := os.MkdirAll(set, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(set, "theme.json"), []byte(shipped), 0o644); err != nil {
		t.Fatal(err)
	}
	if overlay != "" {
		dir := filepath.Join(root, "menus.d", "v3")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "theme.json"), []byte(overlay), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return set
}

// drawn is every colour theme.colors can hold as the board draws it.
func drawn(t *testing.T, set string, board ThemeColors) map[string]any {
	t.Helper()
	theme, err := LoadThemeConfig(set)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	for _, role := range BoardColorRoles {
		out[string(role)], _ = ResolveColor(role, theme, board)
	}
	out["toastFadeColors"] = FormatToastFadeColors(theme.WithBoardColors(board).FadeColors())
	out["sysopChatScreen"] = theme.SysopChatScreen
	return out
}

func moveAndApply(t *testing.T, set string, board ThemeColors) ThemeColorMove {
	t.Helper()
	move, err := PlanThemeColorMove(set, board)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyThemeColorMove(set, move); err != nil {
		t.Fatal(err)
	}
	return move
}

const moveShipped = `{"yesNoHighlightColor": 31, "yesNoRegularColor": 13, "lightbarSelected": 31,
	"lightbarNormal": 13, "chatSysopColor": 11, "chatUserColor": 10, "autocorrectHighlightColor": 14,
	"toastFadeColors": "8,7,15", "sysopChatScreen": "V3CHAT1"}`

// After the move the board draws every colour as before; the overlay's own
// colours are in theme.colors, and what is not a colour stays put.
func TestThemeColorMoveKeepsTheBoardsLook(t *testing.T) {
	cases := map[string]string{
		"colours and a chat screen": `{"chatUserColor": 12, "lightbarHint": 3, "toastFadeColors": "1,2", "sysopChatScreen": "MYCHAT"}`,
		// From before the roles: the selected bar follows the overlay's
		// Yes/No colour, not the shipped role.
		"old overlay":     `{"yesNoHighlightColor": 79, "yesNoRegularColor": 5, "chatSysopColor": 11}`,
		"role set to 0":   `{"lightbarBorder": 0}`,
		"unset role":      `{"lightbarSelected": null}`,
		"unknown key":     `{"chatUserColor": 2, "futureKey": [1, 2]}`,
		"bad fade ramp":   `{"toastFadeColors": "nonsense"}`,
		"only repetition": `{"chatSysopColor": 11, "yesNoRegularColor": 13}`,
	}
	for name, overlay := range cases {
		t.Run(name, func(t *testing.T) {
			set := moveBoard(t, moveShipped, overlay)
			want := drawn(t, set, ThemeColors{})
			move := moveAndApply(t, set, ThemeColors{})
			if got := drawn(t, set, move.Board); !mapsEqual(got, want) {
				t.Errorf("after the move\n got %v\nwant %v\nmove %+v", got, want, move)
			}
			// A second move has nothing left to do.
			again, err := PlanThemeColorMove(set, move.Board)
			if err != nil || !again.Empty() {
				t.Errorf("second plan = %+v, %v; want nothing", again, err)
			}
		})
	}
}

func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestThemeColorMoveResult(t *testing.T) {
	set := moveBoard(t, moveShipped, `{"chatUserColor": 12, "chatSysopColor": 11, "yesNoHighlightColor": 79, "sysopChatScreen": "MYCHAT"}`)
	move := moveAndApply(t, set, ThemeColors{})

	// lightbarSelected is set by the shipped theme, so it no longer follows
	// the Yes/No colour once that is a board colour: it is written too.
	if want := []string{"yesNoHighlightColor", "lightbarSelected", "chatUserColor"}; !slices.Equal(move.Moved, want) {
		t.Errorf("moved %v, want %v", move.Moved, want)
	}
	if want := []string{"chatSysopColor", "chatUserColor", "yesNoHighlightColor"}; !slices.Equal(move.Removed, want) {
		t.Errorf("removed %v, want %v", move.Removed, want)
	}
	if g := move.Board.ChatUserColor; !g.Set || g.Value != 12 {
		t.Errorf("chatUserColor = %+v", g)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(set)), "menus.d", "v3", "theme.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "{\n    \"sysopChatScreen\": \"MYCHAT\"\n}\n" {
		t.Errorf("overlay left = %q; want only sysopChatScreen", got)
	}
}

// A colour theme.colors already has is never overwritten, and the
// overlay's own value for it stays where it is.
func TestThemeColorMoveLeavesBoardColoursAlone(t *testing.T) {
	set := moveBoard(t, moveShipped, `{"chatUserColor": 12, "lightbarHint": 3}`)
	board := ThemeColors{ChatUserColor: SetColor(9)}
	want := drawn(t, set, board)
	move := moveAndApply(t, set, board)
	if g := move.Board.ChatUserColor; g.Value != 9 {
		t.Errorf("chatUserColor = %+v; want the board's 9 kept", g)
	}
	if !slices.Equal(move.Moved, []string{"lightbarHint"}) || !slices.Equal(move.Kept, []string{"chatUserColor"}) {
		t.Errorf("moved %v, kept %v", move.Moved, move.Kept)
	}
	if got := drawn(t, set, move.Board); !mapsEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	theme, _ := LoadThemeConfig(set)
	if theme.ChatUserColor != 12 {
		t.Errorf("the overlay's chatUserColor went: %d", theme.ChatUserColor)
	}
}

func TestThemeColorMoveWithoutOverlay(t *testing.T) {
	set := moveBoard(t, moveShipped, "")
	move, err := PlanThemeColorMove(set, ThemeColors{})
	if err != nil || !move.Empty() {
		t.Errorf("plan = %+v, %v; want nothing", move, err)
	}
	if err := ApplyThemeColorMove(set, move); err != nil {
		t.Error(err)
	}
	set = moveBoard(t, moveShipped, `{`)
	if _, err := PlanThemeColorMove(set, ThemeColors{}); err == nil {
		t.Error("an unreadable overlay planned a move")
	}
}
