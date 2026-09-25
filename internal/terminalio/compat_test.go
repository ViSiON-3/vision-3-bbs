package terminalio

import (
	"bytes"
	"testing"
)

type rwBuf struct{ bytes.Buffer }

func TestCompatWriter_BoldBright(t *testing.T) {
	cases := []struct {
		name   string
		writes []string
		want   string
	}{
		{"bold dark grey", []string{"\x1b[1;30mX"}, "\x1b[1;30;90mX"},
		{"pipe bright green", []string{"\x1b[1;32mA\x1b[0;37mB"}, "\x1b[1;32;92mA\x1b[0;37mB"},
		{"colour change while bold", []string{"\x1b[1;30mA\x1b[37mB"}, "\x1b[1;30;90mA\x1b[37;97mB"},
		{"state spans writes", []string{"\x1b[1m", "\x1b[34mX"}, "\x1b[1;97m\x1b[34;94mX"},
		{"bold off keeps colour normal", []string{"\x1b[1;36mA\x1b[22mB"}, "\x1b[1;36;96mA\x1b[22;36mB"},
		{"bare bold on default", []string{"\x1b[1mA"}, "\x1b[1;97mA"},
		{"reset", []string{"\x1b[1;31mA\x1b[0mB\x1b[mC"}, "\x1b[1;31;91mA\x1b[0mB\x1b[mC"},
		{"non-bold untouched", []string{"\x1b[0;37;40mA\x1b[47m\x1b[2J\x1b[10;5H"}, "\x1b[0;37;40mA\x1b[47m\x1b[2J\x1b[10;5H"},
		{"truecolour untouched", []string{"\x1b[1;38;2;1;2;3mA"}, "\x1b[1;38;2;1;2;3mA"},
		{"incomplete tail passes", []string{"A\x1b[1;3"}, "A\x1b[1;3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf rwBuf
			w := NewCompatWriter(&buf)
			w.SetBoldBright(true)
			for _, s := range tc.writes {
				n, err := w.Write([]byte(s))
				if err != nil || n != len(s) {
					t.Fatalf("Write(%q) = %d, %v", s, n, err)
				}
			}
			if got := buf.String(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCompatWriter_BoldBright_DisabledPassesThrough(t *testing.T) {
	var buf rwBuf
	w := NewCompatWriter(&buf)
	in := "\x1b[1;30mX"
	_, _ = w.Write([]byte(in))
	if buf.String() != in {
		t.Errorf("disabled writer changed output: %q", buf.String())
	}
}

func TestCompatWriter_DECCursor(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"art idiom", "\x1b[s\n\x1b[uX", "\x1b7\n\x1b8X"},
		{"DECSLRM untouched", "\x1b[1;40s", "\x1b[1;40s"},
		{"keyboard protocol untouched", "\x1b[?1u\x1b[>1u", "\x1b[?1u\x1b[>1u"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf rwBuf
			w := NewCompatWriter(&buf)
			w.SetDECCursor(true)
			_, _ = w.Write([]byte(tc.in))
			if got := buf.String(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCompatWriter_RestoreRestoresColourState(t *testing.T) {
	// DECRC restores colours, so the tracked state must follow it: after the
	// restore the terminal is back to plain red, and the next bold must
	// brighten it.
	var buf rwBuf
	w := NewCompatWriter(&buf)
	w.SetBoldBright(true)
	w.SetDECCursor(true)
	_, _ = w.Write([]byte("\x1b[0;31m\x1b[s\x1b[34m\x1b[u\x1b[1mX"))
	if want := "\x1b[0;31m\x1b7\x1b[34m\x1b8\x1b[1;91mX"; buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}
