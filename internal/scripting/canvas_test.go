package scripting

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/editor"
	"github.com/ViSiON-3/vision-3-bbs/internal/pace"
)

func TestCanvasDrawingAndSixel(t *testing.T) {
	h := newHarness(t, harnessOpts{session: func(sc *SessionContext) { sc.Sixel = true }})
	writePNG(t, h, "pic.png")
	if !h.eval(`var c = v3.gfx.canvas(4,4,'pic.png'); c.clear(0,0,0); c.drawImage('pic.png',0,0); c.setPixel(1,1,4,5,6); JSON.stringify(c.getPixel(1,1)) === '[4,5,6,255]' && c.getPixel(-1,0) === null`).ToBoolean() {
		t.Fatal("canvas pixel operations failed")
	}
	if !h.eval(`c.fillRect(0,0,2,2,7,8,9); JSON.stringify(c.getPixel(0,0)) === '[7,8,9,255]' && JSON.stringify(c.getPixel(2,0)) === '[255,0,0,255]'`).ToBoolean() {
		t.Fatal("rectangle clipping failed")
	}
	if !h.eval(`c.clear(0,0,0); c.setPixel(0,0,255,0,0); c.wave(1,4,Math.PI/2,true); JSON.stringify(c.getPixel(1,0)) === '[255,0,0,255]' && JSON.stringify(c.getPixel(0,0)) === '[0,0,0,255]'`).ToBoolean() {
		t.Fatal("wave did not shift pixels with opaque edges")
	}
	if !h.eval(`c.present()`).ToBoolean() || !strings.HasPrefix(h.output(), "\x1bP0;1;0q") {
		t.Fatal("canvas did not present sixel")
	}
}

func TestCanvasBoundsAndFallback(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	if !h.eval(`var c = v3.gfx.canvas(2,2); c.present() === false`).ToBoolean() || h.output() != "" {
		t.Fatal("text terminal should not receive graphics")
	}
	for _, code := range []string{
		`v3.gfx.canvas(0,10)`, `v3.gfx.canvas(999999999,999999999)`, `v3.gfx.canvas(2,2,'../../etc/passwd')`, `c.wave(1,0,0)`, `c.heat(0, 0.2)`,
	} {
		if !h.eval(`try { ` + code + `; false } catch(e) { true }`).ToBoolean() {
			t.Errorf("expected error from %s", code)
		}
	}
}

func TestCanvasPresentDelta(t *testing.T) {
	h := newHarness(t, harnessOpts{session: func(sc *SessionContext) { sc.Sixel = true }})
	if !h.eval(`var c = v3.gfx.canvas(4,4); c.clear(255,0,0); c.presentDelta()`).ToBoolean() || !strings.Contains(h.output(), "\x1bP0;1;0q") {
		t.Fatal("first delta presentation must draw a full frame")
	}
	h.resetOutput()
	if !h.eval(`c.presentDelta()`).ToBoolean() || h.output() != "" {
		t.Fatal("unchanged frame should emit no graphics")
	}
	if !h.eval(`c.setPixel(0,0,250,0,0); c.presentDelta()`).ToBoolean() || h.output() != "" {
		t.Fatal("colours mapped to the same palette register should emit no graphics")
	}
	h.resetOutput()
	if !h.eval(`c.setPixel(0,0,0,0,0); c.presentDelta()`).ToBoolean() {
		t.Fatal("delta update failed")
	}
	if out := h.output(); !strings.HasSuffix(out, "#0@???-\x1b\\") {
		t.Fatalf("changed pixel must draw opaque black while others remain transparent: %q", out)
	}
	h.resetOutput()
	if !h.eval(`try { c.setPixel(0,0,0,0,0,0); c.presentDelta(); false } catch(e) { true }`).ToBoolean() || h.output() != "" {
		t.Fatal("transparent delta must fail without output")
	}
	if !h.eval(`c.clear(0,0,0); c.present(); c.presentDelta()`).ToBoolean() {
		t.Fatal("full redraw should reset the baseline")
	}
}

