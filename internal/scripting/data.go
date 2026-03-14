package scripting

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dop251/goja"
)

// dataStore manages a per-script JSON key-value store in scripts/data/.
type dataStore struct {
	path string
	mu   sync.Mutex
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
		data := store.load()
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
		data := store.load()
		data[key] = value
		store.save(data)
		return goja.Undefined()
	})

	// delete(key) — remove a key.
	obj.Set("delete", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Undefined()
		}
		key := call.Arguments[0].String()
		data := store.load()
		delete(data, key)
		store.save(data)
		return goja.Undefined()
	})

	// keys() — return array of all keys.
	obj.Set("keys", func(call goja.FunctionCall) goja.Value {
		data := store.load()
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
		data := store.load()
		return vm.ToValue(data)
	})

	v3.Set("data", obj)
}

// dataFilePath computes the JSON storage path for a script.
// Given script "voting.js" in working dir "scripts/", produces "scripts/data/voting.json".
func dataFilePath(cfg ScriptConfig) string {
	scriptName := filepath.Base(cfg.Script)
	scriptName = strings.TrimSuffix(scriptName, filepath.Ext(scriptName))

	// Walk up from working dir to find or create scripts/data/.
	// If working dir is under scripts/, use scripts/data/.
	// Otherwise, use <working_dir>/data/.
	dataDir := filepath.Join(cfg.WorkingDir, "..", "data")
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		abs = filepath.Join(cfg.WorkingDir, "data")
	}
	return filepath.Join(abs, scriptName+".json")
}

func (ds *dataStore) load() map[string]any {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	data := make(map[string]any)
	raw, err := os.ReadFile(ds.path)
	if err != nil {
		return data
	}
	json.Unmarshal(raw, &data) //nolint:errcheck
	return data
}

func (ds *dataStore) save(data map[string]any) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	// Ensure directory exists.
	os.MkdirAll(filepath.Dir(ds.path), 0o755) //nolint:errcheck

	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(ds.path, raw, 0o644) //nolint:errcheck
}

func intToDataStr(i int) string {
	return itoa(i)
}
