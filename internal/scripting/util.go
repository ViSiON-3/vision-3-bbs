package scripting

import (
	"math/rand"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dop251/goja"
	"github.com/stlalpha/vision3/internal/ansi"
)

// registerUtil creates the v3.util object with common utility functions.
func registerUtil(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()

	// sleep(ms) — pause execution for the specified milliseconds.
	// Respects context cancellation (returns early if user disconnects).
	obj.Set("sleep", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Undefined()
		}
		ms := call.Arguments[0].ToInteger()
		if ms <= 0 {
			return goja.Undefined()
		}
		select {
		case <-time.After(time.Duration(ms) * time.Millisecond):
		case <-eng.ctx.Done():
			panic(vm.NewGoError(ErrTerminated))
		}
		return goja.Undefined()
	})

	// random(max) — returns a random integer from 0 to max-1.
	// Note: math/rand is automatically seeded since Go 1.20 (we require 1.24+).
	obj.Set("random", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(0)
		}
		max := int(call.Arguments[0].ToInteger())
		if max <= 0 {
			return vm.ToValue(0)
		}
		return vm.ToValue(rand.Intn(max))
	})

	// time() — returns the current Unix timestamp in seconds.
	obj.Set("time", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(time.Now().Unix())
	})

	// date(format) — returns a formatted date string.
	// Format uses Go's reference time (2006-01-02 15:04:05).
	// With no arguments, returns "2006-01-02 15:04:05" format.
	obj.Set("date", func(call goja.FunctionCall) goja.Value {
		layout := "2006-01-02 15:04:05"
		if len(call.Arguments) > 0 {
			layout = call.Arguments[0].String()
		}
		return vm.ToValue(time.Now().Format(layout))
	})

	// padRight(str, width) — pad string on the right to reach width.
	// padRight(str, width, char) — pad with specified character.
	obj.Set("padRight", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return vm.ToValue("")
		}
		str := call.Arguments[0].String()
		width := int(call.Arguments[1].ToInteger())
		padChar := ' '
		if len(call.Arguments) > 2 {
			s := call.Arguments[2].String()
			if len(s) > 0 {
				padChar, _ = utf8.DecodeRuneInString(s)
			}
		}
		return vm.ToValue(ansi.PadVisible(str, width, padChar))
	})

	// padLeft(str, width) — pad string on the left to reach width.
	// padLeft(str, width, char) — pad with specified character.
	obj.Set("padLeft", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return vm.ToValue("")
		}
		str := call.Arguments[0].String()
		width := int(call.Arguments[1].ToInteger())
		padChar := ' '
		if len(call.Arguments) > 2 {
			s := call.Arguments[2].String()
			if len(s) > 0 {
				padChar, _ = utf8.DecodeRuneInString(s)
			}
		}
		visLen := ansi.VisibleLength(str)
		if visLen >= width {
			return vm.ToValue(str)
		}
		padding := strings.Repeat(string(padChar), width-visLen)
		return vm.ToValue(padding + str)
	})

	// center(str, width) — center string within the given width.
	obj.Set("center", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return vm.ToValue("")
		}
		str := call.Arguments[0].String()
		width := int(call.Arguments[1].ToInteger())
		return vm.ToValue(ansi.ApplyWidthConstraintAligned(str, width, ansi.AlignCenter))
	})

	// stripAnsi(str) — remove ANSI escape sequences from a string.
	obj.Set("stripAnsi", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue("")
		}
		return vm.ToValue(ansi.StripAnsi(call.Arguments[0].String()))
	})

	// stripPipe(str) — remove Vision/3 pipe codes from a string.
	obj.Set("stripPipe", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue("")
		}
		str := call.Arguments[0].String()
		// Process pipe codes to ANSI, then strip ANSI.
		processed := ansi.ReplacePipeCodes([]byte(str))
		return vm.ToValue(ansi.StripAnsi(string(processed)))
	})

	// displayLen(str) — returns visible display length of a string,
	// ignoring both ANSI escape sequences and pipe codes.
	obj.Set("displayLen", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(0)
		}
		str := call.Arguments[0].String()
		// Process pipe codes first, then measure visible length.
		processed := ansi.ReplacePipeCodes([]byte(str))
		return vm.ToValue(ansi.VisibleLength(string(processed)))
	})

	v3.Set("util", obj)
}
