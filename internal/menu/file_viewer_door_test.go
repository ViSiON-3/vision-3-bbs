package menu

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dop251/goja"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// viewerDoorEnv is a menu env whose door registry has viewer doors: VIEWER,
// a script that prints the arguments it was given, LOCKED, the same script
// above the caller's level, and NATIVE, which isn't a script door.
func viewerDoorEnv(t *testing.T, archive, text string) *menuEnv {
	t.Helper()
	env := newMenuEnv(t)
	env.sessionContext = true
	dir := t.TempDir()
	script := `v3.console.write("[viewer " + v3.args.join(" ") + "]");`
	if err := os.WriteFile(filepath.Join(dir, "viewer.js"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.js"), []byte(`throw new Error("viewer broke");`), 0o644); err != nil {
		t.Fatal(err)
	}
	env.e.SetDoorRegistry(map[string]config.DoorConfig{
		"VIEWER": {Code: "VIEWER", Type: "v3_script", Script: "viewer.js", WorkingDirectory: dir, Args: []string{"own"}, Hidden: true},
		"LOCKED": {Code: "LOCKED", Type: "v3_script", Script: "viewer.js", WorkingDirectory: dir, MinAccessLevel: 255, Hidden: true},
		"NATIVE": {Code: "NATIVE", Commands: []string{"/bin/true"}},
		"BUSY":   {Code: "BUSY", Type: "v3_script", Script: "viewer.js", WorkingDirectory: dir, SingleInstance: true, Hidden: true},
		"BROKEN": {Code: "BROKEN", Type: "v3_script", Script: "broken.js", WorkingDirectory: dir, Hidden: true},
	})
	cfg := env.e.GetServerConfig()
	cfg.ArchiveViewerDoor, cfg.TextViewerDoor = archive, text
	env.e.SetServerConfig(cfg)
	addFileWithContent(t, env, 1, "NOTES.TXT", []byte("plain text\n"))
	addFileWithContent(t, env, 1, "PACK.ZIP", zipBytes(t, map[string]string{"INSIDE.DOC": "hello"}))
	env.caller.CurrentFileAreaID = 1
	return env
}

// TestViewerDoorsRun checks that VIEW_FILE and TYPE_TEXT_FILE run the
// configured viewer door, by kind, with the area tag and filename after the
// door's own arguments.
func TestViewerDoorsRun(t *testing.T) {
	env := viewerDoorEnv(t, "viewer", "VIEWER")
	area, _ := env.e.FileMgr.GetAreaByID(1)
	r := env.runCmd("VIEW_FILE", env.caller, "", "NOTES.TXT\r")
	if !r.has("[viewer own " + area.Tag + " NOTES.TXT]") {
		t.Errorf("text file not viewed in the door:\n%s", r.text())
	}
	r = env.runCmd("VIEW_FILE", env.caller, "", "PACK.ZIP\r")
	if !r.has("[viewer own "+area.Tag+" PACK.ZIP]") || r.has("ZipLab") {
		t.Errorf("archive not viewed in the door:\n%s", r.text())
	}
	r = env.runCmd("TYPE_TEXT_FILE", env.caller, "", "NOTES.TXT\r")
	if !r.has("[viewer own " + area.Tag + " NOTES.TXT]") {
		t.Errorf("TYPE_TEXT_FILE not routed to the text viewer:\n%s", r.text())
	}
}

// TestViewerDoorsFallBack checks that the built-in viewers run when no door
// is set for the kind, the door is missing or not a script door, or the
// door's own rules refuse the caller.
func TestViewerDoorsFallBack(t *testing.T) {
	for _, tc := range []struct{ name, archive, text string }{
		{"unset", "", ""},
		{"missing", "NOSUCH", "NOSUCH"},
		{"not a script", "NATIVE", "NATIVE"},
		{"refused", "LOCKED", "LOCKED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := viewerDoorEnv(t, tc.archive, tc.text)
			r := env.runCmd("VIEW_FILE", env.caller, "", "NOTES.TXT\r\r")
			if r.has("[viewer") || !r.has("Viewing: NOTES.TXT", "plain text") {
				t.Errorf("text: want the built-in pager:\n%s", r.text())
			}
			r = env.runCmd("VIEW_FILE", env.caller, "", "PACK.ZIP\rq")
			if r.has("[viewer") || !r.has("INSIDE.DOC") {
				t.Errorf("archive: want ZipLab's listing:\n%s", r.text())
			}
		})
	}
}

