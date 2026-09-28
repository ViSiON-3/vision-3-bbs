package scripting

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestFSRoundTrip exercises v3.fs write/append/read/exists/list/mkdir/delete
// and checks the files land inside scripts/data on disk.
func TestFSRoundTrip(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.mustRun(`
		v3.fs.mkdir("notes");
		v3.fs.write("notes/a.txt", "one");
		v3.fs.append("notes/a.txt", "+two");
		v3.fs.append("fresh.txt", "new");
		v3.fs.mkdir("sub");
		v3.fs.mkdir("sub/deeper");
	`)
	if b, err := os.ReadFile(filepath.Join(h.dataDir, "notes", "a.txt")); err != nil || string(b) != "one+two" {
		t.Fatalf("notes/a.txt = %q, %v; want %q", b, err, "one+two")
	}
	if fi, err := os.Stat(filepath.Join(h.dataDir, "sub", "deeper")); err != nil || !fi.IsDir() {
		t.Fatalf("mkdir did not create sub/deeper: %v", err)
	}

	checks := []struct{ expr, want string }{
		{`v3.fs.read("notes/a.txt")`, "one+two"},
		{`v3.fs.read("fresh.txt")`, "new"},
		{`String(v3.fs.exists("fresh.txt"))`, "true"},
		{`String(v3.fs.exists("nope.txt"))`, "false"},
		{`String(v3.fs.exists())`, "false"},
		{`String(v3.fs.exists("../x"))`, "false"},
		{`String(v3.fs.delete("fresh.txt"))`, "true"},
		{`String(v3.fs.delete("fresh.txt"))`, "false"},
		{`String(v3.fs.delete())`, "false"},
		{`String(v3.fs.delete("../test.js"))`, "false"},
		{`v3.fs.list("notes").map(function(e){return e.name+":"+e.isDir+":"+e.size}).join(",")`, "a.txt:false:7"},
		{`v3.fs.list().map(function(e){return e.name}).sort().join(",")`, "notes,sub"},
	}
	for _, c := range checks {
		if got := h.eval(c.expr).String(); got != c.want {
			t.Errorf("%s = %q, want %q", c.expr, got, c.want)
		}
	}
	// delete("../test.js") must not have removed the script outside the sandbox.
	if _, err := os.Stat(filepath.Join(h.scriptsDir, "test.js")); err != nil {
		t.Errorf("script outside sandbox was deleted: %v", err)
	}
}

// TestFSErrorsThrow checks argument validation and I/O failures surface to
// JS as catchable exceptions carrying the Go error text.
func TestFSErrorsThrow(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.mustRun(`v3.fs.write("afile", "x")`)
	// The OS error text differs between platforms (and, for a file in the
	// way of a directory, between mkdir and the create-parents path), so the
	// I/O cases only require the error to name the offending path.
	noFile := "no such file"
	if runtime.GOOS == "windows" {
		noFile = "cannot find the file specified"
	}
	tests := []struct{ expr, want string }{
		{`v3.fs.read()`, "read requires arguments: path"},
		{`v3.fs.write("x")`, "write requires arguments: path, content"},
		{`v3.fs.append("x")`, "append requires arguments: path, content"},
		{`v3.fs.mkdir()`, "mkdir requires arguments: path"},
		{`v3.fs.read("missing.txt")`, noFile},
		{`v3.fs.list("missing")`, noFile},
		{`v3.fs.mkdir("afile/child")`, "afile"},
		{`v3.fs.write("afile/child", "x")`, "afile"},
		{`v3.fs.append("afile/child", "x")`, "afile"},
	}
	for _, tt := range tests {
		if got := h.evalErr(tt.expr); !strings.Contains(got, tt.want) {
			t.Errorf("%s threw %q, want it to contain %q", tt.expr, got, tt.want)
		}
	}
	if b, err := os.ReadFile(filepath.Join(h.dataDir, "afile")); err != nil || string(b) != "x" {
		t.Errorf("afile after failed child writes = %q, %v; want it untouched", b, err)
	}
	// Errors are ordinary JS exceptions a script can catch.
	if got := h.eval(`try { v3.fs.read("missing.txt"); "no" } catch (e) { "caught" }`).String(); got != "caught" {
		t.Errorf("fs error not catchable: %q", got)
	}
}

