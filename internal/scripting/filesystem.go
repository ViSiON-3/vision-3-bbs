package scripting

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
)

// registerFS creates the v3.fs object for sandboxed file operations.
//
// All paths are relative to scripts/data/. Every operation goes through an
// os.Root opened on that directory, so path lookup refuses anything —
// including a path reached through a symbolic link, dangling or not — that
// would leave the sandbox. Paths are also checked lexically first so obvious
// traversal ("../x", absolute paths) fails with a clear message.
func registerFS(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()

	sandbox := sandboxRoot(eng.cfg)

	// throw converts a Go error into a JS exception.
	throw := func(err error) {
		panic(vm.NewGoError(err))
	}

	// read(path) — read a text file, returns string or throws on error.
	jsutil.Set(obj, "read", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			throw(errMissingArgs("read", "path"))
		}
		var data []byte
		err := withSandbox(sandbox, call.Arguments[0].String(), false, func(root *os.Root, rel string) error {
			var err error
			data, err = root.ReadFile(rel)
			return err
		})
		if err != nil {
			throw(err)
		}
		return vm.ToValue(string(data))
	})

	// write(path, content) — write a text file (overwrites if exists).
	// Missing parent directories are created.
	jsutil.Set(obj, "write", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			throw(errMissingArgs("write", "path, content"))
		}
		content := []byte(call.Arguments[1].String())
		err := withSandbox(sandbox, call.Arguments[0].String(), true, func(root *os.Root, rel string) error {
			if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
				return err
			}
			return root.WriteFile(rel, content, 0o644)
		})
		if err != nil {
			throw(err)
		}
		return goja.Undefined()
	})

	// append(path, content) — append content to a file (creates if not exists).
	// Missing parent directories are created.
	jsutil.Set(obj, "append", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			throw(errMissingArgs("append", "path, content"))
		}
		content := call.Arguments[1].String()
		err := withSandbox(sandbox, call.Arguments[0].String(), true, func(root *os.Root, rel string) error {
			if err := root.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
				return err
			}
			f, err := root.OpenFile(rel, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			if _, err := f.WriteString(content); err != nil {
				_ = f.Close() // best-effort; the write error takes precedence
				return err
			}
			return f.Close()
		})
		if err != nil {
			throw(err)
		}
		return goja.Undefined()
	})

	// exists(path) — returns true if the file or directory exists.
	jsutil.Set(obj, "exists", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(false)
		}
		err := withSandbox(sandbox, call.Arguments[0].String(), false, func(root *os.Root, rel string) error {
			_, err := root.Stat(rel)
			return err
		})
		return vm.ToValue(err == nil)
	})

	// delete(path) — delete a file. Returns true if deleted.
	jsutil.Set(obj, "delete", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue(false)
		}
		err := withSandbox(sandbox, call.Arguments[0].String(), false, func(root *os.Root, rel string) error {
			return root.Remove(rel)
		})
		return vm.ToValue(err == nil)
	})

	// list(dir) — list directory contents, returns array of {name, isDir, size}.
	jsutil.Set(obj, "list", func(call goja.FunctionCall) goja.Value {
		dir := ""
		if len(call.Arguments) > 0 {
			dir = call.Arguments[0].String()
		}
		arr := vm.NewArray()
		err := withSandbox(sandbox, dir, false, func(root *os.Root, rel string) error {
			d, err := root.Open(rel)
			if err != nil {
				return err
			}
			entries, err := d.ReadDir(-1)
			_ = d.Close() // read-only handle; nothing to flush
			if err != nil {
				return err
			}
			for i, entry := range entries {
				item := vm.NewObject()
				jsutil.Set(item, "name", entry.Name())
				jsutil.Set(item, "isDir", entry.IsDir())
				// Lstat through the root so metadata lookups stay confined too.
				if info, err := root.Lstat(filepath.Join(rel, entry.Name())); err == nil {
					jsutil.Set(item, "size", info.Size())
				} else {
					jsutil.Set(item, "size", 0)
				}
				jsutil.Set(arr, itoa(i), item)
			}
			return nil
		})
		if err != nil {
			throw(err)
		}
		return arr
	})

	// mkdir(path) — create a directory (and parents).
	jsutil.Set(obj, "mkdir", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			throw(errMissingArgs("mkdir", "path"))
		}
		err := withSandbox(sandbox, call.Arguments[0].String(), true, func(root *os.Root, rel string) error {
			return root.MkdirAll(rel, 0o755)
		})
		if err != nil {
			throw(err)
		}
		return goja.Undefined()
	})

	jsutil.Set(v3, "fs", obj)
}

// sandboxRoot returns the scripts/data/ directory for the current script config.
func sandboxRoot(cfg ScriptConfig) string {
	return resolveDataDir(cfg.WorkingDir)
}

// sandboxRelPath lexically validates a script-supplied path and returns it
// in the cleaned, root-relative form that os.Root expects. An empty path
// means the sandbox directory itself.
//
// This is only a first line of defence that gives traversal attempts a clear
// error; the os.Root used for the actual operation is what enforces the
// sandbox, including against symbolic links.
func sandboxRelPath(userPath string) (string, error) {
	if userPath == "" {
		return ".", nil
	}
	rel := filepath.Clean(filepath.FromSlash(userPath))
	// IsLocal rejects absolute paths, paths that climb out with "..", and
	// (on Windows) reserved device names, while allowing names such as
	// "..foo" that merely start with two dots.
	if !filepath.IsLocal(rel) {
		return "", fmt.Errorf("access denied: path %q is outside sandbox", userPath)
	}
	return rel, nil
}

// withSandbox validates userPath, opens the sandbox directory as an os.Root
// and runs fn with the root and the root-relative path. When create is true
// the sandbox directory itself is created first if it does not exist yet
// (fresh install), so writes work without a pre-made scripts/data.
func withSandbox(sandbox, userPath string, create bool, fn func(root *os.Root, rel string) error) error {
	rel, err := sandboxRelPath(userPath)
	if err != nil {
		return err
	}
	if create {
		if err := os.MkdirAll(sandbox, 0o755); err != nil {
			return fmt.Errorf("creating sandbox directory: %w", err)
		}
	}
	root, err := os.OpenRoot(sandbox)
	if err != nil {
		return fmt.Errorf("opening sandbox directory: %w", err)
	}
	defer func() { _ = root.Close() }() // directory handle; close error is not actionable
	return fn(root, rel)
}
