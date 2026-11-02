package ftn

import (
	"testing"

	"golang.org/x/text/encoding/charmap"
)

// #744: header fields are decoded by the set their CHRS kludge names, as
// message bodies are, and exported back in the same set.
func TestHeaderStringsUseTheCHRSCharset(t *testing.T) {
	const text = "Привет, мир" // "Hello, world" in Russian
	for _, tc := range []struct {
		chrs string
		cm   *charmap.Charmap
	}{
		{"CP866 2", charmap.CodePage866},
		{"+7_FIDO 2", charmap.CodePage866},
		{"+7 2", charmap.CodePage866},
		{"cp866 2", charmap.CodePage866}, // labels are not always upper case
		{"CP1251 2", charmap.Windows1251},
		{"KOI8-R 2", charmap.KOI8R},
		{"KOI8-U 2", charmap.KOI8U},
	} {
		wire, err := tc.cm.NewEncoder().String(text)
		if err != nil {
			t.Fatal(err)
		}
		if got := DecodeFTNString(wire, tc.chrs); got != text {
			t.Errorf("DecodeFTNString(%q, %q) = %q, want %q", wire, tc.chrs, got, text)
		}
		if got := EncodeFTNString(text, tc.chrs); got != wire {
			t.Errorf("EncodeFTNString(%q, %q) = %q, want %q", text, tc.chrs, got, wire)
		}
	}
}

// CP437, an unknown label and no label at all keep the CP437 reading, and a
// UTF-8 label keeps the bytes as they are.
func TestHeaderStringsDefaultToCP437(t *testing.T) {
	for _, chrs := range []string{"", "CP437 2", "LATIN-1 2", "IBMPC 2"} {
		if got := DecodeFTNString("Caf\x82 \xc4", chrs); got != "Café ─" {
			t.Errorf("DecodeFTNString with %q = %q, want %q", chrs, got, "Café ─")
		}
		if got := EncodeFTNString("Café ─", chrs); got != "Caf\x82 \xc4" {
			t.Errorf("EncodeFTNString with %q = %q", chrs, got)
		}
	}
	// Both spellings of the UTF-8 label keep the bytes, as for bodies.
	for _, chrs := range []string{"UTF-8 4", "UTF8 4", "utf8 4"} {
		if got := DecodeFTNString("Café", chrs); got != "Café" {
			t.Errorf("%q label: %q", chrs, got)
		}
		if got := EncodeFTNString("Café", chrs); got != "Café" {
			t.Errorf("%q label on export: %q", chrs, got)
		}
	}
	// A character the set has no byte for still becomes '?'.
	if got := EncodeFTNString("日本", "CP866 2"); got != "??" {
		t.Errorf("EncodeFTNString(CJK, CP866) = %q, want ??", got)
	}
}
