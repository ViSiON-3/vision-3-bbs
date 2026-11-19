package menu

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestMatrixTextCubeReflectsMotionAndSelection(t *testing.T) {
	options := []LightbarOption{{Text: "J  Journey onward"}, {Text: "C  Create an account"}, {Text: "A  Check your access"}, {Text: "D  Disconnect"}}
	first := matrixTextCube(1, options, 0)
	if first.Bounds().Dx() != 320 || first.Bounds().Dy() != 240 {
		t.Fatal("unexpected cube size")
	}
	if bytes.Equal(first.Pix, matrixTextCube(2, options, 0).Pix) {
		t.Fatal("cube did not animate")
	}
	if bytes.Equal(first.Pix, matrixTextCube(1, options, 1).Pix) {
		t.Fatal("selection did not change projected text")
	}
	options[0].Text = "J  Different choice"
	if bytes.Equal(first.Pix, matrixTextCube(1, options, 0).Pix) {
		t.Fatal("cube did not use matrix choices")
	}
}

func TestMatrixCubeRespondsToEvents(t *testing.T) {
	options := []LightbarOption{{Text: "journey onward"}, {Text: "create an account"}, {Text: "check your access"}, {Text: "disconnect"}}
	base := matrixTextCube(3, options, 1)
	for name, fx := range map[string]matrixCubeEffects{"selection": {magnet: 1}, "weld": {weld: 1}, "activation": {activate: 1}} {
		if bytes.Equal(base.Pix, matrixTextCube(3, options, 1, fx).Pix) {
			t.Errorf("%s did not affect the cube", name)
		}
	}
}

// TestMatrixCubeLayout checks where the picture and the cube go for each
// layout name in PDMATRIX.EFX.
func TestMatrixCubeLayout(t *testing.T) {
	wide := image.Rect(0, 0, 480, 160)
	// The default keeps the cube over a centred picture, as before the
	// layout switch existed.
	if got, want := matrixCubeLayout("", wide, wide), (matrixCubeScene{80, 112, 160, 64}); got != want {
		t.Errorf("default layout = %+v, want %+v", got, want)
	}
	if got := matrixCubeLayout("no such layout", wide, wide); got != matrixCubeLayout("", wide, wide) {
		t.Errorf("an unknown layout = %+v, want the default", got)
	}

	// "side": the art is as far from the left border as the cube is from
	// the right border, with the same gap between them.
	picture, art := image.Rect(0, 0, 320, 240), image.Rect(2, 10, 316, 230)
	side := matrixCubeLayout("side", picture, art)
	left := side.logoLeft + art.Min.X
	right := 640 - (side.cubeLeft + matrixCubeMaxX + 1)
	gap := (side.cubeLeft + matrixCubeMinX) - (side.logoLeft + art.Max.X)
	if left != right || left <= 0 || gap < left || gap > left+2 {
		t.Errorf("side layout %+v: left margin %d, right margin %d, gap %d", side, left, right, gap)
	}
	if mid := side.logoTop + 120; mid != side.cubeTop+120 {
		t.Errorf("side layout %+v: picture middle %d, cube middle %d", side, mid, side.cubeTop+120)
	}
	// A picture with no art measured is placed by its own edges.
	if got := matrixCubeLayout("side", picture, image.Rectangle{}); got != matrixCubeLayout("side", picture, picture) {
		t.Errorf("no art = %+v", got)
	}
}

// TestMatrixPictureArt checks the black background around a picture is left
// out of its art.
func TestMatrixPictureArt(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 40, 30))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	if got := matrixPictureArt(img); !got.Empty() {
		t.Errorf("an all-black picture has art %v", got)
	}
	img.SetNRGBA(5, 7, color.NRGBA{200, 0, 180, 255})
	img.SetNRGBA(30, 20, color.NRGBA{200, 0, 180, 255})
	img.SetNRGBA(38, 28, color.NRGBA{8, 8, 8, 255}) // near-black is background
	if got, want := matrixPictureArt(img), image.Rect(5, 7, 31, 21); got != want {
		t.Errorf("art = %v, want %v", got, want)
	}
}

