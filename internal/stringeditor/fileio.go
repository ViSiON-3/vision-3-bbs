package stringeditor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"

	"github.com/ViSiON-3/vision-3-bbs/internal/config"
)

// LoadStrings reads strings.json and returns its string values. A missing
// file is no strings: the BBS prints the shipped text for every key, and the
// file is only written when there is something of the sysop's to save.
func LoadStrings(path string) (map[string]string, error) {
	values, _, err := loadStringsFile(path)
	return values, err
}

// loadStringsFile reads strings.json as its string values and everything
// else (numeric defColorN, the _defaults fingerprints), which a save keeps.
func loadStringsFile(path string) (values map[string]string, extra map[string]json.RawMessage, err error) {
	values, extra = map[string]string{}, map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return values, extra, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	for k, v := range raw {
		var s string
		// A null is an unset key to the BBS, so it is not a value here
		// either; it is kept with the other entries and written back.
		if string(bytes.TrimSpace(v)) != "null" && json.Unmarshal(v, &s) == nil {
			values[k] = s
		} else {
			extra[k] = v
		}
	}
	return values, extra, nil
}

// InheritedStrings returns what the BBS prints for each key the strings.json
// files at paths set, the later files' keys over the earlier ones': for a
// theme's file, the board's configs/strings.json, then for the theme's
// overlay file, the theme's own. Each file is read as the BBS reads a
// theme's (config.ReadStringsLayer), so a value the BBS skips for a format
// mismatch is skipped here too and the one beneath it shows. A missing file
// sets none.
func InheritedStrings(paths ...string) (map[string]string, error) {
	out := map[string]string{}
	for _, path := range paths {
		values, _, err := config.ReadStringsLayer(path)
		if err != nil {
			return nil, err
		}
		maps.Copy(out, values)
	}
	return out, nil
}

// SaveStrings writes the string map back to the JSON file with sorted keys and
// pretty formatting. Keys the editor has no metadata for are written back
// unchanged, so a legacy or hand-added string is never dropped by a save.
func SaveStrings(path string, strings map[string]string) error {
	return saveStringsFile(path, strings, nil)
}

// saveStringsFile writes values, skipping the catalog's reserved placeholders,
// and extra, the entries that are not strings, as one sorted object.
func saveStringsFile(path string, values map[string]string, extra map[string]json.RawMessage) error {
	all := map[string]json.RawMessage{}
	for k, v := range values {
		if isReservedKey(k) && catalogKey(k) {
			continue // the catalog's placeholder slots map to nothing
		}
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("encoding strings: %w", err)
		}
		all[k] = data
	}
	for k, v := range extra {
		all[k] = v
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make([]keyValue, 0, len(keys))
	for _, k := range keys {
		ordered = append(ordered, keyValue{Key: k, Value: all[k]})
	}

	data, err := marshalOrdered(ordered)
	if err != nil {
		return fmt.Errorf("encoding strings: %w", err)
	}

	// A theme's overlay directory may not exist yet.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

type keyValue struct {
	Key   string
	Value json.RawMessage
}

// marshalOrdered produces a JSON object with keys in the given order.
func marshalOrdered(pairs []keyValue) ([]byte, error) {
	buf := []byte("{\n")
	for i, kv := range pairs {
		keyJSON, err := json.Marshal(kv.Key)
		if err != nil {
			return nil, err
		}
		valJSON, err := compactJSON(kv.Value)
		if err != nil {
			return nil, err
		}
		buf = append(buf, "  "...)
		buf = append(buf, keyJSON...)
		buf = append(buf, ": "...)
		buf = append(buf, valJSON...)
		if i < len(pairs)-1 {
			buf = append(buf, ',')
		}
		buf = append(buf, '\n')
	}
	buf = append(buf, "}\n"...)
	return buf, nil
}

// compactJSON puts a raw value on one line, as the file keeps every entry.
func compactJSON(raw json.RawMessage) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// catalogKey reports whether key has an entry in the editor's catalog.
func catalogKey(key string) bool {
	for _, e := range StringEntries() {
		if e.Key == key {
			return true
		}
	}
	return false
}
