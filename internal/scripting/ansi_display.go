package scripting

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
)

// registerAnsi creates the v3.ansi object for ANSI art display.
func registerAnsi(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()

	// display(filename) — read and display an .ANS file with pipe-code processing.
	// File path is resolved relative to the script's working directory,
	// then falls back to menus/v3/ansi/ and menus/v3/templates/.
	jsutil.Set(obj, "display", func(call goja.FunctionCall) goja.Value {
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

		// Pipe codes expand to ASCII escape sequences, so they can be
		// processed on the raw CP437 bytes before the art is encoded.
		processed := ansi.ReplacePipeCodes(content)
		eng.writeBytes(artForSession(eng, processed))
		return goja.Undefined()
	})

	// displayRaw(filename) — display an .ANS file without pipe-code processing.
	jsutil.Set(obj, "displayRaw", func(call goja.FunctionCall) goja.Value {
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

		eng.writeBytes(artForSession(eng, content))
		return goja.Undefined()
	})

	// render(text, {width, rows}) — draw ANSI art the way ANSI.SYS would
	// and return the screen it leaves, one string per row.
	jsutil.Set(obj, "render", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue([]string{})
		}
		width, rows := renderOptions(call)
		return vm.ToValue(renderArt(call.Arguments[0].String(), width, rows))
	})

	// sauce(text) — the SAUCE record's title, author and group, or null.
	jsutil.Set(obj, "sauce", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Null()
		}
		sc, ok := ansi.ReadSAUCE(cp437Bytes(call.Arguments[0].String()))
		if !ok {
			return goja.Null()
		}
		res := vm.NewObject()
		jsutil.Set(res, "title", sc.Title)
		jsutil.Set(res, "author", sc.Author)
		jsutil.Set(res, "group", sc.Group)
		return res
	})

	jsutil.Set(v3, "ansi", obj)
}

// The limits on v3.ansi.render: the widest screen it draws, the most rows
// it keeps, the default number of rows, and the most cells a screen may
// have (80 columns by 4000 rows), so a wide screen keeps fewer rows.
const (
	maxRenderWidth    = 255
	maxRenderRows     = 4000
	defaultRenderRows = 1000
	maxRenderCells    = ansi.ArtWidth * maxRenderRows
)

// renderOptions reads render's optional {width, rows}, defaulting to the
// art width (80) and defaultRenderRows, and clamping each to its limit and
// the screen to maxRenderCells.
func renderOptions(call goja.FunctionCall) (width, rows int) {
	width, rows = ansi.ArtWidth, defaultRenderRows
	if len(call.Arguments) < 2 {
		return width, rows
	}
	opts, ok := call.Arguments[1].Export().(map[string]any)
	if !ok {
		return width, rows
	}
	read := func(key string, def, limit int) int {
		v, ok := opts[key]
		if !ok {
			return def
		}
		var n int
		switch x := v.(type) {
		case int64:
			n = int(x)
		case float64:
			n = int(x)
		default:
			return def
		}
		return min(max(n, 1), limit)
	}
	width = read("width", width, maxRenderWidth)
	return width, min(read("rows", rows, maxRenderRows), maxRenderCells/width)
}

// renderArt draws text, as v3.file.read and readMember return it (one
// character per byte), on a screen width columns wide and up to rows tall,
// with the renderer the message reader uses (ansi.RenderANSIArtToLines):
// ANSI.SYS semantics, auto-wrap at the last column, saved cursor positions.
// Everything from the first Ctrl-Z, where SAUCE and comments start, is left
// out. Trailing blank rows are dropped.
func renderArt(text string, width, rows int) []string {
	if i := strings.IndexByte(text, 0x1a); i >= 0 {
		text = text[:i]
	}
	return ansi.RenderANSIArtToLines(text, width, rows)
}

