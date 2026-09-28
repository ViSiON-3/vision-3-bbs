package scripting

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
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
