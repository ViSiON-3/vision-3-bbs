package syncjs

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dop251/goja"
)

// registerConsole creates the Synchronet-compatible console object on the JS runtime.
func registerConsole(vm *goja.Runtime, eng *Engine) {
	obj := vm.NewObject()

	// --- Properties ---

	// console.screen_columns / console.screen_rows
	obj.Set("screen_columns", eng.session.ScreenWidth)
	obj.Set("screen_rows", eng.session.ScreenHeight)

	// console.line_counter (read/write)
	obj.Set("line_counter", 0)

	// console.attributes (get/set) — Synchronet attribute byte
	obj.DefineAccessorProperty("attributes", vm.ToValue(func(call goja.FunctionCall) goja.Value {
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

	obj.Set("write", func(call goja.FunctionCall) goja.Value {
		eng.writeRaw(argsToString(call))
		return goja.Undefined()
	})

	obj.Set("writeln", func(call goja.FunctionCall) goja.Value {
		eng.writeRaw(argsToString(call) + "\r\n")
		return goja.Undefined()
	})

	obj.Set("print", func(call goja.FunctionCall) goja.Value {
		text := argsToString(call)
		eng.writeRaw(ParseCtrlA(text))
		return goja.Undefined()
	})

	obj.Set("clear", func(call goja.FunctionCall) goja.Value {
		eng.writeRaw("\x1b[2J\x1b[H")
		return goja.Undefined()
	})

	obj.Set("home", func(call goja.FunctionCall) goja.Value {
		eng.writeRaw("\x1b[H")
		return goja.Undefined()
	})

	obj.Set("cleartoeol", func(call goja.FunctionCall) goja.Value {
		eng.writeRaw("\x1b[K")
		return goja.Undefined()
	})

	obj.Set("gotoxy", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return goja.Undefined()
		}
		x := call.Arguments[0].ToInteger()
		y := call.Arguments[1].ToInteger()
		eng.writeRaw(fmt.Sprintf("\x1b[%d;%dH", y, x))
		return goja.Undefined()
	})

	obj.Set("center", func(call goja.FunctionCall) goja.Value {
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

	obj.Set("strlen", func(call goja.FunctionCall) goja.Value {
		text := argsToString(call)
		return vm.ToValue(displayLength(text))
	})

	// --- Input Methods ---

	obj.Set("getkey", func(call goja.FunctionCall) goja.Value {
		key, err := eng.readKey(0)
		if err != nil {
			return vm.ToValue("")
		}
		return vm.ToValue(key)
	})

	obj.Set("inkey", func(call goja.FunctionCall) goja.Value {
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

	obj.Set("getstr", func(call goja.FunctionCall) goja.Value {
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

	obj.Set("getkeys", func(call goja.FunctionCall) goja.Value {
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

	obj.Set("pause", func(call goja.FunctionCall) goja.Value {
		eng.writeRaw("\r\n[Hit a key] ")
		eng.readKey(0) //nolint:errcheck
		eng.writeRaw("\r\n")
		return goja.Undefined()
	})

	// console.noyes(prompt) — returns true for No (default), false for Yes
	obj.Set("noyes", func(call goja.FunctionCall) goja.Value {
		prompt := argsToString(call)
		eng.writeRaw(prompt + " (N/y)? ")
		key, _ := eng.readKey(0)
		eng.writeRaw("\r\n")
		return vm.ToValue(strings.ToUpper(key) != "Y")
	})

	// console.yesno(prompt) — returns true for Yes (default), false for No
	obj.Set("yesno", func(call goja.FunctionCall) goja.Value {
		prompt := argsToString(call)
		eng.writeRaw(prompt + " (Y/n)? ")
		key, _ := eng.readKey(0)
		eng.writeRaw("\r\n")
		return vm.ToValue(strings.ToUpper(key) != "N")
	})

	// console.getnum(max) — reads a number up to max
	obj.Set("getnum", func(call goja.FunctionCall) goja.Value {
		maxVal := 0
		if len(call.Arguments) > 0 {
			maxVal = int(call.Arguments[0].ToInteger())
		}
		result, err := eng.readLine(len(fmt.Sprintf("%d", maxVal)), kNumber)
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

	// console.ctrlkey_passthru — read/write, controls which ctrl keys are passed through
	obj.Set("ctrlkey_passthru", 0)

	// console.autoterm — terminal auto-detection flags (USER_ANSI=1, USER_UTF8=4)
	obj.Set("autoterm", 1) // ANSI enabled

	// Cursor movement methods used by sbbs_console.js
	obj.Set("right", func(call goja.FunctionCall) goja.Value {
		n := int64(1)
		if len(call.Arguments) > 0 {
			n = call.Arguments[0].ToInteger()
		}
		if n > 0 {
			eng.writeRaw(fmt.Sprintf("\x1b[%dC", n))
		}
		return goja.Undefined()
	})

	obj.Set("left", func(call goja.FunctionCall) goja.Value {
		n := int64(1)
		if len(call.Arguments) > 0 {
			n = call.Arguments[0].ToInteger()
		}
		if n > 0 {
			eng.writeRaw(fmt.Sprintf("\x1b[%dD", n))
		}
		return goja.Undefined()
	})

	obj.Set("up", func(call goja.FunctionCall) goja.Value {
		n := int64(1)
		if len(call.Arguments) > 0 {
			n = call.Arguments[0].ToInteger()
		}
		if n > 0 {
			eng.writeRaw(fmt.Sprintf("\x1b[%dA", n))
		}
		return goja.Undefined()
	})

	obj.Set("down", func(call goja.FunctionCall) goja.Value {
		n := int64(1)
		if len(call.Arguments) > 0 {
			n = call.Arguments[0].ToInteger()
		}
		if n > 0 {
			eng.writeRaw(fmt.Sprintf("\x1b[%dB", n))
		}
		return goja.Undefined()
	})

	vm.Set("console", obj)
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
