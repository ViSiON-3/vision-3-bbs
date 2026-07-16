package syncjs

import (
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
)

// registerModuleSystem sets up load(), require(), js.*, and global utility functions.
func registerModuleSystem(vm *goja.Runtime, eng *Engine) {
	registerJSObject(vm, eng)
	registerGlobalFunctions(vm, eng)
	registerPolyfills(vm)
}

// runInternalJS executes fixed, internally-defined JS source. A failure
// indicates a programming error in the embedded source, so it is logged
// rather than propagated.
func runInternalJS(vm *goja.Runtime, src string) {
	if _, err := vm.RunString(src); err != nil {
		slog.Error("syncjs: internal JS snippet failed", "error", err)
	}
}

// registerPolyfills adds Mozilla/SpiderMonkey extensions that Synchronet scripts expect.
func registerPolyfills(vm *goja.Runtime) {
	// toSource() — Mozilla extension used by recordfile.js for deep cloning.
	// Converts a value to a source code string that eval() can recreate.
	// toSource() and getYear() polyfills must be non-enumerable so they
	// don't appear in for-in loops over arrays/objects.
	runInternalJS(vm, `
		(function() {
			function defHidden(obj, name, fn) {
				if (!obj[name]) {
					Object.defineProperty(obj, name, {value: fn, writable: true, configurable: true, enumerable: false});
				}
			}
			defHidden(Object.prototype, 'toSource', function() { return JSON.stringify(this); });
			defHidden(Array.prototype, 'toSource', function() { return JSON.stringify(this); });
			defHidden(Number.prototype, 'toSource', function() { return '(' + this.toString() + ')'; });
			defHidden(String.prototype, 'toSource', function() { return '"' + this.toString().replace(/"/g, '\\"') + '"'; });
			defHidden(Boolean.prototype, 'toSource', function() { return '(' + this.toString() + ')'; });
			defHidden(Date.prototype, 'getYear', function() { return this.getFullYear() - 1900; });
		})();
	`)

	// SpiderMonkey eval() compatibility — SpiderMonkey allows eval('function() { ... }')
	// as a function expression, but ES5 strict parsing treats bare "function" at the
	// statement level as a FunctionDeclaration requiring a name. Wrapping in parens
	// forces expression parsing. Used by Synchronet's l2lib.js (LORD II) extensively.
	runInternalJS(vm, `
		(function() {
			var _nativeEval = eval;
			this.eval = function(code) {
				if (typeof code === 'string') {
					var t = code.replace(/^\s+/, '');
					if (t.substr(0, 9) === 'function(' || t.substr(0, 10) === 'function (') {
						return _nativeEval('(' + code + ')');
					}
				}
				return _nativeEval(code);
			};
		})();
	`)
}

// registerJSObject creates the js.* namespace.
func registerJSObject(vm *goja.Runtime, eng *Engine) {
	obj := vm.NewObject()

	// js.exec_dir — directory of the currently executing script (dynamic)
	jsutil.DefineAccessor(obj, "exec_dir", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(eng.currentExecDir())
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	// js.load_path_list — mutable JS array of search paths.
	// Scripts like dorkit.js call .unshift() to prepend paths at runtime,
	// and resolveModulePath reads this live array for module resolution.
	initPaths := make([]interface{}, len(eng.cfg.LibraryPaths))
	for i, p := range eng.cfg.LibraryPaths {
		initPaths[i] = p
	}
	jsutil.Set(obj, "load_path_list", initPaths)

	// js.terminated — true when context is cancelled
	jsutil.DefineAccessor(obj, "terminated", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		select {
		case <-eng.ctx.Done():
			return vm.ToValue(true)
		default:
			return vm.ToValue(false)
		}
	}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

	// js.on_exit(callback) — register cleanup function
	jsutil.Set(obj, "on_exit", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) > 0 {
			if fn, ok := goja.AssertFunction(call.Arguments[0]); ok {
				eng.addExitHandler(fn)
			} else {
				// Synchronet evaluates string arguments as JS code on exit
				code := call.Arguments[0].String()
				eng.addExitCode(code)
			}
		}
		return goja.Undefined()
	})

	// js.global — reference to global scope
	jsutil.Set(obj, "global", vm.GlobalObject())

	// js.gc() — no-op, Go handles GC
	jsutil.Set(obj, "gc", func(call goja.FunctionCall) goja.Value {
		return goja.Undefined()
	})

	jsutil.Set(vm, "js", obj)
}