// TestMatrixEffectsPictureLimit checks the picture may be as large as the
// cube only when it sits beside the cube.
func TestMatrixEffectsPictureLimit(t *testing.T) {
	for name, tc := range map[string]struct {
		cfg  matrixEffectsConfig
		w, h int
	}{
		"picture above the choices":       {matrixEffectsConfig{Heat: true}, 640, 160},
		"cube over the picture":           {matrixEffectsConfig{ProjectText: true}, 640, 160},
		"side layout without the cube":    {matrixEffectsConfig{Heat: true, Layout: "side"}, 640, 160},
		"picture to the left of the cube": {matrixEffectsConfig{ProjectText: true, Layout: "side"}, 320, 240},
	} {
		if w, h := tc.cfg.pictureLimit(); w != tc.w || h != tc.h {
			t.Errorf("%s: limit %dx%d, want %dx%d", name, w, h, tc.w, tc.h)
		}
	}
}

// TestMatrixCubeTintSetting checks the cubeTint setting is a "#rrggbb" colour
// and anything else leaves the cube amber.
func TestMatrixCubeTintSetting(t *testing.T) {
	if c := (matrixEffectsConfig{CubeTint: "#ff40e0"}).cubeTint(); c != (color.NRGBA{0xff, 0x40, 0xe0, 255}) {
		t.Errorf("cubeTint = %+v", c)
	}
	for _, bad := range []string{"", "ff40e0", "#ff40e", "#gg40e0", "magenta"} {
		if c := (matrixEffectsConfig{CubeTint: bad}).cubeTint(); c.A != 0 {
			t.Errorf("cubeTint %q = %+v, want amber", bad, c)
		}
	}
}

// TestMatrixCubeColours checks the palette gets the cube's own colours, so
// a picture with none of them does not leave the choices unreadable.
func TestMatrixCubeColours(t *testing.T) {
	if got := matrixCubeColours(0, matrixCubeEffects{}); got != nil {
		t.Errorf("no room in the palette, got %d colours", len(got))
	}
	got := matrixCubeColours(96, matrixCubeEffects{})
	if len(got) == 0 || len(got) > 96 {
		t.Fatalf("%d colours, want 1 to 96", len(got))
	}
	// The selected choice is drawn near white; the nearest palette colour
	// must be bright.
	brightest := 0
	for _, c := range got {
		brightest = max(brightest, int(c.R)+int(c.G)+int(c.B))
	}
	if brightest < 600 {
		t.Errorf("brightest cube colour sums to %d, want a near-white", brightest)
	}
}

// TestMatrixScenePaletteFullPicture checks a picture that uses all 256
// colours, none of them bright, still leaves room for the cube's colours, and
// that a picture with fewer colours keeps every one of them.
func TestMatrixScenePaletteFullPicture(t *testing.T) {
	var full color.Palette
	for r := 0; r < 16; r++ {
		for g := 0; g < 16; g++ {
			full = append(full, color.NRGBA{uint8(r), uint8(g), 0, 255})
		}
	}
	fx := matrixCubeEffects{tint: color.NRGBA{0xff, 0x40, 0xe0, 255}}
	pal := matrixScenePalette(full, fx)
	if len(pal) > 256 || len(pal) < 256-matrixCubeReserve+1 {
		t.Fatalf("scene palette has %d colours", len(pal))
	}
	// The selected choice's white lettering must find a white in the palette.
	r, g, b, _ := pal[pal.Index(color.NRGBA{255, 255, 255, 255})].RGBA()
	if r>>8+g>>8+b>>8 < 600 {
		t.Errorf("white maps to %d,%d,%d", r>>8, g>>8, b>>8)
	}

	small := full[:100]
	pal = matrixScenePalette(small, fx)
	for i, c := range small {
		if pal[i] != c {
			t.Fatalf("picture colour %d changed from %v to %v", i, c, pal[i])
		}
	}
	if len(pal) <= len(small) {
		t.Error("no cube colours were added")
	}
}

// TestMatrixCubeTintColoursGlassAndScroller checks a tinted cube has none of
// the amber glass left, and that the scrolling line takes the tint.
func TestMatrixCubeTintColoursGlassAndScroller(t *testing.T) {
	light := color.NRGBA{0xff, 0x40, 0xe0, 255}
	options := []LightbarOption{{Text: "journey onward"}, {Text: "create an account"}}
	lit := matrixTextCube(9, options, 0, matrixCubeEffects{tint: light})
	amber, drawn := 0, 0
	for p := 0; p < len(lit.Pix); p += 4 {
		r, g, b := int(lit.Pix[p]), int(lit.Pix[p+1]), int(lit.Pix[p+2])
		if r+g+b == 0 {
			continue
		}
		drawn++
		// Amber has more green than blue; this tint has more blue.
		if g > b+12 {
			amber++
		}
	}
	if drawn == 0 || amber > 0 {
		t.Errorf("%d of %d cube pixels are still amber", amber, drawn)
	}
	if c := matrixScrollerColour(1, light); c != (color.RGBA{0xff, 0x40, 0xe0, 255}) {
		t.Errorf("scroller at full glow = %+v, want the tint", c)
	}
	if c := matrixScrollerColour(1, color.NRGBA{}); c != (color.RGBA{255, 220, 60, 255}) {
		t.Errorf("unlit scroller at full glow = %+v, want amber", c)
	}
}

