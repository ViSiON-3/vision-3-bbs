package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/menuset"
	"github.com/ViSiON-3/vision-3-bbs/internal/stringformat"
)

// ThemeStringsFile is the name of a theme's strings file, in its menu set
// directory beside theme.json.
const ThemeStringsFile = "strings.json"

// ThemeStrings is what a theme's strings.json sets: the strings a caller on
// that theme reads in place of the board's configs/strings.json. A key it
// leaves out is the board's. Read-only once loaded.
type ThemeStrings struct {
	values map[string]string // by canonical json key
}

// Empty reports whether t sets no strings, so the board's apply unchanged.
func (t *ThemeStrings) Empty() bool {
	return t == nil || len(t.values) == 0
}

// LoadThemeStrings reads the strings.json of the menu set at menuSetPath:
// the shipped set's file, with its overlay's (menus.d/<theme>/strings.json)
// keys over it one by one, as theme.json is merged. Neither file is no
// strings, not an error. Each file is read as ReadStringsLayer reads it; a
// value it drops for a format mismatch is logged, and the value beneath it
// applies: the theme's own file under the overlay, else the board's.
func LoadThemeStrings(menuSetPath string) (*ThemeStrings, error) {
	set := menuset.FromPath(menuSetPath)
	layers := []string{set.Path(menuset.LayerBase, ThemeStringsFile)}
	if set.HasOverlay() {
		layers = append(layers, set.Path(menuset.LayerOverlay, ThemeStringsFile))
	}
	return LoadStringsLayers(layers...)
}

// LoadStringsLayers reads the strings.json files at paths, each as
// ReadStringsLayer reads it, and returns their strings with each file's
// keys over the ones before it. A missing file has no strings. A value
// dropped for a format mismatch is logged, and the value beneath it applies.
func LoadStringsLayers(paths ...string) (*ThemeStrings, error) {
	values := map[string]string{}
	for _, path := range paths {
		layer, problems, err := ReadStringsLayer(path)
		if err != nil {
			return nil, err
		}
		for _, p := range problems {
			slog.Warn("theme string does not match the arguments the BBS passes it; using the value beneath it until it is corrected",
				"path", path, "key", p.Key, "expected", p.Expected, "problem", p.Detail)
		}
		maps.Copy(values, layer)
	}
	return &ThemeStrings{values: values}, nil
}

// ReadStringsLayer reads one strings.json as a layer over other strings,
// as a theme's file is read: its string values by the key the BBS reads,
// with superseded shipped text and empty-value fallbacks applied as
// LoadStrings applies them. Entries that aren't strings, "_" entries and
// keys the BBS doesn't read are left out, and so is a null, as unset. Where spellings in other capitals
// set the same key, the exact spelling wins, else the one that sorts
// first. A value whose format directives don't match what its call site
// passes is left out and returned as a problem. A missing file has no
// values.
func ReadStringsLayer(path string) (map[string]string, []stringformat.Problem, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading strings %s: %w", path, err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parsing strings %s: %w", path, err)
	}
	// A null is an unset key, as LoadStrings reads it: decoding it into a
	// string gives "", which would blank the string beneath.
	for k, v := range raw {
		if string(bytes.TrimSpace(v)) == "null" {
			delete(raw, k)
		}
	}
	values := canonicalStringValues(stringValuesOf(raw))
	for k, v := range values {
		if current, ok := SupersededString(k, v); ok {
			v = current
		}
		if v == "" {
			if fb, ok := StringFallbacks[k]; ok {
				v = fb
			}
		}
		values[k] = v
	}
	defaults := stringformat.MergeDefaults(StringFallbacks, shippedStrings())
	var problems []stringformat.Problem
	for _, p := range stringformat.Validate(values, StringFallbacks, StringMissingDefaults, defaults) {
		if _, set := values[p.Key]; set {
			problems = append(problems, p)
			delete(values, p.Key)
		}
	}
	return values, problems, nil
}

// canonicalStringValues keys values by the json key each sets, leaving out
// "_" entries and keys the BBS doesn't read. Of several spellings of one
// key, the exact one wins, else the one that sorts first, so the result
// doesn't depend on map order.
func canonicalStringValues(values map[string]string) map[string]string {
	spellings := slices.Sorted(maps.Keys(values))
	out := map[string]string{}
	for _, k := range spellings {
		if strings.HasPrefix(k, "_") {
			continue
		}
		key, ok := CanonicalStringKey(k)
		if !ok {
			continue
		}
		if _, exact := values[key]; exact && key != k {
			continue
		}
		if _, have := out[key]; have && key != k {
			continue
		}
		out[key] = values[k]
	}
	return out
}

// WithTheme returns c with the strings t sets over it. c itself is not
// changed, and the legacy colour slots stay c's: a theme's colours are its
// theme.json's.
func (c StringsConfig) WithTheme(t *ThemeStrings) StringsConfig {
	if !t.Empty() {
		setStringFields(&c, t.values)
	}
	return c
}
