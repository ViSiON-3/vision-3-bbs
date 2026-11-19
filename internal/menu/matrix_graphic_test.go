package menu

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/gliderlabs/ssh"
	"golang.org/x/term"
)

type matrixGraphicSession struct {
	*testSession
	term string
}

type matrixDelayedSession struct {
	*matrixGraphicSession
	input io.Reader
	watch *matrixFrameWatch
}

func (s *matrixDelayedSession) Read(p []byte) (int, error) { return s.input.Read(p) }

// matrixFrameWatch closes reached once the output holds want copies of
// marker. Every write passes through it, so it reads the output on the
// writer's goroutine.
type matrixFrameWatch struct {
	*testSession
	marker  string
	want    int
	reached chan struct{}
	once    sync.Once
}

func (w *matrixFrameWatch) Write(p []byte) (int, error) {
	n, err := w.testSession.Write(p)
	if strings.Count(w.output(), w.marker) >= w.want {
		w.once.Do(func() { close(w.reached) })
	}
	return n, err
}

func (s *matrixDelayedSession) Write(p []byte) (int, error) { return s.watch.Write(p) }

func (s *matrixGraphicSession) Pty() (ssh.Pty, <-chan ssh.Window, bool) {
	return ssh.Pty{Term: s.term}, nil, true
}

// sixelSession returns a session whose terminal answered the connect probe
// as sixel capable.
func sixelSession(t *testing.T, ts *testSession) *matrixGraphicSession {
	t.Helper()
	s := &matrixGraphicSession{testSession: ts, term: "xterm"}
	storeSessionSixel(s, true)
	t.Cleanup(func() { sessionSixelCaps.Delete(s) })
	return s
}

func writeMatrixPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestMatrixGraphicCapabilitiesAndBounds(t *testing.T) {
	env := newMenuEnv(t)
	set := chatcovMatrixSet(t, env)
	path := filepath.Join(set, "ansi", "PDMATRIX.PNG")
	writeMatrixPNG(t, path, 8, 12)
	sess := sixelSession(t, newTestSession(""))
	data, col, _ := env.e.matrixGraphic(sess, 80, 24)
	if col != 40 || !bytes.HasPrefix(data, []byte("\x1bP0;1;0q")) {
		t.Fatalf("sixel graphic column=%d data=%q", col, data)
	}
	storeSessionSixel(sess, false)
	if b, _, _ := env.e.matrixGraphic(sess, 80, 24); len(b) != 0 {
		t.Fatal("text terminal received graphics")
	}
	storeSessionSixel(sess, true)
	if b, _, _ := env.e.matrixGraphic(sess, 79, 24); len(b) != 0 {
		t.Fatal("small terminal received graphic")
	}
	writeMatrixPNG(t, path, 641, 1)
	if b, _, _ := env.e.matrixGraphic(sess, 80, 24); len(b) != 0 {
		t.Fatal("oversized graphic accepted")
	}
	if err := os.WriteFile(path, []byte("not png"), 0644); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := env.e.matrixGraphic(sess, 80, 24); len(b) != 0 {
		t.Fatal("invalid graphic accepted")
	}
}

func TestMatrixGraphicKeepsNativeInput(t *testing.T) {
	env := newMenuEnv(t)
	set := chatcovMatrixSet(t, env)
	writeMatrixPNG(t, filepath.Join(set, "ansi", "PDMATRIX.PNG"), 8, 12)
	env.outputMode = ansi.OutputModeCP437
	r := env.run(func(c *cmdCtx, _ string) (*user.User, string, error) {
		s := sixelSession(t, c.s.(*testSession))
		defer resetSessionIH(s)
		action, u, err := c.e.RunMatrixScreen(s, c.terminal, c.userManager, c.nodeNumber, c.outputMode, c.termWidth, c.termHeight)
		return u, action, err
	}, nil, "", "D")
	if r.err != nil || r.next != "DISCONNECT" {
		t.Fatalf("matrix action=%q err=%v", r.next, r.err)
	}
	if !strings.Contains(r.raw, "\x1b[1;40H\x1bP0;1;0q") {
		t.Fatal("matrix did not display its sixel image")
	}
	clearHeader := strings.Index(r.raw, "\x1b[2;1H\x1b[2K")
	image := strings.Index(r.raw, "\x1bP0;1;0q")
	if clearHeader < 0 || clearHeader > image {
		t.Fatal("fallback header must be cleared before sixel")
	}
	if !strings.Contains(r.raw, "Disconnect.") {
		t.Fatal("native option was not displayed")
	}
}