// cp437Bytes turns text back into the bytes v3.file.read decoded it from:
// one byte per character, characters below 0x80 as themselves and the rest
// through ansi.UnicodeToCP437 ('?' for a character with no CP437 byte).
func cp437Bytes(text string) []byte {
	out := make([]byte, 0, len(text))
	for _, r := range text {
		switch {
		case r < 0x80:
			out = append(out, byte(r))
		default:
			if b, ok := ansi.UnicodeToCP437[r]; ok {
				out = append(out, b)
			} else {
				out = append(out, '?')
			}
		}
	}
	return out
}

// artForSession prepares art file bytes for the session, as the menu does for
// its screens: line breaks are made explicit on terminals wider than the art
// (ansi.FitArtToWidth, measured one byte per cell while the bytes are still
// CP437), then CP437 art is converted to UTF-8 for a UTF-8 session
// (ansi.ArtForOutput). A CP437 session gets the file's bytes unchanged. The
// result is written raw: it is already in the session's encoding.
func artForSession(eng *Engine, data []byte) []byte {
	width := eng.session.ArtWidth
	if width <= 0 {
		width = eng.session.ScreenWidth
	}
	return ansi.ArtForOutput(ansi.FitArtToWidth(data, width, false), eng.session.OutputMode)
}

// resolveAnsiPath finds an ANSI file by checking multiple locations:
//  1. Script's working directory
//  2. menus/v3/ansi/
//  3. menus/v3/templates/
//
// The menu set's overlay (menus.d/v3) is searched before the shipped tree
// for the last two, as everywhere else in the BBS.
//
// Returns empty string if not found in any location.
// Symlinks are resolved to prevent traversal outside the intended directories.
func resolveAnsiPath(eng *Engine, filename string) string {
	// Sanitize filename: reject absolute paths and path traversal.
	cleaned := filepath.Clean(filename)
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return ""
	}

	// Try working directory first.
	path := filepath.Join(eng.cfg.WorkingDir, cleaned)
	if _, err := os.Stat(path); err == nil {
		if real := pathUnderBase(eng.cfg.WorkingDir, path); real != "" {
			return real
		}
	}

	// Derive BBS root by walking up from working dir until we find a "menus" subdirectory.
	bbsRoot := findBBSRoot(eng.cfg.WorkingDir)
	if bbsRoot == "" {
		return ""
	}
	menus := menuset.FromPath(filepath.Join(bbsRoot, "menus", "v3"))

	// Try ansi/ then templates/, overlay before shipped set for each.
	for _, sub := range []string{"ansi", "templates"} {
		for _, layer := range []menuset.Layer{menuset.LayerOverlay, menuset.LayerBase} {
			if layer == menuset.LayerOverlay && !menus.HasOverlay() {
				continue
			}
			base := menus.Path(layer, sub)
			path = filepath.Join(base, cleaned)
			if _, err := os.Stat(path); err != nil {
				_, linkErr := os.Lstat(path)
				if !os.IsNotExist(err) || !os.IsNotExist(linkErr) {
					slog.Warn("resolving script art", "path", path, "error", err)
					return ""
				}
				continue
			}
			if real := pathUnderBase(base, path); real != "" {
				return real
			}
		}
	}

	return ""
}

// pathUnderBase resolves symlinks in p and returns the real path only if it
// falls within base. Returns "" if p escapes base or cannot be resolved.
func pathUnderBase(base, p string) string {
	// Resolve base the same way as p. Otherwise a symlinked or (on Windows)
	// 8.3 short-named ancestor makes every file look like it escapes.
	baseAbs, err := filepath.EvalSymlinks(base)
	if err != nil {
		return ""
	}
	if baseAbs, err = filepath.Abs(baseAbs); err != nil {
		return ""
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(baseAbs, real)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return ""
	}
	return real
}

// findBBSRoot walks up from dir until it finds a directory containing a "menus" subdirectory.
func findBBSRoot(dir string) string {
	current, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if info, err := os.Stat(filepath.Join(current, "menus")); err == nil && info.IsDir() {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}
