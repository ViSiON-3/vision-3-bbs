package scripting

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dop251/goja"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// TestAnsiDisplayLookupOrder checks v3.ansi.display resolves art from the
// working dir, then the menu overlay, then the shipped menus/v3 ansi and
// templates dirs, and that display expands pipe codes while displayRaw
// does not.
func TestAnsiDisplayLookupOrder(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.writeFile("scripts/local.ans", "LOCAL|09")
	h.writeFile("menus/v3/ansi/shipped.ans", "SHIPPED")
	h.writeFile("menus/v3/ansi/both.ans", "BASE")
	h.writeFile("menus.d/v3/ansi/both.ans", "OVERLAY")
	h.writeFile("menus/v3/templates/tmpl.ans", "TEMPLATE")

	tests := []struct{ src, want string }{
		{`v3.ansi.display("local.ans")`, "LOCAL" + pipe("|09")},
		{`v3.ansi.displayRaw("local.ans")`, "LOCAL|09"},
		{`v3.ansi.display("shipped.ans")`, "SHIPPED"},
		{`v3.ansi.display("both.ans")`, "OVERLAY"},
		{`v3.ansi.display("tmpl.ans")`, "TEMPLATE"},
		{`v3.ansi.display("missing.ans")`, ""},
		{`v3.ansi.display()`, ""},
		{`v3.ansi.displayRaw()`, ""},
		{`v3.ansi.displayRaw("missing.ans")`, ""},
	}
	for _, tt := range tests {
		h.resetOutput()
		h.mustRun(tt.src)
		if got := h.output(); got != tt.want {
			t.Errorf("%s wrote %q, want %q", tt.src, got, tt.want)
		}
	}
}

// TestAnsiDisplayRejectsEscapes: traversal, absolute paths and symlinks that
// leave the search directories display nothing.
func TestAnsiDisplayRejectsEscapes(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	secret := h.writeFile("secret.ans", "SECRET")
	if err := os.Symlink(secret, filepath.Join(h.scriptsDir, "link.ans")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(h.root, "menus", "v3", "ansi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(h.root, "menus", "v3", "ansi", "menulink.ans")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../secret.ans", "..", secret, "link.ans", "menulink.ans"} {
		h.resetOutput()
		h.mustRun(`v3.ansi.display(` + jsQuote(name) + `)`)
		if got := h.output(); strings.Contains(got, "SECRET") {
			t.Errorf("display(%q) leaked %q", name, got)
		}
	}
}

// TestAnsiDisplayUnreadableOverlayStops: a stat error other than not-exist
// in a search dir aborts the lookup instead of falling through.
func TestAnsiDisplayUnreadableOverlayStops(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs unix directory permissions enforced")
	}
	h := newHarness(t, harnessOpts{})
	h.writeFile("menus/v3/ansi/art.ans", "BASE")
	overlay := filepath.Join(h.root, "menus.d", "v3", "ansi")
	h.writeFile("menus.d/v3/ansi/other.ans", "x")
	if err := os.Chmod(overlay, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(overlay, 0o755) })
	h.mustRun(`v3.ansi.display("art.ans")`)
	if got := h.output(); got != "" {
		t.Errorf("display through unreadable overlay wrote %q, want nothing", got)
	}
}

// TestAnsiDisplayWideTerminalWraps: on a terminal wider than 80 columns
// long art lines get explicit breaks so they render as on 80 columns.
func TestAnsiDisplayWideTerminalWraps(t *testing.T) {
	h := newHarness(t, harnessOpts{session: func(sc *SessionContext) { sc.ScreenWidth = 132 }})
	h.writeFile("scripts/wide.ans", strings.Repeat("x", 100))
	h.mustRun(`v3.ansi.displayRaw("wide.ans")`)
	if got := h.output(); !strings.Contains(got, strings.Repeat("x", 80)+"\r\n") {
		t.Errorf("wide art not wrapped at 80: %q", got)
	}
	// ArtWidth, when set, takes precedence over ScreenWidth.
	h2 := newHarness(t, harnessOpts{session: func(sc *SessionContext) { sc.ScreenWidth = 132; sc.ArtWidth = 80 }})
	h2.writeFile("scripts/wide.ans", strings.Repeat("x", 100))
	h2.mustRun(`v3.ansi.displayRaw("wide.ans")`)
	if got := h2.output(); got != strings.Repeat("x", 100) {
		t.Errorf("ArtWidth=80 output altered: %q", got)
	}
}

