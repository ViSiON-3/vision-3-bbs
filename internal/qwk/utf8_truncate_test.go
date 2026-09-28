package qwk

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// QWK header fields are byte-counted (25 bytes for To, From and Subject), but
// the cut lands on a rune boundary: "AB" then "é"s has its 25th byte inside
// an "é", which is dropped whole.
func TestFormatMessage_FieldsCutOnRuneBoundary(t *testing.T) {
	long := "AB" + strings.Repeat("é", 20)
	want := "AB" + strings.Repeat("é", 11) // 24 bytes; a 12th "é" would need 26
	data := formatMessage(PacketMessage{
		Conference: 1, Number: 1, To: long, From: long, Subject: long,
		DateTime: time.Date(2026, 3, 5, 14, 30, 0, 0, time.UTC), Body: "x",
	})
	for _, f := range []struct {
		name       string
		start, end int
	}{{"To", 21, 46}, {"From", 46, 71}, {"Subject", 71, 96}} {
		got := strings.TrimRight(string(data[f.start:f.end]), " ")
		if !utf8.ValidString(got) {
			t.Errorf("%s field %q is not valid UTF-8", f.name, got)
		}
		if got != want {
			t.Errorf("%s field = %q, want %q", f.name, got, want)
		}
	}
}

// A BBS ID is capped at 8 bytes (a DOS filename) without splitting a
// character: "ABCDEFG" then "é" keeps just "ABCDEFG".
func TestBBSIDCutOnRuneBoundary(t *testing.T) {
	const id = "abcdefgé"

	if pw := NewPacketWriter(id, "Test", "Admin"); pw.bbsID != "ABCDEFG" {
		t.Errorf("NewPacketWriter bbsID = %q, want ABCDEFG", pw.bbsID)
	}
	// ȿ upper-cases to the 3-byte Ȿ: capping first and upper-casing after
	// would leave a 9-byte ID.
	if pw := NewPacketWriter("aaaaaaȿ", "Test", "Admin"); len(pw.bbsID) > 8 || !utf8.ValidString(pw.bbsID) {
		t.Errorf("NewPacketWriter bbsID = %q (%d bytes), want valid UTF-8 within 8 bytes", pw.bbsID, len(pw.bbsID))
	}

	msg := PacketMessage{Conference: 1, Number: 1, From: "A", To: "B", Subject: "S", Body: "x",
		DateTime: time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC)}
	var rep bytes.Buffer
	if err := WriteREP(&rep, id, []PacketMessage{msg}); err != nil {
		t.Fatalf("WriteREP: %v", err)
	}
	var net bytes.Buffer
	nm := NetMessage{Conference: 1, Number: 1, From: "A", To: "B", Subject: "S", Body: "x", DateTime: msg.DateTime}
	if err := WriteNetREP(&net, id, []NetMessage{nm}, NetREPOptions{NoHeaders: true}); err != nil {
		t.Fatalf("WriteNetREP: %v", err)
	}

	for name, data := range map[string][]byte{"REP": rep.Bytes(), "net REP": net.Bytes()} {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("%s: not a valid zip: %v", name, err)
		}
		found := false
		for _, f := range zr.File {
			if !utf8.ValidString(f.Name) {
				t.Errorf("%s: entry name %q is not valid UTF-8", name, f.Name)
			}
			if f.Name == "ABCDEFG.MSG" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no ABCDEFG.MSG entry", name)
		}
	}
}
