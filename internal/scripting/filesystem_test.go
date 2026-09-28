package scripting

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// newSandboxTestEngine builds an engine whose working directory is
// <tmp>/scripts, so its v3.fs sandbox is <tmp>/scripts/data. The sandbox
// directory is NOT created; tests that need it call os.MkdirAll themselves.
// It returns the engine, the temp root and the sandbox path.
func newSandboxTestEngine(t *testing.T) (*Engine, string, string) {
	t.Helper()
	tmp := t.TempDir()
	working := filepath.Join(tmp, "scripts")
	if err := os.MkdirAll(working, 0o755); err != nil {
		t.Fatal(err)
	}
	sess := newInterruptibleSession("")
	eng := NewEngine(context.Background(), &SessionContext{
		Session:    sess,
		OutputMode: ansi.OutputModeCP437,
	}, ScriptConfig{WorkingDir: working}, nil)
	var once sync.Once
	t.Cleanup(func() {
		once.Do(func() {
			sess.closeInterrupt()
			eng.Close()
		})
	})
	return eng, tmp, filepath.Join(working, "data")
}

// runJS runs src in the engine and returns the JS exception, if any.
func runJS(t *testing.T, eng *Engine, src string) error {
	t.Helper()
	_, err := eng.vm.RunString(src)
	return err
}

// jsLiteral returns a JS string literal for s (used to embed Go paths).
func jsLiteral(s string) string {
	b, _ := json.Marshal(s) // marshalling a string cannot fail
	return string(b)
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s must not exist (lstat err = %v)", path, err)
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks not supported here: %v", err)
	}
}

// TestFSDanglingSymlinkEscape is the regression test for #466: a dangling
// symlink inside scripts/data that points outside the sandbox must not let
// write/append create the target.
func TestFSDanglingSymlinkEscape(t *testing.T) {
	eng, tmp, sandbox := newSandboxTestEngine(t)
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(tmp, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outside, "pwned.txt")
	symlinkOrSkip(t, target, filepath.Join(sandbox, "link.txt"))

	for _, op := range []string{"write", "append"} {
		err := runJS(t, eng, `v3.fs.`+op+`("link.txt", "escaped")`)
		if err == nil {
			t.Errorf("%s through a dangling symlink out of the sandbox succeeded", op)
		}
		mustNotExist(t, target)
	}
}

// TestFSSymlinkedDirEscape covers a symlink to a real directory outside the
// sandbox: reads, writes, mkdir and listing through it must all fail.
func TestFSSymlinkedDirEscape(t *testing.T) {
	eng, tmp, sandbox := newSandboxTestEngine(t)
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(tmp, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, outside, filepath.Join(sandbox, "esc"))

	for _, src := range []string{
		`v3.fs.read("esc/secret.txt")`,
		`v3.fs.write("esc/new.txt", "x")`,
		`v3.fs.append("esc/new.txt", "x")`,
		`v3.fs.mkdir("esc/sub")`,
		`v3.fs.list("esc")`,
	} {
		if err := runJS(t, eng, src); err == nil {
			t.Errorf("%s succeeded through a symlink out of the sandbox", src)
		}
	}
	mustNotExist(t, filepath.Join(outside, "new.txt"))
	mustNotExist(t, filepath.Join(outside, "sub"))

	v, err := eng.vm.RunString(`v3.fs.exists("esc/secret.txt")`)
	if err != nil {
		t.Fatal(err)
	}
	if v.ToBoolean() {
		t.Error("exists() reported a file outside the sandbox")
	}
}

