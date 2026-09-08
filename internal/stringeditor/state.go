package stringeditor

import "github.com/ViSiON-3/vision-3-bbs/internal/config"

// valueState classifies why an entry looks the way it does in the list.
//
// A blank row is not one thing. The key may be missing from strings.json
// entirely, present but deliberately set to nothing, or blank here while the
// runtime substitutes a compiled-in default. Treating all three as "unused"
// was how #234's investigation mistook 41 live strings for dead ones.
type valueState int

const (
	// stateCustom: a value that differs from the shipped default.
	stateCustom valueState = iota
	// stateDefault: a value identical to the shipped default.
	stateDefault
	// stateFallback: blank or absent here, but the runtime prints a
	// compiled-in default in its place. The entry is live.
	stateFallback
	// stateEmpty: present in strings.json and deliberately empty. The BBS
	// prints nothing.
	stateEmpty
	// stateUnset: absent from strings.json with no runtime fallback. The BBS
	// prints nothing.
	stateUnset
	// stateReserved: a placeholder key that maps to nothing at runtime.
	stateReserved
)

// label names the state for the description bar.
func (s valueState) label() string {
	switch s {
	case stateCustom:
		return "custom value"
	case stateDefault:
		return "ViSiON/3 default"
	case stateFallback:
		return "blank here; the BBS uses its built-in default"
	case stateEmpty:
		return "explicitly empty; the BBS prints nothing"
	case stateUnset:
		return "not set; the BBS prints nothing"
	case stateReserved:
		return "reserved placeholder; not used by the BBS"
	}
	return ""
}

// marker is the one-character flag shown beside the value column.
func (s valueState) marker() string {
	switch s {
	case stateCustom:
		return "*"
	case stateFallback:
		return "~"
	case stateEmpty:
		return "0"
	case stateUnset:
		return "-"
	case stateReserved:
		return "·"
	}
	return " "
}

// stateOf classifies the entry at the given key.
func (m Model) stateOf(key string) valueState {
	if isReservedKey(key) {
		return stateReserved
	}
	value, present := m.values[key]
	if present && value != "" {
		if def, ok := m.shippedDefaults[key]; ok && def == value {
			return stateDefault
		}
		return stateCustom
	}
	// Blank, either because the key is absent or because it is set to "".
	if _, ok := config.StringFallbacks[key]; ok {
		return stateFallback
	}
	if present {
		return stateEmpty
	}
	return stateUnset
}

// previewValue returns the text to draw in the value column. A fallback entry
// shows what the BBS actually prints, so the row is not misleadingly blank.
func (m Model) previewValue(key string) (string, bool) {
	if v := m.getValue(key); v != "" {
		return v, false
	}
	if fb, ok := config.StringFallbacks[key]; ok && fb != "" {
		return fb, true
	}
	return "", false
}

// isReservedKey reports whether a key is an unused placeholder slot.
func isReservedKey(key string) bool {
	return key != "" && key[0] == '_'
}
