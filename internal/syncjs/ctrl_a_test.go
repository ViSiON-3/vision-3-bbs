package syncjs

import "testing"

func TestParseCtrlA(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"no codes", "Hello World", "Hello World"},
		{"red foreground", "\x01RHello", "\x1b[0;31mHello"},
		{"green foreground", "\x01GHi", "\x1b[0;32mHi"},
		{"blue foreground", "\x01BBye", "\x1b[0;34mBye"},
		{"high intensity", "\x01H\x01RBright Red", "\x1b[1m\x1b[0;31mBright Red"},
		{"normal reset", "\x01RRed\x01NNormal", "\x1b[0;31mRed\x1b[0mNormal"},
		{"background", "\x014Red BG", "\x1b[41mRed BG"},
		{"blink", "\x01IBlink", "\x1b[5mBlink"},
		{"save cursor", "\x01[saved", "\x1b[ssaved"},
		{"restore cursor", "\x01]restored", "\x1b[urestored"},
		{"clear line", "\x01Lcleared", "\x1b[Kcleared"},
		{"unknown code ignored", "\x01Ztext", "text"},
		{"trailing ctrl-a", "text\x01", "text\x01"},
		{"mixed", "\x01WHello \x01H\x01RWorld\x01N!", "\x1b[0;37mHello \x1b[1m\x1b[0;31mWorld\x1b[0m!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseCtrlA(tt.input)
			if got != tt.want {
				t.Errorf("ParseCtrlA(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStripCtrlA(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"Hello", "Hello"},
		{"\x01RHello\x01N World", "Hello World"},
		{"\x01H\x01R\x01GAll codes", "All codes"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := StripCtrlA(tt.input)
			if got != tt.want {
				t.Errorf("StripCtrlA(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAttrToANSI(t *testing.T) {
	tests := []struct {
		name string
		attr uint8
		want string
	}{
		{"default gray on black", 0x07, "\x1b[0;37;40m"},
		{"bright white on black", 0x0F, "\x1b[0;1;37;40m"},
		{"red on blue", 0x14, "\x1b[0;31;44m"},
		{"blink yellow on green", 0xAE, "\x1b[0;1;5;33;42m"},
		{"black on black", 0x00, "\x1b[0;30;40m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AttrToANSI(tt.attr)
			if got != tt.want {
				t.Errorf("AttrToANSI(0x%02X) = %q, want %q", tt.attr, got, tt.want)
			}
		})
	}
}

func TestDisplayLength(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"plain text", "Hello", 5},
		{"with ANSI", "\x1b[31mHello\x1b[0m", 5},
		{"with Ctrl-A", "\x01RHello\x01N", 5},
		{"mixed", "\x01R\x1b[1mHi\x1b[0m\x01N", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := displayLength(tt.input)
			if got != tt.want {
				t.Errorf("displayLength(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
