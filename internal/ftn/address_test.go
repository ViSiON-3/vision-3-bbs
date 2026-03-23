package ftn

import (
	"testing"
)

func TestParseAddress(t *testing.T) {
	tests := []struct {
		input   string
		want    Address
		wantErr bool
	}{
		{"1:123/456", Address{1, 123, 456, 0}, false},
		{"21:1/100", Address{21, 1, 100, 0}, false},
		{"21:4/158.1", Address{21, 4, 158, 1}, false},
		{"2:280/464.0", Address{2, 280, 464, 0}, false},
		{"  21:1/100  ", Address{21, 1, 100, 0}, false},

		// Error cases
		{"", Address{}, true},
		{"21", Address{}, true},
		{"21:1", Address{}, true},
		{":1/100", Address{}, true},
		{"21:/100", Address{}, true},
		{"21:1/", Address{}, true},
		{"0:1/100", Address{}, true},
		{"abc:1/100", Address{}, true},
		{"21:abc/100", Address{}, true},
		{"21:1/abc", Address{}, true},
		{"21:1/100.abc", Address{}, true},
		{"21:1/100.-1", Address{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseAddress(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseAddress(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseAddress(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestAddressString(t *testing.T) {
	tests := []struct {
		addr Address
		want string
	}{
		{Address{21, 1, 100, 0}, "21:1/100"},
		{Address{21, 4, 158, 1}, "21:4/158.1"},
		{Address{1, 123, 456, 0}, "1:123/456"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.addr.String()
			if got != tt.want {
				t.Errorf("Address.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseAddressRoundtrip(t *testing.T) {
	addrs := []string{"1:123/456", "21:1/100", "21:4/158.1", "2:280/464"}
	for _, s := range addrs {
		a, err := ParseAddress(s)
		if err != nil {
			t.Fatalf("ParseAddress(%q) error: %v", s, err)
		}
		got := a.String()
		if got != s {
			t.Errorf("roundtrip: ParseAddress(%q).String() = %q", s, got)
		}
	}
}
