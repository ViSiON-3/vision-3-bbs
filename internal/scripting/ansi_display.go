package scripting

import (
	"os"
	"path/filepath"

	"github.com/dop251/goja"
	"github.com/stlalpha/vision3/internal/ansi"
)

// registerAnsi creates the v3.ansi object for ANSI art display.
func registerAnsi(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()

	// display(filename) — read and display an .ANS file with pipe-code processing.
	// File path is resolved relative to the script's working directory,
	// then falls back to menus/v3/ansi/ and menus/v3/templates/.
	obj.Set("display", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Undefined()
		}
		filename := call.Arguments[0].String()
		path := resolveAnsiPath(eng, filename)
		if path == "" {
			return goja.Undefined()
		}

		content, err := ansi.GetAnsiFileContent(path)
		if err != nil {
			return goja.Undefined()
		}

		// Process pipe codes on raw bytes, then write directly.
		// No CP437→UTF8 conversion — ANSI art bytes are sent as-is.
		processed := ansi.ReplacePipeCodes(content)
		eng.writeBytes(processed)
		return goja.Undefined()
	})

	// displayRaw(filename) — display an .ANS file without pipe-code processing.
	// Sends raw CP437 bytes directly to the terminal.
	obj.Set("displayRaw", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Undefined()
		}
		filename := call.Arguments[0].String()
		path := resolveAnsiPath(eng, filename)
		if path == "" {
			return goja.Undefined()
		}

		content, err := ansi.GetAnsiFileContent(path)
		if err != nil {
			return goja.Undefined()
		}

		eng.writeBytes(content)
		return goja.Undefined()
	})

	v3.Set("ansi", obj)
}

// resolveAnsiPath finds an ANSI file by checking multiple locations:
//  1. Script's working directory
//  2. menus/v3/ansi/
//  3. menus/v3/templates/
//
// Returns empty string if not found in any location.
func resolveAnsiPath(eng *Engine, filename string) string {
	// Try working directory first.
	path := filepath.Join(eng.cfg.WorkingDir, filename)
	if _, err := os.Stat(path); err == nil {
		return path
	}

	// Derive BBS root from working dir (scripts/examples -> BBS root).
	bbsRoot := filepath.Join(eng.cfg.WorkingDir, "..", "..")

	// Try menus/v3/ansi/.
	path = filepath.Join(bbsRoot, "menus", "v3", "ansi", filename)
	if _, err := os.Stat(path); err == nil {
		return path
	}

	// Try menus/v3/templates/.
	path = filepath.Join(bbsRoot, "menus", "v3", "templates", filename)
	if _, err := os.Stat(path); err == nil {
		return path
	}

	return ""
}
