package scripting

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/dop251/goja"
)

// globalDataLocks provides per-file-path mutexes so concurrent sessions writing
// the same script's data file serialize each individual operation.
var globalDataLocks sync.Map // map[string]*sync.Mutex

func dataFileLock(path string) *sync.Mutex {
	mu := &sync.Mutex{}
	actual, _ := globalDataLocks.LoadOrStore(path, mu)
	return actual.(*sync.Mutex)
}

// dataStore manages a per-script JSON key-value store in scripts/data/.
type dataStore struct {
	path string
}

// registerData creates the v3.data object for script-local persistent storage.
// Each script gets its own JSON file in scripts/data/<script-name>.json.
func registerData(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()

	store := &dataStore{
		path: dataFilePath(eng.cfg),
	}

	// get(key) — read a value from the store, returns value or undefined.
	jsutil.Set(obj, "get", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Undefined()
		}
		key := call.Arguments[0].String()
		mu := dataFileLock(store.path)
		mu.Lock()
		data, err := store.loadFile()
		mu.Unlock()
		if err != nil {
			panic(vm.NewGoError(err))
		}
		val, ok := data[key]
		if !ok {
			return goja.Undefined()
		}
		return vm.ToValue(val)
	})

	// set(key, value) — write a JSON-serializable value.
	jsutil.Set(obj, "set", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return goja.Undefined()
		}
		key := call.Arguments[0].String()
		value := call.Arguments[1].Export()
		mu := dataFileLock(store.path)
		mu.Lock()
		defer mu.Unlock()
		data, err := store.loadFile()
		if err != nil {
			panic(vm.NewGoError(err))
		}
		data[key] = value
		if err := store.saveFile(data); err != nil {
			panic(vm.NewGoError(err))
		}
		return goja.Undefined()
	})

	// delete(key) — remove a key.
	jsutil.Set(obj, "delete", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Undefined()
		}
		key := call.Arguments[0].String()
		mu := dataFileLock(store.path)
		mu.Lock()
		defer mu.Unlock()
		data, err := store.loadFile()
		if err != nil {
			panic(vm.NewGoError(err))
		}
		delete(data, key)
		if err := store.saveFile(data); err != nil {
			panic(vm.NewGoError(err))
		}
		return goja.Undefined()
	})

	// keys() — return array of all keys.
	jsutil.Set(obj, "keys", func(call goja.FunctionCall) goja.Value {
		mu := dataFileLock(store.path)
		mu.Lock()
		data, err := store.loadFile()
		mu.Unlock()
		if err != nil {
			panic(vm.NewGoError(err))
		}
		arr := vm.NewArray()
		i := 0
		for k := range data {
			jsutil.Set(arr, intToDataStr(i), k)
			i++
		}
		return arr
	})

	// getAll() — return entire store as an object.
	jsutil.Set(obj, "getAll", func(call goja.FunctionCall) goja.Value {
		mu := dataFileLock(store.path)
		mu.Lock()
		data, err := store.loadFile()
		mu.Unlock()
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(data)
	})

	jsutil.Set(v3, "data", obj)
}

// dataFilePath computes the JSON storage path for a script.
// Given script "voting.js" with working dir "scripts/", produces "scripts/data/voting.json".
// Given working dir "scripts/examples/", produces "scripts/data/voting.json".
func dataFilePath(cfg ScriptConfig) string {
	scriptName := filepath.Base(cfg.Script)
	scriptName = strings.TrimSuffix(scriptName, filepath.Ext(scriptName))
	return filepath.Join(resolveDataDir(cfg.WorkingDir), scriptName+".json")
}

// resolveDataDir returns the scripts/data directory for the given working dir.
// If workingDir IS the scripts dir (i.e. its base name is "scripts"), data lives
// directly inside it. Otherwise we walk up one level, covering the common case
// where the working dir is a subdirectory such as scripts/examples.
func resolveDataDir(workingDir string) string {
	var dataDir string
	if filepath.Base(workingDir) == "scripts" {
		dataDir = filepath.Join(workingDir, "data")
	} else {
		dataDir = filepath.Join(workingDir, "..", "data")
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		abs = filepath.Join(workingDir, "data")
	}
	return abs
}

// loadFile reads the data file without acquiring the mutex (caller must hold it).
func (ds *dataStore) loadFile() (map[string]any, error) {
	data := make(map[string]any)
	raw, err := os.ReadFile(ds.path)
	if errors.Is(err, os.ErrNotExist) {
		return data, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read script data %s: %w", ds.path, err)
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("decode script data %s: %w", ds.path, err)
	}
	if data == nil {
		return nil, fmt.Errorf("decode script data %s: expected JSON object, got null", ds.path)
	}
	return data, nil
}

// saveFile writes the data file without acquiring the mutex (caller must hold it).
func (ds *dataStore) saveFile(data map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(ds.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	// A data-free probe discovers the creation mode after the process umask.
	// It also provides the inherited ACL a replacement would receive.
	probe, err := dataModeProbe(ds.path)
	if err != nil {
		return err
	}
	defer func() { _ = probe.Close(); _ = os.Remove(probe.Name()) }()
	probeInfo, err := probe.Stat()
	if err != nil {
		return fmt.Errorf("stat script data mode probe: %w", err)
	}
	perm := probeInfo.Mode().Perm()
	// Opening without truncation preserves the old write-access check.
	f, err := os.OpenFile(ds.path, os.O_WRONLY, 0)
	if err == nil {
		info, statErr := f.Stat()
		if statErr == nil {
			statErr = checkDataReplacementAccess(f, probe)
		}
		closeErr := f.Close()
		if statErr != nil {
			return fmt.Errorf("prepare script data replacement %s: %w", ds.path, statErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close script data %s: %w", ds.path, closeErr)
		}
		perm = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("open script data for writing %s: %w", ds.path, err)
	}
	return atomicfile.WriteFile(ds.path, raw, perm)
}

func intToDataStr(i int) string {
	return itoa(i)
}

// dataModeProbe creates an empty file with the same requested mode as the
// former os.WriteFile path, without changing the process-wide umask.
func dataModeProbe(path string) (*os.File, error) {
	probe, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".mode-*")
	if err != nil {
		return nil, fmt.Errorf("create temp file for script data mode: %w", err)
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return nil, err
	}
	if err := os.Remove(name); err != nil {
		return nil, err
	}
	// O_EXCL prevents following or modifying any file created in the interval.
	probe, err = os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("create script data mode probe: %w", err)
	}
	return probe, nil
}
