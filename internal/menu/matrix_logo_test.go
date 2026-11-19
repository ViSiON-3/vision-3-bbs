package menu

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// matrixEmberRiseOn is how far above a hot strip at the foot of a
// width x height picture the matrix's embers are drawn, the most over a
// minute of frames.
func matrixEmberRiseOn(t *testing.T, width, height int) int {
	t.Helper()
	picture := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i := 3; i < len(picture.Pix); i += 4 {
		picture.Pix[i] = 255
	}
	// The sources are found on the script's 4-pixel grid; the strip covers
	// its lowest row (148 on the 480x160 logo) across the whole scan.
	strip := int(math.Round(148 * float64(height) / 160))
	for x := 0; x < width; x++ {
		picture.SetNRGBA(x, strip, color.NRGBA{255, 140, 40, 255})
	}
	burn := newMatrixLogoBurn(picture)
	if len(burn.sources) == 0 {
		t.Fatalf("%dx%d: no ember sources on the hot strip at y=%d", width, height, strip)
	}
	top := strip
	frame := image.NewNRGBA(picture.Bounds())
	for step := 0; step < 60*8; step++ {
		copy(frame.Pix, picture.Pix)
		burn.paintEmbers(frame, float64(step)/8)
		for y := 0; y < top; y++ {
			for x := 0; x < width; x++ {
				if c := frame.NRGBAAt(x, y); c.R > 0 || c.G > 0 || c.B > 0 {
					top = y
					break
				}
			}
		}
	}
	return strip - top
}

// TestMatrixEmbersRiseAsFarAsMainMenu checks the matrix logo's embers rise
// as far as the main menu's (menus/vision3/scripts/mainmenu.js). There the
// tallest kind climbs 120 * (1 - e^-1.7), about 98 pixels, by the end of a
// full-stamina life, drifts up 8 more and is drawn with a radius of 3: about
// 109 pixels on the 480x160 logo. The matrix's own embers used to stop 42
// pixels up. On the 320-wide side layout the rise scales with the picture.
func TestMatrixEmbersRiseAsFarAsMainMenu(t *testing.T) {
	farthest := 120*(1-math.Exp(-1.7)) + 8 + 3
	// Distances scale by the smaller of width/480 and height/160.
	for _, size := range []struct {
		w, h int
		k    float64
	}{{480, 160, 1}, {320, 240, 2.0 / 3.0}} {
		rise, k := matrixEmberRiseOn(t, size.w, size.h), size.k
		// A minute of frames at 8 a second gets within 10% of the farthest.
		lo, hi := int(0.9*farthest*k), int(math.Ceil(farthest*k))+1
		if rise < lo || rise > hi {
			t.Errorf("%dx%d: embers rose %d pixels; the main menu's rise %d to %d at scale %.3f", size.w, size.h, rise, lo, hi, k)
		}
	}
}

// A spark covers a bright pixel with its own colour and is, over black, the
// light that used to be added.
func TestLaySparkIsInFront(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{255, 255, 255, 255})
	img.SetNRGBA(1, 0, color.NRGBA{0, 0, 0, 255})
	layer := image.NewNRGBA(img.Bounds())
	laySpark(img, layer, 0, 0, 255, 210, 50, 1)
	laySpark(img, layer, 1, 0, 255, 210, 50, 0.5)
	if got, want := img.NRGBAAt(0, 0), (color.NRGBA{255, 210, 50, 255}); got != want {
		t.Errorf("full-strength spark over white: %v, want the spark's colour %v", got, want)
	}
	if got, want := img.NRGBAAt(1, 0), (color.NRGBA{128, 105, 25, 255}); got != want {
		t.Errorf("half-strength spark over black: %v, want %v, the light added before", got, want)
	}
	if layer.NRGBAAt(0, 0) != img.NRGBAAt(0, 0) || layer.NRGBAAt(1, 0) != img.NRGBAAt(1, 0) {
		t.Error("changed pixels were not copied to the layer")
	}
}
