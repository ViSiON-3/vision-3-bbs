package syncjs

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLoadResolution checks load() search order (exec_dir, js.load_path_list,
// working dir), absolute paths, load arguments, return values, and that
// js.exec_dir tracks the loading module.
func TestLoadResolution(t *testing.T) {
	h := newDoor(t, doorOpts{})
	h.write("lib/libmod.js", `var FROM_LIB = "lib"; js.exec_dir;`)
	h.write("game/local.js", `var FROM_GAME = argv_0 + "-" + argv_1; 7 * 6;`)
	h.write("game/sub/nested.js", `load("sibling.js"); NESTED = js.exec_dir;`)
	h.write("game/sub/sibling.js", `var SIBLING = "found-next-to-caller";`)
	abs := h.write("elsewhere/abs.js", `var ABS = true;`)

	tests := []struct{ expr, want string }{
		{`load("libmod.js") === ` + jsStr(filepath.Join(h.root, "lib")+string(filepath.Separator)), "true"},
		{`FROM_LIB`, "lib"},
		{`String(load("local.js", "a", 2))`, "42"},
		{`FROM_GAME`, "a-2"},
		{`load("sub/nested.js"); SIBLING`, "found-next-to-caller"},
		{`NESTED === ` + jsStr(filepath.Join(h.game, "sub")+string(filepath.Separator)), "true"},
		{`js.exec_dir === ` + jsStr(h.game+string(filepath.Separator)), "true"},
		{`load(` + jsStr(abs) + `); String(ABS)`, "true"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}

	errs := []struct{ expr, want string }{
		{`load()`, "load() requires a filename"},
		{`load("nope.js")`, "module not found: nope.js"},
		{`load(` + jsStr(h.path("missing/abs.js")) + `)`, "module not found"},
		{`load("sub")`, "reading module"},
	}
	h.write("game/broken.js", `throw new Error("module blew up")`)
	errs = append(errs, struct{ expr, want string }{`load("broken.js")`, "module blew up"})
	for _, tt := range errs {
		if got := h.evalErr(tt.expr); !strings.Contains(got, tt.want) {
			t.Errorf("%s threw %q, want %q", tt.expr, got, tt.want)
		}
	}
	// A failed module load pops its exec_dir.
	if got := h.eval(`js.exec_dir`).String(); got != h.game+string(filepath.Separator) {
		t.Errorf("exec_dir after failed load = %q", got)
	}
}

// TestRequire covers require() with and without a scope object, symbol
// verification, and the stub fallback when a scoped require fails.
func TestRequire(t *testing.T) {
	h := newDoor(t, doorOpts{})
	h.write("lib/sym.js", `var MySym = {hello: function(){ return "hi"; }};`)
	h.write("lib/nosym.js", `var Other = 1;`)
	tests := []struct{ expr, want string }{
		{`require("sym.js", "MySym"); MySym.hello()`, "hi"},
		{`require("nosym.js"); String(Other)`, "1"},
		{`var sc = {}; require(sc, "sym.js", "MySym"); sc.MySym.hello()`, "hi"},
		{`var s2 = {}; require(s2, "absent.js", "Thing"); typeof s2.Thing`, "object"},
		{`typeof s2.Thing.deep.method().chain`, "function"},
		{`"" + s2.Thing.anything`, ""},
		{`var s3 = {}; require(s3, "nosym.js", "Missing"); typeof s3.Missing.run()`, "object"},
		{`var s4 = {}; require(s4, "absent.js"); Object.keys(s4).length`, "0"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}
	errs := []struct{ expr, want string }{
		{`require()`, "require() requires a filename"},
		{`require({})`, "require() requires a filename"},
		{`require("absent.js")`, "module not found"},
		{`require("nosym.js", "Missing")`, "symbol 'Missing' not found"},
	}
	for _, tt := range errs {
		if got := h.evalErr(tt.expr); !strings.Contains(got, tt.want) {
			t.Errorf("%s threw %q, want %q", tt.expr, got, tt.want)
		}
	}
}

// TestLiveLoadPaths: getLiveLoadPaths follows the live JS array and falls
// back to config when js.load_path_list is replaced with a non-array.
func TestLiveLoadPaths(t *testing.T) {
	h := newDoor(t, doorOpts{})
	h.eval(`js.load_path_list.push(42); js.load_path_list.unshift("/first")`)
	got := h.eng.getLiveLoadPaths()
	lib := filepath.Join(h.root, "lib") // configured LibraryPaths are native paths
	if len(got) != 2 || got[0] != "/first" || got[1] != lib {
		t.Errorf("live paths = %q (non-strings must be skipped)", got)
	}
	h.eval(`js.load_path_list = "nonsense"`)
	if got := h.eng.getLiveLoadPaths(); len(got) != 1 || got[0] != lib {
		t.Errorf("fallback paths = %q, want configured LibraryPaths", got)
	}
	h.eval(`delete js.load_path_list`)
	if got := h.eng.getLiveLoadPaths(); len(got) != 1 {
		t.Errorf("paths with list deleted = %q, want config", got)
	}
}

// TestGlobalFunctions table-tests the Synchronet global helpers.
func TestGlobalFunctions(t *testing.T) {
	h := newDoor(t, doorOpts{args: []string{`a"b`, `c\d`}})
	sep := string(filepath.Separator)
	tests := []struct{ expr, want string }{
		{`String(random(0)) + random(-3)`, "00"},
		{`String(random(1))`, "0"},
		{`var r = random(); r >= 0 && r < 100`, "true"},
		{`Math.abs(time() - Date.now() / 1000) < 3`, "true"},
		{`String(sleep()) + sleep(0) + sleep(1) + mswait() + mswait(1)`, "undefinedundefinedundefinedundefinedundefined"},
		{`format()`, ""},
		{`format("%u/%s/%d", 7, "x", 3)`, "7/x/3"},
		{`strftime()`, ""},
		{`strftime("%Y-%m-%d %H:%M:%S %% %y", 0).length > 0`, "true"},
		{`String(log("msg")) + log(LOG_ERR, "msg") + log() + alert("a") + alert()`, "undefinedundefinedundefinedundefinedundefined"},
		{`[LOG_EMERG, LOG_ALERT, LOG_CRIT, LOG_ERR, LOG_ERROR, LOG_WARNING, LOG_NOTICE, LOG_INFO, LOG_DEBUG].join()`, "0,1,2,3,3,4,5,6,7"},
		{`String(ascii("A")) + "," + ascii(66) + "," + ascii("") + "," + ascii()`, "65,B,0,0"},
		{`ascii_str(67) + ascii_str()`, "C"},
		{`truncsp("hi  \t\r\n") + "|" + truncsp()`, "hi|"},
		{`backslash("dir") + "|" + backslash("dir/") + "|" + backslash("d\\") + "|" + backslash("") + "|" + backslash()`,
			"dir" + sep + "|dir/|d\\|" + sep + "|" + sep},
		{`strerror(2) + "|" + strerror()`, "Error 2|Unknown error"},
		{`argc + ":" + argv.join("|")`, `2:a"b|c\d`},
		{`var n = 0; for (var k in argv) n++; String(n)`, "2"},
		{`String(js.gc())`, "undefined"},
		{`js.global === this`, "true"},
		{`({a: 1}).toSource() + [1, "x"].toSource()`, `{"a":1}[1,"x"]`},
		{`[5, true, "q\"", {a: [1]}].every(function(v){ return JSON.stringify(eval("(" + v.toSource() + ")")) === JSON.stringify(v) })`, "true"},
		{`new Date(2020, 0, 1).getYear()`, "120"},
		{`var c = 0; for (var k in [1]) c++; for (var k in {x: 1}) c++; String(c)`, "2"},
		{`eval("function() { return 9; }")()`, "9"},
		{`eval("  function (a) { return a; }")(3)`, "3"},
		{`String(eval(5))`, "5"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}

	// No args: argv is an empty array and argc 0.
	h2 := newDoor(t, doorOpts{})
	if got := h2.eval(`argc + ":" + argv.length`).String(); got != "0:0" {
		t.Errorf("argc:argv.length = %q, want 0:0", got)
	}
}

// TestSleepAbortsOnCancel: sleep and mswait return promptly when the
// engine is cancelled mid-wait.
func TestSleepAbortsOnCancel(t *testing.T) {
	for _, fn := range []string{"sleep", "mswait"} {
		h := newDoor(t, doorOpts{})
		go func() {
			time.Sleep(20 * time.Millisecond)
			h.eng.cancel()
		}()
		start := time.Now()
		_ = h.run(fn + `(60000)`)
		if el := time.Since(start); el > 5*time.Second {
			t.Errorf("%s not interrupted: %v", fn, el)
		}
	}
}

// TestStrftimeGo converts C strftime directives to Go layouts.
func TestStrftimeGo(t *testing.T) {
	ts := time.Date(2024, 3, 5, 14, 7, 9, 0, time.UTC)
	tests := map[string]string{
		"%Y-%m-%d":   "2024-03-05",
		"%H:%M:%S":   "14:07:09",
		"%I %p":      "02 PM",
		"%a %A":      "Tue Tuesday",
		"%b %B":      "Mar March",
		"%x %X":      "03/05/24 14:07:09",
		"%Z":         "UTC",
		"a%nb%tc%%":  "a\nb\tc%",
		"%c":         "Tue Mar  5 14:07:09 2024",
		"plain text": "plain text",
	}
	for in, want := range tests {
		if got := strftimeGo(in, ts); got != want {
			t.Errorf("strftimeGo(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestQueueClass covers the in-memory Queue: FIFO order, peek, data_waiting
// and poll/read falling back to session input.
func TestQueueClass(t *testing.T) {
	h := newDoor(t, doorOpts{input: "k\x1b[A"})
	tests := []struct{ expr, want string }{
		{`var q = new Queue(); String(q.data_waiting) + q.poll(1000)`, "falsetrue"},
		{`q.read()`, "k"},
		{`q.write("one"); q.write({n: 2}); q.write(); String(q.data_waiting) + q.peek()`, "trueone"},
		{`q.read() + q.read().n + q.read()`, "one2undefined"},
		{`String(q.peek())`, "undefined"},
		{`q.read() === "KEY_UP\x00\x1b[A"`, "true"},
		{`String(q.poll(10)) + q.poll(0)`, "falsefalse"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}
}

// TestQueueWakesPoller: a notify channel on the queue is signalled by write
// without blocking when nobody is listening.
func TestQueueWakesPoller(t *testing.T) {
	q := &jsQueue{notify: make(chan struct{}, 1)}
	q.write(nil)
	q.write(nil) // channel full: must not block
	select {
	case <-q.notify:
	default:
		t.Fatal("write did not signal notify")
	}
	if len(q.items) != 2 {
		t.Errorf("items = %d, want 2", len(q.items))
	}
}

// TestQueueSyntheticDSR: after a script sends a cursor-position query, the
// next Queue poll/read returns DORKit's POSITION reply for the screen size.
func TestQueueSyntheticDSR(t *testing.T) {
	h := newDoor(t, doorOpts{session: func(sc *SessionContext) { sc.ScreenWidth, sc.ScreenHeight = 132, 50 }})
	got := h.eval(`console.write("\x1b[6n"); var q = new Queue(); q.poll(0) + "|" + JSON.stringify(q.read())`).String()
	if want := `true|"POSITION_50_132\u0000\u001b[50;132R"`; got != want {
		t.Errorf("DSR reply = %s, want %s", got, want)
	}
	if h.eng.pendingDSR {
		t.Error("pendingDSR not cleared after read")
	}
	if h.output() != "" {
		t.Errorf("DSR query leaked to the terminal: %q", h.output())
	}
}

// TestQueueReadAfterDisconnect returns undefined instead of blocking.
func TestQueueReadAfterDisconnect(t *testing.T) {
	h := newDoor(t, doorOpts{})
	h.disconnect()
	v, err := h.eng.vm.RunString(`new Queue().read()`)
	if err == nil && v.String() != "undefined" {
		t.Errorf("read after hangup = %v, want undefined", v)
	}
}

// TestBackgroundLoadInputQueue: load(true, ...) returns an input bridge
// whose poll/read are backed by session input.
func TestBackgroundLoadInputQueue(t *testing.T) {
	h := newDoor(t, doorOpts{input: "xy"})
	tests := []struct{ expr, want string }{
		{`var iq = load(true, "input_thread.js"); String(iq.poll(1000))`, "true"},
		{`iq.read() + iq.read()`, "xy"},
		{`String(iq.poll(10)) + iq.poll()`, "falsefalse"},
		{`String(iq.write("stop"))`, "true"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}
	// With nothing buffered, read() blocks for input; once the caller is
	// gone it returns undefined (or the VM is interrupted).
	h.disconnect()
	if v, err := h.eng.vm.RunString(`iq.read()`); err == nil && v.String() != "undefined" {
		t.Errorf("read after hangup = %v", v)
	}
	h2 := newDoor(t, doorOpts{})
	h2.disconnect()
	if v, err := h2.eng.vm.RunString(`load(true, "t.js").poll(100)`); err == nil && v.ToBoolean() {
		t.Error("poll after hangup = true")
	}
}
