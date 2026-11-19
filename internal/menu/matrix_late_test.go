package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"golang.org/x/term"
)

// Answers to the connect probe that the tests send after its wait.
const (
	lateSixel = "\x1b[<0;4c\x1b[?62;4c"
	lateAudio = ansi.AudioLibsndfileYes + ansi.AudioWAVU8Yes + "\x1b[?62;1c"
	lateUTF8  = "\x1b[2;2R\x1b[?62;1c" // one column of movement: UTF-8
)

// lateMatrixSession returns a session whose terminal said nothing within
// the connect wait; the test answers for it later with s.reply.
func lateMatrixSession(t *testing.T, encoding bool) (*probeSession, *syncLog) {
	t.Helper()
	s := newProbeSession("xterm")
	s.cleanup(t)
	t.Cleanup(func() { ClearSessionOutputMode(s) })
	logs, log := newSyncLog()
	ProbeTerminal(s, 50*time.Millisecond, encoding, log)
	return s, logs
}

type matrixResult struct {
	action string
	err    error
}

// startMatrix runs the login matrix on s, drawn first in mode.
func startMatrix(env *menuEnv, s *probeSession, mode ansi.OutputMode) <-chan matrixResult {
	done := make(chan matrixResult, 1)
	go func() {
		action, _, err := env.e.RunMatrixScreen(s, term.NewTerminal(s, ""), env.um, 1, mode, 80, 24)
		done <- matrixResult{action, err}
	}()
	return done
}

