package jam

import "testing"

func TestParseAddress(t *testing.T) {
	tests := []struct {
		input   string
		zone    int
		net     int
		node    int
		point   int
		wantErr bool
	}{
		{"1:103/705", 1, 103, 705, 0, false},
		{"1:103/705.0", 1, 103, 705, 0, false},
		{"1:103/705.2", 1, 103, 705, 2, false},
		{"21:3/110", 21, 3, 110, 0, false},
		{"2:5020/1042.1", 2, 5020, 1042, 1, false},
		{"65535:65535/65535.65535", 65535, 65535, 65535, 65535, false},
		{"invalid", 0, 0, 0, 0, true},
		// Out of range for the 16-bit fields these occupy in an FTN packet:
		// accepting them would wrap once converted to uint16.
		{"0:1/100", 0, 0, 0, 0, true},
		{"-1:1/100", 0, 0, 0, 0, true},
		{"65536:1/100", 0, 0, 0, 0, true},
		{"1:65536/100", 0, 0, 0, 0, true},
		{"1:1/65536", 0, 0, 0, 0, true},
		{"1:1/100.65536", 0, 0, 0, 0, true},
		{"1:-1/100", 0, 0, 0, 0, true},
		{"1:1/-1", 0, 0, 0, 0, true},
		{"1:1/100.-1", 0, 0, 0, 0, true},
		{"1:2", 0, 0, 0, 0, true},
		{"abc:def/ghi", 0, 0, 0, 0, true},
		{"", 0, 0, 0, 0, true},
	}

	for _, tt := range tests {
		addr, err := ParseAddress(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseAddress(%q) expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseAddress(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if addr.Zone != tt.zone || addr.Net != tt.net || addr.Node != tt.node || addr.Point != tt.point {
			t.Errorf("ParseAddress(%q) = %d:%d/%d.%d, want %d:%d/%d.%d",
				tt.input, addr.Zone, addr.Net, addr.Node, addr.Point,
				tt.zone, tt.net, tt.node, tt.point)
		}
	}
}

func TestFidoAddressString(t *testing.T) {
	tests := []struct {
		addr FidoAddress
		full string
		d2   string
	}{
		{FidoAddress{1, 103, 705, 0}, "1:103/705", "103/705"},
		{FidoAddress{1, 103, 705, 2}, "1:103/705.2", "103/705"},
		{FidoAddress{21, 3, 110, 0}, "21:3/110", "3/110"},
	}

	for _, tt := range tests {
		if got := tt.addr.String(); got != tt.full {
			t.Errorf("String() = %q, want %q", got, tt.full)
		}
		if got := tt.addr.String2D(); got != tt.d2 {
			t.Errorf("String2D() = %q, want %q", got, tt.d2)
		}
	}
}
