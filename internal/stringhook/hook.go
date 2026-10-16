// Package stringhook recognises strings.json values that hand off to a script
// or a display file instead of printing their text, after PCBoard's text-table
// records:
//
//	!name [args]    run the V3 script door name in place of the text
//	%file.ext [text] display file.ext, then print text
//	!|text          print "!text" (an escape for text that starts with "!")
//
// Parsing lives here, apart from the menu package that runs the hooks, so the
// format validator and the strings editor can recognise a hook value without
// importing the runtime.
package stringhook

import (
	"regexp"
	"strings"
)

// Kind says what a value does.
type Kind int

const (
	// Text is an ordinary value, printed as it stands.
	Text Kind = iota
	// Script runs the V3 script door Target, passing Args.
	Script
	// File displays the file Target, then prints Rest.
	File
	// Escaped prints Rest, which already carries the leading "!".
	Escaped
)

// Hook is a parsed value.
type Hook struct {
	Kind Kind
	// Target is the door name for Script and the file name for File.
	Target string
	// Args are the space-separated words after a script's door name.
	Args []string
	// Rest is the text a File prints after the file, or the text an Escaped
	// value prints. It may still hold format verbs, which the caller formats
	// with the call site's arguments.
	Rest string
	// Prefix is the part of the value that named the hook, kept so a hook
	// that cannot run can print the value as written.
	Prefix string
}

// scriptRe matches "!name" with optional arguments. The name uses the
// characters a doors.json key may hold; anything else after the "!" is text.
var scriptRe = regexp.MustCompile(`^!([A-Za-z0-9_\-]+)(?:[ \t]+(.*))?$`)

// fileRe matches "%name.ext" with optional text after a single space. The file
// name uses the same characters as a %%file.ext%% include and must carry an
// extension, which keeps format strings such as "%d of %d" or "%s (default)"
// from being read as a file.
var fileRe = regexp.MustCompile(`(?s)^%([A-Za-z0-9_\-]+\.[A-Za-z0-9]+)(?: (.*))?$`)

// Parse reports what a value does. A value that is not a hook comes back as
// Text with Rest set to the whole value.
func Parse(value string) Hook {
	switch {
	case strings.HasPrefix(value, "!|"):
		return Hook{Kind: Escaped, Rest: "!" + value[2:], Prefix: "!|"}
	case strings.HasPrefix(value, "!"):
		// A script value is one line; a multi-line value starting with "!" is
		// text that happens to begin with an exclamation.
		if m := scriptRe.FindStringSubmatch(value); m != nil {
			h := Hook{Kind: Script, Target: m[1], Prefix: value}
			if args := strings.Fields(m[2]); len(args) > 0 {
				h.Args = args
			}
			return h
		}
	case strings.HasPrefix(value, "%"):
		if m := fileRe.FindStringSubmatch(value); m != nil {
			prefix := "%" + m[1]
			if len(value) > len(prefix) {
				prefix += " "
			}
			return Hook{Kind: File, Target: m[1], Rest: m[2], Prefix: prefix}
		}
	}
	return Hook{Kind: Text, Rest: value}
}

// IsHook reports whether a value runs a script or displays a file.
func IsHook(value string) bool {
	k := Parse(value).Kind
	return k == Script || k == File
}
