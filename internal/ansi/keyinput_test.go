package ansi

import (
	"bytes"
	"testing"
)

// TestDecodeExtendedKey_CP437UnmappedByteDropped exercises the drop path for
// a CP437 byte whose Cp437ToUnicode entry is 0: it must not be stored (which
// would put invalid/unintended data into stored records) and must not be
// echoed, matching how line readers silently drop anything else they won't
// accept.
func TestDecodeExtendedKey_CP437UnmappedByteDropped(t *testing.T) {
	line, echo, pending := DecodeExtendedKey(nil, OutputModeCP437, 0, nil)
	if len(line) != 0 {
		t.Errorf("line = %q, want empty (unmapped byte must not be stored)", line)
	}
	if len(echo) != 0 {
		t.Errorf("echo = %v, want nil (unmapped byte must not be echoed)", echo)
	}
	if len(pending) != 0 {
		t.Errorf("pending = %v, want nil", pending)
	}
}

// TestDecodeExtendedKey_CP437 maps a CP437 byte to its Unicode character and
// echoes the raw byte.
func TestDecodeExtendedKey_CP437(t *testing.T) {
	line, echo, pending := DecodeExtendedKey([]byte("x"), OutputModeCP437, 0xC4, nil)
	if string(line) != "x─" {
		t.Errorf("line = %q, want %q", line, "x─")
	}
	if !bytes.Equal(echo, []byte{0xC4}) {
		t.Errorf("echo = % X, want C4", echo)
	}
	if pending != nil {
		t.Errorf("pending = % X, want nil", pending)
	}
}

// TestDecodeExtendedKey_UTF8Accumulates feeds "€" (E2 82 AC) one byte at a
// time: nothing is stored or echoed until the sequence completes.
func TestDecodeExtendedKey_UTF8Accumulates(t *testing.T) {
	var line, echo, pending []byte
	for i, b := range []byte("€") {
		line, echo, pending = DecodeExtendedKey(line, OutputModeUTF8, b, pending)
		if i < 2 && (len(line) != 0 || len(echo) != 0) {
			t.Fatalf("byte %d: line=%q echo=%q, want nothing before completion", i, line, echo)
		}
	}
	if string(line) != "€" || string(echo) != "€" || len(pending) != 0 {
		t.Errorf("line=%q echo=%q pending=% X, want € / € / none", line, echo, pending)
	}
}

// TestDecodeExtendedKey_UTF8MalformedResync: a stray lead byte followed by a
// valid character's lead byte is dropped without swallowing that character.
func TestDecodeExtendedKey_UTF8MalformedResync(t *testing.T) {
	var line, pending []byte
	for _, b := range []byte{0xC3, 0xC3, 0xA9} {
		line, _, pending = DecodeExtendedKey(line, OutputModeUTF8, b, pending)
	}
	if string(line) != "é" || len(pending) != 0 {
		t.Errorf("line=%q pending=% X, want é and none pending", line, pending)
	}
}

func TestBackspaceRune(t *testing.T) {
	cases := map[string]string{"": "", "a": "", "ab": "a", "aé": "a", "a€": "a", "€é": "€"}
	for in, want := range cases {
		if got := string(BackspaceRune([]byte(in))); got != want {
			t.Errorf("BackspaceRune(%q) = %q, want %q", in, got, want)
		}
	}
}
