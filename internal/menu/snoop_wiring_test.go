package menu

import (
	"testing"

	"github.com/gliderlabs/ssh"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/snoop"
)

type tappedSession struct {
	ssh.Session
	t *snoop.Tap
}

func (s *tappedSession) Tap() *snoop.Tap { return s.t }

func TestSetSessionOutputModeUpdatesTap(t *testing.T) {
	s := &tappedSession{t: snoop.NewTap()}
	SetSessionOutputMode(s, ansi.OutputModeCP437)
	if !s.t.CP437() {
		t.Fatal("tap not marked CP437")
	}
	SetSessionOutputMode(s, ansi.OutputModeUTF8)
	if s.t.CP437() {
		t.Fatal("tap still CP437")
	}
}

func TestTapOfPlainSessionIsNil(t *testing.T) {
	if tapOf(nil) != nil {
		t.Fatal("nil session must have no tap")
	}
}
