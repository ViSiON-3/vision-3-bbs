package menu

import (
	"time"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
)

// ProbeSessionEncoding asks the caller's terminal whether it decodes UTF-8 by
// drawing a box-drawing glyph and reading back how far the cursor moved. It
// returns OutputModeAuto when the terminal does not answer within timeout or
// the answer settles nothing; the caller then falls back to the terminal type.
//
// It runs at connect, before sessionHandler reads from the session directly,
// so it stops the session input handler it reads through; anything the caller
// typed during the probe is dropped with it.
func ProbeSessionEncoding(s ssh.Session, timeout time.Duration) ansi.OutputMode {
	_, _ = s.Write([]byte(ansi.EncodingProbe)) // best-effort probe
	_, col, ok := getSessionIH(s).ReadCursorReport(timeout)
	resetSessionIH(s)
	_, _ = s.Write([]byte(ansi.EncodingProbeClear)) // best-effort display
	if !ok {
		return ansi.OutputModeAuto
	}
	return ansi.EncodingFromProbeColumn(col)
}
