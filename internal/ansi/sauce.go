package ansi

import (
	"bytes"
	"strings"
)

// Sauce is the descriptive part of a SAUCE record, the metadata block art
// editors append to ANSI files.
type Sauce struct {
	Title  string
	Author string
	Group  string
}

// ReadSAUCE returns the title, author and group of data's SAUCE record,
// converted from CP437 with their padding trimmed. ok is false when data
// ends in no SAUCE record.
func ReadSAUCE(data []byte) (s Sauce, ok bool) {
	const sauceSize = 128
	if len(data) < sauceSize {
		return Sauce{}, false
	}
	rec := data[len(data)-sauceSize:]
	if !bytes.HasPrefix(rec, []byte("SAUCE")) {
		return Sauce{}, false
	}
	// ID(5) Version(2) Title(35) Author(20) Group(20), each space or NUL
	// padded.
	return Sauce{
		Title:  sauceField(rec[7:42]),
		Author: sauceField(rec[42:62]),
		Group:  sauceField(rec[62:82]),
	}, true
}

// sauceField converts one CP437 field to text, dropping control characters
// and the padding.
func sauceField(field []byte) string {
	var b strings.Builder
	for _, c := range field {
		switch {
		case c < 0x20:
			continue
		case c < 0x80:
			b.WriteByte(c)
		default:
			b.WriteRune(Cp437ToUnicode[c])
		}
	}
	return strings.TrimSpace(b.String())
}
