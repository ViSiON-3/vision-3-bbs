package scripting

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dop251/goja"
)

// globalDataLocks provides per-file-path mutexes so concurrent sessions writing
// the same script's data file do not overwrite each other.
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
	obj.Set("get", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Undefined()
		}
		key := call.Arguments[0].String()
		mu := dataFileLock(store.path)
		mu.Lock()
		data := store.loadFile()
		mu.Unlock()
		val, ok := data[key]
		if !ok {
			return goja.Undefined()
		}
		return vm.ToValue(val)
	})

	// set(key, value) — write a JSON-serializable value.
	obj.Set("set", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return goja.Undefined()
		}
		key := call.Arguments[0].String()
		value := call.Arguments[1].Export()
		mu := dataFileLock(store.path)
		mu.Lock()
		defer mu.Unlock()
		data := store.loadFile()
		data[key] = value
		if err := store.saveFile(data); err != nil {
			panic(vm.NewGoError(err))
		}
		return goja.Undefined()
	})

	// delete(key) — remove a key.
	obj.Set("delete", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Undefined()
		}
		key := call.Arguments[0].String()
		mu := dataFileLock(store.path)
		mu.Lock()
		defer mu.Unlock()
		data := store.loadFile()
		delete(data, key)
		if err := store.saveFile(data); err != nil {
			panic(vm.NewGoError(err))
		}
		return goja.Undefined()
	})

	// keys() — return array of all keys.
	obj.Set("keys", func(call goja.FunctionCall) goja.Value {
		mu := dataFileLock(store.path)
		mu.Lock()
		data := store.loadFile()
		mu.Unlock()
		arr := vm.NewArray()
		i := 0
		for k := range data {
			arr.Set(intToDataStr(i), k)
			i++
		}
		return arr
	})

	// getAll() — return entire store as an object.
	obj.Set("getAll", func(call goja.FunctionCall) goja.Value {
		mu := dataFileLock(store.path)
		mu.Lock()
		data := store.loadFile()
		mu.Unlock()
		return vm.ToValue(data)
	})

	v3.Set("data", obj)
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
func (ds *dataStore) loadFile() map[string]any {
	data := make(map[string]any)
	raw, err := os.ReadFile(ds.path)
	if err != nil {
		return data
	}
	json.Unmarshal(raw, &data) //nolint:errcheck
	return data
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
	return os.WriteFile(ds.path, raw, 0o644)
}

func intToDataStr(i int) string {
	return itoa(i)
}
