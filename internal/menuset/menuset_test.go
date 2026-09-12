package menuset

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixture builds menus/v3 with a couple of shipped files and returns the Set
// derived from it. The overlay directory is not created.
func fixture(t *testing.T) Set {
	t.Helper()
	root := t.TempDir()
	base := filepath.Join(root, "menus", "v3")
	write(t, filepath.Join(base, "ansi", "MAIN.ANS"), "base main")
	write(t, filepath.Join(base, "ansi", "LOGIN.ANS"), "base login")
	write(t, filepath.Join(base, "templates", "FILEAREA.TOP.ANS"), "base top")
	write(t, filepath.Join(base, "theme.json"), "{}")
	s := FromPath(base)
	if want := filepath.Join(root, "menus.d", "v3"); s.Overlay != want {
		t.Fatalf("Overlay = %q, want %q", s.Overlay, want)
	}
	return s
}

func TestOverlayFor(t *testing.T) {
	cases := map[string]string{
		"menus/v3":              filepath.Join("menus.d", "v3"),
		"menus/v3/":             filepath.Join("menus.d", "v3"),
		"./menus/v3":            filepath.Join("menus.d", "v3"),
		"/opt/vision3/menus/v3": "/opt/vision3/menus.d/v3",
		"/vision3/menus/v3":     "/vision3/menus.d/v3",
		"custom/sets/mine":      filepath.Join("custom", "sets.d", "mine"),
		"v3":                    "",
		"/v3":                   "",
	}
	for in, want := range cases {
		if got := OverlayFor(in); got != want {
			t.Errorf("OverlayFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveFallsBackToBase(t *testing.T) {
	s := fixture(t)
	got := s.Resolve("ansi", "MAIN.ANS")
	if got != filepath.Join(s.Base, "ansi", "MAIN.ANS") {
		t.Errorf("Resolve = %q", got)
	}
	// Missing everywhere: still the base path, so errors name the right place.
	got = s.Resolve("ansi", "NOPE.ANS")
	if got != filepath.Join(s.Base, "ansi", "NOPE.ANS") {
		t.Errorf("Resolve(missing) = %q", got)
	}
	if s.Exists("ansi", "NOPE.ANS") {
		t.Error("Exists(missing) = true")
	}
}

func TestResolvePrefersOverlay(t *testing.T) {
	s := fixture(t)
	over := filepath.Join(s.Overlay, "ansi", "MAIN.ANS")
	write(t, over, "mine")

	path, layer, ok := s.Locate("ansi", "MAIN.ANS")
	if !ok || layer != LayerOverlay || path != over {
		t.Errorf("Locate = %q %v %v", path, layer, ok)
	}
	// A sibling that is not overridden still comes from the base.
	if _, layer, _ := s.Locate("ansi", "LOGIN.ANS"); layer != LayerBase {
		t.Errorf("LOGIN.ANS layer = %v", layer)
	}
	// A new file that only the overlay has resolves too.
	write(t, filepath.Join(s.Overlay, "ansi", "EXTRA.ANS"), "x")
	if !s.Exists("ansi", "EXTRA.ANS") {
		t.Error("overlay-only file not found")
	}
	// A directory in the overlay is not a file match.
	if err := os.MkdirAll(filepath.Join(s.Overlay, "ansi", "LOGIN.ANS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, layer, _ := s.Locate("ansi", "LOGIN.ANS"); layer != LayerBase {
		t.Errorf("directory in overlay shadowed a base file")
	}
}

func TestBareHasNoOverlay(t *testing.T) {
	s := fixture(t)
	bare := Bare(s.Base)
	write(t, filepath.Join(s.Overlay, "ansi", "MAIN.ANS"), "mine")
	if bare.HasOverlay() {
		t.Error("Bare set reports an overlay")
	}
	if _, layer, _ := bare.Locate("ansi", "MAIN.ANS"); layer != LayerBase {
		t.Error("Bare set read the overlay")
	}
	if got := bare.WritePath("mnu", "X.MNU"); got != filepath.Join(s.Base, "mnu", "X.MNU") {
		t.Errorf("WritePath = %q", got)
	}
}

func TestWritePathIsOverlay(t *testing.T) {
	s := fixture(t)
	if got := s.WritePath("cfg", "MAIN.CFG"); got != filepath.Join(s.Overlay, "cfg", "MAIN.CFG") {
		t.Errorf("WritePath = %q", got)
	}
}

func TestResolveFirst(t *testing.T) {
	s := fixture(t)
	// Shipped set has FILEAREA.TOP.ANS only; the bare name is tried first and
	// the .ANS candidate matches.
	got := s.ResolveFirst("templates", "FILEAREA.TOP", "FILEAREA.TOP.ANS")
	if got != filepath.Join(s.Base, "templates", "FILEAREA.TOP.ANS") {
		t.Errorf("ResolveFirst = %q", got)
	}
	// An overlay copy under the *other* candidate name still wins — the
	// candidate order is by name, the layer order within each name.
	write(t, filepath.Join(s.Overlay, "templates", "FILEAREA.TOP"), "mine")
	got = s.ResolveFirst("templates", "FILEAREA.TOP", "FILEAREA.TOP.ANS")
	if got != filepath.Join(s.Overlay, "templates", "FILEAREA.TOP") {
		t.Errorf("ResolveFirst with overlay = %q", got)
	}
	// Nothing matches: shipped path of the first name.
	got = s.ResolveFirst("templates", "NOPE.TOP", "NOPE.TOP.ANS")
	if got != filepath.Join(s.Base, "templates", "NOPE.TOP") {
		t.Errorf("ResolveFirst(missing) = %q", got)
	}
}

func TestReadDirMerges(t *testing.T) {
	s := fixture(t)
	write(t, filepath.Join(s.Overlay, "ansi", "MAIN.ANS"), "mine")
	write(t, filepath.Join(s.Overlay, "ansi", "EXTRA.ANS"), "new")
	if err := os.MkdirAll(filepath.Join(s.Overlay, "ansi", "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := s.ReadDir("ansi")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	layers := map[string]Layer{}
	for _, e := range entries {
		names = append(names, e.Name)
		layers[e.Name] = e.Layer
	}
	if want := []string{"EXTRA.ANS", "LOGIN.ANS", "MAIN.ANS"}; !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	if layers["MAIN.ANS"] != LayerOverlay || layers["EXTRA.ANS"] != LayerOverlay || layers["LOGIN.ANS"] != LayerBase {
		t.Errorf("layers = %v", layers)
	}
}

func TestReadDirErrors(t *testing.T) {
	s := fixture(t)
	// Neither layer has the directory: error.
	if _, err := s.ReadDir("bar"); err == nil {
		t.Error("expected error for missing dir in both layers")
	}
	// Only the overlay has it: fine.
	write(t, filepath.Join(s.Overlay, "bar", "MAIN.BAR"), "x")
	entries, err := s.ReadDir("bar")
	if err != nil || len(entries) != 1 {
		t.Errorf("overlay-only dir: entries=%v err=%v", entries, err)
	}
	// Only the base has it: fine.
	entries, err = s.ReadDir("templates")
	if err != nil || len(entries) != 1 {
		t.Errorf("base-only dir: entries=%v err=%v", entries, err)
	}
}

func TestGlob(t *testing.T) {
	s := fixture(t)
	hdr := filepath.Join("templates", "message_headers")
	write(t, filepath.Join(s.Base, hdr, "MSGHDR.1.ans"), "1")
	write(t, filepath.Join(s.Base, hdr, "MSGHDR.2.ans"), "2")
	write(t, filepath.Join(s.Base, hdr, "MSGHDR.ANS"), "sel")
	write(t, filepath.Join(s.Overlay, hdr, "MSGHDR.2.ans"), "mine")
	write(t, filepath.Join(s.Overlay, hdr, "MSGHDR.3.ans"), "3")

	got, err := s.Glob(hdr, "MSGHDR.*.ans")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
	}
	if want := []string{"MSGHDR.1.ans", "MSGHDR.2.ans", "MSGHDR.3.ans"}; !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	if got[1].Layer != LayerOverlay {
		t.Errorf("MSGHDR.2.ans should come from the overlay")
	}

	// A missing directory is an empty result, like filepath.Glob.
	if got, err := s.Glob("nowhere", "*"); err != nil || len(got) != 0 {
		t.Errorf("missing dir: got=%v err=%v", got, err)
	}
	if _, err := s.Glob(hdr, "["); err == nil {
		t.Error("bad pattern should error")
	}
}

func TestSummarize(t *testing.T) {
	s := fixture(t)
	if _, ok, err := s.Summarize(); ok || err != nil {
		t.Errorf("absent overlay: ok=%v err=%v", ok, err)
	}
	write(t, filepath.Join(s.Overlay, "ansi", "MAIN.ANS"), "mine")
	write(t, filepath.Join(s.Overlay, "ansi", "EXTRA.ANS"), "new")
	write(t, filepath.Join(s.Overlay, "theme.json"), "{}")
	write(t, filepath.Join(s.Overlay, "screens", "OOPS.ANS"), "typo") // not a shipped subdir
	write(t, filepath.Join(s.Overlay, "templates", "message_headers", "MSGHDR.9.ans"), "deep")

	sum, ok, err := s.Summarize()
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	want := Summary{Overrides: 2, Additions: 3, UnknownDirs: []string{"screens"}}
	if !reflect.DeepEqual(sum, want) {
		t.Errorf("Summary = %+v, want %+v", sum, want)
	}
}
