package scripting

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dop251/goja"
)

// registerFS creates the v3.fs object for sandboxed file operations.
// All paths are resolved relative to scripts/data/ and path traversal is blocked.
func registerFS(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()

	sandbox := sandboxRoot(eng.cfg)

	// read(path) — read a text file, returns string or throws on error.
	obj.Set("read", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(vm.NewGoError(errMissingArgs("read", "path")))
		}
		path, err := resolveSandboxPath(sandbox, call.Arguments[0].String())
		if err != nil {
			panic(vm.NewGoError(err))
		}
		data, err := os.ReadFile(path)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(string(data))
	})

	// write(path, content) — write a text file (overwrites if exists).
	obj.Set("write", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			panic(vm.NewGoError(errMissingArgs("write", "path, content")))
		}
		path, err := resolveSandboxPath(sandbox, call.Arguments[0].String())
		if err != nil {
			panic(vm.NewGoError(err))
		}
		os.MkdirAll(filepath.Dir(path), 0o755) //nolint:errcheck
		if err := os.WriteFile(path, []byte(call.Arguments[1].String()), 0o644); err != nil {
			panic(vm.NewGoError(err))
		}
		return goja.Undefined()
	})

	// append(path, content) — append content to a file (creates if not exists).
	obj.Set("append", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			panic(vm.NewGoError(errMissingArgs("append", "path, content")))
		}
		path, err := resolveSandboxPath(sandbox, call.Arguments[0].String())
		if err != nil {
			panic(vm.NewGoError(err))
		}
		os.MkdirAll(filepath.Dir(path), 0o755) //nolint:errcheck
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		defer f.Close()
		if _, err := f.WriteString(call.Arguments[1].String()); err != nil {
			panic(vm.NewGoError(err))
		}
		return goja.Undefined()
	})

	// exists(path) — returns true if the file or directory exists.
	obj.Set("exists", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(false)
		}
		path, err := resolveSandboxPath(sandbox, call.Arguments[0].String())
		if err != nil {
			return vm.ToValue(false)
		}
		_, err = os.Stat(path)
		return vm.ToValue(err == nil)
	})

	// delete(path) — delete a file. Returns true if deleted.
	obj.Set("delete", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(false)
		}
		path, err := resolveSandboxPath(sandbox, call.Arguments[0].String())
		if err != nil {
			panic(vm.NewGoError(err))
		}
		err = os.Remove(path)
		return vm.ToValue(err == nil)
	})

	// list(dir) — list directory contents, returns array of {name, isDir, size}.
	obj.Set("list", func(call goja.FunctionCall) goja.Value {
		dir := ""
		if len(call.Arguments) > 0 {
			dir = call.Arguments[0].String()
		}
		path, err := resolveSandboxPath(sandbox, dir)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		arr := vm.NewArray()
		for i, entry := range entries {
			info, _ := entry.Info()
			obj := vm.NewObject()
			obj.Set("name", entry.Name())
			obj.Set("isDir", entry.IsDir())
			if info != nil {
				obj.Set("size", info.Size())
			} else {
				obj.Set("size", 0)
			}
			arr.Set(itoa(i), obj)
		}
		return arr
	})

	// mkdir(path) — create a directory (and parents).
	obj.Set("mkdir", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(vm.NewGoError(errMissingArgs("mkdir", "path")))
		}
		path, err := resolveSandboxPath(sandbox, call.Arguments[0].String())
		if err != nil {
			panic(vm.NewGoError(err))
		}
		if err := os.MkdirAll(path, 0o755); err != nil {
			panic(vm.NewGoError(err))
		}
		return goja.Undefined()
	})

	v3.Set("fs", obj)
}

// sandboxRoot returns the scripts/data/ directory for the current script config.
func sandboxRoot(cfg ScriptConfig) string {
	// Same logic as dataFilePath but just the directory.
	dataDir := filepath.Join(cfg.WorkingDir, "..", "data")
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		abs = filepath.Join(cfg.WorkingDir, "data")
	}
	return abs
}

// resolveSandboxPath resolves a user-provided path within the sandbox.
// Returns an error if the resolved path escapes the sandbox directory.
func resolveSandboxPath(sandbox, userPath string) (string, error) {
	if userPath == "" {
		return sandbox, nil
	}

	// Join and clean the path.
	resolved := filepath.Join(sandbox, filepath.Clean(userPath))
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("invalid path: %s", userPath)
	}

	// Ensure the resolved path is within the sandbox.
	sandboxAbs, _ := filepath.Abs(sandbox)
	if !strings.HasPrefix(abs, sandboxAbs+string(filepath.Separator)) && abs != sandboxAbs {
		return "", fmt.Errorf("access denied: path %q is outside sandbox", userPath)
	}

	return abs, nil
}
