package syncjs

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/dop251/goja"
)

// bytesToLatin1 converts raw bytes to a Go string using Latin-1 mapping,
// preserving all byte values 0-255 as their corresponding Unicode codepoints.
// This is essential for CP437 ANSI art files where bytes > 127 must survive
// the round-trip through JS strings back to raw bytes for terminal output.
func bytesToLatin1(data []byte) string {
	runes := make([]rune, len(data))
	for i, b := range data {
		runes[i] = rune(b)
	}
	return string(runes)
}

// jsFile wraps an os.File for Synchronet JS File class semantics.
type jsFile struct {
	path    string
	f       *os.File
	reader  *bufio.Reader
	isOpen  bool
	lastErr int
	eng     *Engine
}

// registerFileClass registers the File constructor and file_exists global on the JS runtime.
func registerFileClass(vm *goja.Runtime, eng *Engine) {
	vm.Set("File", func(call goja.ConstructorCall) *goja.Object {
		path := ""
		if len(call.Arguments) > 0 {
			path = call.Arguments[0].String()
		}
		path = eng.resolveFilePath(path)

		jf := &jsFile{path: path, eng: eng}
		obj := call.This

		// --- Properties ---
		obj.Set("name", path)
		obj.DefineAccessorProperty("is_open", vm.ToValue(func(goja.FunctionCall) goja.Value {
			return vm.ToValue(jf.isOpen)
		}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

		obj.DefineAccessorProperty("exists", vm.ToValue(func(goja.FunctionCall) goja.Value {
			_, err := os.Stat(jf.path)
			return vm.ToValue(err == nil)
		}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

		obj.DefineAccessorProperty("position", vm.ToValue(func(goja.FunctionCall) goja.Value {
			if jf.f == nil {
				return vm.ToValue(0)
			}
			pos, _ := jf.f.Seek(0, io.SeekCurrent)
			// Adjust for bufio.Reader's read-ahead buffer
			if jf.reader != nil {
				pos -= int64(jf.reader.Buffered())
			}
			return vm.ToValue(pos)
		}), vm.ToValue(func(call goja.FunctionCall) goja.Value {
			if jf.f == nil || len(call.Arguments) == 0 {
				return goja.Undefined()
			}
			pos := call.Arguments[0].ToInteger()
			jf.f.Seek(pos, io.SeekStart)
			jf.reader = nil // invalidate buffered reader
			return goja.Undefined()
		}), goja.FLAG_FALSE, goja.FLAG_TRUE)

		obj.DefineAccessorProperty("length", vm.ToValue(func(goja.FunctionCall) goja.Value {
			if jf.f == nil {
				info, err := os.Stat(jf.path)
				if err != nil {
					return vm.ToValue(0)
				}
				return vm.ToValue(info.Size())
			}
			info, err := jf.f.Stat()
			if err != nil {
				return vm.ToValue(0)
			}
			return vm.ToValue(info.Size())
		}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

		obj.DefineAccessorProperty("error", vm.ToValue(func(goja.FunctionCall) goja.Value {
			return vm.ToValue(jf.lastErr)
		}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

		obj.DefineAccessorProperty("eof", vm.ToValue(func(goja.FunctionCall) goja.Value {
			if jf.f == nil {
				return vm.ToValue(true)
			}
			pos, _ := jf.f.Seek(0, io.SeekCurrent)
			info, err := jf.f.Stat()
			if err != nil {
				return vm.ToValue(true)
			}
			return vm.ToValue(pos >= info.Size())
		}), nil, goja.FLAG_FALSE, goja.FLAG_FALSE)

		// --- Methods ---
		obj.Set("open", func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(jf.open(call))
		})
		obj.Set("close", func(call goja.FunctionCall) goja.Value {
			jf.close()
			return goja.Undefined()
		})
		obj.Set("flush", func(call goja.FunctionCall) goja.Value {
			if jf.f != nil {
				jf.f.Sync()
			}
			return vm.ToValue(true)
		})
		obj.Set("read", func(call goja.FunctionCall) goja.Value {
			return jf.read(vm, call)
		})
		obj.Set("readln", func(call goja.FunctionCall) goja.Value {
			return jf.readln(vm)
		})
		obj.Set("readAll", func(call goja.FunctionCall) goja.Value {
			return jf.readAll(vm)
		})
		obj.Set("write", func(call goja.FunctionCall) goja.Value {
			return jf.write(vm, call)
		})
		obj.Set("writeln", func(call goja.FunctionCall) goja.Value {
			return jf.writeln(vm, call)
		})
		obj.Set("writeAll", func(call goja.FunctionCall) goja.Value {
			return jf.writeAll(vm, call)
		})
		obj.Set("readBin", func(call goja.FunctionCall) goja.Value {
			return jf.readBin(vm, call)
		})
		obj.Set("writeBin", func(call goja.FunctionCall) goja.Value {
			return jf.writeBin(vm, call)
		})
		obj.Set("truncate", func(call goja.FunctionCall) goja.Value {
			if jf.f == nil {
				return vm.ToValue(false)
			}
			length := int64(0)
			if len(call.Arguments) > 0 {
				length = call.Arguments[0].ToInteger()
			}
			err := jf.f.Truncate(length)
			return vm.ToValue(err == nil)
		})
		obj.Set("lock", func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(jf.lock(call, true))
		})
		obj.Set("unlock", func(call goja.FunctionCall) goja.Value {
			return vm.ToValue(jf.lock(call, false))
		})

		// INI file methods — Synchronet File objects can read/write INI format
		obj.Set("iniGetValue", func(call goja.FunctionCall) goja.Value {
			section := ""
			key := ""
			var defVal goja.Value
			switch len(call.Arguments) {
			case 3:
				if !goja.IsNull(call.Arguments[0]) && !goja.IsUndefined(call.Arguments[0]) {
					section = call.Arguments[0].String()
				}
				key = call.Arguments[1].String()
				defVal = call.Arguments[2]
			case 2:
				if !goja.IsNull(call.Arguments[0]) && !goja.IsUndefined(call.Arguments[0]) {
					section = call.Arguments[0].String()
				}
				key = call.Arguments[1].String()
			default:
				return goja.Undefined()
			}
			val, found := jf.iniGet(section, key)
			if !found {
				if defVal != nil {
					return defVal
				}
				return goja.Undefined()
			}
			return vm.ToValue(val)
		})

		obj.Set("iniGetSections", func(call goja.FunctionCall) goja.Value {
			sections := jf.iniGetSections()
			return vm.ToValue(sections)
		})

		obj.Set("iniGetKeys", func(call goja.FunctionCall) goja.Value {
			section := ""
			if len(call.Arguments) > 0 && !goja.IsNull(call.Arguments[0]) {
				section = call.Arguments[0].String()
			}
			keys := jf.iniGetKeys(section)
			return vm.ToValue(keys)
		})

		return nil
	})

	// Global file_exists() function
	vm.Set("file_exists", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(false)
		}
		path := eng.resolveFilePath(call.Arguments[0].String())
		_, err := os.Stat(path)
		return vm.ToValue(err == nil)
	})

	// Global file_remove() function
	vm.Set("file_remove", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(false)
		}
		path := eng.resolveFilePath(call.Arguments[0].String())
		err := os.Remove(path)
		return vm.ToValue(err == nil)
	})

	// Global directory() function — list files in directory
	vm.Set("directory", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(vm.NewArray())
		}
		pattern := eng.resolveFilePath(call.Arguments[0].String())
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return vm.ToValue(vm.NewArray())
		}
		arr := make([]interface{}, len(matches))
		for i, m := range matches {
			arr[i] = m
		}
		return vm.ToValue(arr)
	})

	// Global mkdir() function
	vm.Set("mkdir", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(false)
		}
		path := eng.resolveFilePath(call.Arguments[0].String())
		err := os.MkdirAll(path, 0o755)
		return vm.ToValue(err == nil)
	})

	// Global file_isdir() function
	vm.Set("file_isdir", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(false)
		}
		path := eng.resolveFilePath(call.Arguments[0].String())
		info, err := os.Stat(path)
		return vm.ToValue(err == nil && info.IsDir())
	})
}

