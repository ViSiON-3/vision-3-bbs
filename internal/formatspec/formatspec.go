// Package formatspec parses the fmt directives in a BBS string and describes
// the arguments it expects, so a sysop's edit can be checked against the
// argument list the call site actually passes.
//
// Editing a formatted string is the one way to break a prompt without the BBS
// noticing: Sprintf does not fail on the wrong arity, it prints
// %!d(MISSING) or %!(EXTRA int=3) into the middle of the message the user
// sees. Comparing the edited string's signature against the shipped default's
// catches that before it reaches a caller.
//
// The parser understands what fmt understands: escaped %%, the flags
// "+-# 0", a width and precision given literally or as a * argument, and an
// explicit argument index in [n] form. Padding and flags never affect the
// signature, so cosmetic changes such as %s to %-20s raise no warning.
package formatspec

import (
	"fmt"
	"sort"
	"strings"
)

// ArgKind is the coarse type a directive requires of its argument. It is
// deliberately coarse: the goal is to catch a %d where a %s belongs, not to
// reproduce the type checker.
type ArgKind int

const (
	// KindAny accepts anything (%v, %T).
	KindAny ArgKind = iota
	// KindString accepts a string (%s, %q).
	KindString
	// KindInt accepts an integer (%d, %b, %o, %O, %c, %U).
	KindInt
	// KindFloat accepts a float (%e, %E, %f, %F, %g, %G).
	KindFloat
	// KindBool accepts a bool (%t).
	KindBool
	// KindPointer accepts a pointer (%p).
	KindPointer
	// KindIntOrString accepts either (%x, %X).
	KindIntOrString
)

// String names the kind for a diagnostic message.
func (k ArgKind) String() string {
	switch k {
	case KindAny:
		return "any"
	case KindString:
		return "string"
	case KindInt:
		return "integer"
	case KindFloat:
		return "number"
	case KindBool:
		return "true/false"
	case KindPointer:
		return "pointer"
	case KindIntOrString:
		return "integer or string"
	}
	return "unknown"
}

// Accepts reports whether a value of kind got satisfies a directive wanting k.
func (k ArgKind) Accepts(got ArgKind) bool {
	if k == got || k == KindAny || got == KindAny {
		return true
	}
	switch k {
	case KindIntOrString:
		return got == KindInt || got == KindString
	case KindInt, KindString:
		return got == KindIntOrString
	}
	return false
}

// Spec is the argument signature of a format string: the kind required at each
// argument position, in the order Sprintf will consume them.
type Spec struct {
	Args []ArgKind
}

// Arity returns how many arguments the string consumes.
func (s Spec) Arity() int { return len(s.Args) }

// String renders the signature for a diagnostic message.
func (s Spec) String() string {
	if len(s.Args) == 0 {
		return "no arguments"
	}
	parts := make([]string, len(s.Args))
	for i, a := range s.Args {
		parts[i] = a.String()
	}
	return fmt.Sprintf("%d argument(s): %s", len(s.Args), strings.Join(parts, ", "))
}