// registerGlobalFunctions adds load(), require(), and utility functions.
func registerGlobalFunctions(vm *goja.Runtime, eng *Engine) {
	// load(filename, ...) — load and execute a JS file.
	// Synchronet supports load(true, filename, ...) for background thread loading.
	// We return a stub Queue since goja is single-threaded.
	jsutil.Set(vm, "load", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(vm.NewTypeError("load() requires a filename"))
		}

		args := call.Arguments
		// load(true, filename, ...) — background thread mode.
		// Synchronet runs the script in a separate thread. Since goja is
		// single-threaded, we return an input Queue backed by session I/O.
		if bg, ok := args[0].Export().(bool); ok && bg {
			slog.Info("load(true, ...) background thread requested; returning input Queue bridge")
			return eng.createInputQueue()
		}

		filename := args[0].String()

		// Pass remaining args as load() arguments accessible in the loaded script
		var loadArgs []goja.Value
		if len(args) > 1 {
			loadArgs = args[1:]
		}

		result, err := eng.loadModule(filename, loadArgs)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return result
	})

	// require([scope], filename, symbol) — load and verify a symbol exists.
	// If the first arg is an object (not a string), it's a scope to load into.
	jsutil.Set(vm, "require", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			panic(vm.NewTypeError("require() requires a filename"))
		}

		args := call.Arguments
		var scope *goja.Object

		// Detect require(scope, filename, symbol) form:
		// if first arg exports as non-string, treat it as scope object
		if _, isStr := args[0].Export().(string); !isStr && args[0].ToObject(vm) != nil {
			scope = args[0].ToObject(vm)
			args = args[1:]
		}

		if len(args) < 1 {
			panic(vm.NewTypeError("require() requires a filename"))
		}
		filename := args[0].String()
		symbol := ""
		if len(args) > 1 {
			symbol = args[1].String()
		}

		_, err := eng.loadModule(filename, nil)
		if err != nil {
			// When loading into a scope, treat errors as non-fatal.
			// Set a stub on the scope so callers doing scope.X.method() get
			// a callable stub instead of "cannot read property of undefined".
			if scope != nil {
				slog.Warn("require failed", "filename", filename, "error", err)
				if symbol != "" {
					jsutil.Set(scope, symbol, eng.newStubObject())
				}
				return goja.Undefined()
			}
			panic(vm.NewGoError(err))
		}

		if symbol != "" {
			val := vm.Get(symbol)
			if val == nil || goja.IsUndefined(val) {
				if scope != nil {
					slog.Warn("require: symbol not found", "filename", filename, "symbol", symbol)
					jsutil.Set(scope, symbol, eng.newStubObject())
					return goja.Undefined()
				}
				panic(vm.NewTypeError(fmt.Sprintf("require: symbol '%s' not found after loading '%s'", symbol, filename)))
			}
			// If scope provided, set the symbol on the scope object
			if scope != nil {
				jsutil.Set(scope, symbol, val)
			}
		}
		return goja.Undefined()
	})

	// exit(code) — terminate script execution via goja interrupt
	jsutil.Set(vm, "exit", func(call goja.FunctionCall) goja.Value {
		eng.cancel()
		eng.vm.Interrupt(exitCode(0))
		// The interrupt is checked at the next JS instruction boundary.
		// Return undefined; the runtime will halt shortly.
		return goja.Undefined()
	})

	// random(max) — return random integer 0 to max-1
	jsutil.Set(vm, "random", func(call goja.FunctionCall) goja.Value {
		max := int64(100)
		if len(call.Arguments) > 0 {
			max = call.Arguments[0].ToInteger()
		}
		if max <= 0 {
			return vm.ToValue(0)
		}
		return vm.ToValue(rand.Int63n(max))
	})

	// time() — current Unix timestamp
	jsutil.Set(vm, "time", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(time.Now().Unix())
	})

	// sleep(ms) — sleep for milliseconds
	jsutil.Set(vm, "sleep", func(call goja.FunctionCall) goja.Value {
		ms := int64(0)
		if len(call.Arguments) > 0 {
			ms = call.Arguments[0].ToInteger()
		}
		if ms > 0 {
			select {
			case <-time.After(time.Duration(ms) * time.Millisecond):
			case <-eng.ctx.Done():
			}
		}
		return goja.Undefined()
	})

	// format(fmt, ...) — printf-style string formatting
	jsutil.Set(vm, "format", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue("")
		}
		fmtStr := call.Arguments[0].String()
		result := sprintfJS(fmtStr, call.Arguments[1:])
		return vm.ToValue(result)
	})

	// strftime(fmt, time) — format a timestamp
	jsutil.Set(vm, "strftime", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue("")
		}
		fmtStr := call.Arguments[0].String()
		ts := time.Now()
		if len(call.Arguments) > 1 {
			ts = time.Unix(call.Arguments[1].ToInteger(), 0)
		}
		return vm.ToValue(strftimeGo(fmtStr, ts))
	})

	// log([level,] msg) — log a message with optional syslog-style level
	jsutil.Set(vm, "log", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Undefined()
		}
		msg := ""
		if len(call.Arguments) >= 2 {
			// log(level, message) — Synchronet style
			msg = call.Arguments[1].String()
		} else {
			msg = call.Arguments[0].String()
		}
		slog.Info("script log", "message", msg)
		return goja.Undefined()
	})

	// Syslog-level constants used by Synchronet's log() function
	jsutil.Set(vm, "LOG_EMERG", 0)
	jsutil.Set(vm, "LOG_ALERT", 1)
	jsutil.Set(vm, "LOG_CRIT", 2)
	jsutil.Set(vm, "LOG_ERR", 3)
	jsutil.Set(vm, "LOG_ERROR", 3) // alias used by some scripts (e.g. lord2.js)
	jsutil.Set(vm, "LOG_WARNING", 4)
	jsutil.Set(vm, "LOG_NOTICE", 5)
	jsutil.Set(vm, "LOG_INFO", 6)
	jsutil.Set(vm, "LOG_DEBUG", 7)

	// alert(msg) — alias for log
	jsutil.Set(vm, "alert", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) > 0 {
			slog.Info("script alert", "message", call.Arguments[0].String())
		}
		return goja.Undefined()
	})

	// ascii(str) — return ASCII code of first character (Synchronet built-in)
	jsutil.Set(vm, "ascii", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(0)
		}
		arg := call.Arguments[0]
		// If it's a number, convert to character string
		if n, ok := arg.Export().(int64); ok {
			return vm.ToValue(string(rune(n)))
		}
		s := arg.String()
		if len(s) == 0 {
			return vm.ToValue(0)
		}
		return vm.ToValue(int(s[0]))
	})

	// ascii_str(code) — return character for ASCII code (Synchronet built-in)
	jsutil.Set(vm, "ascii_str", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue("")
		}
		code := call.Arguments[0].ToInteger()
		return vm.ToValue(string(rune(code)))
	})

	// truncsp(str) — truncate trailing spaces (Synchronet built-in)
	jsutil.Set(vm, "truncsp", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue("")
		}
		return vm.ToValue(strings.TrimRight(call.Arguments[0].String(), " \t\r\n"))
	})

	// backslash(path) — ensure path ends with directory separator (OS-native)
	jsutil.Set(vm, "backslash", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(string(filepath.Separator))
		}
		p := call.Arguments[0].String()
		if p == "" || (p[len(p)-1] != '/' && p[len(p)-1] != '\\') {
			p += string(filepath.Separator)
		}
		return vm.ToValue(p)
	})

	// mswait(ms) — alias for sleep (millisecond wait)
	jsutil.Set(vm, "mswait", func(call goja.FunctionCall) goja.Value {
		ms := int64(0)
		if len(call.Arguments) > 0 {
			ms = call.Arguments[0].ToInteger()
		}
		if ms > 0 {
			select {
			case <-time.After(time.Duration(ms) * time.Millisecond):
			case <-eng.ctx.Done():
			}
		}
		return goja.Undefined()
	})

	// file_mutex(filename [, text]) — atomically create a lock file.
	// Returns true if created, false if already exists.
	jsutil.Set(vm, "file_mutex", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(false)
		}
		path := eng.resolveFilePath(call.Arguments[0].String())
		content := ""
		if len(call.Arguments) > 1 {
			content = call.Arguments[1].String()
		}
		// O_CREATE|O_EXCL ensures atomic creation — fails if file exists
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return vm.ToValue(false)
		}
		if content != "" {
			if _, err := f.WriteString(content); err != nil {
				slog.Warn("file_mutex: writing lock-file content", "path", path, "error", err)
			}
		}
		if err := f.Close(); err != nil {
			slog.Warn("file_mutex: closing lock file", "path", path, "error", err)
		}
		// Track for cleanup on engine close
		eng.lockFiles = append(eng.lockFiles, path)
		return vm.ToValue(true)
	})

	// file_getcase(path) — case-insensitive file lookup.
	// Returns the actual path with correct case, or undefined if not found.
	jsutil.Set(vm, "file_getcase", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Undefined()
		}
		target := call.Arguments[0].String()
		resolved := eng.resolveFilePath(target)
		// First try exact match
		if _, err := os.Stat(resolved); err == nil {
			return vm.ToValue(resolved)
		}
		// Case-insensitive search in the parent directory
		dir := filepath.Dir(resolved)
		base := filepath.Base(resolved)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return goja.Undefined()
		}
		for _, e := range entries {
			if strings.EqualFold(e.Name(), base) {
				return vm.ToValue(filepath.Join(dir, e.Name()))
			}
		}
		return goja.Undefined()
	})

	// file_size(path) — return file size in bytes, or -1 if not found
	jsutil.Set(vm, "file_size", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(-1)
		}
		path := eng.resolveFilePath(call.Arguments[0].String())
		info, err := os.Stat(path)
		if err != nil {
			return vm.ToValue(-1)
		}
		return vm.ToValue(info.Size())
	})

	// file_isdir(path) — check if path is a directory
	jsutil.Set(vm, "file_isdir", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(false)
		}
		path := eng.resolveFilePath(call.Arguments[0].String())
		info, err := os.Stat(path)
		return vm.ToValue(err == nil && info.IsDir())
	})

	// mkdir(path) — create a directory (including parents)
	jsutil.Set(vm, "mkdir", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(false)
		}
		path := eng.resolveFilePath(call.Arguments[0].String())
		err := os.MkdirAll(path, 0o755)
		return vm.ToValue(err == nil)
	})

	// file_removecase(filename) — case-insensitive file removal (stub, just calls remove)
	jsutil.Set(vm, "file_removecase", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(false)
		}
		path := eng.resolveFilePath(call.Arguments[0].String())
		err := os.Remove(path)
		return vm.ToValue(err == nil)
	})

	// file_rename(oldname, newname) — rename/move a file
	jsutil.Set(vm, "file_rename", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return vm.ToValue(false)
		}
		oldPath := eng.resolveFilePath(call.Arguments[0].String())
		newPath := eng.resolveFilePath(call.Arguments[1].String())
		err := os.Rename(oldPath, newPath)
		return vm.ToValue(err == nil)
	})

	// strerror(errno) — return string description of an error number
	jsutil.Set(vm, "strerror", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue("Unknown error")
		}
		errno := int(call.Arguments[0].ToInteger())
		return vm.ToValue(fmt.Sprintf("Error %d", errno))
	})

	// argv — script arguments as a native JS Array.
	// Set via _argv_tmp + JSON.parse so for-in only enumerates numeric indices.
	if len(eng.cfg.Args) > 0 {
		// Build JSON array of strings
		parts := make([]string, len(eng.cfg.Args))
		for i, a := range eng.cfg.Args {
			// Escape for JSON string
			escaped := strings.ReplaceAll(a, `\`, `\\`)
			escaped = strings.ReplaceAll(escaped, `"`, `\"`)
			parts[i] = `"` + escaped + `"`
		}
		runInternalJS(vm, `var argv = [`+strings.Join(parts, ",")+`];`)
	} else {
		runInternalJS(vm, `var argv = [];`)
	}

	// argc
	jsutil.Set(vm, "argc", len(eng.cfg.Args))
}

