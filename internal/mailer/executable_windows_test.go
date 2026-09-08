//go:build windows

package mailer

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCheckExecutableWindows covers the extension rule that replaces the unix
// execute bit, and the file-type check alongside it.
func TestCheckExecutableWindows(t *testing.T) {
	dir := t.TempDir()

	write := func(name string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("stub"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")

	for _, name := range []string{"binkd.exe", "binkd.EXE", "binkd.cmd", "binkd.bat"} {
		p := write(name)
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkExecutable(p, info); err != nil {
			t.Errorf("checkExecutable(%s) = %v, want nil", name, err)
		}
	}

	// No extension at all was the shape that broke every Windows install: the
	// unix check rejected it for having no execute bit, which Windows never
	// sets on anything.
	for _, name := range []string{"binkd", "binkd.txt", "binkd.dll"} {
		p := write(name)
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkExecutable(p, info); err == nil {
			t.Errorf("checkExecutable(%s) = nil, want an error", name)
		}
	}

	// A directory named like an executable is still not one.
	d := filepath.Join(dir, "notabinary.exe")
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkExecutable(d, info); err == nil {
		t.Error("checkExecutable on a directory = nil, want an error")
	}
}

// TestExecutableExtsHonoursPATHEXT checks a site can extend the list, and that
// an empty or unset PATHEXT falls back rather than accepting everything.
func TestExecutableExtsHonoursPATHEXT(t *testing.T) {
	t.Setenv("PATHEXT", ".EXE;.PS1")
	got := executableExts()
	want := map[string]bool{".exe": true, ".ps1": true}
	if len(got) != len(want) {
		t.Fatalf("exts = %v, want %v", got, want)
	}
	for _, e := range got {
		if !want[e] {
			t.Errorf("unexpected extension %q", e)
		}
	}

	t.Setenv("PATHEXT", "")
	if got := executableExts(); len(got) != len(defaultExecutableExts) {
		t.Errorf("empty PATHEXT gave %v, want the default list", got)
	}

	t.Setenv("PATHEXT", ";  ;")
	if got := executableExts(); len(got) != len(defaultExecutableExts) {
		t.Errorf("blank PATHEXT gave %v, want the default list", got)
	}
}