// TestMatrixTintedCubeIsClear checks a tinted cube's faces are mostly dark,
// clear glass with bright detail on them, not a solid block of the tint.
func TestMatrixTintedCubeIsClear(t *testing.T) {
	options := []LightbarOption{{Text: "journey onward"}, {Text: "create an account"}}
	lit := matrixTextCube(9, options, 0, matrixCubeEffects{tint: color.NRGBA{0xff, 0x40, 0xe0, 255}})
	// Sample a strip of the front face.
	dim, bright, n := 0, 0, 0
	for y := 60; y < 180; y++ {
		for x := 70; x < 100; x++ {
			p := lit.NRGBAAt(x, y)
			level := max(int(p.R), max(int(p.G), int(p.B)))
			if level == 0 {
				continue
			}
			n++
			if level < 90 {
				dim++
			}
			if level > 200 {
				bright++
			}
		}
	}
	if n == 0 || dim*10 < n*6 {
		t.Errorf("%d of %d face pixels are dim; the glass is not clear", dim, n)
	}
	// Edges are drawn at full brightness somewhere on the cube.
	edge := 0
	for p := 0; p < len(lit.Pix); p += 4 {
		if lit.Pix[p] > 240 {
			edge++
		}
	}
	if edge < 200 {
		t.Errorf("only %d full-brightness pixels; edges and lettering are dimmed", edge)
	}
	_ = bright
}

// TestMatrixCubeGlitter checks the glitter setting replaces the embers with
// flakes that move from frame to frame and flash white.
func TestMatrixCubeGlitter(t *testing.T) {
	options := []LightbarOption{{Text: "journey onward"}, {Text: "create an account"}}
	tint := color.NRGBA{0xff, 0x40, 0xe0, 255}
	differ := func(a, b *image.NRGBA) (n int) {
		for p := 0; p < len(a.Pix); p += 4 {
			if a.Pix[p] != b.Pix[p] || a.Pix[p+1] != b.Pix[p+1] || a.Pix[p+2] != b.Pix[p+2] {
				n++
			}
		}
		return n
	}
	embers := matrixTextCube(9, options, 0, matrixCubeEffects{tint: tint})
	glitter := matrixTextCube(9, options, 0, matrixCubeEffects{tint: tint, glitter: true})
	if n := differ(embers, glitter); n < 100 {
		t.Errorf("glitter changed %d pixels of the ember cube", n)
	}
	// With no particles the two would differ only where the cube turned;
	// here the cube is the same and only the flakes separate the two.
	white := 0
	for p := 0; p < len(glitter.Pix); p += 4 {
		if embers.Pix[p] == glitter.Pix[p] && embers.Pix[p+1] == glitter.Pix[p+1] && embers.Pix[p+2] == glitter.Pix[p+2] {
			continue
		}
		if glitter.Pix[p] > 225 && glitter.Pix[p+1] > 200 && glitter.Pix[p+2] > 225 {
			white++
		}
	}
	if white == 0 {
		t.Error("no flake is flashing white")
	}
	if got := (matrixEffectsConfig{Glitter: true, CubeTint: "#ff40e0"}).cubeEffects(); !got.glitter || got.tint != tint {
		t.Errorf("cubeEffects = %+v", got)
	}
}