// exitCode is a panic value used by exit() to halt script execution.
type exitCode int

// loadModule resolves and executes a JS file, returning its result.
func (eng *Engine) loadModule(filename string, args []goja.Value) (goja.Value, error) {
	resolved := eng.resolveModulePath(filename)
	if resolved == "" {
		return goja.Undefined(), fmt.Errorf("module not found: %s", filename)
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return goja.Undefined(), fmt.Errorf("reading module %s: %w", resolved, err)
	}

	// Push exec_dir for the loaded module
	moduleDir := filepath.Dir(resolved) + string(filepath.Separator)
	eng.pushExecDir(moduleDir)
	defer eng.popExecDir()

	// Set load arguments if provided
	if len(args) > 0 {
		for i, arg := range args {
			jsutil.Set(eng.vm, fmt.Sprintf("argv_%d", i), arg)
		}
	}

	result, err := eng.vm.RunScript(resolved, stripUseStrict(string(data)))
	if err != nil {
		return goja.Undefined(), fmt.Errorf("executing module %s: %w", resolved, err)
	}
	return result, nil
}

// stripUseStrict removes 'use strict' directives from script source code.
// Synchronet uses SpiderMonkey 1.8.5 which doesn't fully enforce strict mode,
// so many Synchronet scripts declare 'use strict' but rely on non-strict
// behavior (e.g. undeclared for-in variables, implicit globals). Goja enforces
// strict mode correctly, causing breakage. Stripping the directive restores
// Synchronet-compatible behavior.
func stripUseStrict(src string) string {
	return strings.NewReplacer(
		"'use strict';", "",
		"\"use strict\";", "",
		"'use strict'", "",
		"\"use strict\"", "",
	).Replace(src)
}