// Parse returns the argument signature of a format string.
//
// It reports an error only for a directive fmt itself could not render: a
// trailing bare %, an unterminated or non-numeric argument index, or an
// unknown verb character. A well-formed string with zero directives parses to
// an empty signature, not an error.
func Parse(format string) (Spec, error) {
	// kinds accumulates by 1-based argument position, since an explicit index
	// can bind out of order and can bind the same argument twice.
	kinds := map[int]ArgKind{}
	next := 1 // the argument the next directive consumes
	maxArg := 0

	bind := func(pos int, kind ArgKind) error {
		if prev, seen := kinds[pos]; seen {
			// Two directives on the same argument must agree, or no single
			// value can satisfy both.
			switch {
			case prev.Accepts(kind) && kind.Accepts(prev):
				if prev == KindAny {
					kinds[pos] = kind
				}
			default:
				return fmt.Errorf("argument %d is used as both %s and %s", pos, prev, kind)
			}
		} else {
			kinds[pos] = kind
		}
		if pos > maxArg {
			maxArg = pos
		}
		return nil
	}

	runes := []rune(format)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '%' {
			continue
		}
		i++
		if i >= len(runes) {
			return Spec{}, fmt.Errorf("format ends with a bare %%; write %%%% for a literal percent sign")
		}
		if runes[i] == '%' {
			continue // an escaped literal percent, not a directive
		}

		start := i

		// Flags. These are cosmetic and never change the signature.
		for i < len(runes) && strings.ContainsRune("+-# 0", runes[i]) {
			i++
		}

		// Explicit argument index, e.g. %[2]s.
		if i < len(runes) && runes[i] == '[' {
			idx, end, err := parseIndex(runes, i)
			if err != nil {
				return Spec{}, err
			}
			next = idx
			i = end
		}

		// Width: digits, or * to take it from an argument.
		var err error
		if i, err = parseSize(runes, i, &next, bind); err != nil {
			return Spec{}, err
		}

		// Precision: .digits, or .* to take it from an argument.
		if i < len(runes) && runes[i] == '.' {
			i++
			// A precision may carry its own argument index.
			if i < len(runes) && runes[i] == '[' {
				idx, end, err := parseIndex(runes, i)
				if err != nil {
					return Spec{}, err
				}
				next = idx
				i = end
			}
			if i, err = parseSize(runes, i, &next, bind); err != nil {
				return Spec{}, err
			}
		}

		// The verb may carry its own argument index too, e.g. %6.2[3]f.
		if i < len(runes) && runes[i] == '[' {
			idx, end, err := parseIndex(runes, i)
			if err != nil {
				return Spec{}, err
			}
			next = idx
			i = end
		}

		if i >= len(runes) {
			return Spec{}, fmt.Errorf("unfinished directive %%%s", string(runes[start:]))
		}
		kind, ok := verbKind(runes[i])
		if !ok {
			return Spec{}, fmt.Errorf("unknown verb %%%c", runes[i])
		}
		if err := bind(next, kind); err != nil {
			return Spec{}, err
		}
		next++
	}

	// Flatten to a dense, ordered signature. A gap means the format skips an
	// argument it never prints; treat the gap as "any" so arity still matches.
	args := make([]ArgKind, maxArg)
	for pos := 1; pos <= maxArg; pos++ {
		args[pos-1] = kinds[pos] // absent positions default to KindAny
	}
	return Spec{Args: args}, nil
}

// parseIndex reads an explicit argument index "[n]" starting at runes[i] and
// returns the index and the position just past the closing bracket.
func parseIndex(runes []rune, i int) (idx, end int, err error) {
	j := i + 1
	n := 0
	digits := 0
	for j < len(runes) && runes[j] >= '0' && runes[j] <= '9' {
		n = n*10 + int(runes[j]-'0')
		digits++
		j++
	}
	if digits == 0 || j >= len(runes) || runes[j] != ']' {
		return 0, 0, fmt.Errorf("malformed argument index: expected [n], got %q", tail(runes, i))
	}
	if n < 1 {
		return 0, 0, fmt.Errorf("argument index [%d] must be 1 or greater", n)
	}
	return n, j + 1, nil
}

// parseSize consumes a width or precision, binding an integer argument when it
// is given as *. It returns the position after the size.
func parseSize(runes []rune, i int, next *int, bind func(int, ArgKind) error) (int, error) {
	if i < len(runes) && runes[i] == '*' {
		if err := bind(*next, KindInt); err != nil {
			return 0, err
		}
		*next++
		return i + 1, nil
	}
	for i < len(runes) && runes[i] >= '0' && runes[i] <= '9' {
		i++
	}
	return i, nil
}

// tail returns a short excerpt for an error message.
func tail(runes []rune, i int) string {
	end := i + 6
	if end > len(runes) {
		end = len(runes)
	}
	return string(runes[i:end])
}

// verbKind maps a verb character to the argument kind it requires.
func verbKind(r rune) (ArgKind, bool) {
	switch r {
	case 'v', 'T':
		return KindAny, true
	case 's', 'q':
		return KindString, true
	case 'd', 'b', 'o', 'O', 'c', 'U':
		return KindInt, true
	case 'e', 'E', 'f', 'F', 'g', 'G':
		return KindFloat, true
	case 't':
		return KindBool, true
	case 'p':
		return KindPointer, true
	case 'x', 'X':
		return KindIntOrString, true
	}
	return KindAny, false
}

// Compatible reports whether a string with signature got can stand in for one
// with signature want. Both must consume the same number of arguments, and
// each argument must accept the value the call site passes.
func Compatible(want, got Spec) error {
	if len(want.Args) != len(got.Args) {
		return fmt.Errorf("takes %d argument(s) but %d are supplied", len(got.Args), len(want.Args))
	}
	var problems []string
	for i := range want.Args {
		if !got.Args[i].Accepts(want.Args[i]) {
			problems = append(problems, fmt.Sprintf(
				"argument %d is used as %s but %s is supplied", i+1, got.Args[i], want.Args[i]))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}
