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

// Validate compares each formatted string in values against the signature of
// the corresponding shipped default, and returns one Problem per mismatch.
//
// A key absent from values is not a problem: the runtime falls back to its
// built-in default, which is by definition correct. A key absent from defaults
// cannot be checked and is skipped rather than guessed at.
func Validate(values, defaults map[string]string) []Problem {
	var problems []Problem

	for _, key := range FormattedKeys {
		value, ok := values[key]
		if !ok || value == "" {
			continue // not configured; the runtime default applies
		}
		def, ok := defaults[key]
		if !ok {
			continue // nothing to compare against
		}
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

// ValidateValue checks a single configured value against its shipped default.
// It returns nil for a key that is not formatted, so the editor can call it for
// every entry without special-casing.
func ValidateValue(key, value, def string) error {
	if !IsFormatted(key) || value == "" {
		return nil
	}
	want, err := formatspec.Parse(def)
	if err != nil {
		return nil // the template is broken; do not blame the sysop's edit
	}
	got, err := formatspec.Parse(value)
	if err != nil {
		return err
	}
	return formatspec.Compatible(got, want)
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
