package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// runSelectFileAreaLightbar's renderItemArea clamps the highlighted row's
// stripped text to termWidth with len(stripped) and stripped[:termWidth] —
// both byte counts, the same shape as the site D bug fixed in
// area_lightbar_render.go (see 13acfda). The row's Description field is
// inserted by buildItemLine without any padRight/truncateStr constraint, so
// a long, multi-byte file-area description (sysop-editable UTF-8 JSON) can
// push the row's byte length over termWidth while landing the clamp
// mid-rune.
func TestRunSelectFileAreaLightbarHighlightedRowClampsByRunes(t *testing.T) {
	menuSetPath := t.TempDir()
	templatesDir := filepath.Join(menuSetPath, "templates")
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "FILEAREA.TOP"), []byte("Areas\r\n"), 0644); err != nil {
		t.Fatalf("write TOP: %v", err)
	}
	// ^ID(3) + " " + ^TAG(16) + " " + ^NA(38) + " " + ^DS(unconstrained) = 60
	// fixed ASCII visible columns before the description.
	if err := os.WriteFile(filepath.Join(templatesDir, "FILEAREA.MID"), []byte("^ID ^TAG ^NA ^DS\r\n"), 0644); err != nil {
		t.Fatalf("write MID: %v", err)
	}

	// 30 CJK runes (90 bytes) pushes the row's visible width to 60+30=90,
	// over termWidth=80, and its byte length to 60+90=150 bytes, so a
	// byte-based clamp at 80 lands well inside the description.
	longDesc := strings.Repeat("日", 30)

	dataDir, cfgDir := t.TempDir(), t.TempDir()
	areasJSON := `[{"id":1,"tag":"UTILS","name":"Utilities","description":"` + longDesc + `","path":"utils","acs_list":""}]`
	if err := os.WriteFile(filepath.Join(cfgDir, "file_areas.json"), []byte(areasJSON), 0644); err != nil {
		t.Fatalf("write areas: %v", err)
	}
	fm, err := file.NewFileManager(dataDir, cfgDir)
	if err != nil {
		t.Fatalf("NewFileManager: %v", err)
	}

	um, err := user.NewUserManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewUserManager: %v", err)
	}
	u, err := um.AddUser("password", "Tester", "Real Name", "Loc")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	u.AccessLevel = 255

	e := &MenuExecutor{FileMgr: fm, MenuSetPath: menuSetPath}
	ts := newTestSession("q")
	terminal := newTestTerminal(ts)

	c := &cmdCtx{
		e: e, s: ts, terminal: terminal, userManager: um, currentUser: u,
		nodeNumber: 1, outputMode: ansi.OutputModeUTF8, termWidth: 80, termHeight: 24,
	}

	if _, _, err := runSelectFileAreaLightbar(c, ""); err != nil {
		t.Fatalf("runSelectFileAreaLightbar: %v", err)
	}

	out := ts.output()
	// A rune-correct clamp to termWidth=80 keeps all 30 "日" runes intact
	// (60 fixed ASCII columns + 30 description columns = 90, over 80, so the
	// row IS clamped — but on a rune boundary, so every rune that survives
	// is intact; the point under test is that none are split).
	if strings.Contains(out, "�") {
		t.Errorf("output contains the Unicode replacement character, row was split mid-rune: %q", out)
	}
	// Provably wrong under the byte-based bug: it keeps only 20/3=6 whole
	// runes (18 of the 20 bytes remaining after the 60-byte ASCII prefix)
	// plus a stray glyph. A rune-correct clamp keeps min(30, 80-60)=20 runes.
	if got, want := strings.Count(out, "日"), 20; got != want {
		t.Errorf("highlighted row has %d intact '日' runes, want %d (output %q)", got, want, out)
	}
}
