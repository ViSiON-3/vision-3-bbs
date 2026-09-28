package syncjs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFileReadWriteModes exercises File open modes and the text read/write
// methods, checking both JS results and bytes on disk.
func TestFileReadWriteModes(t *testing.T) {
	h := newDoor(t, doorOpts{})
	h.mustRun(`
		var f = new File("notes.txt");
		f.open("w");
		f.writeln("line one");
		f.write("line two\r\n");
		f.writeln();
		f.writeAll(["x", 1]);
		f.close();
		f.open("a");
		f.write("tail");
		f.close();
	`)
	path := filepath.Join(h.game, "notes.txt")
	if got, want := readFile(t, path), "line one\nline two\r\n\nx\n1\ntail"; got != want {
		t.Fatalf("file content = %q, want %q", got, want)
	}

	tests := []struct{ expr, want string }{
		{`var f = new File("notes.txt"); f.open("r"); var a = f.readAll(); f.close(); JSON.stringify(a)`, `["line one","line two","","x","1","tail"]`},
		{`f.open("r"); var l1 = f.readln(), l2 = f.readln(); f.close(); l1 + "|" + l2`, "line one|line two"},
		{`f.open("r"); f.read(4) + "/" + f.position`, "line/4"},
		{`f.position = 5; f.read(3)`, "one"},
		{`f.close(); f.open("r"); while (f.readln() !== null) {}; String(f.eof)`, "true"},
		{`String(f.readln())`, "null"},
		{`f.close(); String(f.readln())`, "null"},
		{`f.read()`, ""},
		{`JSON.stringify(f.readAll())`, "[]"},
		{`String(f.length)`, "28"},
		{`String(f.eof) + String(f.position)`, "true0"},
		{`String(f.is_open) + String(f.exists)`, "falsetrue"},
		{`f.name === ` + jsStr(path), "true"},
		{`String(f.write("x")) + String(f.writeln("x")) + String(f.writeAll(["x"])) + String(f.writeBin(1)) + String(f.truncate())`, "falsefalsefalsefalsefalse"},
		{`String(f.readBin())`, "0"},
		{`f.open(); var all = f.read(); f.close(); all.length`, "28"},
		{`var g = new File("missing.txt"); String(g.open("r")) + g.error + g.exists + g.length`, "false-1false0"},
		{`String(g.open("r+"))`, "false"},
		{`var n = new File(); n.name === ` + jsStr(h.game), "true"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}
}

// TestFileModesTable maps Synchronet mode strings to open behaviour.
func TestFileModesTable(t *testing.T) {
	tests := []struct {
		mode        string
		create      bool // creates a missing file
		truncate    bool // empties an existing file
		appendWrite bool // writes go to the end
	}{
		{"r", false, false, false},
		{"rb", false, false, false},
		{"r+", false, false, false},
		{"rb+", false, false, false},
		{"w", true, true, false},
		{"wb", true, true, false},
		{"w+", true, true, false},
		{"wb+", true, true, false},
		{"a", true, false, true},
		{"ab", true, false, true},
		{"a+", true, false, true},
		{"ab+", true, false, true},
		{" W ", true, true, false},
		{"bogus", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			h := newDoor(t, doorOpts{})
			missing := h.eval(`String(new File("new.dat").open(` + jsStr(tt.mode) + `))`).String()
			if (missing == "true") != tt.create {
				t.Errorf("open(%q) on missing file = %s, want create=%v", tt.mode, missing, tt.create)
			}
			h.write("game/old.dat", "0123456789")
			h.eval(`var f = new File("old.dat"); f.open(` + jsStr(tt.mode) + `); f.write("AB"); f.close()`)
			got := readFile(t, filepath.Join(h.game, "old.dat"))
			switch {
			case tt.truncate && got != "AB":
				t.Errorf("mode %q: content %q, want truncated then AB", tt.mode, got)
			case tt.appendWrite && got != "0123456789AB":
				t.Errorf("mode %q: content %q, want appended", tt.mode, got)
			case !tt.truncate && !tt.appendWrite && strings.HasPrefix(tt.mode, "r+") && got != "AB23456789":
				t.Errorf("mode %q: content %q, want overwrite at start", tt.mode, got)
			case !tt.create && !strings.Contains(tt.mode, "+") && got != "0123456789":
				t.Errorf("mode %q: read-only open changed file to %q", tt.mode, got)
			}
		})
	}
}