// TestViewerDoorsBusyOrFailing checks that a viewer door that can't run
// still leaves the caller the file: a single_instance door in use on
// another node falls back to the built-in viewer, and one that fails shows
// the door error and then falls back.
func TestViewerDoorsBusyOrFailing(t *testing.T) {
	t.Run("busy", func(t *testing.T) {
		env := viewerDoorEnv(t, "", "BUSY")
		if err := acquireDoorLock("BUSY", 99); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { releaseDoorLock("BUSY", 99) })
		r := env.runCmd("VIEW_FILE", env.caller, "", "NOTES.TXT\r\r")
		if r.has("[viewer") || !r.has("Viewing: NOTES.TXT", "plain text") {
			t.Errorf("want the built-in pager:\n%s", r.text())
		}
	})
	t.Run("failing", func(t *testing.T) {
		env := viewerDoorEnv(t, "", "BROKEN")
		r := env.runCmd("VIEW_FILE", env.caller, "", "NOTES.TXT\r\r\r")
		if !r.has("Error running viewer 'BROKEN'", "viewer broke") || !r.has("Viewing: NOTES.TXT", "plain text") {
			t.Errorf("want the door error, then the built-in pager:\n%s", r.text())
		}
	})
}

// TestWarnViewerDoors checks the startup warning: one line per setting that
// names a door the BBS can't use, none for an unset or usable one.
func TestWarnViewerDoors(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	viewerDoorEnv(t, "NOSUCH", "NATIVE").e.WarnViewerDoors()
	out := buf.String()
	if strings.Count(out, "viewer door can't be used") != 2 ||
		!strings.Contains(out, "setting=archiveViewerDoor door=NOSUCH") || !strings.Contains(out, "no door has that code") ||
		!strings.Contains(out, "setting=textViewerDoor door=NATIVE") || !strings.Contains(out, "not a v3_script door") {
		t.Errorf("warnings:\n%s", out)
	}
	buf.Reset()
	viewerDoorEnv(t, "", "VIEWER").e.WarnViewerDoors()
	if buf.Len() != 0 {
		t.Errorf("unset or usable doors warned:\n%s", buf.String())
	}
}

// stockViewerEnv is a menu env with the stock ARTVIEW and TEXTVIEW doors,
// running the scripts in scripts/examples, set as the viewers.
func stockViewerEnv(t *testing.T) *menuEnv {
	t.Helper()
	env := newMenuEnv(t)
	env.sessionContext = true
	scripts, err := filepath.Abs(filepath.Join("..", "..", "scripts", "examples"))
	if err != nil {
		t.Fatal(err)
	}
	env.e.SetDoorRegistry(map[string]config.DoorConfig{
		"ARTVIEW":  {Code: "ARTVIEW", Type: "v3_script", Script: "artview.js", WorkingDirectory: scripts, Hidden: true},
		"TEXTVIEW": {Code: "TEXTVIEW", Type: "v3_script", Script: "textview.js", WorkingDirectory: scripts, Hidden: true},
	})
	cfg := env.e.GetServerConfig()
	cfg.ArchiveViewerDoor, cfg.TextViewerDoor = "ARTVIEW", "TEXTVIEW"
	env.e.SetServerConfig(cfg)
	// The stock area 1 needs level 25 to download, which reading in a
	// viewer needs too, so these run as the sysop.
	env.sysop.CurrentFileAreaID = 1
	return env
}

// The stock viewer tests check that each script in scripts/examples loads,
// reads its file and draws its first screen without a script error. Keys
// typed after the door starts don't reach it in this harness (the line
// reader has already buffered the input), so paging, Find and Tag are left
// to a test on a running board.

// TestStockTextViewer runs scripts/examples/textview.js on a text file: the
// header names the file and the first screen shows the text and the bar.
func TestStockTextViewer(t *testing.T) {
	env := stockViewerEnv(t)
	addFileWithContent(t, env, 1, "NOTES.TXT", []byte("first line\r\nsecond line\r\n\x1b[1;32mgreen\x1b[0m line\r\n"))
	r := env.runCmd("VIEW_FILE", env.sysop, "", "NOTES.TXT\r")
	if !r.has("NOTES.TXT", "first line", "second line", "More", "Find", "Tag", "Quit") || r.has("Error") {
		t.Errorf("text viewer:\n%s", r.text())
	}
	if !strings.Contains(stripANSIForTest(r.text()), "green line") {
		t.Errorf("ANSI text not drawn:\n%s", r.text())
	}
	checkRuleAbove(t, r.raw, "Quit")
}