// resolveModulePath searches for a module file in configured paths.
// It reads js.load_path_list from the live JS runtime so that scripts
// which call js.load_path_list.unshift() at runtime are respected.
func (eng *Engine) resolveModulePath(filename string) string {
	// If absolute, use directly
	if filepath.IsAbs(filename) {
		if _, err := os.Stat(filename); err == nil {
			return filename
		}
		return ""
	}

	// Search order: (1) current exec_dir, (2) live js.load_path_list, (3) working directory
	searchPaths := []string{eng.currentExecDir()}
	searchPaths = append(searchPaths, eng.getLiveLoadPaths()...)
	if eng.cfg.WorkingDir != "" {
		searchPaths = append(searchPaths, eng.cfg.WorkingDir)
	}

	for _, base := range searchPaths {
		if base == "" {
			continue
		}
		candidate := filepath.Join(base, filename)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// getLiveLoadPaths reads js.load_path_list from the running JS runtime,
// returning the current array contents as Go strings.
func (eng *Engine) getLiveLoadPaths() []string {
	jsObj := eng.vm.Get("js")
	if jsObj == nil {
		return eng.cfg.LibraryPaths
	}
	obj := jsObj.ToObject(eng.vm)
	listVal := obj.Get("load_path_list")
	if listVal == nil {
		return eng.cfg.LibraryPaths
	}
	exported := listVal.Export()
	switch arr := exported.(type) {
	case []interface{}:
		paths := make([]string, 0, len(arr))
		for _, v := range arr {
			if s, ok := v.(string); ok {
				paths = append(paths, s)
			}
		}
		return paths
	case []string:
		return arr
	default:
		return eng.cfg.LibraryPaths
	}
}

// resolveFilePath resolves a file path for File class operations.
// After resolving, it validates that the path stays within allowed directories
// and logs a warning if it doesn't (but still allows it for game compatibility).
func (eng *Engine) resolveFilePath(path string) string {
	var resolved string
	if filepath.IsAbs(path) {
		resolved = filepath.Clean(path)
	} else {
		resolved = filepath.Join(eng.cfg.WorkingDir, path)
	}

	// Validate against allowed directories
	allowed := []string{
		eng.cfg.WorkingDir,
		eng.cfg.DataDir,
		eng.cfg.NodeDir,
		eng.cfg.ExecDir,
	}
	allowed = append(allowed, eng.cfg.LibraryPaths...)
	allowed = append(allowed, os.TempDir())

	for _, dir := range allowed {
		if dir == "" {
			continue
		}
		cleanDir := filepath.Clean(dir)
		if strings.HasPrefix(resolved, cleanDir+string(filepath.Separator)) || resolved == cleanDir {
			return resolved
		}
	}

	slog.Warn("resolved file path is outside all allowed directories", "path", resolved)
	return resolved
}

// sprintfJS provides basic printf-style formatting for JS format() calls.
// Go's fmt.Sprintf doesn't support %u (unsigned int), which is used by
// dorkit's local_console.js and ansi_console.js for cursor positioning.
// We convert %u → %d since the values are always non-negative integers.
func sprintfJS(fmtStr string, args []goja.Value) string {
	goArgs := make([]interface{}, len(args))
	for i, a := range args {
		goArgs[i] = a.Export()
	}
	fmtStr = strings.ReplaceAll(fmtStr, "%u", "%d")
	return fmt.Sprintf(fmtStr, goArgs...)
}

// strftimeGo converts C-style strftime format to Go time format and formats the time.
func strftimeGo(format string, t time.Time) string {
	replacer := strings.NewReplacer(
		"%Y", "2006", "%m", "01", "%d", "02",
		"%H", "15", "%M", "04", "%S", "05",
		"%p", "PM", "%I", "03",
		"%a", "Mon", "%A", "Monday",
		"%b", "Jan", "%B", "January",
		"%c", "Mon Jan _2 15:04:05 2006",
		"%x", "01/02/06", "%X", "15:04:05",
		"%Z", "MST", "%n", "\n", "%t", "\t",
		"%%", "%",
	)
	goFmt := replacer.Replace(format)
	return t.Format(goFmt)
}