// TestFileBinaryAndLatin1 checks little-endian binary IO, truncate, and
// that bytes 128-255 round-trip through JS strings unchanged.
func TestFileBinaryAndLatin1(t *testing.T) {
	h := newDoor(t, doorOpts{})
	h.mustRun(`
		var f = new File("bin.dat");
		f.open("w+b");
		f.writeBin(0x04030201);
		f.writeBin(0xBEEF, 2);
		f.writeBin(0x7F, 1);
		f.write("°ÿ");
	`)
	if got := readFile(t, filepath.Join(h.game, "bin.dat")); got != "\x01\x02\x03\x04\xef\xbe\x7f\xb0\xff" {
		t.Fatalf("bin.dat = %q", got)
	}
	tests := []struct{ expr, want string }{
		{`f.position = 0; String(f.readBin())`, "67305985"},
		{`String(f.readBin(2))`, "48879"},
		{`String(f.readBin(1))`, "127"},
		{`var s = f.read(); s.charCodeAt(0) + "," + s.charCodeAt(1)`, "176,255"},
		{`String(f.readBin(4))`, "0"},
		{`String(f.truncate(3)) + f.length`, "true3"},
		{`f.flush(); f.close(); String(f.flush())`, "true"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}
}

// TestFileLockUnlock: byte-range locks succeed on an open file and fail on
// a closed one.
func TestFileLockUnlock(t *testing.T) {
	h := newDoor(t, doorOpts{})
	got := h.eval(`
		var f = new File("lock.dat");
		var closed = f.lock();
		f.open("w+");
		[closed, f.lock(), f.unlock(), f.lock(0, 10), f.unlock(0, 10)].join()
	`).String()
	if got != "false,true,true,true,true" {
		t.Errorf("lock results = %q", got)
	}
}

// TestFileINI reads INI values, sections and keys through File methods.
func TestFileINI(t *testing.T) {
	h := newDoor(t, doorOpts{})
	h.write("game/game.ini", "; comment\n# also comment\nroot = top\n\n[General]\nName = LORD\nlevel=12\n[Paths]\ndata = ./data\n[General]\nextra = yes\n[broken\n")
	tests := []struct{ expr, want string }{
		{`var f = new File("game.ini"); f.open("r"); f.iniGetValue("General", "Name")`, "LORD"},
		{`f.iniGetValue(null, "root")`, "top"},
		{`f.iniGetValue(undefined, "root", "d")`, "top"},
		{`f.iniGetValue("General", "extra")`, "yes"},
		{`f.iniGetValue("General", "missing", 42)`, "42"},
		{`String(f.iniGetValue("General", "missing"))`, "undefined"},
		{`String(f.iniGetValue("Nope", "x"))`, "undefined"},
		{`String(f.iniGetValue("General"))`, "undefined"},
		{`f.iniGetSections().sort().join()`, "General,Paths"},
		{`f.iniGetKeys("General").sort().join()`, "Name,extra,level"},
		{`f.iniGetKeys(null).join()`, "root"},
		{`f.iniGetKeys().join()`, "root"},
		{`f.iniGetKeys("Nope").length`, "0"},
		{`f.close(); String(f.iniGetValue("General", "Name", "closed"))`, "closed"},
		{`f.iniGetSections().length`, "0"},
		{`f.iniGetKeys("General").length`, "0"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}
}

// TestFileGlobals covers the file_* and directory helpers.
func TestFileGlobals(t *testing.T) {
	h := newDoor(t, doorOpts{})
	h.write("game/a.dat", "12345")
	h.write("game/b.dat", "x")
	h.write("game/Mixed.TXT", "m")
	tests := []struct{ expr, want string }{
		{`String(file_exists("a.dat")) + file_exists("zz") + file_exists()`, "truefalsefalse"},
		{`String(file_size("a.dat")) + "," + file_size("zz") + "," + file_size()`, "5,-1,-1"},
		{`String(file_isdir("a.dat")) + file_isdir(".") + file_isdir()`, "falsetruefalse"},
		{`directory("*.dat").map(function(p){return p.split(/[\\/]/).pop()}).sort().join()`, "a.dat,b.dat"},
		{`directory().length + directory("[").length`, "0"},
		{`String(mkdir("sub/deeper")) + file_isdir("sub/deeper") + mkdir()`, "truetruefalse"},
		{`file_getcase("mixed.txt").split(/[\\/]/).pop()`, "Mixed.TXT"},
		{`file_getcase("a.dat").split(/[\\/]/).pop()`, "a.dat"},
		{`String(file_getcase("none.txt")) + file_getcase() + file_getcase("nodir/x")`, "undefinedundefinedundefined"},
		{`String(file_rename("b.dat", "c.dat")) + file_exists("c.dat") + file_rename("zz", "yy") + file_rename("c.dat")`, "truetruefalsefalse"},
		{`String(file_remove("c.dat")) + file_remove("c.dat") + file_remove()`, "truefalsefalse"},
		{`String(file_removecase("a.dat")) + file_removecase("a.dat") + file_removecase()`, "truefalsefalse"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}
}

// TestResolveFilePath: relative paths join the working dir; absolute paths
// are cleaned. Paths outside the configured dirs are still returned (doors
// are trusted code; the resolver only logs a warning).
func TestResolveFilePath(t *testing.T) {
	h := newDoor(t, doorOpts{})
	tests := []struct{ in, want string }{
		{"x.dat", filepath.Join(h.game, "x.dat")},
		{h.path("data/../data/y.dat"), filepath.Join(h.root, "data", "y.dat")},
		{h.path("node"), filepath.Join(h.root, "node")},
		{h.path("lib/z.js"), filepath.Join(h.root, "lib", "z.js")},
		{filepath.Join(os.TempDir(), "t.tmp"), filepath.Join(os.TempDir(), "t.tmp")},
		{"../outside.dat", filepath.Join(h.root, "outside.dat")},
	}
	for _, tt := range tests {
		if got := h.eng.resolveFilePath(tt.in); got != tt.want {
			t.Errorf("resolveFilePath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// jsStr renders s as a JS string literal.
func jsStr(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
