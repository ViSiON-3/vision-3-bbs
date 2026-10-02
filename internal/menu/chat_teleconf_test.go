package menu

import (
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
)

type tappedTestSession struct {
	*testSession
	tap *snoop.Tap
}

func (s *tappedTestSession) Tap() *snoop.Tap { return s.tap }

func TestTeleconferenceSetsSnoopMode(t *testing.T) {
	env := newMenuEnv(t)
	tap := snoop.NewTap()
	t.Cleanup(tap.Close)
	ts := &tappedTestSession{testSession: newTestSession("/q\r"), tap: tap}
	SetSessionOutputMode(ts, env.outputMode)
	t.Cleanup(func() {
		resetSessionIH(ts)
		ClearSessionOutputMode(ts)
	})
	var during snoop.Mode = -1
	ts.whenOutput("Joined #lobby", func() { during = tap.Mode() })
	c := &cmdCtx{
		e: env.e, s: ts, terminal: newTestTerminal(ts.testSession), userManager: env.um,
		currentUser: env.caller, nodeNumber: 1, sessionStartTime: time.Now(),
		outputMode: env.outputMode, termWidth: 80, termHeight: 24,
	}
	if _, _, err := runChat(c, ""); err != nil {
		t.Fatalf("runChat: %v", err)
	}
	if during != snoop.ModeTeleconf {
		t.Fatalf("mode in teleconference = %v, want teleconference", during)
	}
	if m := tap.Mode(); m != snoop.ModeBBS {
		t.Fatalf("mode after teleconference = %v, want bbs", m)
	}
}