// TestFindBBSRoot walks up to the nearest dir holding "menus".
func TestFindBBSRoot(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(filepath.Join(root, "menus"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findBBSRoot(deep); got != root {
		t.Errorf("findBBSRoot = %q, want %q", got, root)
	}
	if got := pathUnderBase(root, filepath.Join(root, "nope")); got != "" {
		t.Errorf("pathUnderBase(missing) = %q, want empty", got)
	}
}

// jsQuote renders s as a JS string literal.
func jsQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// TestAnsiRender: v3.ansi.render draws art as the message reader does and
// returns its rows; the art stops at Ctrl-Z, and width and rows are
// honoured and bounded.
func TestAnsiRender(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	rows := func(expr string) []string {
		t.Helper()
		var out []string
		if err := h.eng.vm.ExportTo(h.eval(expr), &out); err != nil {
			t.Fatal(err)
		}
		for i, l := range out {
			out[i] = ansi.StripAnsi(l)
		}
		return out
	}
	// Cursor right, saved position, and a SAUCE tail after Ctrl-Z.
	got := rows(`v3.ansi.render("ab\x1b[3Cc\r\n\x1b[sxy\x1b[uZ\x1aSAUCE00junk")`)
	if len(got) != 2 || got[0] != "ab   c" || got[1] != "Zy" {
		t.Errorf("render = %q", got)
	}
	// Width wraps; rows cuts.
	if got := rows(`v3.ansi.render("abcdef", {width: 4})`); len(got) != 2 || got[0] != "abcd" || got[1] != "ef" {
		t.Errorf("width 4 = %q", got)
	}
	if got := rows(`v3.ansi.render("1\r\n2\r\n3\r\n4", {rows: 2})`); len(got) != 2 {
		t.Errorf("rows 2 = %q", got)
	}
	if got := rows(`v3.ansi.render()`); len(got) != 0 {
		t.Errorf("no argument = %q", got)
	}
	w, r := renderOptionsFor(t, h, `({width: 9999, rows: 99999})`)
	if w != maxRenderWidth || w*r > maxRenderCells {
		t.Errorf("bounds: width %d rows %d", w, r)
	}
}

// renderOptionsFor runs renderOptions on a JS options object.
func renderOptionsFor(t *testing.T, h *scriptHarness, opts string) (int, int) {
	t.Helper()
	return renderOptions(goja.FunctionCall{Arguments: []goja.Value{h.eng.vm.ToValue(""), h.eval(opts)}})
}

// TestAnsiSauce reads a SAUCE record from text as v3.file.read returns it,
// CP437 high bytes included, and gives null without one.
func TestAnsiSauce(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	rec := make([]byte, 128)
	copy(rec, "SAUCE00")
	copy(rec[7:], "Caf\x82 Night")
	copy(rec[42:], "artist")
	copy(rec[62:], "group")
	h.eng.vm.Set("art", cp437Text(append([]byte("art\x1a"), rec...)))
	v := h.eval(`JSON.stringify(v3.ansi.sauce(art))`).String()
	if v != `{"title":"Café Night","author":"artist","group":"group"}` {
		t.Errorf("sauce = %s", v)
	}
	if v := h.eval(`v3.ansi.sauce("no record")`); !goja.IsNull(v) {
		t.Errorf("no record: %v", v)
	}
}

// TestAnsiGlyphs: the control bytes CP437 draws as glyphs reach a UTF-8
// caller as those glyphs, from v3.ansi.glyphs and in v3.ansi.render's rows,
// and a CP437 caller as the bytes themselves, which it draws. Tab, CR, LF
// and escape sequences are left alone.
func TestAnsiGlyphs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mode  ansi.OutputMode
		glyph string
	}{
		{"utf8", ansi.OutputModeUTF8, "☺♥▲"},
		{"cp437", ansi.OutputModeCP437, "\x01\x03\x1e"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, harnessOpts{session: func(s *SessionContext) { s.OutputMode = tc.mode }})
			if got := h.eval(`v3.ansi.glyphs("a\x01\x03\x1eb\tc\r\n\x1b[1m")`).String(); got != "a"+tc.glyph+"b\tc\r\n\x1b[1m" {
				t.Errorf("glyphs = %q", got)
			}
			var rows []string
			if err := h.eng.vm.ExportTo(h.eval(`v3.ansi.render("\x1b[31m\x01\x03\x1e!")`), &rows); err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || ansi.StripAnsi(rows[0]) != tc.glyph+"!" {
				t.Errorf("render = %q", rows)
			}
		})
	}
	h := newHarness(t, harnessOpts{})
	if got := h.eval(`v3.ansi.glyphs()`).String(); got != "" {
		t.Errorf("no argument = %q", got)
	}
}
