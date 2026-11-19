package scripting

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"
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
