package ansi

import "testing"

func TestParseCPR(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		row, col int
		ok       bool
	}{
		{"plain reply", "\x1b[12;4R", 12, 4, true},
		{"single digits", "\x1b[1;2R", 1, 2, true},
		{"keystrokes before the reply", "ab\r\x1b[5;3R", 5, 3, true},
		{"bytes after the reply", "\x1b[5;3Rxyz", 5, 3, true},
		{"partial reply", "\x1b[5;3", 0, 0, false},
		{"missing column", "\x1b[5R", 0, 0, false},
		{"other CSI sequence", "\x1b[A", 0, 0, false},
		{"empty", "", 0, 0, false},
		{"garbage", "hello", 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row, col, ok := ParseCPR([]byte(tt.in))
			if row != tt.row || col != tt.col || ok != tt.ok {
				t.Errorf("ParseCPR(%q) = (%d, %d, %v), want (%d, %d, %v)",
					tt.in, row, col, ok, tt.row, tt.col, tt.ok)
			}
		})
	}
}

func TestEncodingFromProbeColumn(t *testing.T) {
	tests := []struct {
		col  int
		want OutputMode
	}{
		{2, OutputModeUTF8},  // the 3-byte glyph drew as one cell
		{3, OutputModeUTF8},  // drawn double-width
		{4, OutputModeCP437}, // each byte drew as its own cell
		{1, OutputModeAuto},  // nothing drawn
		{5, OutputModeAuto},
		{0, OutputModeAuto},
	}
	for _, tt := range tests {
		if got := EncodingFromProbeColumn(tt.col); got != tt.want {
			t.Errorf("EncodingFromProbeColumn(%d) = %v, want %v", tt.col, got, tt.want)
		}
	}
}
