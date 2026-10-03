package menu

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

func TestProbeSessionEncoding(t *testing.T) {
	tests := []struct {
		name  string
		reply string
		want  ansi.OutputMode
	}{
		{"glyph took one cell", "\x1b[5;2R", ansi.OutputModeUTF8},
		{"glyph took three cells", "\x1b[5;4R", ansi.OutputModeCP437},
		{"no reply", "", ansi.OutputModeAuto},
		{"unexpected column", "\x1b[5;9R", ansi.OutputModeAuto},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := newTestSession(tt.reply)
			t.Cleanup(func() { resetSessionIH(sess) })

			if got := ProbeSessionEncoding(sess, time.Second); got != tt.want {
				t.Errorf("ProbeSessionEncoding = %v, want %v", got, tt.want)
			}
			if out, want := sess.out.String(), ansi.EncodingProbe+ansi.EncodingProbeClear; out != want {
				t.Errorf("wrote %q, want %q", out, want)
			}
		})
	}
}

// The probe runs before sessionHandler's own terminal.ReadLine prompts, so it
// must not leave an input handler reading the session behind it.
func TestProbeSessionEncodingStopsItsReader(t *testing.T) {
	sess := newTestSession("\x1b[5;2R")
	t.Cleanup(func() { resetSessionIH(sess) })
	ProbeSessionEncoding(sess, time.Second)
	if _, ok := sessionInputHandlers.Load(sess); ok {
		t.Fatal("ProbeSessionEncoding left the session input handler running")
	}
}
