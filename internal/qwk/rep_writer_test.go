package qwk

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// buildREP is a small helper that returns the bytes of a REP packet for the
// given messages.
func buildREP(t *testing.T, bbsID string, msgs []PacketMessage) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteREP(&buf, bbsID, msgs); err != nil {
		t.Fatalf("WriteREP failed: %v", err)
	}
	return buf.Bytes()
}

func TestWriteREP_ContainsMSGFile(t *testing.T) {
	data := buildREP(t, "vision3", []PacketMessage{{
		Conference: 1,
		Number:     1,
		From:       "TestUser",
		To:         "SysOp",
		Subject:    "Hi",
		DateTime:   time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC),
		Body:       "Body text",
	}})

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a valid zip: %v", err)
	}
	found := false
	for _, f := range zr.File {
		if f.Name == "VISION3.MSG" {
			found = true
		}
	}
	if !found {
		t.Error("REP packet missing VISION3.MSG (bbsID should be upper-cased)")
	}
}

func TestREP_RoundTrip(t *testing.T) {
	in := []PacketMessage{
		{
			Conference: 1,
			Number:     1,
			From:       "Alice",
			To:         "Bob",
			Subject:    "First reply",
			DateTime:   time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC),
			Body:       "Line one\nLine two\nLine three",
		},
		{
			Conference: 7,
			Number:     2,
			From:       "Alice",
			To:         "Carol",
			Subject:    "Second reply",
			DateTime:   time.Date(2026, 3, 6, 11, 30, 0, 0, time.UTC),
			Body:       "A different message body.",
		},
	}

	data := buildREP(t, "VISION3", in)

	out, err := ReadREP(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err != nil {
		t.Fatalf("ReadREP failed: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("round-trip message count: want %d, got %d", len(in), len(out))
	}

	for i := range in {
		if out[i].Conference != in[i].Conference {
			t.Errorf("msg %d conference: want %d, got %d", i, in[i].Conference, out[i].Conference)
		}
		if out[i].To != in[i].To {
			t.Errorf("msg %d to: want %q, got %q", i, in[i].To, out[i].To)
		}
		if out[i].Subject != in[i].Subject {
			t.Errorf("msg %d subject: want %q, got %q", i, in[i].Subject, out[i].Subject)
		}
		// Body lines should survive the QWK 0xE3 encoding round-trip.
		wantFirstLine := strings.SplitN(in[i].Body, "\n", 2)[0]
		if !strings.Contains(out[i].Body, wantFirstLine) {
			t.Errorf("msg %d body: %q does not contain %q", i, out[i].Body, wantFirstLine)
		}
	}
}

// TestREP_ConferenceHighByte documents that the in-message header preserves the
// full 16-bit conference number (positions 123-124), even though the separate
// .NDX index record only retains the low byte.
func TestREP_ConferenceHighByte(t *testing.T) {
	data := buildREP(t, "VISION3", []PacketMessage{{
		Conference: 300, // > 255, exercises the high byte
		Number:     1,
		To:         "SysOp",
		Subject:    "High conf",
		DateTime:   time.Now(),
		Body:       "x",
	}})

	out, err := ReadREP(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err != nil {
		t.Fatalf("ReadREP failed: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 message, got %d", len(out))
	}
	if out[0].Conference != 300 {
		t.Errorf("conference high byte lost: want 300, got %d", out[0].Conference)
	}
}

func TestReadREP_EmptyMSG(t *testing.T) {
	// A REP with only a spacer block and no messages should parse to zero
	// messages without error.
	data := buildREP(t, "VISION3", nil)
	out, err := ReadREP(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err != nil {
		t.Fatalf("ReadREP failed: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("want 0 messages, got %d", len(out))
	}
}

func TestReadREP_NotAZip(t *testing.T) {
	data := []byte("this is not a zip archive at all")
	_, err := ReadREP(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err == nil {
		t.Fatal("expected error for non-zip data")
	}
}

func TestReadREP_TruncatedData(t *testing.T) {
	// .MSG present but shorter than a single block: must error, not panic.
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	w, _ := zw.Create("VISION3.MSG")
	w.Write([]byte("short")) // < BlockSize
	zw.Close()

	data := zipBuf.Bytes()
	_, err := ReadREP(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err == nil {
		t.Fatal("expected error for truncated .MSG data")
	}
}

func TestReadREP_StopsOnBadBlockCount(t *testing.T) {
	// Build a spacer + a header whose block-count field is garbage. The parser
	// should stop cleanly and return no messages rather than looping or panicking.
	buf := make([]byte, BlockSize*2)
	for i := range buf {
		buf[i] = ' '
	}
	// Corrupt the block-count field (positions 116-121) of the second block.
	copy(buf[BlockSize+116:BlockSize+122], []byte("??????"))

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	w, _ := zw.Create("VISION3.MSG")
	w.Write(buf)
	zw.Close()

	data := zipBuf.Bytes()
	out, err := ReadREP(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err != nil {
		t.Fatalf("ReadREP returned error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("want 0 messages from corrupt block, got %d", len(out))
	}
}

func TestWriteREP_EmitsBBSIDInFirstBlock(t *testing.T) {
	data := buildREP(t, "vision3", []PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Hi",
			DateTime: time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC), Body: "reply"},
	})

	p, err := ReadREPPacket(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err != nil {
		t.Fatalf("ReadREPPacket: %v", err)
	}
	if p.BBSID != "VISION3" {
		t.Errorf("first-block BBSID = %q, want VISION3", p.BBSID)
	}
}

func TestWriteREP_CapsBBSIDToEightChars(t *testing.T) {
	// A configured ID longer than the 8-char QWK limit must be truncated for
	// both the .MSG filename and the first-block ID (matching NewPacketWriter).
	data := buildREP(t, "LongerName123", []PacketMessage{
		{Conference: 1, Number: 1, To: "SysOp", Subject: "Hi",
			DateTime: time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC), Body: "reply"},
	})

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	var msgName string
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".MSG") {
			msgName = f.Name
		}
	}
	if msgName != "LONGERNA.MSG" {
		t.Errorf("REP .MSG filename = %q, want LONGERNA.MSG", msgName)
	}

	p, err := ReadREPPacket(bytes.NewReader(data), int64(len(data)), "LONGERNA")
	if err != nil {
		t.Fatalf("ReadREPPacket: %v", err)
	}
	if p.BBSID != "LONGERNA" {
		t.Errorf("first-block BBSID = %q, want LONGERNA", p.BBSID)
	}
}

func TestWriteREP_EmitsHeadersDAT(t *testing.T) {
	longSubject := "A very long subject line beyond the 25-character base limit"
	data := buildREP(t, "VISION3", []PacketMessage{
		{Conference: 1, Number: 1, From: "SysOp", To: "SomebodyWithALongHandle",
			Subject: longSubject, DateTime: time.Date(2026, 3, 5, 14, 30, 0, 0, time.UTC), Body: "x"},
	})

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	var hdr []byte
	for _, f := range zr.File {
		if f.Name == "HEADERS.DAT" {
			rc, _ := f.Open()
			hdr, _ = io.ReadAll(rc)
			rc.Close()
		}
	}
	if hdr == nil {
		t.Fatal("REP packet missing HEADERS.DAT")
	}
	// First message header is at offset 128 -> section [80].
	got := parseHeadersDAT(hdr)
	h, ok := got[128]
	if !ok {
		t.Fatalf("no section at offset 128; sections=%v", got)
	}
	if h.Subject != longSubject {
		t.Errorf("HEADERS.DAT subject: want full, got %q", h.Subject)
	}
	if h.To != "SomebodyWithALongHandle" {
		t.Errorf("HEADERS.DAT to: want full, got %q", h.To)
	}
}

func TestREP_RoundTripReplyToNumber(t *testing.T) {
	in := []PacketMessage{
		{Conference: 1, Number: 1, From: "a", To: "b", Subject: "First",
			DateTime: time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC), Body: "root"},
		{Conference: 1, Number: 2, From: "a", To: "b", Subject: "Re: First",
			DateTime: time.Date(2026, 3, 5, 11, 0, 0, 0, time.UTC), Body: "reply",
			ReplyToNumber: 1},
	}
	data := buildREP(t, "VISION3", in)
	out, err := ReadREP(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err != nil {
		t.Fatalf("ReadREP: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 messages, got %d", len(out))
	}
	if out[0].ReplyToNumber != 0 {
		t.Errorf("root ReplyToNumber: want 0, got %d", out[0].ReplyToNumber)
	}
	if out[1].ReplyToNumber != 1 {
		t.Errorf("reply ReplyToNumber: want 1, got %d", out[1].ReplyToNumber)
	}
}
