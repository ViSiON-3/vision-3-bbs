package main

import (
	"testing"
)

func TestParseConnect(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantUser string
		wantAddr string
		wantErr  bool
	}{
		{
			name:     "valid ssh URL",
			input:    "ssh://sysop@bbs.example.com:6023",
			wantUser: "sysop",
			wantAddr: "bbs.example.com:6023",
			wantErr:  false,
		},
		{
			name:    "non-ssh scheme",
			input:   "http://sysop@bbs.example.com:6023",
			wantErr: true,
		},
		{
			name:    "missing user",
			input:   "ssh://bbs.example.com:6023",
			wantErr: true,
		},
		{
			name:    "missing port",
			input:   "ssh://sysop@bbs.example.com",
			wantErr: true,
		},
		{
			name:    "missing host",
			input:   "ssh://sysop@:6023",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			user, addr, err := parseConnect(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseConnect(%q) = (%q, %q, nil); want error", tc.input, user, addr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseConnect(%q) error: %v", tc.input, err)
			}
			if user != tc.wantUser {
				t.Errorf("parseConnect(%q) user = %q; want %q", tc.input, user, tc.wantUser)
			}
			if addr != tc.wantAddr {
				t.Errorf("parseConnect(%q) addr = %q; want %q", tc.input, addr, tc.wantAddr)
			}
		})
	}
}
