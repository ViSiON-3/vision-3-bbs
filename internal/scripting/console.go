package scripting

import (
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/stlalpha/vision3/internal/ansi"
)

// registerConsole creates the v3.console object with terminal I/O bindings.
func registerConsole(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()

	// --- Properties ---

	obj.Set("width", eng.session.ScreenWidth)
	obj.Set("height", eng.session.ScreenHeight)

	// --- Output ---

	// write(text) — raw output, no pipe-code processing
	obj.Set("write", func(call goja.FunctionCall) goja.Value {
		eng.writeRaw(argsToString(call))
		return goja.Undefined()
	})

	// writeln(text) — raw output + CRLF
	obj.Set("writeln", func(call goja.FunctionCall) goja.Value {
		eng.writeRaw(argsToString(call) + "\r\n")
		return goja.Undefined()
	})

	// print(text) — output with pipe-code processing (|07, |09, etc.)
	obj.Set("print", func(call goja.FunctionCall) goja.Value {
		text := argsToString(call)
		processed := ansi.ReplacePipeCodes([]byte(text))
		eng.writeRaw(string(processed))
		return goja.Undefined()
	})

	// println(text) — print with pipe-codes + CRLF
	obj.Set("println", func(call goja.FunctionCall) goja.Value {
		text := argsToString(call)
		processed := ansi.ReplacePipeCodes([]byte(text))
		eng.writeRaw(string(processed) + "\r\n")
		return goja.Undefined()
	})

	// clear() / cls() — clear screen
	clearFn := func(call goja.FunctionCall) goja.Value {
		eng.writeRaw("\x1b[2J\x1b[H")
		return goja.Undefined()
	}
	obj.Set("clear", clearFn)
	obj.Set("cls", clearFn)

	// gotoxy(x, y) — cursor positioning (1-based)
	obj.Set("gotoxy", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return goja.Undefined()
		}
		x := call.Arguments[0].ToInteger()
		y := call.Arguments[1].ToInteger()
		eng.writeRaw(fmt.Sprintf("\x1b[%d;%dH", y, x))
		return goja.Undefined()
	})

	// color(fg) or color(fg, bg) — set color by number
	obj.Set("color", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Undefined()
		}
		fg := int(call.Arguments[0].ToInteger())
		code := fmt.Sprintf("|%02d", fg)
		if len(call.Arguments) > 1 {
			bg := int(call.Arguments[1].ToInteger())
			code += fmt.Sprintf("|B%d", bg)
		}
		processed := ansi.ReplacePipeCodes([]byte(code))
		eng.writeRaw(string(processed))
		return goja.Undefined()
	})

	// reset() — reset terminal attributes
	obj.Set("reset", func(call goja.FunctionCall) goja.Value {
		eng.writeRaw("\x1b[0m")
		return goja.Undefined()
	})

	// center(text) — center text on screen using pipe-code-aware width
	obj.Set("center", func(call goja.FunctionCall) goja.Value {
		text := argsToString(call)
		processed := ansi.ReplacePipeCodes([]byte(text))
		displayLen := displayLength(string(processed))
		cols := eng.session.ScreenWidth
		if displayLen < cols {
			pad := (cols - displayLen) / 2
			eng.writeRaw(strings.Repeat(" ", pad))
		}
		eng.writeRaw(string(processed) + "\r\n")
		return goja.Undefined()
	})

	// --- Input ---

	// getkey() or getkey(timeout_ms) — single key read
	obj.Set("getkey", func(call goja.FunctionCall) goja.Value {
		timeout := time.Duration(0)
		if len(call.Arguments) > 0 {
			ms := call.Arguments[0].ToInteger()
			if ms > 0 {
				timeout = time.Duration(ms) * time.Millisecond
			}
		}
		key, err := eng.readKey(timeout)
		if err != nil {
			return vm.ToValue("")
		}
		return vm.ToValue(key)
	})

	// getstr(maxlen) or getstr(maxlen, opts) — line input with editing
	// opts: {echo: false, upper: true, number: true}
	obj.Set("getstr", func(call goja.FunctionCall) goja.Value {
		maxLen := 128
		var opts lineOpts
		if len(call.Arguments) > 0 {
			maxLen = int(call.Arguments[0].ToInteger())
		}
		if len(call.Arguments) > 1 {
			optsObj := call.Arguments[1].ToObject(vm)
			if v := optsObj.Get("echo"); v != nil && !v.Equals(goja.Undefined()) {
				opts.noEcho = !v.ToBoolean()
			}
			if v := optsObj.Get("upper"); v != nil && !v.Equals(goja.Undefined()) {
				opts.upper = v.ToBoolean()
			}
			if v := optsObj.Get("number"); v != nil && !v.Equals(goja.Undefined()) {
				opts.numberOnly = v.ToBoolean()
			}
		}
		result, err := eng.readLine(maxLen, opts)
		if err != nil {
			return vm.ToValue("")
		}
		return vm.ToValue(result)
	})

	// yesno(prompt) — Y/n prompt, returns true for Yes (default)
	obj.Set("yesno", func(call goja.FunctionCall) goja.Value {
		prompt := argsToString(call)
		processed := ansi.ReplacePipeCodes([]byte(prompt + " (Y/n)? "))
		eng.writeRaw(string(processed))
		key, _ := eng.readKey(0)
		eng.writeRaw("\r\n")
		return vm.ToValue(strings.ToUpper(key) != "N")
	})

	// noyes(prompt) — N/y prompt, returns true for No (default)
	obj.Set("noyes", func(call goja.FunctionCall) goja.Value {
		prompt := argsToString(call)
		processed := ansi.ReplacePipeCodes([]byte(prompt + " (N/y)? "))
		eng.writeRaw(string(processed))
		key, _ := eng.readKey(0)
		eng.writeRaw("\r\n")
		return vm.ToValue(strings.ToUpper(key) != "Y")
	})

	// pause() — "press any key" prompt
	obj.Set("pause", func(call goja.FunctionCall) goja.Value {
		eng.writeRaw("\r\n[Press any key] ")
		eng.readKey(0) //nolint:errcheck
		eng.writeRaw("\r\n")
		return goja.Undefined()
	})

	// getnum(max) — read a number up to max
	obj.Set("getnum", func(call goja.FunctionCall) goja.Value {
		maxVal := 0
		if len(call.Arguments) > 0 {
			maxVal = int(call.Arguments[0].ToInteger())
		}
		// When maxVal is 0 (no limit), allow up to 10 digits.
		maxLen := len(fmt.Sprintf("%d", maxVal))
		if maxVal == 0 {
			maxLen = 10
		}
		result, err := eng.readLine(maxLen, lineOpts{numberOnly: true})
		if err != nil || result == "" {
			return vm.ToValue(0)
		}
		n := 0
		fmt.Sscanf(result, "%d", &n)
		if maxVal > 0 && n > maxVal {
			n = maxVal
		}
		return vm.ToValue(n)
	})

	v3.Set("console", obj)
}

// argsToString concatenates all JS function arguments into a single string.
func argsToString(call goja.FunctionCall) string {
	if len(call.Arguments) == 0 {
		return ""
	}
	if len(call.Arguments) == 1 {
		return call.Arguments[0].String()
	}
	var b strings.Builder
	for _, arg := range call.Arguments {
		b.WriteString(arg.String())
	}
	return b.String()
}

// displayLength returns the visible display length of a string,
// stripping ANSI escape sequences.
func displayLength(s string) int {
	return ansi.VisibleLength(s)
}
