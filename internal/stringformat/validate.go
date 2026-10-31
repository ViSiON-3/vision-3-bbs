// Package stringformat checks that a configured BBS string still matches the
// argument list its call site passes.
//
// Only some strings are formatted. Most are printed verbatim, and a few of
// those legitimately contain a percent sign that is not a directive at all --
// badUDRatio ends "(|15|RA%|09)", where the % is a literal followed by a pipe
// colour code. Running every string through a format parser would report those
// as broken, so validation is scoped to FormattedKeys: the keys that a call
// site actually hands to fmt. A test derives that set from the source and
// fails if it drifts, so a newly formatted string cannot slip past unchecked.
package stringformat

import (
	"fmt"
	"sort"

	"github.com/ViSiON-3/vision-3-bbs/internal/formatspec"
	"github.com/ViSiON-3/vision-3-bbs/internal/stringhook"
)

// Problem describes one string whose directives no longer match its call site.
type Problem struct {
	Key      string // strings.json key
	Expected string // the shipped default's signature
	Detail   string // what is wrong with the configured value
}

// Error renders the problem for a log line or a status bar.
func (p Problem) Error() string {
	return fmt.Sprintf("%s: %s (expected %s)", p.Key, p.Detail, p.Expected)
}

// Validate compares each formatted string against the signature of the
// arguments its call site supplies, and returns one Problem per mismatch.
//
// values holds what strings.json carries, fallbacks holds the defaults the
// runtime substitutes for an empty field, missing holds the defaults it uses
// only for a key absent from the file, and defaults holds the shipped template.
// The effective value is the configured one where it is non-empty, the missing
// default for an absent key, and the fallback otherwise, matching what the BBS
// will actually print.
//
// An empty effective value is a problem in its own right for a formatted key.
// Call sites pass their arguments unconditionally, so fmt renders
// "%!(EXTRA string=...)" onto the caller's screen rather than printing nothing.
// The exception is a key in missing that the file sets blank: that switches the
// string off, and its call sites check for it. A key absent from defaults cannot
// be checked and is skipped rather than guessed at.
func Validate(values, fallbacks, missing, defaults map[string]string) []Problem {
	var problems []Problem

	for _, key := range FormattedKeys {
		def, ok := defaults[key]
		if !ok || def == "" {
			continue // nothing to compare against
		}
		value := effectiveValue(key, values, fallbacks, missing)
		want, err := formatspec.Parse(def)
		if err != nil {
			// A malformed shipped default is a bug in the template, reported
			// against the template rather than against the sysop's edit.
			problems = append(problems, Problem{
				Key:      key,
				Expected: "a valid format string",
				Detail:   fmt.Sprintf("the shipped default is malformed: %v", err),
			})
			continue
		}
		if value == "" {
			if _, canSwitchOff := missing[key]; !canSwitchOff && want.Arity() > 0 {
				problems = append(problems, Problem{
					Key:      key,
					Expected: want.String(),
					Detail: fmt.Sprintf("is empty, so the BBS prints %%!(EXTRA ...) instead of "+
						"nothing: its %d argument(s) are supplied regardless", want.Arity()),
				})
			}
			continue
		}
		value, check := formattedPart(key, value)
		if !check {
			continue
		}
		got, err := formatspec.Parse(value)
		if err != nil {
			problems = append(problems, Problem{
				Key:      key,
				Expected: want.String(),
				Detail:   err.Error(),
			})
			continue
		}
		if err := formatspec.Compatible(got, want); err != nil {
			problems = append(problems, Problem{
				Key:      key,
				Expected: want.String(),
				Detail:   err.Error(),
			})
		}
	}

	sort.Slice(problems, func(i, j int) bool { return problems[i].Key < problems[j].Key })
	return problems
}

// ValidateValue checks a single configured value against the arguments its call
// site supplies. It returns nil for a key that is not formatted, so the editor
// can call it for every entry without special-casing.
//
// fallback is the value the runtime substitutes when the field is empty, or ""
// when there is none. Clearing a formatted string that has no fallback is
// reported: the call site still passes its arguments, so the caller sees
// "%!(EXTRA ...)" rather than nothing.
func ValidateValue(key, value, fallback, def string) error {
	if !IsFormatted(key) || def == "" {
		return nil
	}
	want, err := formatspec.Parse(def)
	if err != nil {
		return nil // the template is broken; do not blame the sysop's edit
	}
	if value == "" {
		if fallback != "" || want.Arity() == 0 {
			return nil // the runtime substitutes, or nothing is supplied
		}
		return fmt.Errorf("is empty, so the BBS prints %%!(EXTRA ...) instead of "+
			"nothing: its %d argument(s) are supplied regardless", want.Arity())
	}
	value, check := formattedPart(key, value)
	if !check {
		return nil
	}
	got, err := formatspec.Parse(value)
	if err != nil {
		return err
	}
	return formatspec.Compatible(got, want)
}

// formattedPart returns the part of a configured value that the call site's
// arguments are formatted into, and whether it needs checking at all.
//
// A value that runs a script (see stringhook) is never formatted: the script
// receives the arguments instead. A value that displays a file formats only
// the text after the file name, and with no text there is nothing to format.
//
// A key in stringhook.NoHookKeys never runs a hook, so its value is checked as
// written.
func formattedPart(key, value string) (string, bool) {
	if !stringhook.Supported(key) {
		return value, true
	}
	h := stringhook.Parse(value)
	switch h.Kind {
	case stringhook.Script:
		return "", false
	case stringhook.File:
		return h.Rest, h.Rest != ""
	case stringhook.Escaped:
		return h.Rest, true
	}
	return value, true
}

// effectiveValue returns what the BBS will actually print for a key: the
// configured value where it is non-empty, the missing-key default where the key
// is absent and has one, otherwise the runtime's fallback.
func effectiveValue(key string, values, fallbacks, missing map[string]string) string {
	v, present := values[key]
	if v != "" {
		return v
	}
	if m := missing[key]; !present && m != "" {
		return m
	}
	return fallbacks[key]
}

// MergeDefaults combines the shipped template with the runtime's fallback table
// into the signature source Validate compares against.
//
// Both are needed. A key added after the template last shipped lives only in
// the fallback table, and without its signature here Validate would have
// nothing to compare a sysop's override against and would skip it silently.
// The template wins where both carry a key, so a distribution can adjust a
// default without the compiled-in value overriding it.
func MergeDefaults(fallbacks, shipped map[string]string) map[string]string {
	merged := make(map[string]string, len(fallbacks)+len(shipped))
	for k, v := range fallbacks {
		merged[k] = v
	}
	for k, v := range shipped {
		if v != "" {
			merged[k] = v
		}
	}
	return merged
}

// IsFormatted reports whether a key's value is passed to fmt at some call site.
func IsFormatted(key string) bool {
	_, ok := formattedKeySet[key]
	return ok
}

// Signature returns a human-readable description of the arguments a key's
// value must consume, for a message shown to the sysop.
func Signature(key, def string) string {
	spec, err := formatspec.Parse(def)
	if err != nil {
		return ""
	}
	return spec.String()
}

var formattedKeySet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(FormattedKeys))
	for _, k := range FormattedKeys {
		set[k] = struct{}{}
	}
	return set
}()
