//go:build !windows

package menu

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

// themeScriptSrc prints its arguments and data/who.txt, draws on a canvas
// with data/pal.png as its palette, keeps a value in v3.data, and answers
// with the menu's first item.
const themeScriptSrc = `v3.console.print("[args " + v3.args.join(",") + "]");
v3.console.print("[who " + v3.fs.read("who.txt") + "]");
v3.gfx.canvas(1, 1, "pal.png");
v3.console.print("[palette]");
v3.data.set("ran", true);
v3.prompt.answer(v3.menu.items[0].key);`

// writeThemeFile writes content to elem under dir, making its directories.
func writeThemeFile(t *testing.T, content []byte, dir string, elem ...string) {
	t.Helper()
	p := filepath.Join(append([]string{dir}, elem...)...)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

// onePixelPNG is a 1x1 PNG, usable as a canvas palette.
func onePixelPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// writeTheme makes the menu set dir (or an overlay tree) hold
// scripts/mainmenu.js and the data it reads, with who.txt saying who.
func writeTheme(t *testing.T, dir, who string) {
	t.Helper()
	writeThemeFile(t, []byte(themeScriptSrc), dir, "scripts", "mainmenu.js")
	writeThemeFile(t, []byte(who), dir, "data", "who.txt")
	writeThemeFile(t, onePixelPNG(t), dir, "data", "pal.png")
}

// themeFixture is a string hook fixture whose menu set is the theme name
// under a fresh menus/ directory, returned with the theme's directory.
func themeFixture(t *testing.T, name string) (*stringHookFixture, string) {
	t.Helper()
	f := newStringHookFixture(t, "")
	dir := menuset.ThemeDir(t.TempDir(), name)
	f.e.MenuSetPath = dir
	return f, dir
}

var themeScriptCommands = []CommandRecord{{Keys: "W", Command: "RUN:WHO", Label: "Who"}}

func TestThemeScriptDrawsMenu(t *testing.T) {
	t.Run("two themes each run their own mainmenu.js with their own data", func(t *testing.T) {
		for _, theme := range []string{"alpha", "beta"} {
			f, dir := themeFixture(t, theme)
			writeTheme(t, dir, theme)
			_, line, act, handled, out := f.runMenuScript(t, &MenuRecord{Script: "mainmenu.js blue"}, themeScriptCommands)
			if !handled || act != loopFallthrough || line != "W" {
				t.Errorf("%s: handled=%v act=%v line=%q; output=%q", theme, handled, act, line, out)
			}
			for _, want := range []string{"[args blue]", "[who " + theme + "]", "[palette]"} {
				if !strings.Contains(out, want) {
					t.Errorf("%s: output lacks %q: %q", theme, want, out)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "data", "mainmenu.json")); err != nil {
				t.Errorf("%s: v3.data store not in the theme's data/: %v", theme, err)
			}
		}
	})

	t.Run("menu set's menuScript can name a theme script", func(t *testing.T) {
		f, dir := themeFixture(t, "alpha")
		writeTheme(t, dir, "alpha")
		f.e.SetTheme(config.ThemeConfig{MenuScript: "mainmenu.js"})
		_, line, _, handled, out := f.runMenuScript(t, &MenuRecord{}, themeScriptCommands)
		if !handled || line != "W" || !strings.Contains(out, "[who alpha]") {
			t.Errorf("handled=%v line=%q; output=%q", handled, line, out)
		}
	})

	t.Run("the suffix is matched without regard to case", func(t *testing.T) {
		f, dir := themeFixture(t, "alpha")
		writeThemeFile(t, []byte(themeScriptSrc), dir, "scripts", "MAIN.JS")
		writeThemeFile(t, []byte("alpha"), dir, "data", "who.txt")
		writeThemeFile(t, onePixelPNG(t), dir, "data", "pal.png")
		_, _, _, handled, out := f.runMenuScript(t, &MenuRecord{Script: "MAIN.JS"}, themeScriptCommands)
		if !handled || !strings.Contains(out, "[who alpha]") {
			t.Errorf("handled=%v; output=%q", handled, out)
		}
	})

	t.Run("overlay script runs with the overlay's data", func(t *testing.T) {
		f, dir := themeFixture(t, "alpha")
		writeTheme(t, dir, "shipped")
		overlay := menuset.OverlayFor(dir)
		writeTheme(t, overlay, "overlay")
		// The overlay's script says which it is, apart from the data it reads.
		writeThemeFile(t, []byte(`v3.console.print("[overlay script]");`+themeScriptSrc), overlay, "scripts", "mainmenu.js")
		_, _, _, handled, out := f.runMenuScript(t, &MenuRecord{Script: "mainmenu.js"}, themeScriptCommands)
		if !handled || !strings.Contains(out, "[overlay script]") || !strings.Contains(out, "[who overlay]") {
			t.Errorf("handled=%v; output=%q", handled, out)
		}
	})

	for name, script := range map[string]string{
		"missing theme script":     "nosuch.js",
		"name with a directory":    "sub/mainmenu.js",
		"name leaving scripts/":    "../mainmenu.js",
		"script outside the theme": "/etc/mainmenu.js",
	} {
		t.Run(name+" leaves the menu to its screen", func(t *testing.T) {
			f, dir := themeFixture(t, "alpha")
			writeTheme(t, dir, "alpha")
			writeThemeFile(t, []byte(themeScriptSrc), dir, "scripts", "sub", "mainmenu.js")
			writeThemeFile(t, []byte(themeScriptSrc), dir, "mainmenu.js")
			_, line, act, handled, out := f.runMenuScript(t, &MenuRecord{Script: script}, themeScriptCommands)
			if handled || act != loopFallthrough || line != "" || strings.Contains(out, "[args") {
				t.Errorf("handled=%v act=%v line=%q; output=%q", handled, act, line, out)
			}
		})
	}

	t.Run("a theme script has no run limit", func(t *testing.T) {
		// A menu waits on its caller for as long as they stay, as the
		// scripted menus' doors did with max_run_minutes -1; the caller's
		// idle timeout and time limit still apply.
		var got config.DoorConfig
		orig := v3ScriptRunLimit
		v3ScriptRunLimit = func(cfg config.DoorConfig) time.Duration {
			got = cfg
			return orig(cfg)
		}
		t.Cleanup(func() { v3ScriptRunLimit = orig })
		f, dir := themeFixture(t, "alpha")
		writeTheme(t, dir, "alpha")
		_, _, _, handled, out := f.runMenuScript(t, &MenuRecord{Script: "mainmenu.js"}, themeScriptCommands)
		if !handled {
			t.Fatalf("theme script did not run; output=%q", out)
		}
		if got.MaxRunMinutes >= 0 || orig(got) >= 0 {
			t.Errorf("theme script MaxRunMinutes = %d, run limit %v; want none", got.MaxRunMinutes, orig(got))
		}
	})
}
