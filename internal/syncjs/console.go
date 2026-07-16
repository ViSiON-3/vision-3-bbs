package syncjs

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
)

// registerConsole creates the Synchronet-compatible console object on the JS runtime.
func registerConsole(vm *goja.Runtime, eng *Engine) {
	obj := vm.NewObject()

	// --- Properties ---

	// console.screen_columns / console.screen_rows
	jsutil.Set(obj, "screen_columns", eng.session.ScreenWidth)
	jsutil.Set(obj, "screen_rows", eng.session.ScreenHeight)

	// console.line_counter (read/write)
	jsutil.Set(obj, "line_counter", 0)

	// console.attributes (get/set) — Synchronet attribute byte
	jsutil.DefineAccessor(obj, "attributes", vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(eng.currentAttr)
	}), vm.ToValue(func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) > 0 {
			attr := uint8(call.Arguments[0].ToInteger())
			eng.currentAttr = attr
			eng.writeRaw(AttrToANSI(attr))
		}
		return goja.Undefined()
	}), goja.FLAG_FALSE, goja.FLAG_TRUE)

	// --- Output Methods ---

	jsutil.Set(obj, "write", func(call goja.FunctionCall) goja.Value {
		eng.writeRaw(argsToString(call))
		return goja.Undefined()
	})

	jsutil.Set(obj, "writeln", func(call goja.FunctionCall) goja.Value {
		eng.writeRaw(argsToString(call) + "\r\n")
		return goja.Undefined()
	})

	jsutil.Set(obj, "print", func(call goja.FunctionCall) goja.Value {
		text := argsToString(call)
		eng.writeRaw(ParseCtrlA(text))
		return goja.Undefined()
	})

	jsutil.Set(obj, "clear", func(call goja.FunctionCall) goja.Value {
		eng.writeRaw("\x1b[2J\x1b[H")
		return goja.Undefined()
	})

	jsutil.Set(obj, "home", func(call goja.FunctionCall) goja.Value {
		eng.writeRaw("\x1b[H")
		return goja.Undefined()
	})

	jsutil.Set(obj, "cleartoeol", func(call goja.FunctionCall) goja.Value {
		eng.writeRaw("\x1b[K")
		return goja.Undefined()
	})

	jsutil.Set(obj, "gotoxy", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return goja.Undefined()
		}
		x := call.Arguments[0].ToInteger()
		y := call.Arguments[1].ToInteger()
		eng.writeRaw(fmt.Sprintf("\x1b[%d;%dH", y, x))
		return goja.Undefined()
	})

	jsutil.Set(obj, "center", func(call goja.FunctionCall) goja.Value {
		text := argsToString(call)
		displayLen := displayLength(text)
		cols := eng.session.ScreenWidth
		if displayLen < cols {
			pad := (cols - displayLen) / 2
			eng.writeRaw(strings.Repeat(" ", pad))
		}
		eng.writeRaw(text + "\r\n")
		return goja.Undefined()
	})

	jsutil.Set(obj, "strlen", func(call goja.FunctionCall) goja.Value {
		text := argsToString(call)
		return vm.ToValue(displayLength(text))
	})

	// --- Input Methods ---

	jsutil.Set(obj, "getkey", func(call goja.FunctionCall) goja.Value {
		key, err := eng.readKey(0)
		if err != nil {
			return vm.ToValue("")
		}
		return vm.ToValue(key)
	})

	jsutil.Set(obj, "inkey", func(call goja.FunctionCall) goja.Value {
		timeout := int64(0)
		if len(call.Arguments) > 0 {
			timeout = call.Arguments[0].ToInteger()
		}
		key, err := eng.readKey(time.Duration(timeout) * time.Millisecond)
		if err != nil {
			return vm.ToValue("")
		}
		return vm.ToValue(key)
	})

	jsutil.Set(obj, "getstr", func(call goja.FunctionCall) goja.Value {
		maxLen := 128
		mode := int64(0)
		if len(call.Arguments) > 0 {
			maxLen = int(call.Arguments[0].ToInteger())
		}
		if len(call.Arguments) > 1 {
			mode = call.Arguments[1].ToInteger()
		}
		result, err := eng.readLine(maxLen, mode)
		if err != nil {
			return vm.ToValue("")
		}
		return vm.ToValue(result)
	})

	jsutil.Set(obj, "getkeys", func(call goja.FunctionCall) goja.Value {
		validKeys := ""
		if len(call.Arguments) > 0 {
			validKeys = strings.ToUpper(call.Arguments[0].String())
		}
		for {
			key, err := eng.readKey(0)
			if err != nil {
				panic(vm.NewGoError(err))
			}
			upper := strings.ToUpper(key)
			if validKeys == "" || strings.Contains(validKeys, upper) {
				return vm.ToValue(upper)
			}
		}
	})

	jsutil.Set(obj, "pause", func(call goja.FunctionCall) goja.Value {
		eng.writeRaw("\r\n[Hit a key] ")
		eng.readKey(0) //nolint:errcheck
		eng.writeRaw("\r\n")
		return goja.Undefined()
	})

	// console.noyes(prompt) — returns true for No (default), false for Yes
	jsutil.Set(obj, "noyes", func(call goja.FunctionCall) goja.Value {
		prompt := argsToString(call)
		eng.writeRaw(prompt + " (N/y)? ")
		key, _ := eng.readKey(0)
		eng.writeRaw("\r\n")
		return vm.ToValue(strings.ToUpper(key) != "Y")
	})

	// console.yesno(prompt) — returns true for Yes (default), false for No
	jsutil.Set(obj, "yesno", func(call goja.FunctionCall) goja.Value {
		prompt := argsToString(call)
		eng.writeRaw(prompt + " (Y/n)? ")
		key, _ := eng.readKey(0)
		eng.writeRaw("\r\n")
		return vm.ToValue(strings.ToUpper(key) != "N")
	})

	// console.getnum(max) — reads a number up to max
	jsutil.Set(obj, "getnum", func(call goja.FunctionCall) goja.Value {
		maxVal := 0
		if len(call.Arguments) > 0 {
			maxVal = int(call.Arguments[0].ToInteger())
		}
		result, err := eng.readLine(len(fmt.Sprintf("%d", maxVal)), kNumber)
		if err != nil || result == "" {
			return vm.ToValue(0)
		}
		n := 0
		_, _ = fmt.Sscanf(result, "%d", &n) // best-effort parse; n stays 0 on failure
		if maxVal > 0 && n > maxVal {
			n = maxVal
		}
		return vm.ToValue(n)
	})

	// console.ctrlkey_passthru — read/write, controls which ctrl keys are passed through
	jsutil.Set(obj, "ctrlkey_passthru", 0)

	// console.autoterm — terminal auto-detection flags (USER_ANSI=1, USER_UTF8=4)
	jsutil.Set(obj, "autoterm", 1) // ANSI enabled

	// Cursor movement methods used by sbbs_console.js
	jsutil.Set(obj, "right", func(call goja.FunctionCall) goja.Value {
		n := int64(1)
		if len(call.Arguments) > 0 {
			n = call.Arguments[0].ToInteger()
		}
		if n > 0 {
			eng.writeRaw(fmt.Sprintf("\x1b[%dC", n))
		}
		return goja.Undefined()
	})

	jsutil.Set(obj, "left", func(call goja.FunctionCall) goja.Value {
		n := int64(1)
		if len(call.Arguments) > 0 {
			n = call.Arguments[0].ToInteger()
		}
		if n > 0 {
			eng.writeRaw(fmt.Sprintf("\x1b[%dD", n))
		}
		return goja.Undefined()
	})

	jsutil.Set(obj, "up", func(call goja.FunctionCall) goja.Value {
		n := int64(1)
		if len(call.Arguments) > 0 {
			n = call.Arguments[0].ToInteger()
		}
		if n > 0 {
			eng.writeRaw(fmt.Sprintf("\x1b[%dA", n))
		}
		return goja.Undefined()
	})

	jsutil.Set(obj, "down", func(call goja.FunctionCall) goja.Value {
		n := int64(1)
		if len(call.Arguments) > 0 {
			n = call.Arguments[0].ToInteger()
		}
		if n > 0 {
			eng.writeRaw(fmt.Sprintf("\x1b[%dB", n))
		}
		return goja.Undefined()
	})

	jsutil.Set(vm, "console", obj)
}

// Input mode flags matching Synchronet's sbbsdefs.js K_* constants.
const (
	kUpper  = 1 << 0 // Convert to uppercase
	kNumber = 1 << 2 // Allow numbers only
	kNoEcho = 1 << 4 // Don't echo input
	kNoCRLF = 1 << 5 // Don't add CRLF after input
	kEdit   = 1 << 6 // Edit existing string
	kTrim   = 1 << 8 // Trim whitespace
)

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
// stripping ANSI escape sequences and Ctrl-A codes.
func displayLength(s string) int {
	// Strip Ctrl-A codes first
	s = StripCtrlA(s)
	// Strip ANSI escape sequences
	result := 0
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			// Skip ANSI CSI sequence
			i += 2
			for i < len(s) && !isANSITerminator(s[i]) {
				i++
			}
			if i < len(s) {
				i++ // skip terminator
			}
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		result++
		i += size
	}
	return result
}

func isANSITerminator(b byte) bool {
	return b >= 0x40 && b <= 0x7E
}