// TestMatrixSidePictureWithoutScene checks a side-layout picture taller than
// 160 pixels is left out when no scene can be drawn from it, since the
// fallback draws it above the choices and it would cover them.
func TestMatrixSidePictureWithoutScene(t *testing.T) {
	env := newMenuEnv(t)
	set := chatcovMatrixSet(t, env)
	// Transparent pixels rule out a scene.
	writeMatrixPNG(t, filepath.Join(set, "ansi", "PDMATRIX.PNG"), 320, 240)
	if err := os.WriteFile(filepath.Join(set, "ansi", "PDMATRIX.EFX"), []byte(`{"projectText":true,"layout":"side"}`), 0644); err != nil {
		t.Fatal(err)
	}
	env.outputMode = ansi.OutputModeCP437
	r := env.run(func(c *cmdCtx, _ string) (*user.User, string, error) {
		s := sixelSession(t, c.s.(*testSession))
		defer resetSessionIH(s)
		// The side layout admits the picture; only the missing scene drops it.
		if data, col, size := c.e.matrixGraphic(s, c.termWidth, c.termHeight); len(data) == 0 || size != image.Pt(320, 240) {
			t.Errorf("matrixGraphic returned %d bytes, size %v", len(data), size)
		} else if c.e.newMatrixScene(c.termWidth, c.termHeight, col, c.nodeNumber) != nil {
			t.Error("a scene was built from a picture with transparent pixels")
		}
		action, u, err := c.e.RunMatrixScreen(s, c.terminal, c.userManager, c.nodeNumber, c.outputMode, c.termWidth, c.termHeight)
		return u, action, err
	}, nil, "", "D")
	if r.err != nil || r.next != "DISCONNECT" {
		t.Fatalf("matrix action=%q err=%v", r.next, r.err)
	}
	if strings.Contains(r.raw, "\x1bP0;1;0q") {
		t.Error("a 320x240 picture was drawn over the choices")
	}
	if !strings.Contains(r.raw, "Disconnect.") {
		t.Error("native option was not displayed")
	}
}

func TestMatrixEffectsAndNativeInput(t *testing.T) {
	env := newMenuEnv(t)
	set := chatcovMatrixSet(t, env)
	logo, err := os.ReadFile("../../menus/vision3/data/vision3-logo.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(set, "ansi", "PDMATRIX.PNG"), logo, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(set, "ansi", "PDMATRIX.EFX"), []byte(`{"heat":true,"polygons":true,"textFade":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	env.outputMode = ansi.OutputModeCP437
	r := env.run(func(c *cmdCtx, _ string) (*user.User, string, error) {
		s := sixelSession(t, c.s.(*testSession))
		defer resetSessionIH(s)
		action, u, err := c.e.RunMatrixScreen(s, c.terminal, c.userManager, c.nodeNumber, c.outputMode, c.termWidth, c.termHeight)
		return u, action, err
	}, nil, "", "D")
	if r.err != nil || r.next != "DISCONNECT" {
		t.Fatalf("matrix action=%q err=%v", r.next, r.err)
	}
	for _, want := range []string{"\x1b[11;2H\x1bP0;1;0q", "\x1b[11;56H\x1bP0;1;0q", "\x1b[38;2;", "> "} {
		if !strings.Contains(r.raw, want) {
			t.Errorf("missing matrix effect %q", want)
		}
	}
}

func TestMatrixPolygonMoves(t *testing.T) {
	a, b := matrixPolygon(0, 1), matrixPolygon(2, 1)
	if a.Bounds().Dx() != 192 || a.Bounds().Dy() != 208 {
		t.Fatalf("polygon canvas is %v", a.Bounds())
	}
	if bytes.Equal(a.Pix, b.Pix) {
		t.Fatal("polygon did not tumble or morph")
	}
	var bright int
	for i := 0; i < len(a.Pix); i += 4 {
		if a.Pix[i] > 30 || a.Pix[i+1] > 30 || a.Pix[i+2] > 30 {
			bright++
		}
	}
	if bright < 100 {
		t.Fatalf("polygon has only %d visible pixels", bright)
	}
}

func TestMatrixEffectsRespondAfterAnimation(t *testing.T) {
	env := newMenuEnv(t)
	set := chatcovMatrixSet(t, env)
	logo, err := os.ReadFile("../../menus/vision3/data/vision3-logo.png")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(set, "ansi", "PDMATRIX.PNG"), logo, 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(set, "ansi", "PDMATRIX.EFX"), []byte(`{"heat":true,"polygons":true,"textFade":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ts := newTestSession("")
	const frame = "\x1b[11;2H\x1bP0;1;0q" // the left polygon's sixel, once per animation frame
	watch := &matrixFrameWatch{testSession: ts, marker: frame, want: 2, reached: make(chan struct{})}
	s := &matrixDelayedSession{matrixGraphicSession: sixelSession(t, ts), input: reader, watch: watch}
	storeSessionSixel(s, true)
	defer sessionSixelCaps.Delete(s)
	defer resetSessionIH(s)
	type result struct {
		action string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		action, _, err := env.e.RunMatrixScreen(s, term.NewTerminal(watch, ""), env.um, 1, ansi.OutputModeCP437, 80, 24)
		done <- result{action, err}
	}()
	// Input goes in once the screen has animated, however long that takes.
	select {
	case <-watch.reached:
	case r := <-done:
		t.Fatalf("matrix screen ended before input: action=%q err=%v", r.action, r.err)
	case <-time.After(handlerWait):
		t.Fatal("matrix screen drew fewer than two polygon frames")
	}
	go func() { _, _ = writer.Write([]byte("D")) }()
	select {
	case r := <-done:
		if r.err != nil || r.action != "DISCONNECT" {
			t.Fatalf("action=%q err=%v", r.action, r.err)
		}
		n := strings.Count(ts.output(), frame)
		if n < 2 {
			t.Fatalf("expected animated polygon frames, got %d", n)
		}
		t.Logf("left polygon: %d frames before input; total output %d bytes", n, len(ts.output()))
	case <-time.After(handlerWait):
		t.Fatal("matrix did not respond after animation")
	}
}
