package stringeditor

import (
	"github.com/ViSiON-3/vision-3-bbs/internal/config"
	"github.com/ViSiON-3/vision-3-bbs/internal/stringhistory"
)

// valueState classifies why an entry looks the way it does in the list.
//
// A blank row is not one thing. The key may be left out of strings.json,
// so the BBS prints the default, set to nothing on purpose, or blank here
// while the runtime substitutes a compiled-in default. Treating these alike
// was how #234's investigation mistook 41 live strings for dead ones.
type valueState int

const (
	// stateCustom: a value that prints differently from the default.
	stateCustom valueState = iota
	// stateDefault: left out of strings.json, or set to what prints the
	// same; the BBS prints the shipped default.
	stateDefault
	// stateFallback: blank here, but the runtime prints a compiled-in
	// default in its place. The entry is live.
	stateFallback
	// stateEmpty: present in strings.json and deliberately empty. The BBS
	// prints nothing.
	stateEmpty
	// stateUnset: left out of strings.json with no default at all. The BBS
	// prints nothing.
	stateUnset
	// stateReserved: a placeholder key that maps to nothing at runtime.
	stateReserved
	// stateChanged: a custom value whose shipped default has changed since
	// the sysop set it (#1100).
	stateChanged
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
	case stateChanged:
		return "custom value; the ViSiON/3 default has changed since you set it (F4 takes the new one, F5 keeps yours)"
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
	case stateChanged:
		return "!"
	}
	return " "
}

// stateOf classifies the entry at the given key.
func (m Model) stateOf(key string) valueState {
	if isReservedKey(key) {
		return stateReserved
	}
	value, present := m.values[key]
	if !present || m.isDefault(key) {
		if def, _ := m.defaultFor(key); def == "" {
			return stateUnset
		}
		return stateDefault
	}
	if m.defaultChanged(key) {
		return stateChanged // a deliberate blank included: F5 keeps it
	}
	if value != "" {
		return stateCustom
	}
	// Set blank on purpose.
	if _, ok := config.StringFallbacks[key]; ok {
		return stateFallback
	}
	return stateEmpty
}

// defaultChanged reports whether the shipped default of a customised string
// has changed since the sysop set it, and they haven't kept theirs since.
func (m Model) defaultChanged(key string) bool {
	base, ok := m.bases[key]
	if !ok || m.kept[key] {
		return false
	}
	return base != stringhistory.Hash(m.effective(key, "", false))
}

// previousDefault is the default a customised string replaced, when its
// default has since changed and the history knows the old text.
func (m Model) previousDefault(key string) (string, bool) {
	if !m.defaultChanged(key) {
		return "", false
	}
	return stringhistory.ByHash(key, m.bases[key])
}

// previewValue returns the text to draw in the value column. A blank entry
// with a fallback shows, dimmed, what the BBS actually prints, so the row is
// not misleadingly blank; a string left out shows its default.
func (m Model) previewValue(key string) (string, bool) {
	v, present := m.values[key]
	if !present {
		return m.effective(key, "", false), false
	}
	if v != "" {
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
