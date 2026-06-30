package qwk

import (
	"archive/zip"
	"bytes"
	"testing"
	"time"
)

func TestFirstBlockID(t *testing.T) {
	mk := func(s string) []byte {
		b := make([]byte, BlockSize)
		for i := range b {
			b[i] = ' '
		}
		copy(b, s)
		return b
	}
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"plain id", mk("VISION3"), "VISION3"},
		{"lowercased", mk("vision3"), "VISION3"},
		{"all spaces", mk(""), ""},
		{"leading spaces", mk("   ABC"), "ABC"},
		{"too long capped at 8", mk("LONGERNAME123"), "LONGERNA"},
		{"short input", []byte("xx"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstBlockID(tt.in); got != tt.want {
				t.Errorf("firstBlockID = %q, want %q", got, tt.want)
			}
		})
	}
}

// buildREPZip writes a zip containing <name> whose .MSG content is the given
// first block followed by one formatted message.
func buildREPZip(t *testing.T, name string, firstBlock []byte) []byte {
	t.Helper()
	var msgBuf bytes.Buffer
	msgBuf.Write(firstBlock)
	m := formatMessage(PacketMessage{
		Conference: 1, Number: 1, To: "SysOp", Subject: "Hi",
		DateTime: time.Date(2026, 3, 5, 10, 0, 0, 0, time.UTC), Body: "reply",
	})
	numBlocks := (len(m) + BlockSize - 1) / BlockSize
	padded := make([]byte, numBlocks*BlockSize)
	for i := range padded {
		padded[i] = ' '
	}
	copy(padded, m)
	msgBuf.Write(padded)

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	w.Write(msgBuf.Bytes())
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return zipBuf.Bytes()
}

func TestReadREPPacket_ExtractsBBSIDAndPayload(t *testing.T) {
	first := make([]byte, BlockSize)
	for i := range first {
		first[i] = ' '
	}
	copy(first, "VISION3")
	data := buildREPZip(t, "VISION3.MSG", first)

	p, err := ReadREPPacket(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err != nil {
		t.Fatalf("ReadREPPacket: %v", err)
	}
	if p.BBSID != "VISION3" {
		t.Errorf("BBSID = %q, want VISION3", p.BBSID)
	}
	if len(p.Messages) != 1 {
		t.Errorf("messages = %d, want 1", len(p.Messages))
	}
	if len(p.Payload) == 0 {
		t.Error("payload should be non-empty")
	}
}

func TestReadREP_StillReturnsMessages(t *testing.T) {
	first := make([]byte, BlockSize)
	for i := range first {
		first[i] = ' '
	}
	data := buildREPZip(t, "VISION3.MSG", first)

	msgs, err := ReadREP(bytes.NewReader(data), int64(len(data)), "VISION3")
	if err != nil {
		t.Fatalf("ReadREP: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("messages = %d, want 1", len(msgs))
	}
}