// TestStockArchiveViewer runs scripts/examples/artview.js on a ZIP: the
// list shows every entry, with a piece's SAUCE title and artist.
func TestStockArchiveViewer(t *testing.T) {
	env := stockViewerEnv(t)
	sauce := make([]byte, 128)
	copy(sauce, "SAUCE00")
	copy(sauce[7:], "Night Train")
	copy(sauce[42:], "someartist")
	art := append([]byte("\x1b[1;32mGREEN\x1b[0m\r\n"), 0x1a)
	art = append(art, sauce...)
	addFileWithContent(t, env, 1, "PACK.ZIP", zipBytes(t, map[string]string{"TRAIN.ANS": string(art), "PROG.EXE": "MZ\x00\x00"}))
	r := env.runCmd("VIEW_FILE", env.sysop, "", "PACK.ZIP\r")
	if !r.has("PACK.ZIP", "2 entries", "TRAIN.ANS", "Night Train by someartist", "PROG.EXE", "Enter", "view") || r.has("Error") {
		t.Errorf("archive viewer:\n%s", r.text())
	}
	checkRuleAbove(t, r.raw, "leave")
}

// checkRuleAbove checks that a stock viewer's screen has a rule on the row
// above its bottom bar, the row holding marker (BOTTOM_RULE, on in stock).
func checkRuleAbove(t *testing.T, raw, marker string) {
	t.Helper()
	// Render only up to the bar: the viewer clears the screen as it exits,
	// and whether that clear reaches the output before the session ends
	// depends on timing.
	if at := strings.LastIndex(raw, marker); at >= 0 {
		raw = raw[:at+len(marker)]
	}
	lines := ansi.RenderANSIArtToLines(raw, 80, 100)
	for i := len(lines) - 1; i > 0; i-- {
		if strings.Contains(lines[i], marker) {
			if !strings.HasPrefix(strings.TrimSpace(stripANSIForTest(lines[i-1])), "──────────") {
				t.Errorf("no rule above the bar (row %d):\n%s", i, strings.Join(lines, "\n"))
			}
			return
		}
	}
	t.Errorf("no bar holding %q:\n%s", marker, strings.Join(lines, "\n"))
}

// stripANSIForTest drops escape sequences so rendered art reads as text.
func stripANSIForTest(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// viewerSharedBlock returns the "what the viewers can show" block of a
// stock viewer script.
func viewerSharedBlock(t *testing.T, script string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "scripts", "examples", script))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	start := strings.Index(s, "// --- What the viewers can show.")
	end := strings.Index(s, "// --- End of the shared block.")
	if start < 0 || end < start {
		t.Fatalf("%s has no shared block", script)
	}
	return s[start:end]
}

// TestViewerSharedBlockMatches checks that the two stock viewers decide
// what they can show the same way.
func TestViewerSharedBlockMatches(t *testing.T) {
	if viewerSharedBlock(t, "textview.js") != viewerSharedBlock(t, "artview.js") {
		t.Error("the shared block differs between textview.js and artview.js")
	}
}

// TestViewerKindOf pins the stock viewers' kindOf: NUL bytes or a quarter
// control bytes are binary; text with no control bytes, or with a few and
// a name that says it's text, is text; a few control bytes under any other
// name ask the caller.
func TestViewerKindOf(t *testing.T) {
	vm := goja.New()
	if _, err := vm.RunString(viewerSharedBlock(t, "textview.js")); err != nil {
		t.Fatal(err)
	}
	kindOf, ok := goja.AssertFunction(vm.Get("kindOf"))
	if !ok {
		t.Fatal("no kindOf")
	}
	art := "\x1b[1;31m\x03\x03 love \x01\x02\r\n" + strings.Repeat("plain text line\r\n", 4)
	for _, tc := range []struct{ name, data, want string }{
		{"README.TXT", "hello\r\n\tworld\r\n", "text"},
		{"NOTES.XYZ", "hello\r\n\x1b[1mworld\r\n", "text"},
		{"HEART.ICE", art, "text"},
		{"RULES.MSG", art, "text"},
		{"INFO.INF", art, "text"},
		{"BOARD.BBS", art, "text"},
		{"README", art, "text"},
		{"docs/FILE_ID", art, "text"},
		{"HEART.XYZ", art, "ask"},
		{"PROG.EXE", "MZ\x00\x00\x90", "binary"},
		{"NOISE.TXT", strings.Repeat("\x01\x02a", 20), "binary"},
		{"SAUCED.ANS", "art\x1a" + strings.Repeat("\x00", 40), "text"},
		{"EMPTY", "", "text"},
	} {
		got, err := kindOf(goja.Undefined(), vm.ToValue(tc.name), vm.ToValue(tc.data))
		if err != nil {
			t.Fatal(err)
		}
		if got.String() != tc.want {
			t.Errorf("kindOf(%q) = %s, want %s", tc.name, got, tc.want)
		}
	}
}