func TestHeatCanvas(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 100, 80))
	for i := range src.Pix {
		src.Pix[i] = byte(i * 17)
	}
	original := append([]byte(nil), src.Pix...)
	HeatCanvas(src, 0, 0, false)
	if !bytes.Equal(src.Pix, original) {
		t.Fatal("zero heat changed image")
	}
	HeatCanvas(src, 1, 0.1, false)
	if !bytes.Equal(src.Pix[:src.Stride], original[:src.Stride]) {
		t.Fatal("heat affected the top edge")
	}
	if bytes.Equal(src.Pix, original) {
		t.Fatal("heat did not displace the image")
	}
	a := append([]byte(nil), src.Pix...)
	copy(src.Pix, original)
	HeatCanvas(src, 8, 0.1, false)
	if bytes.Equal(src.Pix, a) {
		t.Fatal("heat field did not change with time")
	}
}

func TestHeatHotOnlyLeavesCoolMetalAlone(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 128, 96))
	for y := 0; y < 96; y++ {
		for x := 0; x < 128; x++ {
			if x < 64 {
				img.SetNRGBA(x, y, color.NRGBA{R: uint8(50 + x), G: 80, B: 100, A: 255})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{R: 255, G: uint8(100 + x), B: 10, A: 255})
			}
		}
	}
	before := append([]byte(nil), img.Pix...)
	HeatCanvas(img, 2, 0.1, true)
	changedHot := false
	for y := 0; y < 96; y++ {
		for x := 0; x < 128; x++ {
			i := y*img.Stride + x*4
			if !bytes.Equal(img.Pix[i:i+4], before[i:i+4]) {
				if x < 64 {
					t.Fatalf("cool pixel changed at %d,%d", x, y)
				}
				changedHot = true
			}
		}
	}
	if !changedHot {
		t.Fatal("hot metal did not shimmer")
	}
}