// waitOutput waits until s's output satisfies ok and returns it.
func waitOutput(t *testing.T, s *probeSession, what string, ok func(string) bool) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		out := s.written()
		if ok(out) {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("output never showed %s; it ends %q", what, out[max(0, len(out)-200):])
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func outputHas(sub string) func(string) bool {
	return func(out string) bool { return strings.Contains(out, sub) }
}

// endMatrix waits for the matrix to finish with the caller's disconnect.
func endMatrix(t *testing.T, done <-chan matrixResult) {
	t.Helper()
	select {
	case r := <-done:
		if r.err != nil || r.action != "DISCONNECT" {
			t.Fatalf("matrix action=%q err=%v, want DISCONNECT", r.action, r.err)
		}
	case <-time.After(handlerWait):
		t.Fatal("matrix did not finish")
	}
}

// A sixel answer held back until the text matrix shows makes it repaint
// once as the picture scene, with the highlighted choice kept, and a key
// sent right behind the answer acts after the repaint.
func TestMatrixLateSixelRepaints(t *testing.T) {
	env := newMenuEnv(t)
	set := chatcovMatrixSet(t, env)
	logo, err := os.ReadFile("../../menus/vision3/data/vision3-logo.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(set, "ansi", "PDMATRIX.PNG"), logo, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(set, "ansi", "PDMATRIX.EFX"), []byte(`{"heat":true,"polygons":true,"textFade":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, _ := lateMatrixSession(t, false)
	done := startMatrix(env, s, ansi.OutputModeCP437)
	out := waitOutput(t, s, "the text matrix", outputHas(chatcovHighlight("Journey onward.")))
	if strings.Contains(out, "\x1bP") {
		t.Fatal("sixel drawn before the terminal answered")
	}
	s.reply(keyDown)
	before := len(waitOutput(t, s, "the moved highlight", outputHas(chatcovHighlight("Create an account."))))
	s.reply(lateSixel + "D")
	endMatrix(t, done)

	after := s.written()[before:]
	if n := strings.Count(after, ansi.ClearScreen()); n != 1 {
		t.Fatalf("screen cleared %d times after the late answer, want 1 repaint", n)
	}
	repaint := after[strings.Index(after, ansi.ClearScreen()):]
	if !strings.Contains(repaint, "\x1bP0;1;0q") {
		t.Error("repaint has no sixel picture")
	}
	if !strings.Contains(repaint, chatcovHighlight("Create an account.")) || strings.Contains(repaint, chatcovHighlight("Journey onward.")) {
		t.Error("repaint does not keep the highlight on the second choice")
	}
	if !strings.Contains(repaint, "Disconnecting") {
		t.Error("the key sent behind the answer was not acted on after the repaint")
	}
}

// A late audio answer loads the tones at the repaint, and the next move
// plays one.
func TestMatrixLateAudioLoadsTones(t *testing.T) {
	env := newMenuEnv(t)
	set := chatcovMatrixSet(t, env)
	for name, b := range validTones() {
		if err := os.WriteFile(filepath.Join(set, "ansi", name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, _ := lateMatrixSession(t, false)
	done := startMatrix(env, s, ansi.OutputModeCP437)
	before := len(waitOutput(t, s, "the text matrix", outputHas(chatcovHighlight("Journey onward."))))
	s.reply(lateAudio)
	waitOutput(t, s, "the tones loaded", outputHas(loadMoveTone))
	s.reply(keyDown + "D")
	endMatrix(t, done)

	after := s.written()[before:]
	if n := strings.Count(after, ansi.ClearScreen()); n != 1 {
		t.Errorf("screen cleared %d times after the late answer, want 1 repaint", n)
	}
	if strings.Index(after, loadMoveTone) < strings.Index(after, ansi.ClearScreen()) {
		t.Error("tones loaded before the repaint")
	}
	if n := strings.Count(after, playMoveTone); n != 1 {
		t.Errorf("move tone played %d times, want 1", n)
	}
}

// A late cursor report from a UTF-8 terminal repaints the matrix in UTF-8,
// and UTF-8 stays the session's encoding after it.
func TestMatrixLateCursorReportSetsEncoding(t *testing.T) {
	env := newMenuEnv(t)
	chatcovMatrixSet(t, env)
	s, _ := lateMatrixSession(t, true)
	SetSessionOutputMode(s, ansi.OutputModeCP437)
	var changed []ansi.OutputMode
	OnLateEncoding(s, func(m ansi.OutputMode) { changed = append(changed, m) })
	done := startMatrix(env, s, ansi.OutputModeCP437)
	out := waitOutput(t, s, "the text matrix", outputHas(chatcovHighlight("Journey onward.")))
	if !strings.Contains(out, "\xdb") || strings.Contains(out, "█") {
		t.Fatal("matrix not first drawn in CP437")
	}
	before := len(out)
	s.reply(lateUTF8)
	waitOutput(t, s, "the matrix in UTF-8", func(out string) bool { return strings.Contains(out[before:], "█") })
	s.reply("D")
	endMatrix(t, done)

	after := s.written()[before:]
	if n := strings.Count(after, ansi.ClearScreen()); n != 1 {
		t.Errorf("screen cleared %d times after the late answer, want 1 repaint", n)
	}
	if strings.Contains(after, "\xdb") {
		t.Error("repaint still sends CP437 bytes")
	}
	if got := sessionOutputMode(s); got != ansi.OutputModeUTF8 {
		t.Errorf("session encoding after the repaint = %v, want UTF-8", got)
	}
	if len(changed) != 1 || changed[0] != ansi.OutputModeUTF8 {
		t.Errorf("encoding changes reported = %v, want one to UTF-8", changed)
	}
}

// An answer that came while the connection check was up decides the
// encoding the matrix is first drawn in; nothing is repainted.
func TestMatrixFirstDrawUsesAnswerFromCheck(t *testing.T) {
	env := newMenuEnv(t)
	chatcovMatrixSet(t, env)
	s, _ := lateMatrixSession(t, true)
	var changed []ansi.OutputMode
	OnLateEncoding(s, func(m ansi.OutputMode) { changed = append(changed, m) })
	// The check reads the caller's key; the answer before it is handled by then.
	s.reply(lateUTF8 + "x")
	if k, err := readKeyBounded(s); err != nil || k != 'x' {
		t.Fatalf("ReadKey = %q, %v; want 'x'", rune(k), err)
	}
	start := len(s.written())
	done := startMatrix(env, s, ansi.OutputModeCP437)
	waitOutput(t, s, "the matrix", outputHas(chatcovHighlight("Journey onward.")))
	s.reply("D")
	endMatrix(t, done)

	out := s.written()[start:]
	if !strings.Contains(out, "█") || strings.Contains(out, "\xdb") {
		t.Error("matrix not drawn in UTF-8")
	}
	if n := strings.Count(out, ansi.ClearScreen()); n != 1 {
		t.Errorf("screen cleared %d times, want 1", n)
	}
	if len(changed) != 1 || changed[0] != ansi.OutputModeUTF8 {
		t.Errorf("encoding changes reported = %v, want one to UTF-8", changed)
	}
}

// For a terminal that never answers, the matrix sends what it sends to a
// session that was not probed.
func TestMatrixSilentTerminalUnchanged(t *testing.T) {
	run := func(probed bool) string {
		env := newMenuEnv(t)
		chatcovMatrixSet(t, env)
		var s *probeSession
		if probed {
			s, _ = lateMatrixSession(t, true)
		} else {
			s = newProbeSession("xterm")
			s.cleanup(t)
		}
		start := len(s.written())
		done := startMatrix(env, s, ansi.OutputModeCP437)
		waitOutput(t, s, "the matrix", outputHas(chatcovHighlight("Journey onward.")))
		s.reply(keyDown + "D")
		endMatrix(t, done)
		return s.written()[start:]
	}
	if probed, plain := run(true), run(false); probed != plain {
		t.Errorf("matrix after a silent probe sent\n%q\nwant\n%q", probed, plain)
	}
}

// Once the matrix has ended a late answer repaints nothing and leaves the
// encoding.
func TestMatrixLateAnswerAfterMatrix(t *testing.T) {
	env := newMenuEnv(t)
	chatcovMatrixSet(t, env)
	s, logs := lateMatrixSession(t, true)
	SetSessionOutputMode(s, ansi.OutputModeCP437)
	var changed []ansi.OutputMode
	OnLateEncoding(s, func(m ansi.OutputMode) { changed = append(changed, m) })
	done := startMatrix(env, s, ansi.OutputModeCP437)
	waitOutput(t, s, "the matrix", outputHas(chatcovHighlight("Journey onward.")))
	s.reply("D")
	endMatrix(t, done)

	end := len(s.written())
	s.reply(lateUTF8 + "x")
	if k, err := readKeyBounded(s); err != nil || k != 'x' {
		t.Fatalf("ReadKey = %q, %v; want 'x'", rune(k), err)
	}
	if n := len(s.written()); n != end {
		t.Errorf("%d bytes sent after the matrix ended: %q", n-end, s.written()[end:])
	}
	if got := sessionOutputMode(s); got != ansi.OutputModeCP437 || len(changed) != 0 {
		t.Errorf("encoding = %v, changes reported = %v; want CP437 and none", got, changed)
	}
	if !strings.Contains(logs.String(), "encoding left as decided") {
		t.Errorf("late report not logged as left alone:\n%s", logs.String())
	}
}