// TestFSSymlinkInsideSandboxAllowed makes sure confinement doesn't break a
// symlink whose target stays inside the sandbox.
func TestFSSymlinkInsideSandboxAllowed(t *testing.T) {
	eng, _, sandbox := newSandboxTestEngine(t)
	if err := os.MkdirAll(filepath.Join(sandbox, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, "real", filepath.Join(sandbox, "alias"))
	if err := runJS(t, eng, `v3.fs.write("alias/f.txt", "ok")`); err != nil {
		t.Fatalf("write through in-sandbox symlink: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(sandbox, "real", "f.txt")); err != nil || string(b) != "ok" {
		t.Fatalf("file = %q, %v; want \"ok\"", b, err)
	}
}

// TestFSTraversalRejected checks that lexical traversal and absolute paths
// are refused with the sandbox error and create nothing.
func TestFSTraversalRejected(t *testing.T) {
	eng, tmp, sandbox := newSandboxTestEngine(t)
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(tmp, "abs.txt")
	for _, p := range []string{
		"..",
		"../x.txt",
		"../../x.txt",
		"a/../../x.txt",
		"..\\x.txt", // a plain name on Unix, traversal on Windows; must not escape either way
		abs,
	} {
		err := runJS(t, eng, `v3.fs.write(`+jsLiteral(p)+`, "x")`)
		if filepath.IsLocal(filepath.Clean(filepath.FromSlash(p))) {
			// Only "..\x.txt" on Unix: a legal in-sandbox filename.
			if err != nil {
				t.Errorf("write(%q) = %v; want success (in-sandbox name)", p, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("write(%q) succeeded; want access denied", p)
			continue
		}
		if !strings.Contains(err.Error(), "outside sandbox") {
			t.Errorf("write(%q) error = %v; want \"outside sandbox\"", p, err)
		}
	}
	mustNotExist(t, filepath.Join(filepath.Dir(sandbox), "x.txt"))
	mustNotExist(t, filepath.Join(tmp, "x.txt"))
	mustNotExist(t, abs)
}

// TestFSNestedCreate is the regression test for the first half of #459:
// mkdir and write/append must create missing parents, even when the sandbox
// directory itself does not exist yet.
func TestFSNestedCreate(t *testing.T) {
	eng, _, sandbox := newSandboxTestEngine(t)
	// Sandbox deliberately absent (fresh install).

	if err := runJS(t, eng, `v3.fs.mkdir("a/b/c")`); err != nil {
		t.Fatalf("mkdir(a/b/c): %v", err)
	}
	if fi, err := os.Stat(filepath.Join(sandbox, "a", "b", "c")); err != nil || !fi.IsDir() {
		t.Fatalf("a/b/c not created: %v", err)
	}

	if err := runJS(t, eng, `v3.fs.write("newdir/sub/f.txt", "hello")`); err != nil {
		t.Fatalf("write(newdir/sub/f.txt): %v", err)
	}
	if err := runJS(t, eng, `v3.fs.append("logs/2026/app.log", "one\n"); v3.fs.append("logs/2026/app.log", "two\n")`); err != nil {
		t.Fatalf("append(logs/2026/app.log): %v", err)
	}

	v, err := eng.vm.RunString(`v3.fs.read("newdir/sub/f.txt") + "|" + v3.fs.read("logs/2026/app.log")`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "hello|one\ntwo\n"; got != want {
		t.Fatalf("read back %q; want %q", got, want)
	}
}

// TestFSDotDotPrefixedNames is the regression test for the second half of
// #459: names that merely start with ".." are ordinary in-sandbox names.
func TestFSDotDotPrefixedNames(t *testing.T) {
	eng, _, sandbox := newSandboxTestEngine(t)
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	v, err := eng.vm.RunString(`
		v3.fs.write("..foo", "a");
		v3.fs.write("dir/..bar", "b");
		v3.fs.read("..foo") + v3.fs.read("dir/..bar") + "|" +
			v3.fs.exists("..foo") + "|" +
			v3.fs.list("").map(function (e) { return e.name; }).sort().join(",")
	`)
	if err != nil {
		t.Fatalf("..-prefixed names rejected: %v", err)
	}
	if got, want := v.String(), "ab|true|..foo,dir"; got != want {
		t.Fatalf("got %q; want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "..foo")); err != nil {
		t.Fatalf("..foo not created inside the sandbox: %v", err)
	}
	if err := runJS(t, eng, `if (!v3.fs.delete("..foo")) throw new Error("delete failed")`); err != nil {
		t.Fatal(err)
	}
}

// TestFSListAndDeleteBasics covers list/exists/delete on the sandbox root.
func TestFSListAndDeleteBasics(t *testing.T) {
	eng, _, _ := newSandboxTestEngine(t)
	v, err := eng.vm.RunString(`
		v3.fs.write("f.txt", "12345");
		v3.fs.mkdir("d");
		var l = v3.fs.list();
		var f = l.filter(function (e) { return e.name === "f.txt"; })[0];
		var d = l.filter(function (e) { return e.name === "d"; })[0];
		[l.length, f.size, f.isDir, d.isDir, v3.fs.delete("f.txt"), v3.fs.exists("f.txt"), v3.fs.delete("nope")].join(",")
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "2,5,false,true,true,false,false"; got != want {
		t.Fatalf("got %q; want %q", got, want)
	}
}
