// Package keys turns the {enter}-style escapes agents type into terminal bytes.
// Forked from v3agents/internal/keys for the local regression MCP.
package keys

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

var named = map[string]string{
	"enter": "\r", "esc": "\x1b", "up": "\x1b[A", "down": "\x1b[B",
	"right": "\x1b[C", "left": "\x1b[D", "bs": "\x08", "tab": "\t",
}

// ascii replaces punctuation CP437 lacks with the ASCII a typist would use.
var ascii = strings.NewReplacer(
	"‘", "'", "’", "'", "“", `"`, "”", `"`,
	"—", "-", "–", "-", "…", "...", " ", " ",
)

// Stroke is one keypress: one character of text, or one named key such as
// {enter}, whose escape sequence goes out as a unit.
type Stroke struct {
	Bytes []byte
	Key   string // the key's name for a named or control key; empty for text
}

// Parse turns input into the bytes to send to a board whose term encoding is
// encoding ("cp437" or "utf8"). Text for a cp437 board is encoded as CP437;
// a character CP437 cannot show is an error.
func Parse(input, encoding string) ([]byte, error) {
	strokes, err := Strokes(input, encoding)
	if err != nil {
		return nil, err
	}
	var out []byte
	for _, k := range strokes {
		out = append(out, k.Bytes...)
	}
	return out, nil
}

// Strokes is Parse split into keypresses, for typing one key at a time.
func Strokes(input, encoding string) ([]Stroke, error) {
	var out []Stroke
	text := func(s string) error {
		if encoding != "cp437" {
			for _, r := range s {
				out = append(out, Stroke{Bytes: []byte(string(r))})
			}
			return nil
		}
		for _, r := range ascii.Replace(s) {
			if r < utf8.RuneSelf {
				out = append(out, Stroke{Bytes: []byte{byte(r)}})
				continue
			}
			b, ok := charmap.CodePage437.EncodeRune(r)
			if !ok {
				return fmt.Errorf("this board can't show %q (U+%04X); use plain ASCII instead", r, r)
			}
			out = append(out, Stroke{Bytes: []byte{b}})
		}
		return nil
	}
	start := 0
	for i := 0; i < len(input); i++ {
		if input[i] != '{' {
			continue
		}
		if err := text(input[start:i]); err != nil {
			return nil, err
		}
		if i+1 < len(input) && input[i+1] == '{' {
			out = append(out, Stroke{Bytes: []byte{'{'}})
			i++
			start = i + 1
			continue
		}
		end := strings.IndexByte(input[i:], '}')
		if end < 0 {
			return nil, fmt.Errorf("unclosed { at %d", i)
		}
		name := input[i+1 : i+end]
		seq, ok := named[name]
		if !ok {
			seq, ok = ctrl(name)
		}
		if !ok {
			return nil, fmt.Errorf("unknown key {%s}; use enter, esc, up, down, left, right, bs, tab, ctrl-a to ctrl-z, or {{ for a literal {", name)
		}
		name = strings.ToLower(name)
		out = append(out, Stroke{Bytes: []byte(seq), Key: name})
		i += end
		start = i + 1
	}
	if err := text(input[start:]); err != nil {
		return nil, err
	}
	return out, nil
}

// ctrl maps "ctrl-z" to 0x1a, for editors that save or abort on a control key.
func ctrl(name string) (string, bool) {
	l, ok := strings.CutPrefix(strings.ToLower(name), "ctrl-")
	if !ok || len(l) != 1 || l[0] < 'a' || l[0] > 'z' {
		return "", false
	}
	return string(rune(l[0] - 'a' + 1)), true
}