func (jf *jsFile) open(call goja.FunctionCall) bool {
	if jf.isOpen {
		jf.close()
	}
	mode := "r"
	if len(call.Arguments) > 0 {
		mode = call.Arguments[0].String()
	}

	flag, perm := modeToFlags(mode)
	f, err := os.OpenFile(jf.path, flag, perm)
	if err != nil {
		jf.lastErr = -1
		return false
	}
	jf.f = f
	jf.isOpen = true
	jf.lastErr = 0
	jf.reader = nil
	return true
}

func (jf *jsFile) close() {
	if jf.f != nil {
		jf.f.Close()
		jf.f = nil
	}
	jf.isOpen = false
	jf.reader = nil
}

func (jf *jsFile) read(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	if jf.f == nil {
		return vm.ToValue("")
	}
	count := -1
	if len(call.Arguments) > 0 {
		count = int(call.Arguments[0].ToInteger())
	}
	if count < 0 {
		data, err := io.ReadAll(jf.f)
		if err != nil {
			jf.lastErr = -1
			return vm.ToValue("")
		}
		return vm.ToValue(bytesToLatin1(data))
	}
	buf := make([]byte, count)
	n, err := jf.f.Read(buf)
	if err != nil && err != io.EOF {
		jf.lastErr = -1
	}
	return vm.ToValue(bytesToLatin1(buf[:n]))
}

