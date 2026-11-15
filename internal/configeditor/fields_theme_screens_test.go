package configeditor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
)

func writeBar(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const (
	shippedHi = "; stock\n0,0,31,7,*,*,Highlight\n"
	sysopHi   = "; mine\n0,0,79,7,*,*,Highlight\n"
)

// The Screen Overrides page lists the *HI.BAR overrides that are present,
// each with its colour and layer as its source. Choosing Inherit for one
// writes an inherit marker to the overlay on save, which works for a
// shipped BAR too, and leaves the shipped file and the others alone.
func TestScreenOverridesListedAndInherited(t *testing.T) {
	m, root := newThemeModel(t)
	writeBar(t, root, "menus/v3/bar/FILELISTHI.BAR", shippedHi)
	writeBar(t, root, "menus.d/v3/bar/MSGAREAHI.BAR", sysopHi)
	// Already inheriting, and absent: neither is an override to list.
	writeBar(t, root, "menus/v3/bar/MSGCONFHI.BAR", shippedHi)
	writeBar(t, root, "menus.d/v3/bar/MSGCONFHI.BAR", menuset.BarInheritFile)

	bars := themeScreenBars(m.configs.themeSet)
	if len(bars) != 2 || bars[0].Name != "FILELISTHI" || bars[0].Highlight != 31 || bars[0].Overlay ||
		bars[1].Name != "MSGAREAHI" || bars[1].Highlight != 79 || !bars[1].Overlay {
		t.Fatalf("overrides found: %+v", bars)
	}

	m = press(t, m, "C", "4")
	if m.mode != modeSysConfigEdit || len(m.sysFields) != 2 || m.sysFields[0].Label != "File List" || m.sysFields[1].Label != "Message Areas" {
		t.Fatalf("mode=%v fields=%d", m.mode, len(m.sysFields))
	}
	wantScreen(t, m, "File List", "screen override, colour 31 (shipped)", "Message Areas", "screen override, colour 79 (menus.d)")
	if !strings.Contains(m.sysFields[0].Help, "FILELISTHI.BAR") {
		t.Errorf("help doesn't name the file: %q", m.sysFields[0].Help)
	}

	// Inherit shared colours for the file list: nothing is written yet.
	m = press(t, m, "enter")
	if m.sysFields[0].Get() != "Y" || !m.dirty {
		t.Fatalf("inherit=%s dirty=%v", m.sysFields[0].Get(), m.dirty)
	}
	wantScreen(t, m, "will inherit colour 31 when saved")
	marker := filepath.Join(root, "menus.d", "v3", "bar", "FILELISTHI.BAR")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("marker written before the save (stat err %v)", err)
	}
	if !m.saveAll() {
		t.Fatalf("saveAll: %s", m.message)
	}
	data, err := os.ReadFile(marker)
	if err != nil || !menuset.BarInherits(data) {
		t.Fatalf("overlay FILELISTHI.BAR after save = %q, %v; want an inherit marker", data, err)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "menus", "v3", "bar", "FILELISTHI.BAR")); string(data) != shippedHi {
		t.Errorf("shipped BAR changed: %q", data)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "menus.d", "v3", "bar", "MSGAREAHI.BAR")); string(data) != sysopHi {
		t.Errorf("the sysop's BAR changed: %q", data)
	}
	if len(m.configs.barInherit) != 0 {
		t.Errorf("still pending after the save: %v", m.configs.barInherit)
	}

	// The file list no longer has an override; the sysop's BAR still does.
	bars = themeScreenBars(m.configs.themeSet)
	if len(bars) != 1 || bars[0].Name != "MSGAREAHI" {
		t.Errorf("overrides after the save: %+v", bars)
	}
}

// Toggling Inherit back to N before saving writes nothing.
func TestScreenOverrideInheritCancelled(t *testing.T) {
	m, root := newThemeModel(t)
	writeBar(t, root, "menus/v3/bar/DOORMENUHI.BAR", shippedHi)
	m = press(t, m, "C", "4", "enter", "enter")
	if m.sysFields[0].Label != "Door Menu" || m.sysFields[0].Get() != "N" || len(m.configs.barInherit) != 0 {
		t.Fatalf("field %q = %s, pending %v", m.sysFields[0].Label, m.sysFields[0].Get(), m.configs.barInherit)
	}
	if !m.saveAll() {
		t.Fatalf("saveAll: %s", m.message)
	}
	if _, err := os.Stat(filepath.Join(root, "menus.d", "v3", "bar", "DOORMENUHI.BAR")); !os.IsNotExist(err) {
		t.Errorf("a marker was written (stat err %v)", err)
	}
}

// With no *HI.BAR present the page says so, and has nothing to change.
func TestScreenOverridesNone(t *testing.T) {
	m, _ := newThemeModel(t)
	fields := themeFieldsScreens(themeScreenBars(m.configs.themeSet), &m.configs.Theme, &config.ThemeColors{}, map[string]bool{})
	if len(fields) != 1 || fields[0].Type != ftDisplay || !strings.Contains(fields[0].Get(), "every lightbar inherits") {
		t.Errorf("fields = %+v", fields)
	}
}

// The inherited colour shown is the one the board draws with: its override
// over the theme's.
func TestScreenOverrideShowsTheSharedColour(t *testing.T) {
	th := config.ThemeConfig{YesNoHighlightColor: 31}
	board := config.ThemeColors{LightbarSelected: config.SetColor(78)}
	pending := map[string]bool{"FILEAREAHI": true}
	f := themeFieldsScreens([]screenBar{{ScreenBar: menuset.ScreenBars[1], Highlight: 30}}, &th, &board, pending)[0]
	if f.Label != "File Areas" || f.Get() != "Y" || f.Source() != "will inherit colour 78 when saved" {
		t.Errorf("%s = %s, %q", f.Label, f.Get(), f.Source())
	}
	if err := f.Set("N"); err != nil || pending["FILEAREAHI"] || f.Source() != "screen override, colour 30 (shipped)" {
		t.Errorf("after N: pending %v, %q", pending, f.Source())
	}
}
