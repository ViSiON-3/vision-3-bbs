package qwk

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestReadZipEntryLimited(t *testing.T) {
	data := buildZip(t, map[string][]byte{"A.DAT": bytes.Repeat([]byte{'x'}, 1000)})
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := readZipEntryLimited(zr.File[0], 1000); err != nil || len(got) != 1000 {
		t.Fatalf("at the limit: %d bytes, err=%v", len(got), err)
	}
	if _, err := readZipEntryLimited(zr.File[0], 999); err == nil {
		t.Fatal("entry over the limit was read")
	}
}

// A CONTROL.DAT that inflates past its cap (a zip bomb in miniature: a few
// KB of deflated zeros) must fail the packet, not be read into memory.
func TestReadPacket_RejectsOversizedMember(t *testing.T) {
	bomb := bytes.Repeat([]byte{0}, maxControlDataSize+1)
	data := buildZip(t, map[string][]byte{"CONTROL.DAT": bomb})
	if len(data) > maxControlDataSize/100 {
		t.Fatalf("test archive did not compress (%d bytes)", len(data))
	}
	_, err := ReadPacket(bytes.NewReader(data), int64(len(data)))
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("oversized CONTROL.DAT: err=%v", err)
	}
}