func (jf *jsFile) readln(vm *goja.Runtime) goja.Value {
	if jf.f == nil {
		return goja.Null()
	}
	if jf.reader == nil {
		jf.reader = bufio.NewReader(jf.f)
	}
	line, err := jf.reader.ReadBytes('\n')
	if err != nil && err != io.EOF {
		jf.lastErr = -1
		return goja.Null()
	}
	if err == io.EOF && len(line) == 0 {
		return goja.Null()
	}
	// Trim CRLF/LF
	line = bytes.TrimRight(line, "\r\n")
	return vm.ToValue(bytesToLatin1(line))
}

func (jf *jsFile) readAll(vm *goja.Runtime) goja.Value {
	if jf.f == nil {
		return vm.ToValue(vm.NewArray())
	}
	if jf.reader == nil {
		jf.reader = bufio.NewReader(jf.f)
	}
	var lines []interface{}
	reader := bufio.NewReader(jf.reader)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimRight(line, "\r\n")
			lines = append(lines, bytesToLatin1(line))
		}
		if err != nil {
			break
		}
	}
	return vm.ToValue(lines)
}

func (jf *jsFile) write(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	if jf.f == nil || len(call.Arguments) == 0 {
		return vm.ToValue(false)
	}
	data := call.Arguments[0].String()
	_, err := jf.f.Write(runesToBytes(data))
	jf.reader = nil // invalidate
	return vm.ToValue(err == nil)
}

func (jf *jsFile) writeln(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	if jf.f == nil {
		return vm.ToValue(false)
	}
	data := ""
	if len(call.Arguments) > 0 {
		data = call.Arguments[0].String()
	}
	_, err := jf.f.Write(append(runesToBytes(data), '\n'))
	jf.reader = nil
	return vm.ToValue(err == nil)
}

func (jf *jsFile) writeAll(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	if jf.f == nil || len(call.Arguments) == 0 {
		return vm.ToValue(false)
	}
	// Expect an array
	arr, ok := call.Arguments[0].Export().([]interface{})
	if !ok {
		return vm.ToValue(false)
	}
	for _, item := range arr {
		if _, err := fmt.Fprintf(jf.f, "%v\n", item); err != nil {
			return vm.ToValue(false)
		}
	}
	jf.reader = nil
	return vm.ToValue(true)
}

// readBin reads a little-endian unsigned integer of 1, 2, or 4 bytes.
func (jf *jsFile) readBin(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	if jf.f == nil {
		return vm.ToValue(0)
	}
	size := 4
	if len(call.Arguments) > 0 {
		size = int(call.Arguments[0].ToInteger())
	}
	buf := make([]byte, size)
	n, err := io.ReadFull(jf.f, buf)
	if err != nil || n < size {
		return vm.ToValue(0)
	}
	var val uint64
	for i := 0; i < size; i++ {
		val |= uint64(buf[i]) << (uint(i) * 8)
	}
	return vm.ToValue(val)
}

