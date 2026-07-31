package testterm

import "testing"

func TestSGRAppliesToSubsequentCells(t *testing.T) {
	tt := New(20, 3)
	tt.Write([]byte("\x1b[1;36;44mHi\x1b[0mLo"))

	h := tt.Cell(1, 1)
	if h.Fg != 36 || h.Bg != 44 || !h.Bold {
		t.Errorf("Cell(1,1) = %+v, want Fg=36 Bg=44 Bold=true", h)
	}
	l := tt.Cell(1, 3)
	if l.Fg != -1 || l.Bg != -1 || l.Bold {
		t.Errorf("Cell(1,3) = %+v, want defaults after reset", l)
	}
}

// A bare ESC[m is a reset, as is ESC[0m.
func TestSGRBareResets(t *testing.T) {
	tt := New(20, 3)
	tt.Write([]byte("\x1b[31mA\x1b[mB"))

	if got := tt.Cell(1, 1).Fg; got != 31 {
		t.Errorf("Cell(1,1).Fg = %d, want 31", got)
	}
	if got := tt.Cell(1, 2).Fg; got != -1 {
		t.Errorf("Cell(1,2).Fg = %d, want -1", got)
	}
}

func TestSGRBrightColours(t *testing.T) {
	tt := New(20, 3)
	tt.Write([]byte("\x1b[93;104mX"))

	c := tt.Cell(1, 1)
	if c.Fg != 93 || c.Bg != 104 {
		t.Errorf("Cell(1,1) = %+v, want Fg=93 Bg=104", c)
	}
}

// SGR must never reach Unhandled, or every coloured write would trip a test
// that asserts the list is empty.
func TestSGRIsNotRecordedAsUnhandled(t *testing.T) {
	tt := New(20, 3)
	tt.Write([]byte("\x1b[0m\x1b[1;36;44m\x1b[22m\x1b[39;49mX"))

	if got := tt.Unhandled(); len(got) != 0 {
		t.Errorf("Unhandled() = %q, want empty", got)
	}
}

// Unknown SGR parameters (blink, reverse, 256-colour) must not vanish
// silently: internal/ansi's |B12 pipe code expands to
// ESC[104m ESC[48;5;12m, and the 48;5;12 half of that is unmodelled.
func TestSGRUnknownParametersAreRecorded(t *testing.T) {
	tt := New(20, 3)
	tt.Write([]byte("\x1b[5mA"))
	if got := tt.Unhandled(); len(got) != 1 || got[0] != "SGR 5" {
		t.Errorf("Unhandled() after ESC[5m = %q, want [%q]", got, "SGR 5")
	}

	tt2 := New(20, 3)
	tt2.Write([]byte("\x1b[7mA"))
	if got := tt2.Unhandled(); len(got) != 1 || got[0] != "SGR 7" {
		t.Errorf("Unhandled() after ESC[7m = %q, want [%q]", got, "SGR 7")
	}

	tt3 := New(20, 3)
	tt3.Write([]byte("\x1b[48;5;12mA"))
	want := []string{"SGR 48", "SGR 5", "SGR 12"}
	got := tt3.Unhandled()
	if len(got) != len(want) {
		t.Fatalf("Unhandled() after ESC[48;5;12m = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Unhandled()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// A reset combined with other codes in the same sequence must apply left to
// right, so the reset only clears what came before it. This shape is
// production-reachable: screen.go emits ESC[0;34m directly, and pipe codes
// |01-|07 all expand to ESC[0;3Xm.
func TestSGRResetMidSequenceThenNewColour(t *testing.T) {
	tt := New(20, 3)
	tt.Write([]byte("\x1b[31;0;36mX"))

	c := tt.Cell(1, 1)
	if c.Fg != 36 || c.Bold || c.Bg != -1 {
		t.Errorf("Cell(1,1) = %+v, want Fg=36 Bold=false Bg=-1", c)
	}
}

func TestSGRBoldOffAndDefaultColours(t *testing.T) {
	tt := New(20, 3)
	tt.Write([]byte("\x1b[1;31;41mA\x1b[22mB\x1b[39mC\x1b[49mD"))

	if c := tt.Cell(1, 1); !c.Bold || c.Fg != 31 || c.Bg != 41 {
		t.Errorf("Cell(1,1) = %+v, want Bold Fg=31 Bg=41", c)
	}
	if c := tt.Cell(1, 2); c.Bold || c.Fg != 31 {
		t.Errorf("Cell(1,2) = %+v, want bold off, Fg still 31", c)
	}
	if c := tt.Cell(1, 3); c.Fg != -1 || c.Bg != 41 {
		t.Errorf("Cell(1,3) = %+v, want Fg default, Bg still 41", c)
	}
	if c := tt.Cell(1, 4); c.Bg != -1 {
		t.Errorf("Cell(1,4) = %+v, want Bg default", c)
	}
}
