package scripting

import (
	"math/rand"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
	"github.com/rivo/uniseg"
)

// registerUtil creates the v3.util object with common utility functions.
func registerUtil(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()

	// sleep(ms) — pause execution for the specified milliseconds.
	// Respects context cancellation (returns early if user disconnects).
	jsutil.Set(obj, "sleep", func(call goja.FunctionCall) goja.Value {
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
	jsutil.Set(obj, "random", func(call goja.FunctionCall) goja.Value {
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
	jsutil.Set(obj, "time", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(time.Now().Unix())
	})

	// date(format) — returns a formatted date string.
	// Format uses Go's reference time (2006-01-02 15:04:05).
	// With no arguments, returns "2006-01-02 15:04:05" format.
	jsutil.Set(obj, "date", func(call goja.FunctionCall) goja.Value {
		layout := "2006-01-02 15:04:05"
		if len(call.Arguments) > 0 {
			layout = call.Arguments[0].String()
		}
		return vm.ToValue(time.Now().Format(layout))
	})

	// padRight(str, width) — pad string on the right to reach width.
	// padRight(str, width, char) — pad with specified character.
	jsutil.Set(obj, "padRight", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return vm.ToValue("")
		}
		str := call.Arguments[0].String()
		width := int(call.Arguments[1].ToInteger())
		checkWidth(vm, "v3.util.padRight", width)
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
	jsutil.Set(obj, "padLeft", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return vm.ToValue("")
		}
		str := call.Arguments[0].String()
		width := int(call.Arguments[1].ToInteger())
		checkWidth(vm, "v3.util.padLeft", width)
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
	jsutil.Set(obj, "center", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return vm.ToValue("")
		}
		str := call.Arguments[0].String()
		width := int(call.Arguments[1].ToInteger())
		checkWidth(vm, "v3.util.center", width)
		return vm.ToValue(ansi.ApplyWidthConstraintAligned(str, width, ansi.AlignCenter))
	})

	// stripAnsi(str) — remove ANSI escape sequences from a string.
	jsutil.Set(obj, "stripAnsi", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue("")
		}
		return vm.ToValue(ansi.StripAnsi(call.Arguments[0].String()))
	})

	// stripPipe(str) — remove Vision/3 pipe codes from a string.
	jsutil.Set(obj, "stripPipe", func(call goja.FunctionCall) goja.Value {
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
	jsutil.Set(obj, "displayLen", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(0)
		}
		str := call.Arguments[0].String()
		// Process pipe codes first, then measure visible length.
		processed := ansi.ReplacePipeCodes([]byte(str))
		return vm.ToValue(ansi.VisibleLength(string(processed)))
	})

	// fitColumns(str, width) — str cut to at most width terminal columns,
	// whole characters at a time, and padded with spaces to width. Columns
	// are counted as the caller's terminal draws them: by grapheme cluster
	// on UTF-8, one per character on CP437. str is measured as written, so
	// pipe codes and escape sequences count as text.
	jsutil.Set(obj, "fitColumns", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return vm.ToValue("")
		}
		width := int(call.Arguments[1].ToInteger())
		checkWidth(vm, "v3.util.fitColumns", width)
		utf8Out := eng.session != nil && eng.session.OutputMode == ansi.OutputModeUTF8
		return vm.ToValue(fitColumns(call.Arguments[0].String(), width, utf8Out))
	})

	jsutil.Set(v3, "util", obj)
}

// fitColumns cuts s to at most width columns between whole characters and
// pads it with spaces to width; a width below one gives "". On UTF-8 a character is a grapheme cluster
// as uniseg measures it; otherwise each rune is a one-column character. Both
// match how the User Konfig editor measures its fields.
func fitColumns(s string, width int, utf8Out bool) string {
	// A width below one, as a script that computes it may pass, fits
	// nothing.
	if width < 1 {
		return ""
	}
	var b strings.Builder
	used := 0
	if !utf8Out {
		for _, r := range s {
			if used+1 > width {
				break
			}
			b.WriteRune(r)
			used++
		}
		return b.String() + strings.Repeat(" ", width-used)
	}
	for rest := s; rest != ""; {
		var cluster string
		var w int
		cluster, rest, w, _ = uniseg.FirstGraphemeClusterInString(rest, -1)
		if used+w > width {
			break
		}
		b.WriteString(cluster)
		used += w
	}
	if used < width {
		b.WriteString(strings.Repeat(" ", width-used))
	}
	return b.String()
}

// checkWidth throws a limitError when width, given to the v3.util call named
// call, is over maxPadWidth.
func checkWidth(vm *goja.Runtime, call string, width int) {
	if width > maxPadWidth {
		panic(vm.NewGoError(limitError(call, "width", width, maxPadWidth, "columns")))
	}
}
