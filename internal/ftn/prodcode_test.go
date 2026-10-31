package ftn

import (
	"bytes"
	"testing"
)

func TestProductRevision(t *testing.T) {
	cases := []struct {
		in           string
		major, minor uint8
	}{
		{"0.9.5", 0, 9},
		{"v1.2.3", 1, 2},
		{"12.300.1", 12, 0},
		{"1", 1, 0},
		{"", 0, 0},
		{"x.y", 0, 0},
	}
	for _, c := range cases {
		major, minor := productRevision(c.in)
		if major != c.major || minor != c.minor {
			t.Errorf("productRevision(%q) = %d.%d, want %d.%d", c.in, major, minor, c.major, c.minor)
		}
	}
}

// A packet from a product with no FTSC code carries 0xFE, not 0 (Fido), so
// the receiving tosser does not log it as another product.
func TestPacketHeaderCarriesUnassignedProductCode(t *testing.T) {
	hdr := NewPacketHeader(3, 633, 2744, 11, 3, 633, 2744, 0, "")
	if hdr.ProdCode != ProdCodeUnassigned {
		t.Errorf("ProdCode = 0x%02X, want 0x%02X", hdr.ProdCode, ProdCodeUnassigned)
	}
	if hdr.ProdCode2 != 0 {
		t.Errorf("ProdCode2 = 0x%02X, want 0", hdr.ProdCode2)
	}

	var buf bytes.Buffer
	if err := WritePacket(&buf, hdr, nil); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	got, _, err := ReadPacket(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	if got.ProdCode != ProdCodeUnassigned || got.ProdRev != hdr.ProdRev || got.ProdRev2 != hdr.ProdRev2 {
		t.Errorf("round trip: code 0x%02X rev %d.%d, want 0x%02X rev %d.%d",
			got.ProdCode, got.ProdRev, got.ProdRev2, ProdCodeUnassigned, hdr.ProdRev, hdr.ProdRev2)
	}
}
