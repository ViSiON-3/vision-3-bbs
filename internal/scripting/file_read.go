package scripting

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/ViSiON-3/vision-3-bbs/internal/pkzip1"
	"github.com/dop251/goja"
)

// maxScriptRead caps what v3.file.read and v3.file.readMember return, as
// the built-in text pager caps what it pages.
const maxScriptRead = 4 * 1024 * 1024

// maxArchiveMembers caps how many entries v3.file.members lists.
const maxArchiveMembers = 4096

// registerFileRead adds the calls that read a file's contents to v3.file:
// read, members and readMember. Each needs the caller to be allowed to
// download from the file's area (Providers.CanDownload); without that, or
// for a file that is missing or too big, they return null.
//
// Text comes back as one character per byte, CP437 bytes 0x80-0xFF as their
// Unicode characters and every byte below 0x80 as itself, so ANSI art keeps
// its escape sequences and prints as drawn through v3.console.write on CP437
// and UTF-8 terminals alike.
func registerFileRead(obj *goja.Object, eng *Engine) {
	vm := eng.vm

	// read(areaID, filename) — the whole file as text, or null.
	jsutil.Set(obj, "read", func(call goja.FunctionCall) goja.Value {
		path, ok := eng.readableFile(call)
		if !ok {
			return goja.Null()
		}
		f, err := os.Open(path)
		if err != nil {
			return goja.Null()
		}
		defer func() { _ = f.Close() }() // read-only
		data, ok := readCapped(f)
		if !ok {
			return goja.Null()
		}
		return vm.ToValue(cp437Text(data))
	})

	// members(areaID, filename) — a ZIP's entries [{name, size, date, isDir,
	// method, encrypted, readable}], or null for a file that is not a ZIP.
	// readable is whether readMember can unpack the entry: not encrypted,
	// and stored, deflated or one of PKZIP 1.x's methods.
	jsutil.Set(obj, "members", func(call goja.FunctionCall) goja.Value {
		path, ok := eng.readableFile(call)
		if !ok {
			return goja.Null()
		}
		zr, err := zip.OpenReader(path)
		if err != nil {
			return goja.Null()
		}
		defer func() { _ = zr.Close() }() // read-only
		arr := vm.NewArray()
		for i, m := range zr.File {
			if i >= maxArchiveMembers {
				break
			}
			o := vm.NewObject()
			jsutil.Set(o, "name", m.Name)
			jsutil.Set(o, "size", int64(m.UncompressedSize64))
			jsutil.Set(o, "date", m.Modified.Unix())
			jsutil.Set(o, "isDir", m.FileInfo().IsDir())
			jsutil.Set(o, "method", int(m.Method))
			jsutil.Set(o, "encrypted", pkzip1.Encrypted(m))
			jsutil.Set(o, "readable", pkzip1.Supported(m.Method) && !pkzip1.Encrypted(m))
			jsutil.Set(arr, itoa(i), o)
		}
		return arr
	})

	// readMember(areaID, filename, member) — one ZIP entry as text, or null,
	// including for an entry that unpacks to more than maxScriptRead.
	// Nothing is extracted to disk.
	jsutil.Set(obj, "readMember", func(call goja.FunctionCall) goja.Value {
		path, ok := eng.readableFile(call)
		if !ok || len(call.Arguments) < 3 {
			return goja.Null()
		}
		name := call.Arguments[2].String()
		zr, err := zip.OpenReader(path)
		if err != nil {
			return goja.Null()
		}
		defer func() { _ = zr.Close() }() // read-only
		for _, m := range zr.File {
			if m.Name != name || m.FileInfo().IsDir() || m.UncompressedSize64 > maxScriptRead {
				continue
			}
			// The header's size can lie, so what is unpacked is capped
			// too: readCapped stops past maxScriptRead. PKZIP 1.x members
			// (shrunk, reduced, imploded) are unpacked by pkzip1.
			r, err := pkzip1.Open(m, maxScriptRead)
			if err != nil {
				return goja.Null()
			}
			data, ok := readCapped(r)
			_ = r.Close()
			if !ok {
				return goja.Null()
			}
			return vm.ToValue(cp437Text(data))
		}
		return goja.Null()
	})
}

// readableFile resolves call's (areaID, filename) to the file's path when the
// caller may download from that area.
func (eng *Engine) readableFile(call goja.FunctionCall) (string, bool) {
	rec, ok := eng.callFile(call, true)
	if !ok {
		return "", false
	}
	path, err := eng.providers.FileMgr.GetFilePath(rec.ID)
	return path, err == nil
}

// callFile finds the file named by call's (areaID, filename). The filename
// matches as the file menu matches it, ignoring case. With needDownload, the
// caller must also be allowed to download from the area
// (Providers.CanDownload).
func (eng *Engine) callFile(call goja.FunctionCall, needDownload bool) (file.FileRecord, bool) {
	mgr := eng.providers.FileMgr
	if len(call.Arguments) < 2 || mgr == nil {
		return file.FileRecord{}, false
	}
	areaID := int(call.Arguments[0].ToInteger())
	name := call.Arguments[1].String()
	area, ok := mgr.GetAreaByID(areaID)
	if !ok {
		return file.FileRecord{}, false
	}
	if needDownload {
		if can := eng.providers.CanDownload; can == nil || !can(area) {
			return file.FileRecord{}, false
		}
	}
	for _, r := range mgr.GetFilesForArea(areaID) {
		if strings.EqualFold(r.Filename, name) {
			return r, true
		}
	}
	return file.FileRecord{}, false
}

// readCapped reads r to its end, refusing more than maxScriptRead bytes.
func readCapped(r io.Reader) ([]byte, bool) {
	data, err := io.ReadAll(io.LimitReader(r, maxScriptRead+1))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false
	}
	return data, len(data) <= maxScriptRead
}

// cp437Text maps data to a string with one character per byte: bytes below
// 0x80 as themselves (control characters included), the rest through
// ansi.Cp437ToUnicode.
func cp437Text(data []byte) string {
	var b strings.Builder
	b.Grow(len(data))
	for _, c := range data {
		if c < 0x80 {
			b.WriteByte(c)
		} else if r := ansi.Cp437ToUnicode[c]; r != 0 {
			b.WriteRune(r)
		} else {
			b.WriteByte('?')
		}
	}
	return b.String()
}
