package scripting

import (
	"reflect"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

func TestMenuFieldRecordsPositions(t *testing.T) {
	view := &MenuView{Name: "KONFIG", Items: []MenuItemView{{Key: "H", Label: "Location"}}}
	h := newHarness(t, harnessOpts{providers: &Providers{Menu: view}})
	h.mustRun(`v3.menu.field("h", 40, 9, 20);
v3.menu.field("Q", 5, 6);
v3.menu.field("A", 0, 3);
v3.menu.field("B", 3);
v3.menu.field("", 1, 1);
v3.menu.field("C", 2, 2, -4);
v3.menu.promptRow(0);
v3.menu.promptRow(21);
v3.menu.promptRow(-3);`)
	want := map[string]FieldPos{"H": {X: 40, Y: 9, Width: 20}, "Q": {X: 5, Y: 6}, "C": {X: 2, Y: 2}}
	if !reflect.DeepEqual(view.Fields, want) {
		t.Fatalf("fields = %+v, want %+v", view.Fields, want)
	}
	if view.PromptRow != 21 {
		t.Errorf("prompt row = %d, want 21", view.PromptRow)
	}
}

func TestFitColumns(t *testing.T) {
	heart := "\u2764\ufe0f"
	for _, tc := range []struct {
		s     string
		width int
		utf8  bool
		want  string
	}{
		{"plain", 7, true, "plain  "},
		// A width below one fits nothing; it must not panic.
		{"abc", 0, true, ""},
		{"abc", -1, true, ""},
		{"abc", 0, false, ""},
		{"abc", -1, false, ""},
		{"toolong", 4, true, "tool"},
		{"界界", 3, true, "界 "},
		// A heart and its VS16 go together or not at all.
		{"ab" + heart, 3, true, "ab "},
		{"ab" + heart, 4, true, "ab" + heart},
		{"|13x", 4, true, "|13x"},
		{"界界", 3, false, "界界 "},
		{"e\u0301x", 2, false, "e\u0301"},
		{"e\u0301x", 1, false, "e"},
	} {
		if got := fitColumns(tc.s, tc.width, tc.utf8); got != tc.want {
			t.Errorf("fitColumns(%+q, %d, utf8 %v) = %+q, want %+q", tc.s, tc.width, tc.utf8, got, tc.want)
		}
	}
}

// A script that computes a negative width gets an empty fit, on either
// encoding, rather than stopping the session.
func TestFitColumnsNegativeWidthFromAScript(t *testing.T) {
	for _, mode := range []ansi.OutputMode{ansi.OutputModeCP437, ansi.OutputModeUTF8} {
		h := newHarness(t, harnessOpts{session: func(sc *SessionContext) { sc.OutputMode = mode }})
		if got := h.eval(`"[" + v3.util.fitColumns("abc", -1) + "][" + v3.util.fitColumns("abc", 0) + "]"`).String(); got != "[][]" {
			t.Errorf("mode %v: fitColumns = %q, want empty fits", mode, got)
		}
	}
}
