package menuset

import (
	"errors"
	"io/fs"
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
		"/opt/vision3/menus/v3": filepath.FromSlash("/opt/vision3/menus.d/v3"),
		"/vision3/menus/v3":     filepath.FromSlash("/vision3/menus.d/v3"),
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
	got, err := s.Resolve("ansi", "MAIN.ANS")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(s.Base, "ansi", "MAIN.ANS") {
		t.Errorf("Resolve = %q", got)
	}
	// Missing everywhere: still the base path, so errors name the right place.
	got, err = s.Resolve("ansi", "NOPE.ANS")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(s.Base, "ansi", "NOPE.ANS") {
		t.Errorf("Resolve(missing) = %q", got)
	}
	if exists, err := s.Exists("ansi", "NOPE.ANS"); err != nil || exists {
		t.Error("Exists(missing) = true")
	}
}

func TestResolvePrefersOverlay(t *testing.T) {
	s := fixture(t)
	over := filepath.Join(s.Overlay, "ansi", "MAIN.ANS")
	write(t, over, "mine")

	path, layer, ok, err := s.Locate("ansi", "MAIN.ANS")
	if err != nil || !ok || layer != LayerOverlay || path != over {
		t.Errorf("Locate = %q %v %v", path, layer, ok)
	}
	// A sibling that is not overridden still comes from the base.
	if _, layer, _, err := s.Locate("ansi", "LOGIN.ANS"); err != nil || layer != LayerBase {
		t.Errorf("LOGIN.ANS layer = %v", layer)
	}
	// A new file that only the overlay has resolves too.
	write(t, filepath.Join(s.Overlay, "ansi", "EXTRA.ANS"), "x")
	if exists, err := s.Exists("ansi", "EXTRA.ANS"); err != nil || !exists {
		t.Error("overlay-only file not found")
	}
	// A directory in the overlay is not a file match.
	if err := os.MkdirAll(filepath.Join(s.Overlay, "ansi", "LOGIN.ANS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, layer, _, err := s.Locate("ansi", "LOGIN.ANS"); err != nil || layer != LayerBase {
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
	if _, layer, _, err := bare.Locate("ansi", "MAIN.ANS"); err != nil || layer != LayerBase {
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
	got, err := s.ResolveFirst("templates", "FILEAREA.TOP", "FILEAREA.TOP.ANS")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(s.Base, "templates", "FILEAREA.TOP.ANS") {
		t.Errorf("ResolveFirst = %q", got)
	}
	// An overlay copy under the *other* candidate name still wins — the
	// candidate order is by name, the layer order within each name.
	write(t, filepath.Join(s.Overlay, "templates", "FILEAREA.TOP"), "mine")
	got, err = s.ResolveFirst("templates", "FILEAREA.TOP", "FILEAREA.TOP.ANS")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(s.Overlay, "templates", "FILEAREA.TOP") {
		t.Errorf("ResolveFirst with overlay = %q", got)
	}
	// Nothing matches: shipped path of the first name.
	got, err = s.ResolveFirst("templates", "NOPE.TOP", "NOPE.TOP.ANS")
	if err != nil {
		t.Fatal(err)
	}
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

func TestReadDirDoesNotHideBrokenLayer(t *testing.T) {
	for _, layer := range []Layer{LayerBase, LayerOverlay} {
		t.Run(layer.String(), func(t *testing.T) {
			s := fixture(t)
			other := LayerBase
			if layer == LayerBase {
				other = LayerOverlay
			}
			write(t, s.Path(layer, "bar"), "not a directory")
			write(t, s.Path(other, "bar", "MAIN.BAR"), "readable")
			if entries, err := s.ReadDir("bar"); err == nil || errors.Is(err, fs.ErrNotExist) || entries != nil {
				t.Fatalf("broken layer hidden: entries=%v err=%v", entries, err)
			}
			if entries, err := s.Glob("bar", "*"); err == nil || entries != nil {
				t.Fatalf("Glob hid broken layer: entries=%v err=%v", entries, err)
			}
		})
	}
}

func TestReadDirCaseVariantOverlay(t *testing.T) {
	s := fixture(t)
	write(t, s.Path(LayerBase, "mnu", "MAIN.MNU"), "shipped")
	write(t, s.Path(LayerOverlay, "mnu", "main.mnu"), "overlay")
	// Detect semantics of the actual filesystem, including case-insensitive
	// macOS volumes, rather than assuming them from the operating system.
	_, aliasErr := os.Lstat(s.Path(LayerOverlay, "mnu", "MAIN.MNU"))
	insensitive := aliasErr == nil
	if aliasErr != nil && !os.IsNotExist(aliasErr) {
		t.Fatal(aliasErr)
	}
	entries, err := s.ReadDir("mnu")
	if err != nil {
		t.Fatal(err)
	}
	want := 2
	if insensitive {
		want = 1
	}
	if len(entries) != want {
		t.Fatalf("entries=%v want %d (case insensitive=%v)", entries, want, insensitive)
	}
	if insensitive && (entries[0].Layer != LayerOverlay || entries[0].Name != "main.mnu") {
		t.Errorf("case variant did not shadow shipped menu: %v", entries)
	}
	glob, err := s.Glob("mnu", "*")
	if err != nil || !reflect.DeepEqual(glob, entries) {
		t.Errorf("Glob=%v err=%v, want %v", glob, err, entries)
	}
	// An exact overlay spelling on a case-sensitive volume must not hide
	// another distinct file just because their names differ only in case.
	if !insensitive {
		write(t, s.Path(LayerOverlay, "mnu", "MAIN.MNU"), "another override")
		entries, err = s.ReadDir("mnu")
		if err != nil || len(entries) != 2 {
			t.Fatalf("distinct names lost: %v, %v", entries, err)
		}
		for _, entry := range entries {
			if entry.Layer != LayerOverlay {
				t.Errorf("entry=%v", entry)
			}
		}
	}
}

func TestDanglingOverlayDoesNotFallBack(t *testing.T) {
	s := fixture(t)
	path := s.Path(LayerOverlay, "ansi", "MAIN.ANS")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-target", path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	got, layer, ok, err := s.Locate("ansi", "MAIN.ANS")
	if err == nil || got != path || layer != LayerOverlay || ok {
		t.Fatalf("Locate=%q %v %v %v", got, layer, ok, err)
	}
	if got, err := s.Resolve("ansi", "MAIN.ANS"); err == nil || got != path {
		t.Errorf("Resolve=%q %v", got, err)
	}
	if got, err := s.ResolveFirst("ansi", "MAIN.ANS", "LOGIN.ANS"); err == nil || got != path {
		t.Errorf("ResolveFirst=%q %v", got, err)
	}
	if exists, err := s.Exists("ansi", "MAIN.ANS"); err == nil || exists {
		t.Errorf("Exists=%v %v", exists, err)
	}
}

// A symlink loop produces a non-missing stat error without depending on Unix
// permissions or whether the test runs as root.
func TestResolutionPropagatesStatErrors(t *testing.T) {
	for _, layer := range []Layer{LayerOverlay, LayerBase} {
		t.Run(layer.String(), func(t *testing.T) {
			s := fixture(t)
			if layer == LayerBase {
				s = Bare(s.Base)
			}
			path := s.Path(layer, "ansi", "BROKEN.ANS")
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("BROKEN.ANS", path); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			_, statErr := os.Stat(path)
			if statErr == nil || os.IsNotExist(statErr) {
				t.Fatalf("expected non-missing stat error, got %v", statErr)
			}
			if layer == LayerOverlay {
				write(t, s.Path(LayerBase, "ansi", "BROKEN.ANS"), "must not be read")
			}
			checkErr := func(err error) {
				t.Helper()
				var want, got *os.PathError
				if !errors.As(statErr, &want) || !errors.As(err, &got) || got.Path != path || !errors.Is(err, want.Err) {
					t.Errorf("error = %v, want original stat error %v", err, statErr)
				}
			}
			got, gotLayer, ok, err := s.Locate("ansi", "BROKEN.ANS")
			checkErr(err)
			if got != path || gotLayer != layer || ok {
				t.Errorf("Locate = %q %v %v", got, gotLayer, ok)
			}
			got, err = s.Resolve("ansi", "BROKEN.ANS")
			checkErr(err)
			if got != path {
				t.Errorf("Resolve selected %s instead of failing path", got)
			}
			// A readable alternative must not mask the first candidate's error.
			got, err = s.ResolveFirst("ansi", "BROKEN.ANS", "MAIN.ANS")
			checkErr(err)
			if got != path {
				t.Errorf("ResolveFirst selected %s instead of failing path", got)
			}
			exists, err := s.Exists("ansi", "BROKEN.ANS")
			checkErr(err)
			if exists {
				t.Error("Exists returned true on stat error")
			}
		})
	}
}