// TestDataStore exercises v3.data get/set/delete/keys/getAll and checks the
// JSON file it persists to.
func TestDataStore(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.mustRun(`
		v3.data.set("count", 3);
		v3.data.set("obj", {a: [1, 2], b: "x"});
		v3.data.set("gone", true);
		v3.data.delete("gone");
		v3.data.set("only-key");   // ignored: needs a value
		v3.data.delete();          // ignored: needs a key
	`)
	raw, err := os.ReadFile(filepath.Join(h.dataDir, "test.json"))
	if err != nil {
		t.Fatalf("data file not written: %v", err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored["count"] != float64(3) {
		t.Fatalf("stored = %v, want count=3 and obj only", stored)
	}

	checks := map[string]string{
		`String(v3.data.get("count"))`:                                           "3",
		`v3.data.get("obj").a[1] + v3.data.get("obj").b`:                         "2x",
		`String(v3.data.get("gone"))`:                                            "undefined",
		`String(v3.data.get())`:                                                  "undefined",
		`v3.data.keys().sort().join(",")`:                                        "count,obj",
		`var all = v3.data.getAll(); all.count + all.obj.b + all.obj.a.join("")`: "3x12",
	}
	for expr, want := range checks {
		if got := h.eval(expr).String(); got != want {
			t.Errorf("%s = %q, want %q", expr, got, want)
		}
	}

	// A second engine for the same script sees the persisted values.
	eng2 := NewEngine(t.Context(), h.sc, h.eng.cfg, nil)
	defer eng2.Close()
	v, err := eng2.vm.RunString(`v3.data.get("count")`)
	if err != nil || v.ToInteger() != 3 {
		t.Errorf("second engine get(count) = %v, %v; want 3", v, err)
	}
}

// TestDataStoreCorruptAndUnwritable: a corrupt file reads as empty, and a
// failed save throws to JS.
func TestDataStoreCorruptAndUnwritable(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.writeFile("scripts/data/test.json", "{not json")
	if got := h.eval(`v3.data.keys().length`).ToInteger(); got != 0 {
		t.Errorf("corrupt store has %d keys, want 0", got)
	}
	// Replace the data dir with a file so MkdirAll fails.
	if err := os.RemoveAll(h.dataDir); err != nil {
		t.Fatal(err)
	}
	h.writeFile("scripts/data", "blocker")
	want := "not a directory"
	if runtime.GOOS == "windows" {
		want = "cannot find the path specified"
	}
	for _, expr := range []string{`v3.data.set("k", 1)`, `v3.data.delete("k")`} {
		if got := h.evalErr(expr); !strings.Contains(got, want) {
			t.Errorf("%s threw %q, want a save error", expr, got)
		}
	}
}

// TestDataFilePath maps script names and working dirs to the store location.
func TestDataFilePath(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		cfg  ScriptConfig
		want string
	}{
		{ScriptConfig{Script: "voting.js", WorkingDir: filepath.Join(root, "scripts")}, filepath.Join(root, "scripts", "data", "voting.json")},
		{ScriptConfig{Script: "sub/poll.js", WorkingDir: filepath.Join(root, "scripts", "examples")}, filepath.Join(root, "scripts", "data", "poll.json")},
		{ScriptConfig{Script: "noext", WorkingDir: filepath.Join(root, "scripts")}, filepath.Join(root, "scripts", "data", "noext.json")},
	}
	for _, tt := range tests {
		if got := dataFilePath(tt.cfg); got != tt.want {
			t.Errorf("dataFilePath(%+v) = %q, want %q", tt.cfg, got, tt.want)
		}
	}
	if sandboxRoot(tests[1].cfg) != filepath.Join(root, "scripts", "data") {
		t.Errorf("sandboxRoot for subdir = %q", sandboxRoot(tests[1].cfg))
	}
}

// TestDataFromSubdirWorkingDir: a script run from scripts/examples shares
// scripts/data with its parent.
func TestDataFromSubdirWorkingDir(t *testing.T) {
	h := newHarness(t, harnessOpts{workingDir: "scripts/examples"})
	h.mustRun(`v3.data.set("k", "v"); v3.fs.write("f.txt", "y")`)
	if _, err := os.Stat(filepath.Join(h.dataDir, "test.json")); err != nil {
		t.Errorf("data store not in scripts/data: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.dataDir, "f.txt")); err != nil {
		t.Errorf("fs sandbox not in scripts/data: %v", err)
	}
}