func TestHeatPreservesHotLetterSilhouette(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 128, 96))
	for y := 0; y < 96; y++ {
		for x := 0; x < 128; x++ {
			c := color.NRGBA{A: 255}
			if x >= 30 && x < 90 && y >= 20 && y < 75 {
				c = color.NRGBA{R: 240, G: uint8(100 + x), B: 10, A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	HeatCanvas(img, 2, 0.1, true)
	for y := 0; y < 96; y++ {
		for x := 0; x < 128; x++ {
			inside := x >= 30 && x < 90 && y >= 20 && y < 75
			if hot := img.NRGBAAt(x, y).R > 0; hot != inside {
				t.Fatalf("letter outline changed at %d,%d", x, y)
			}
		}
	}
}

func TestHeatDoesNotPullDarkEdgeAcrossBrightMetal(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 128, 96))
	for y := 0; y < 96; y++ {
		for x := 0; x < 128; x++ {
			c := color.NRGBA{R: 250, G: 170, B: 15, A: 255}
			if x < 40 {
				c = color.NRGBA{R: 45, G: 10, B: 0, A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	HeatCanvas(img, 5, 0.08, true)
	for y := 0; y < 96; y++ {
		for x := 40; x < 128; x++ {
			if c := img.NRGBAAt(x, y); c.R < 180 {
				t.Fatalf("dark band at %d,%d: %+v", x, y, c)
			}
		}
	}
}

func TestCanvasSizeLimit(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		ok   bool
	}{
		{1280, 1024, true},
		{1, 1, true},
		{1281, 1024, false},
		{1280, 1025, false},
		{2560, 512, false},
		{512, 2560, false},
		{1281, 1, false},
		{1, 1025, false},
	} {
		h := newHarness(t, harnessOpts{})
		code := fmt.Sprintf(`try { v3.gfx.canvas(%d,%d); true } catch(e) { false }`, tc.w, tc.h)
		if got := h.eval(code).ToBoolean(); got != tc.ok {
			t.Errorf("v3.gfx.canvas(%d,%d) created=%v, want %v", tc.w, tc.h, got, tc.ok)
		}
	}
}

// Canvases presented in turn share the paced frames: each draws in its
// turn, and one the script stops presenting does not hold the others.
func TestCanvasesTakeTurnsAtPacedFrames(t *testing.T) {
	gate := pace.NewGate()
	h := newHarness(t, harnessOpts{session: func(sc *SessionContext) { sc.Sixel, sc.Frames = true, gate }})
	h.eval(`var cs = [v3.gfx.canvas(2, 2), v3.gfx.canvas(2, 2), v3.gfx.canvas(2, 2)]; cs.forEach(function (c) { c.clear(255, 0, 0); })`)
	// drew reports whether the last call sent a frame.
	drew := func() bool {
		ok := strings.Contains(h.output(), "\x1bP")
		h.resetOutput()
		return ok
	}
	// answer lets one frame go: the test holds the other slot.
	answer := func() {
		for gate.Answer() {
		}
		gate.Begin()
	}
	order := ""
	for range 6 {
		answer()
		for i := range 3 {
			if h.eval(fmt.Sprintf(`cs[%d].present()`, i)); drew() {
				order += string(rune('a' + i))
			}
		}
	}
	if order != "abcabc" {
		t.Errorf("canvases drew in the order %q, want abcabc", order)
	}
	order = ""
	for range 3 {
		answer()
		if h.eval(`cs[0].present()`); drew() {
			order += "a"
		}
	}
	if order != "aaa" {
		t.Errorf("a alone drew %q, want aaa", order)
	}

	// a draws and b skips, then b is no longer presented: a yields to it
	// once only.
	answer()
	h.eval(`cs[0].present(); cs[1].present()`)
	if !drew() {
		t.Fatal("a did not draw")
	}
	order = ""
	for range 3 {
		answer()
		if h.eval(`cs[0].present()`); drew() {
			order += "a"
		}
	}
	if order != "aa" {
		t.Errorf("a drew %q after b stopped, want aa (one yield, then every frame)", order)
	}
}

// A frame's answer split after its ESC or its ESC [ reaches the gate,
// through readkey or getkey, and is not read as keys.
func TestCanvasAnswerSplitAfterEsc(t *testing.T) {
	for _, split := range [][]string{{"\x1b", "[?62;4cq"}, {"\x1b[", "?62;4cq"}} {
		for _, read := range []string{"readkey", "getkey"} {
			gate := pace.NewGate()
			trap := editor.NewReplyTrap(0, func(reply []byte, complete bool) {
				if complete && pace.IsAnswer(reply) {
					gate.Answer()
				}
			})
			h := newHarness(t, harnessOpts{inputChunks: split, session: func(sc *SessionContext) {
				sc.Sixel, sc.Frames, sc.StripReplies, sc.TakeReply = true, gate, trap.StripReplies, trap.Take
			}})
			keys := h.eval(`var c = v3.gfx.canvas(2, 2); c.clear(255, 0, 0); c.present();
var keys = []; for (;;) { var k = v3.console.` + read + `(2000); keys.push(JSON.stringify(k)); if (k === 'q' || k === '') break; } keys.join(',')`).String()
			if keys != `"q"` {
				t.Errorf("%s after %q read %s, want \"q\"", read, split[0], keys)
			}
			if gate.Waiting() {
				t.Errorf("%s: the answer split after %q did not reach the gate", read, split[0])
			}
		}
	}
}

func TestCanvasAnswerSplitAfterEscWithoutKeyReads(t *testing.T) {
	gate := pace.NewGate()
	trap := editor.NewReplyTrap(0, func(reply []byte, complete bool) {
		if complete && pace.IsAnswer(reply) {
			gate.Answer()
		}
	})
	h := newHarness(t, harnessOpts{inputChunks: []string{"\x1b", "[?62;4cq"}, session: func(sc *SessionContext) {
		sc.Sixel, sc.Frames, sc.StripReplies, sc.TakeReply = true, gate, trap.StripReplies, trap.Take
	}})
	h.eval(`var c = v3.gfx.canvas(2, 2); c.clear(255, 0, 0); c.present()`)
	// The script only draws: the second frame goes once the split answer
	// is taken while the canvas waits.
	deadline := time.Now().Add(2 * time.Second)
	for strings.Count(h.output(), "\x1bP") < 2 {
		if time.Now().After(deadline) {
			t.Fatal("the split answer never let the second frame go")
		}
		h.eval(`c.present()`)
		time.Sleep(10 * time.Millisecond)
	}
	if k := h.eval(`v3.console.readkey(1000)`).String(); k != "q" {
		t.Errorf("key after the answer = %q, want q", k)
	}
}