// writeBin writes a little-endian unsigned integer of 1, 2, or 4 bytes.
func (jf *jsFile) writeBin(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	if jf.f == nil || len(call.Arguments) == 0 {
		return vm.ToValue(false)
	}
	val := uint64(call.Arguments[0].ToInteger())
	size := 4
	if len(call.Arguments) > 1 {
		size = int(call.Arguments[1].ToInteger())
	}
	buf := make([]byte, size)
	for i := 0; i < size; i++ {
		buf[i] = byte(val >> (uint(i) * 8))
	}
	_, err := jf.f.Write(buf)
	jf.reader = nil
	return vm.ToValue(err == nil)
}

// lock/unlock using fcntl byte-range locks for multi-node safety.
func (jf *jsFile) lock(call goja.FunctionCall, doLock bool) bool {
	if jf.f == nil {
		return false
	}
	offset := int64(0)
	length := int64(0)
	if len(call.Arguments) > 0 {
		offset = call.Arguments[0].ToInteger()
	}
	if len(call.Arguments) > 1 {
		length = call.Arguments[1].ToInteger()
	}

	lockType := syscall.F_UNLCK
	if doLock {
		lockType = syscall.F_WRLCK
	}

	flock := syscall.Flock_t{
		Type:   int16(lockType),
		Whence: io.SeekStart,
		Start:  offset,
		Len:    length,
	}
	err := syscall.FcntlFlock(jf.f.Fd(), syscall.F_SETLKW, &flock)
	return err == nil
}

// modeToFlags converts a Synchronet file mode string to os.OpenFile flags.
func modeToFlags(mode string) (int, os.FileMode) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	switch mode {
	case "r":
		return os.O_RDONLY, 0
	case "r+", "r+b", "rb+":
		return os.O_RDWR, 0
	case "w":
		return os.O_WRONLY | os.O_CREATE | os.O_TRUNC, 0o644
	case "w+", "w+b", "wb+":
		return os.O_RDWR | os.O_CREATE | os.O_TRUNC, 0o644
	case "a":
		return os.O_WRONLY | os.O_CREATE | os.O_APPEND, 0o644
	case "a+", "a+b", "ab+":
		return os.O_RDWR | os.O_CREATE | os.O_APPEND, 0o644
	case "rb":
		return os.O_RDONLY, 0
	case "wb":
		return os.O_WRONLY | os.O_CREATE | os.O_TRUNC, 0o644
	default:
		return os.O_RDONLY, 0
	}
}

// --- INI file methods ---

// iniParse reads the file and returns a map of section -> key -> value.
// The empty string key "" represents the root section (before any [section] header).
func (jf *jsFile) iniParse() map[string]map[string]string {
	if jf.f == nil {
		return nil
	}
	// Seek to beginning
	jf.f.Seek(0, io.SeekStart)
	jf.reader = nil

	sections := make(map[string]map[string]string)
	currentSection := ""
	sections[currentSection] = make(map[string]string)

	scanner := bufio.NewScanner(jf.f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == ';' || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			end := strings.IndexByte(line, ']')
			if end > 0 {
				currentSection = line[1:end]
				if sections[currentSection] == nil {
					sections[currentSection] = make(map[string]string)
				}
			}
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq > 0 {
			key := strings.TrimSpace(line[:eq])
			val := strings.TrimSpace(line[eq+1:])
			sections[currentSection][key] = val
		}
	}
	return sections
}

func (jf *jsFile) iniGet(section, key string) (string, bool) {
	data := jf.iniParse()
	if data == nil {
		return "", false
	}
	sec, ok := data[section]
	if !ok {
		return "", false
	}
	val, found := sec[key]
	return val, found
}

func (jf *jsFile) iniGetSections() []string {
	data := jf.iniParse()
	if data == nil {
		return nil
	}
	result := make([]string, 0, len(data))
	for k := range data {
		if k != "" {
			result = append(result, k)
		}
	}
	return result
}

func (jf *jsFile) iniGetKeys(section string) []string {
	data := jf.iniParse()
	if data == nil {
		return nil
	}
	sec, ok := data[section]
	if !ok {
		return nil
	}
	result := make([]string, 0, len(sec))
	for k := range sec {
		result = append(result, k)
	}
	return result
}
