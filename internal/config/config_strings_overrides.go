package config

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/ViSiON-3/vision-3-bbs/internal/stringhistory"
	configtemplates "github.com/ViSiON-3/vision-3-bbs/templates/configs"
)

// StringBasesKey is the strings.json entry that records, for each string the
// sysop has customised, a fingerprint (stringhistory.Hash) of the shipped
// default it replaced. When a later release changes that default, the
// fingerprint no longer matches, and doctor and ./strings say so. The BBS
// itself ignores the entry, as it does every "_" key.
const StringBasesKey = "_defaults"

// stringKeys is the set of json keys StringsConfig reads.
var stringKeys = sync.OnceValue(func() map[string]bool {
	keys := map[string]bool{}
	t := reflect.TypeFor[StringsConfig]()
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Type.Kind() != reflect.String {
			continue
		}
		if name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ","); name != "" && name != "-" {
			keys[name] = true
		}
	}
	return keys
})

// IsStringKey reports whether key is the json tag of a string the BBS
// reads from strings.json.
func IsStringKey(key string) bool {
	return stringKeys()[key]
}

// CanonicalStringKey returns the json tag a strings.json key sets.
// encoding/json matches field names without regard to case, so
// "PauseString" sets pauseString. ok is false for a key the BBS doesn't
// read.
func CanonicalStringKey(key string) (string, bool) {
	if stringKeys()[key] {
		return key, true
	}
	for k := range stringKeys() {
		if strings.EqualFold(k, key) {
			return k, true
		}
	}
	return "", false
}

// StringDefault is what the BBS prints for key when strings.json leaves it
// out.
func StringDefault(key string) string {
	return EffectiveString(key, "", false)
}

// EffectiveString is what LoadStrings makes of one key: value if present
// (taken from the file), otherwise the shipped default, with the same
// superseded, fallback and missing-key rules applied. The format check is
// not applied.
func EffectiveString(key, value string, present bool) string {
	return EffectiveStringWith(shippedStrings(), key, value, present)
}

// shippedStrings is the embedded template, parsed once, for read-only use
// here.
var shippedStrings = sync.OnceValue(func() map[string]string {
	shipped, _ := configtemplates.StringDefaults()
	return shipped
})

// EffectiveStringWith is EffectiveString with the shipped defaults given.
func EffectiveStringWith(shipped map[string]string, key, value string, present bool) string {
	v := value
	if !present {
		v = shipped[key]
	} else if current, ok := SupersededString(key, v); ok {
		v = current
	}
	if v == "" {
		if fb, ok := StringFallbacks[key]; ok {
			v = fb
		}
	}
	if !present {
		if def, ok := StringMissingDefaults[key]; ok {
			v = def
		}
	}
	return v
}

// StringBases reads the StringBasesKey entry of a parsed strings.json.
func StringBases(raw map[string]json.RawMessage) map[string]string {
	var bases map[string]string
	if v, ok := raw[StringBasesKey]; ok {
		_ = json.Unmarshal(v, &bases)
	}
	return bases
}

// ChangedStringDefaults lists, sorted, the customised strings whose shipped
// default has changed since the sysop set them: those with a recorded
// fingerprint that no longer matches the current default.
func ChangedStringDefaults(raw map[string]json.RawMessage) []string {
	var changed []string
	for key, hash := range StringBases(raw) {
		if _, present := raw[key]; !present || !IsStringKey(key) {
			continue
		}
		if hash != stringhistory.Hash(StringDefault(key)) {
			changed = append(changed, key)
		}
	}
	slices.Sort(changed)
	return changed
}

// StringsTrim is what TrimStrings found in a strings.json.
type StringsTrim struct {
	// Kept is the file as it should be: the sysop's own values, any
	// legacy colour slot defaults that still apply, and the fingerprints.
	Kept map[string]json.RawMessage
	// Shipped are the keys removed for holding a value the key shipped
	// with, in this release or an earlier one, or one that prints the same
	// as leaving the key out.
	Shipped []string
	// Unknown are the keys removed because the BBS doesn't read them.
	Unknown []string
}

// TrimStrings works out the overrides-only form of a parsed strings.json
// (#1100): it drops every value the key has shipped with, in any release,
// every value that prints the same as the default, and every key the BBS
// doesn't read; it keeps everything else, which is the sysop's own work, and
// records the current default's fingerprint for each string kept, unless
// one is already recorded.
func TrimStrings(raw map[string]json.RawMessage) StringsTrim {
	t := StringsTrim{Kept: map[string]json.RawMessage{}}
	bases := StringBases(raw)
	if bases == nil {
		bases = map[string]string{}
	}
	legacy := legacyColorSlots(raw)
	for _, key := range slices.Sorted(maps.Keys(raw)) {
		msg := raw[key]
		switch {
		case key == StringBasesKey:
			continue // rebuilt below
		case strings.HasPrefix(key, "_"):
			t.Kept[key] = msg
			continue
		case strings.HasPrefix(key, "defColor") && len(key) == len("defColor1"):
			if n := key[len(key)-1]; n >= '1' && n <= '7' && legacy[n-'1'] != "" {
				t.Kept[key] = msg
				continue
			}
		}
		canon, ok := CanonicalStringKey(key)
		if !ok {
			t.Unknown = append(t.Unknown, key)
			continue
		}
		if canon != key {
			if _, exact := raw[canon]; exact {
				t.Shipped = append(t.Shipped, key) // the exact spelling is the one kept
				continue
			}
			key = canon // written back in the spelling the BBS documents
		}
		var v string
		if json.Unmarshal(msg, &v) != nil {
			t.Kept[key] = msg // not a string: leave it for the sysop to look at
			continue
		}
		if stringhistory.Shipped(key, v) || EffectiveString(key, v, true) == StringDefault(key) {
			t.Shipped = append(t.Shipped, key)
			continue
		}
		t.Kept[key] = msg
		if _, ok := bases[key]; !ok {
			bases[key] = stringhistory.Hash(StringDefault(key))
		}
	}
	for key := range bases {
		if _, kept := t.Kept[key]; !kept {
			delete(bases, key)
		}
	}
	if len(bases) > 0 {
		data, _ := json.Marshal(bases)
		t.Kept[StringBasesKey] = data
	}
	return t
}