// TestMatrixCubeSelectedChoiceStandsOut checks a tinted cube marks the
// selected choice with a bar: its row is brighter overall than the same row
// when another choice is selected.
func TestMatrixCubeSelectedChoiceStandsOut(t *testing.T) {
	options := []LightbarOption{{Text: "journey onward"}, {Text: "create an account"}, {Text: "check your access"}}
	fx := matrixCubeEffects{tint: color.NRGBA{0xff, 0x40, 0xe0, 255}}
	// At elapsed 0 the cube is close to square-on, so the rows of the front
	// face run across the picture.
	first, second := matrixTextCube(0, options, 0, fx), matrixTextCube(0, options, 1, fx)
	rowLight := func(img *image.NRGBA, y int) (n int) {
		for x := 110; x < 210; x++ {
			p := img.NRGBAAt(x, y)
			n += int(p.R) + int(p.G) + int(p.B)
		}
		return n
	}
	// The row that gains most light when a choice is selected is its bar.
	gains := func(on, off *image.NRGBA) (row, gain int) {
		for y := 0; y < 240; y++ {
			if g := rowLight(on, y) - rowLight(off, y); g > gain {
				row, gain = y, g
			}
		}
		return row, gain
	}
	a, gainA := gains(first, second)
	b, gainB := gains(second, first)
	// The bar follows the selection down the face.
	if gainA < 5000 || gainB < 5000 || a >= b {
		t.Errorf("first choice's bar at row %d (gain %d), second's at row %d (gain %d)", a, gainA, b, gainB)
	}
}

// TestMatrixCubePixelAspect checks pixelAspect shortens the cube in pixel
// rows, and that out-of-range values are ignored.
func TestMatrixCubePixelAspect(t *testing.T) {
	options := []LightbarOption{{Text: "journey onward"}}
	height := func(fx matrixCubeEffects) int {
		img := matrixTextCube(0, options, 0, fx)
		top, bottom := -1, -1
		for y := 0; y < 240; y++ {
			for x := 0; x < 320; x++ {
				if p := img.NRGBAAt(x, y); int(p.R)+int(p.G)+int(p.B) > 30 {
					if top < 0 {
						top = y
					}
					bottom = y
					break
				}
			}
		}
		return bottom - top + 1
	}
	square, tall := height(matrixCubeEffects{}), height(matrixCubeEffects{pixelAspect: 1.2})
	if want := float64(square) / 1.2; float64(tall) < want-3 || float64(tall) > want+3 {
		t.Errorf("cube is %d rows with square pixels and %d with 1.2; want about %.0f", square, tall, want)
	}
	// Pixels wider than tall narrow the cube instead of stretching it past
	// the top and bottom of the canvas, at either end of the accepted range
	// and as it turns.
	options = []LightbarOption{{Text: "journey onward"}, {Text: "create an account"}, {Text: "check your access"}, {Text: "disconnect"}}
	for _, aspect := range []float64{.5, 1.2, 2} {
		for _, at := range []float64{0, 2.3, 5.1} {
			img := matrixTextCube(at, options, 0, matrixCubeEffects{pixelAspect: aspect})
			for x := 0; x < 320; x++ {
				for _, y := range []int{0, 239} {
					if p := img.NRGBAAt(x, y); int(p.R)+int(p.G)+int(p.B) > 30 {
						t.Fatalf("pixelAspect %v at %v: the cube reaches row %d", aspect, at, y)
					}
				}
			}
		}
	}
	width := func(fx matrixCubeEffects) int {
		img := matrixTextCube(0, options, 0, fx)
		left, right := -1, -1
		for x := 0; x < 320; x++ {
			for y := 0; y < 240; y++ {
				if p := img.NRGBAAt(x, y); int(p.R)+int(p.G)+int(p.B) > 30 {
					if left < 0 {
						left = x
					}
					right = x
					break
				}
			}
		}
		return right - left + 1
	}
	wide, narrow := width(matrixCubeEffects{}), width(matrixCubeEffects{pixelAspect: .5})
	if want := float64(wide) * .5; float64(narrow) < want-3 || float64(narrow) > want+3 {
		t.Errorf("cube is %d columns with square pixels and %d with 0.5; want about %.0f", wide, narrow, want)
	}
	if h := height(matrixCubeEffects{pixelAspect: .5}); h < square-3 || h > square+3 {
		t.Errorf("cube is %d rows with 0.5, want about %d", h, square)
	}
	for _, bad := range []float64{0, .2, 3, -1} {
		if fx := (matrixEffectsConfig{PixelAspect: bad}).cubeEffects(); fx.pixelAspect != 0 {
			t.Errorf("pixelAspect %v was accepted as %v", bad, fx.pixelAspect)
		}
	}
	if fx := (matrixEffectsConfig{PixelAspect: 1.2}).cubeEffects(); fx.pixelAspect != 1.2 {
		t.Errorf("pixelAspect 1.2 came through as %v", fx.pixelAspect)
	}
}
