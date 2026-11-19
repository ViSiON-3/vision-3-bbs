package menu

import (
	"image"
	"image/color"
	"math"
	"sort"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/sixel"
)

// matrixContrastOptions are the login matrix's four choices.
var matrixContrastOptions = []LightbarOption{{Text: "journey onward"}, {Text: "create an account"}, {Text: "check your access"}, {Text: "disconnect"}}

// matrixContrastNow is a fixed board time for the scrolling line.
var matrixContrastNow = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

// relativeLuminance is c's relative luminance as WCAG 2 defines it.
func relativeLuminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	lin := func(v uint32) float64 {
		x := float64(v) / 0xffff
		if x <= .04045 {
			return x / 12.92
		}
		return math.Pow((x+.055)/1.055, 2.4)
	}
	return .2126*lin(r) + .7152*lin(g) + .0722*lin(b)
}

// contrastRatio is the WCAG 2 contrast ratio of a and b, 1 to 21.
func contrastRatio(a, b color.Color) float64 {
	l1, l2 := relativeLuminance(a), relativeLuminance(b)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + .05) / (l2 + .05)
}

// solidMatrixPicture is a w x h picture of one colour.
func solidMatrixPicture(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
	}
	return img
}

// choiceContrast is the median contrast, as the terminal is sent it, of
// one choice's front-face lettering against what the scene shows at the
// same pixels without that lettering. A lettering pixel is one the choice's
// text makes at least 60 levels brighter on the cube and at least 110 of
// 255; the far face's lettering is dimmer and is left out.
func choiceContrast(t *testing.T, s *matrixScene, elapsed float64, selected, choice int) float64 {
	t.Helper()
	blank := append([]LightbarOption(nil), matrixContrastOptions...)
	blank[choice].Text = ""
	with := s.centeredMatrixFrame(elapsed, matrixContrastOptions, selected, matrixContrastNow)
	without := s.centeredMatrixFrame(elapsed, blank, selected, matrixContrastNow)
	cubeWith := matrixTextCube(elapsed, matrixContrastOptions, selected, s.cubeEffects(0))
	cubeWithout := matrixTextCube(elapsed, blank, selected, s.cubeEffects(0))
	place := matrixCubeLayout(s.config.Layout, s.source.Bounds(), s.art)
	var ratios []float64
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			a, b := cubeWith.NRGBAAt(x, y), cubeWithout.NRGBAAt(x, y)
			if hi := max(a.R, a.G, a.B); hi < 110 || int(hi)-int(max(b.R, b.G, b.B)) < 60 {
				continue
			}
			p := image.Pt(place.cubeLeft+x, place.cubeTop+y)
			ink := s.pal[s.pal.Index(with.NRGBAAt(p.X, p.Y))]
			behind := s.pal[s.pal.Index(without.NRGBAAt(p.X, p.Y))]
			ratios = append(ratios, contrastRatio(ink, behind))
		}
	}
	if len(ratios) < 100 {
		t.Fatalf("only %d lettering pixels found for choice %d at %.0fs", len(ratios), choice, elapsed)
	}
	sort.Float64s(ratios)
	mid := len(ratios) / 2
	if len(ratios)%2 == 0 {
		return (ratios[mid-1] + ratios[mid]) / 2
	}
	return ratios[mid]
}

// testMatrixScene is a scene of picture with cfg's effects, coloured with
// the palette the terminal is sent.
func testMatrixScene(picture *image.NRGBA, cfg matrixEffectsConfig) *matrixScene {
	return &matrixScene{
		source: picture,
		art:    matrixPictureArt(picture),
		pal:    matrixScenePalette(sixel.Palette(picture), cfg.cubeEffects()),
		config: cfg,
		node:   1,
	}
}

// matrixMinChoiceContrast is the least contrast a tinted cube's choices may
// have against what is behind them, as the terminal is sent them. The
// unselected choices of a tinted cube in the side layout over black have
// 1.49; this is that contrast with rounding to spare.
const matrixMinChoiceContrast = 1.45

// TestMatrixTintedCubeChoicesOverBrightPicture checks every choice on a
// tinted cube keeps its contrast where the default layout draws the cube
// over a bright picture.
func TestMatrixTintedCubeChoicesOverBrightPicture(t *testing.T) {
	cfg := matrixEffectsConfig{ProjectText: true, CubeTint: "#ff40e0"}
	for _, picture := range []struct {
		name string
		c    color.NRGBA
	}{
		{"black", color.NRGBA{0, 0, 0, 255}},
		{"grey", color.NRGBA{128, 128, 128, 255}},
		{"light grey", color.NRGBA{200, 200, 200, 255}},
		{"white", color.NRGBA{255, 255, 255, 255}},
		{"yellow", color.NRGBA{255, 230, 90, 255}},
		{"green", color.NRGBA{0, 255, 0, 255}},
		{"pink", color.NRGBA{255, 120, 230, 255}},
	} {
		// The default layout's largest picture, centred under the cube.
		s := testMatrixScene(solidMatrixPicture(640, 160, picture.c), cfg)
		for _, elapsed := range []float64{5, 9, 13} {
			for choice := range matrixContrastOptions {
				if got := choiceContrast(t, s, elapsed, 0, choice); got < matrixMinChoiceContrast {
					kind := "unselected"
					if choice == 0 {
						kind = "selected"
					}
					t.Errorf("%s picture, %.0fs: %s choice %d has contrast %.2f, want at least %.2f", picture.name, elapsed, kind, choice, got, matrixMinChoiceContrast)
				}
			}
		}
	}
}
