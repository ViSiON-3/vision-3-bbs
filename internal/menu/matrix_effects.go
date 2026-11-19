package menu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"math/rand"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/sixel"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/version"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"golang.org/x/term"
)

// Matrix effects are opt-in: existing PDMATRIX layouts may put controls where
// the side graphics would otherwise be drawn.
type matrixEffectsConfig struct {
	Heat        bool `json:"heat"`
	Polygons    bool `json:"polygons"`
	TextFade    bool `json:"textFade"`
	ProjectText bool `json:"projectText"`
	CRTOn       bool `json:"crtOn"`
	// Layout places the picture and the projectText cube: "" has the cube
	// over the centred picture, "side" has the picture to the left of the
	// cube.
	Layout string `json:"layout"`
	// CubeTint is a colour, as "#rrggbb", for the projectText cube. The
	// cube is then clear glass of that colour and the scrolling line is
	// drawn in it. Empty leaves both amber.
	CubeTint string `json:"cubeTint"`
	// Glitter swirls the cube's particles round its axis as flakes that
	// flash white, in place of embers rising through it.
	Glitter bool `json:"glitter"`
	// PixelAspect is the height of a pixel over its width on the callers'
	// screens. SyncTERM shows its 640x400 screen at 4:3, so its pixels are
	// 1.2 times as tall as they are wide and a cube drawn for square pixels
	// looks tall; 1.2 here draws it as tall as it is wide there. Values
	// outside 0.5 to 2 are ignored.
	PixelAspect float64 `json:"pixelAspect"`
}

// cubeEffects is the cube's standing look from the effect file.
func (cfg matrixEffectsConfig) cubeEffects() matrixCubeEffects {
	fx := matrixCubeEffects{tint: cfg.cubeTint(), glitter: cfg.Glitter}
	if cfg.PixelAspect >= .5 && cfg.PixelAspect <= 2 {
		fx.pixelAspect = cfg.PixelAspect
	}
	return fx
}

// cubeTint is CubeTint as a colour; its alpha is zero when the setting is
// empty or not a "#rrggbb" value.
func (cfg matrixEffectsConfig) cubeTint() color.NRGBA {
	var r, g, b uint8
	if len(cfg.CubeTint) != 7 {
		return color.NRGBA{}
	}
	if n, err := fmt.Sscanf(cfg.CubeTint, "#%02x%02x%02x", &r, &g, &b); err != nil || n != 3 {
		return color.NRGBA{}
	}
	return color.NRGBA{R: r, G: g, B: b, A: 255}
}

// matrixCubeScene is where drawCenteredMatrix puts the picture and the cube,
// in pixels of its 640x368 frame. cubeTop is where the cube comes to rest.
type matrixCubeScene struct{ logoLeft, logoTop, cubeLeft, cubeTop int }

// The cube's width changes as it turns: its corners reach columns 61 to 257
// of its 320-pixel frame at the widest, and a face seen square-on spans about
// 88 to 232. The side layout measures the cube by the middle of that range,
// so its distance from the border matches the picture's on average.
const matrixCubeMinX, matrixCubeMaxX = 74, 245

// matrixCubeLayout places a picture and the 320x240 cube for a layout name.
// art is the part of the picture that is not black background.
//
// "side" puts the picture's art on the left and the cube on the right, with
// the art as far from the left border as the cube is from the right border,
// and the same distance between the two. Both sit in
// the middle of the rows above the scrolling line. Anything else keeps the
// cube over the centred picture.
func matrixCubeLayout(layout string, picture, art image.Rectangle) matrixCubeScene {
	const width, cubeW, cubeH, cubeTop = 640, 320, 240, 64
	if layout == "side" {
		const sideTop = 48
		if art.Empty() {
			art = picture
		}
		margin := max(0, (width-art.Dx()-(matrixCubeMaxX-matrixCubeMinX+1))/3)
		return matrixCubeScene{
			logoLeft: margin - (art.Min.X - picture.Min.X),
			logoTop:  sideTop + (cubeH-picture.Dy())/2,
			cubeLeft: width - margin - (matrixCubeMaxX + 1),
			cubeTop:  sideTop,
		}
	}
	return matrixCubeScene{logoLeft: (width - picture.Dx()) / 2, logoTop: 112, cubeLeft: (width - cubeW) / 2, cubeTop: cubeTop}
}

// matrixPictureArt is the smallest rectangle holding every pixel of img that
// is not near-black background.
func matrixPictureArt(img *image.NRGBA) image.Rectangle {
	b := img.Bounds()
	art := image.Rectangle{}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.NRGBAAt(x, y)
			if int(c.R)+int(c.G)+int(c.B) > 60 {
				art = art.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return art
}

type matrixScene struct {
	source           *image.NRGBA
	logoFrame        *image.NRGBA
	weldFrame        *image.NRGBA
	pal              color.Palette
	previous         []int16
	polygonPrevious  [2][]int16
	textCubePrevious []int16
	scrollFrames     int // frames drawn since the scrolling line began
	selectionChange  float64
	selectionActive  bool
	activation       float64
	weldLight        float64
	cache            map[color.NRGBA]int16
	burn             *matrixLogoBurn // the main menu's logo effects, made on first use
	art              image.Rectangle // the picture without its black background
	col              int
	config           matrixEffectsConfig
	node             int
	seed             float64
}

// matrixEffects reads PDMATRIX.EFX. ok is false when the menu set has none,
// it cannot be read, or it turns no effect on.
func (e *MenuExecutor) matrixEffects() (cfg matrixEffectsConfig, ok bool) {
	configPath, _, found, err := e.Menus().Locate("ansi", "PDMATRIX.EFX")
	if err != nil || !found {
		return cfg, false
	}
	f, err := os.Open(configPath)
	if err != nil {
		return cfg, false
	}
	defer func() { _ = f.Close() }()
	if err = json.NewDecoder(io.LimitReader(f, 4096)).Decode(&cfg); err != nil || (!cfg.Heat && !cfg.Polygons && !cfg.TextFade && !cfg.ProjectText) {
		return matrixEffectsConfig{}, false
	}
	return cfg, true
}

// pictureLimit is the largest PDMATRIX.PNG the matrix draws, in pixels. The
// picture normally sits above the choices and is held to the top ten rows.
// Beside the cube it has the left half of the screen and may be as large as
// the cube.
func (cfg matrixEffectsConfig) pictureLimit() (w, h int) {
	if cfg.ProjectText && cfg.Layout == "side" {
		return 320, 240
	}
	return 640, 160
}

// matrixCubeColours returns up to n colours that the projected cube and the
// scrolling line are drawn with, for a palette taken from a picture that
// may have none of them. Without them the choices on the cube are mapped to
// the picture's nearest colours and can be unreadable.
func matrixCubeColours(n int, fx matrixCubeEffects) []color.NRGBA {
	if n <= 0 {
		return nil
	}
	options := []LightbarOption{{Text: "journey onward"}, {Text: "create an account"}, {Text: "check your access"}, {Text: "disconnect"}}
	seen := map[color.NRGBA]bool{}
	var all []color.NRGBA
	add := func(c color.NRGBA) {
		c.A = 255
		if !seen[c] {
			seen[c] = true
			all = append(all, c)
		}
	}
	for i, at := range []float64{0.4, 1.7, 3.1, 4.6, 6.3, 8.2} {
		cube := matrixTextCube(at, options, i%len(options), fx)
		for p := 0; p < len(cube.Pix); p += 4 {
			add(color.NRGBA{R: cube.Pix[p], G: cube.Pix[p+1], B: cube.Pix[p+2]})
		}
	}
	// The scrolling line's ramp (see matrixScrollerColour).
	for g := 0; g <= 10; g++ {
		c := matrixScrollerColour(float64(g)/10, fx.tint)
		add(color.NRGBA{R: c.R, G: c.G, B: c.B})
	}
	return matrixMedianCut(all, n)
}

// matrixCubeReserve is how many of the scene's 256 colours are kept for the
// cube and the scrolling line. A picture with more than the rest is reduced
// to fit.
const matrixCubeReserve = 64

// matrixMedianCut reduces colours to at most n by splitting the widest box
// until there are n, then averaging each. It returns colours unchanged when
// there are n or fewer.
func matrixMedianCut(all []color.NRGBA, n int) []color.NRGBA {
	if len(all) <= n {
		return all
	}
	boxes := [][]color.NRGBA{all}
	channel := func(c color.NRGBA, i int) int { return int([3]uint8{c.R, c.G, c.B}[i]) }
	for len(boxes) < n {
		box, ch, widest := -1, 0, 0
		for bi, b := range boxes {
			if len(b) < 2 {
				continue
			}
			for i := 0; i < 3; i++ {
				lo, hi := 255, 0
				for _, c := range b {
					lo, hi = min(lo, channel(c, i)), max(hi, channel(c, i))
				}
				if hi-lo > widest {
					box, ch, widest = bi, i, hi-lo
				}
			}
		}
		if box < 0 {
			break
		}
		b := boxes[box]
		sort.Slice(b, func(i, j int) bool { return channel(b[i], ch) < channel(b[j], ch) })
		boxes[box] = b[:len(b)/2]
		boxes = append(boxes, b[len(b)/2:])
	}
	out := make([]color.NRGBA, 0, len(boxes))
	for _, b := range boxes {
		var r, g, bl int
		for _, c := range b {
			r, g, bl = r+int(c.R), g+int(c.G), bl+int(c.B)
		}
		out = append(out, color.NRGBA{R: uint8(r / len(b)), G: uint8(g / len(b)), B: uint8(bl / len(b)), A: 255})
	}
	return out
}

// matrixScenePalette is a picture's palette with the cube's colours added.
// A picture with more than 256-matrixCubeReserve colours is first reduced to
// that many, and its pixels are drawn in the nearest of them.
func matrixScenePalette(picture color.Palette, fx matrixCubeEffects) color.Palette {
	pal := picture
	if len(pal) > 256-matrixCubeReserve {
		all := make([]color.NRGBA, len(pal))
		for i, c := range pal {
			all[i] = color.NRGBAModel.Convert(c).(color.NRGBA)
		}
		pal = make(color.Palette, 0, 256)
		for _, c := range matrixMedianCut(all, 256-matrixCubeReserve) {
			pal = append(pal, c)
		}
	}
	have := map[color.NRGBA]bool{}
	for _, c := range pal {
		have[color.NRGBAModel.Convert(c).(color.NRGBA)] = true
	}
	for _, c := range matrixCubeColours(256-len(pal), fx) {
		if !have[c] {
			have[c] = true
			pal = append(pal, c)
		}
	}
	return pal
}

func (e *MenuExecutor) newMatrixScene(width, height, col, node int) *matrixScene {
	if width < 80 || height < 24 {
		return nil
	}
	cfg, ok := e.matrixEffects()
	if !ok {
		return nil
	}
	path, _, ok, err := e.Menus().Locate("ansi", "PDMATRIX.PNG")
	if err != nil || !ok {
		return nil
	}
	f2, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f2.Close() }()
	// Check this read independently: the overlay could change between loads.
	raw, err := io.ReadAll(io.LimitReader(f2, 8*1024*1024+1))
	if err != nil || len(raw) > 8*1024*1024 {
		return nil
	}
	dim, err := png.DecodeConfig(bytes.NewReader(raw))
	maxW, maxH := cfg.pictureLimit()
	if err != nil || dim.Width <= 0 || dim.Height <= 0 || dim.Width > maxW || dim.Height > maxH {
		return nil
	}
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	b := decoded.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 || b.Dx() > maxW || b.Dy() > maxH {
		return nil
	}
	pal := sixel.Palette(decoded)
	if len(pal) == 0 {
		return nil
	} // fixed registers are essential
	if cfg.ProjectText {
		pal = matrixScenePalette(pal, cfg.cubeEffects())
	}
	img := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(img, img.Bounds(), decoded, b.Min, draw.Src)
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 255 {
			return nil
		}
	} // deltas need opaque base
	scene := &matrixScene{source: img, pal: pal, col: col, config: cfg, node: node, cache: make(map[color.NRGBA]int16), seed: rand.Float64() * 6.28}
	scene.art = matrixPictureArt(img)
	scene.previous = scene.indices(img)
	return scene
}
func (s *matrixScene) indices(img *image.NRGBA) []int16 {
	out := make([]int16, img.Bounds().Dx()*img.Bounds().Dy())
	for i := range out {
		p := i * 4
		c := color.NRGBA{R: img.Pix[p], G: img.Pix[p+1], B: img.Pix[p+2], A: 255}
		idx, ok := s.cache[c]
		if !ok {
			idx = int16(s.pal.Index(c))
			if len(s.cache) < 65536 {
				s.cache[c] = idx
			}
		}
		out[i] = idx
	}
	return out
}
func (s *matrixScene) reset() {
	s.previous = s.indices(s.source)
	s.polygonPrevious = [2][]int16{}
	s.textCubePrevious = nil
	s.logoFrame = s.source
}
func (s *matrixScene) draw(t *term.Terminal, elapsed float64, options []LightbarOption, selected int, outputMode ansi.OutputMode) error {
	// The logo layer is redrawn on every frame. On a clock of its own it
	// fell on some frames and not others, and the embers moved in uneven steps.
	if s.config.Heat {
		img := image.NewNRGBA(s.source.Bounds())
		copy(img.Pix, s.source.Pix)
		weld := image.NewNRGBA(img.Bounds())
		// Heat, embers and weld bursts as the main menu's logo.
		if s.burn == nil {
			s.burn = newMatrixLogoBurn(s.source)
		}
		s.weldLight = s.burn.paint(img, weld, elapsed)
		// An infrequent clean white "ting" on the finished 3.
		glintPhase := math.Mod(elapsed+s.seed*2, 23)
		if glintPhase < .8 && img.Bounds().Dx() >= 480 && img.Bounds().Dy() >= 140 {
			origin := matrixLogoEdge(s.source, image.Pt(457, 22), 410, 479)
			power := math.Sin(math.Pi * glintPhase / .8)
			for dy := -5; dy <= 5; dy++ {
				for dx := -5; dx <= 5; dx++ {
					if dx != 0 && dy != 0 {
						continue
					}
					p := origin.Add(image.Pt(dx, dy))
					if !p.In(img.Bounds()) {
						continue
					}
					strength := power * math.Max(0, 1-float64(absInt(dx)+absInt(dy))/6)
					old := img.NRGBAAt(p.X, p.Y)
					c := color.NRGBA{uint8(float64(old.R)*(1-strength) + 255*strength), uint8(float64(old.G)*(1-strength) + 255*strength), uint8(float64(old.B)*(1-strength) + 255*strength), 255}
					img.SetNRGBA(p.X, p.Y, c)
					if strength > .04 {
						weld.SetNRGBA(p.X, p.Y, c)
					}
				}
			}
		}
		now := s.indices(img)
		delta := make([]int16, len(now))
		changed := false
		for i, p := range now {
			if p == s.previous[i] {
				delta[i] = -1
			} else {
				delta[i] = p
				changed = true
			}
		}
		s.logoFrame = img
		s.weldFrame = weld
		if changed && s.config.ProjectText {
			s.previous = now
		}
		if changed && !s.config.ProjectText {
			// The logo overlaps the projected cube. Redraw the cube afterward
			// so the header animation cannot paint over its upper edge.
			if s.config.ProjectText {
				s.textCubePrevious = nil
			}
			if _, err := fmt.Fprintf(t, "\x1b[1;%dH", s.col); err != nil {
				return err
			}
			if _, err := t.Write(sixel.EncodeIndexedReuse(img.Bounds(), s.pal, delta)); err != nil {
				return err
			}
			s.previous = now
		}
	}
	if s.config.ProjectText {
		return s.drawCenteredMatrix(t, elapsed, options, selected)
	}
	if s.config.Polygons {
		for side := 0; side < 2; side++ {
			col := 2
			if side == 1 {
				col = 56
			}
			poly := matrixPolygon(elapsed+float64(side)*1.3, s.seed+float64(side)*2)
			current := s.indices(poly)
			var encoded []byte
			if s.polygonPrevious[side] == nil {
				encoded = sixel.EncodePaletteReuse(poly, s.pal)
			} else {
				delta := make([]int16, len(current))
				changed := false
				for i, p := range current {
					if p == s.polygonPrevious[side][i] {
						delta[i] = -1
					} else {
						delta[i] = p
						changed = true
					}
				}
				if changed {
					encoded = sixel.EncodeIndexedReuse(poly.Bounds(), s.pal, delta)
				}
			}
			if len(encoded) > 0 {
				if _, err := fmt.Fprintf(t, "\x1b[11;%dH", col); err != nil {
					return err
				}
				if _, err := t.Write(encoded); err != nil {
					return err
				}
			}
			s.polygonPrevious[side] = current
		}
	}
	if s.config.TextFade {
		return drawMatrixFadedOptions(t, options, selected, elapsed, outputMode)
	}
	return nil
}

// The moving grayscale highlight follows the menu lettering. The marker
// makes selection clear without replacing the letters with an opaque bar.
func drawMatrixFadedOptions(t *term.Terminal, options []LightbarOption, selected int, elapsed float64, outputMode ansi.OutputMode) error {
	for i, opt := range options {
		var out strings.Builder
		fmt.Fprintf(&out, "\x1b[%d;%dH\x1b[0;40m", opt.Y, max(1, opt.X-2))
		if i == selected {
			out.WriteString("\x1b[38;2;255;235;205m> ")
		} else {
			out.WriteString("  ")
		}
		text := []rune(opt.Text)
		sweep := math.Mod(elapsed*5+float64(i)*2, float64(len(text)+10)) - 5
		for j, ch := range text {
			glow := math.Max(0, 1-math.Abs(float64(j)-sweep)/5)
			base := 90.0
			if i == selected {
				base = 155
			}
			level := int(math.Round(base + (255-base)*glow))
			fmt.Fprintf(&out, "\x1b[38;2;%d;%d;%dm%c", level, level, level, ch)
		}
		out.WriteString("\x1b[0m")
		if err := terminalio.WriteProcessedBytes(t, []byte(out.String()), outputMode); err != nil {
			return err
		}
	}
	return nil
}

type matrixVec struct{ x, y, z float64 }
type matrixPoint struct{ x, y, z float64 }

// matrixPolygon uses the same cube/pyramid/diamond vertices, translucent
// faces, ember particles, and three-axis tumble as the main-menu indicator.
func matrixPolygon(t, seed float64) *image.NRGBA {
	const w, h = 192, 208
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	cycle := int(math.Floor((t + seed) / 7))
	sequence := [7]int{0, 1, 2, 0, 2, 1, 0}
	if int(seed*100)%2 != 0 {
		sequence = [7]int{0, 2, 1, 0, 1, 2, 0}
	}
	from, to := sequence[cycle%6], sequence[cycle%6+1]
	blend := math.Mod(t+seed, 7)
	blend = math.Max(0, math.Min(1, blend/1.8))
	blend = blend * blend * (3 - 2*blend)
	vertex := func(shape, v int) matrixVec {
		if shape == 0 {
			return matrixVec{float64((v&1)*2 - 1), float64(((v>>1)&1)*2 - 1), float64(((v>>2)&1)*2 - 1)}
		}
		if shape == 1 {
			if v < 4 {
				return matrixVec{float64((v&1)*2 - 1), -1, float64(((v>>1)&1)*2 - 1)}
			}
			return matrixVec{0, 1.25, 0}
		}
		d := [8]matrixVec{{-1.35, 0, 0}, {0, 0, -1.35}, {0, 0, 1.35}, {1.35, 0, 0}, {0, 1.6, 0}, {0, 1.6, 0}, {0, -1.6, 0}, {0, -1.6, 0}}
		return d[v]
	}
	clock := t * 6.25
	yaw := clock*(.075+.025*math.Sin(seed)) + .42*math.Sin(clock*.027+seed)
	pitch := .4 + clock*(.035+.012*math.Cos(seed*1.3)) + .34*math.Sin(clock*.021+seed*1.7)
	roll := clock*(.025+.010*math.Sin(seed*1.9)) + .24*math.Sin(clock*.033+seed*.7)
	cs, sn := math.Cos(yaw), math.Sin(yaw)
	ct, st := math.Cos(pitch), math.Sin(pitch)
	cr, sr := math.Cos(roll), math.Sin(roll)
	project := func(p matrixVec) matrixPoint {
		rx := p.x*cs + p.z*sn
		rz := p.z*cs - p.x*sn
		ry := p.y*ct - rz*st
		rz = p.y*st + rz*ct
		xx := rx*cr - ry*sr
		yy := rx*sr + ry*cr
		persp := 3.8 / (4.8 + rz)
		return matrixPoint{w/2 + xx*persp*48, h/2 - yy*persp*48, rz}
	}
	pts := [8]matrixPoint{}
	for i := range pts {
		a, b := vertex(from, i), vertex(to, i)
		pts[i] = project(matrixVec{a.x + (b.x-a.x)*blend, a.y + (b.y-a.y)*blend, a.z + (b.z-a.z)*blend})
	}
	// Internal embers are projected in object space before the glass faces.
	for i := 0; i < 32; i++ {
		phase := seed + float64(i)*2.399
		age := clock*.075 + phase
		p := project(matrixVec{.32 * math.Sin(age*.73+phase), .32 * math.Sin(age*.91+phase*.7), .32 * math.Cos(age*.61+phase)})
		flick := .5 + .5*math.Sin(age*2.3+phase)
		x, y := int(math.Round(p.x)), int(math.Round(p.y))
		if image.Pt(x, y).In(img.Bounds()) {
			img.SetNRGBA(x, y, color.NRGBA{255, uint8(100 + 135*flick), uint8(15 + 100*flick), 255})
		}
	}
	faces := [][4]int{{0, 1, 3, 2}, {4, 5, 7, 6}, {0, 1, 5, 4}, {2, 3, 7, 6}, {0, 2, 6, 4}, {1, 3, 7, 5}}
	sort.SliceStable(faces, func(i, j int) bool {
		sum := func(f [4]int) float64 { return pts[f[0]].z + pts[f[1]].z + pts[f[2]].z + pts[f[3]].z }
		return sum(faces[i]) > sum(faces[j])
	})
	edge := func(a, b matrixPoint, x, y float64) float64 { return (x-a.x)*(b.y-a.y) - (y-a.y)*(b.x-a.x) }
	triangle := func(a, b, c matrixPoint, shade float64) {
		area := edge(a, b, c.x, c.y)
		if math.Abs(area) < .1 {
			return
		}
		x0 := max(0, int(math.Floor(math.Min(a.x, math.Min(b.x, c.x)))))
		x1 := min(w-1, int(math.Ceil(math.Max(a.x, math.Max(b.x, c.x)))))
		y0 := max(0, int(math.Floor(math.Min(a.y, math.Min(b.y, c.y)))))
		y1 := min(h-1, int(math.Ceil(math.Max(a.y, math.Max(b.y, c.y)))))
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				e1, e2, e3 := edge(a, b, float64(x)+.5, float64(y)+.5), edge(b, c, float64(x)+.5, float64(y)+.5), edge(c, a, float64(x)+.5, float64(y)+.5)
				if area > 0 && (e1 < 0 || e2 < 0 || e3 < 0) || area < 0 && (e1 > 0 || e2 > 0 || e3 > 0) {
					continue
				}
				old := img.NRGBAAt(x, y)
				alpha := .24
				clamp := func(v float64) uint8 { return uint8(math.Min(255, math.Max(0, v))) }
				img.SetNRGBA(x, y, color.NRGBA{clamp(float64(old.R)*(1-alpha) + 200*shade*alpha), clamp(float64(old.G)*(1-alpha) + 70*shade*alpha), clamp(float64(old.B)*(1-alpha) + 12*shade*alpha), 255})
			}
		}
	}
	for i, f := range faces {
		shade := .75 + float64(i)/5*.55
		triangle(pts[f[0]], pts[f[1]], pts[f[2]], shade)
		triangle(pts[f[0]], pts[f[2]], pts[f[3]], shade)
	}
	edges := [][2]int{{0, 1}, {2, 3}, {4, 5}, {6, 7}, {0, 2}, {1, 3}, {4, 6}, {5, 7}, {0, 4}, {1, 5}, {2, 6}, {3, 7}}
	if from == 2 || to == 2 {
		edges = append(edges, [2]int{0, 6}, [2]int{1, 7}, [2]int{2, 4}, [2]int{3, 5})
	}
	for _, e := range edges {
		a, b := pts[e[0]], pts[e[1]]
		steps := max(1, int(math.Ceil(math.Max(math.Abs(b.x-a.x), math.Abs(b.y-a.y)))))
		shine := math.Max(0, math.Min(1, 1-((a.z+b.z)*.5+1.5)/3))
		for i := 0; i <= steps; i++ {
			u := float64(i) / float64(steps)
			x, y := int(math.Round(a.x+(b.x-a.x)*u)), int(math.Round(a.y+(b.y-a.y)*u))
			if image.Pt(x, y).In(img.Bounds()) {
				img.SetNRGBA(x, y, color.NRGBA{255, uint8(110 + 125*shine), uint8(25 + 120*shine), 255})
			}
		}
	}
	return img
}

// matrixLogoEdge finds the nearest bright silhouette pixel within one letter.
func matrixLogoEdge(img *image.NRGBA, target image.Point, minX, maxX int) image.Point {
	best := target
	bestDist := int(^uint(0) >> 1)
	for y := 1; y < img.Bounds().Dy()-1; y++ {
		for x := max(1, minX); x <= min(maxX, img.Bounds().Dx()-2); x++ {
			c := img.NRGBAAt(x, y)
			if int(c.R)+int(c.G)+int(c.B) < 75 {
				continue
			}
			edge := false
			for _, d := range []image.Point{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
				n := img.NRGBAAt(x+d.X, y+d.Y)
				if int(n.R)+int(n.G)+int(n.B) < 45 {
					edge = true
					break
				}
			}
			if !edge {
				continue
			}
			dist := (x-target.X)*(x-target.X) + (y-target.Y)*(y-target.Y)
			if dist < bestDist {
				bestDist = dist
				best = image.Pt(x, y)
			}
		}
	}
	return best
}
func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// cubeEffects is the cube's look for this frame: its standing settings and
// the passing ones.
func (s *matrixScene) cubeEffects(magnet float64) matrixCubeEffects {
	fx := s.config.cubeEffects()
	fx.magnet, fx.weld, fx.activate = magnet, s.weldLight, s.activation
	return fx
}

// drawCenteredMatrix composites the animated logo and translucent cube in one
// raster, so moving one layer never erases or paints over the other.
func (s *matrixScene) drawCenteredMatrix(t *term.Terminal, elapsed float64, options []LightbarOption, selected int) error {
	frame := s.centeredMatrixFrame(elapsed, options, selected, boardTime(time.Now()))
	current := s.indices(frame)
	var encoded []byte
	if s.textCubePrevious == nil {
		encoded = sixel.EncodePalette(frame, s.pal)
	} else {
		delta := make([]int16, len(current))
		changed := false
		for i, p := range current {
			if p == s.textCubePrevious[i] {
				delta[i] = -1
			} else {
				delta[i] = p
				changed = true
			}
		}
		if changed {
			encoded = sixel.EncodeIndexedReuse(frame.Bounds(), s.pal, delta)
		}
	}
	if len(encoded) > 0 {
		if _, err := t.Write([]byte("\x1b[1;1H")); err != nil {
			return err
		}
		if _, err := t.Write(encoded); err != nil {
			return err
		}
	}
	s.textCubePrevious = current
	return nil
}

// matrixTintedGlassLight is the brightest, of 0 to 255, that a tinted cube
// lets the picture behind it show.
const matrixTintedGlassLight = 80

// centeredMatrixFrame is the scene's 640x368 picture at elapsed seconds, with
// the scrolling line showing the board time now.
func (s *matrixScene) centeredMatrixFrame(elapsed float64, options []LightbarOption, selected int, now time.Time) *image.NRGBA {
	const width, height = 640, 368
	frame := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i := 3; i < len(frame.Pix); i += 4 {
		frame.Pix[i] = 255
	}
	logo := s.logoFrame
	if logo == nil {
		logo = s.source
	}
	place := matrixCubeLayout(s.config.Layout, s.source.Bounds(), s.art)
	logoLeft, logoTop := place.logoLeft, place.logoTop
	// Fade the centered logo up from black before introducing the cube.
	const fadeDuration = 1.5
	fade := math.Max(0, math.Min(1, elapsed/fadeDuration))
	fade = fade * fade * (3 - 2*fade)
	if fade >= 1 {
		draw.Draw(frame, image.Rect(logoLeft, logoTop, logoLeft+logo.Bounds().Dx(), logoTop+logo.Bounds().Dy()), logo, image.Point{}, draw.Src)
	} else {
		for y := 0; y < logo.Bounds().Dy(); y++ {
			for x := 0; x < logo.Bounds().Dx(); x++ {
				c := logo.NRGBAAt(x, y)
				frame.SetNRGBA(logoLeft+x, logoTop+y, color.NRGBA{uint8(float64(c.R) * fade), uint8(float64(c.G) * fade), uint8(float64(c.B) * fade), 255})
			}
		}
	}
	magnet := 0.0
	if s.selectionActive {
		age := elapsed - s.selectionChange
		if age >= 0 && age < 1.4 {
			magnet = math.Exp(-age * 2)
		}
	}
	cube := matrixTextCube(elapsed, options, selected, s.cubeEffects(magnet))
	// Once the logo is visible, ease the cube up from below the screen.
	progress := math.Max(0, math.Min(1, (elapsed-fadeDuration)/2.8))
	rest := 1 - progress
	ease := 1 - rest*rest*rest
	top := int(math.Round(float64(height)*(1-ease) + float64(place.cubeTop)*ease))
	left := place.cubeLeft
	tinted := s.config.cubeTint().A != 0
	for y := 0; y < cube.Bounds().Dy(); y++ {
		for x := 0; x < cube.Bounds().Dx(); x++ {
			dst := image.Pt(left+x, top+y)
			if !dst.In(frame.Bounds()) {
				continue
			}
			p := cube.NRGBAAt(x, y)
			if p.R == 0 && p.G == 0 && p.B == 0 {
				continue
			}
			bg := frame.NRGBAAt(dst.X, dst.Y)
			// Subpixel-sized refraction: sample a nearby logo pixel only when
			// it has similar brightness, preserving the letter silhouettes.
			if p.R < 205 && dst.X > logoLeft+2 && dst.X < logoLeft+logo.Bounds().Dx()-2 && dst.Y >= logoTop && dst.Y < logoTop+logo.Bounds().Dy() {
				shift := int(math.Round(1.1 * math.Sin(float64(y)*.08+elapsed*1.3)))
				candidate := logo.NRGBAAt(dst.X-logoLeft+shift, dst.Y-logoTop)
				difference := absInt(int(bg.R)-int(candidate.R)) + absInt(int(bg.G)-int(candidate.G)) + absInt(int(bg.B)-int(candidate.B))
				if difference < 42 {
					bg = candidate
				}
			}
			// Tinted glass passes no more than matrixTintedGlassLight of
			// the picture behind it, so the choices on it keep their contrast
			// over a bright picture. A picture no brighter than that, such as
			// the black behind the cube in the side layout, passes unchanged.
			if hi := max(bg.R, bg.G, bg.B); tinted && hi > matrixTintedGlassLight {
				f := float64(matrixTintedGlassLight) / float64(hi)
				bg = color.NRGBA{uint8(float64(bg.R) * f), uint8(float64(bg.G) * f), uint8(float64(bg.B) * f), 255}
			}
			// Transmitted light adds the glass tint without darkening the logo
			// edge. A hard brightness cutoff here creates black moving bands.
			// The amber cube is clearer than a tinted one.
			glassShare := .58
			if !tinted {
				glassShare = .44
			}
			transmit := func(background, glass uint8) uint8 {
				b, g := float64(background), float64(glass)
				return uint8(math.Min(255, b+g*glassShare*(1-b/255)))
			}
			out := color.NRGBA{transmit(bg.R, p.R), transmit(bg.G, p.G), transmit(bg.B, p.B), 255}
			// Keep the bright front-face engraving readable above the logo.
			if p.R > 205 && p.G > 75 {
				out = color.NRGBA{uint8(float64(p.R)*.94 + float64(bg.R)*.06), uint8(float64(p.G)*.94 + float64(bg.G)*.06), uint8(float64(p.B)*.94 + float64(bg.B)*.06), 255}
			}
			frame.SetNRGBA(dst.X, dst.Y, out)
		}
	}
	// Welds, sparks and the 3's glint sit above both surfaces.
	if s.weldFrame != nil && fade >= 1 {
		draw.Draw(frame, image.Rect(logoLeft, logoTop, logoLeft+s.weldFrame.Bounds().Dx(), logoTop+s.weldFrame.Bounds().Dy()), s.weldFrame, image.Point{}, draw.Over)
	}
	if elapsed >= fadeDuration+2.8 {
		s.scrollFrames++
		drawMatrixScroller(frame, float64(s.scrollFrames)*3/42, s.node, now, s.config.cubeTint())
	}
	return frame
}

// matrixScrollerColour is the scrolling line's colour at a point of its
// glow, 0 to 1: an amber ramp, or with a tint (the cube's) that colour
// from two thirds to full brightness.
func matrixScrollerColour(glow float64, tint color.NRGBA) color.RGBA {
	if tint.A == 0 {
		return color.RGBA{255, uint8(120 + 100*glow), uint8(25 + 35*glow), 255}
	}
	level := .66 + .34*glow
	return color.RGBA{uint8(float64(tint.R) * level), uint8(float64(tint.G) * level), uint8(float64(tint.B) * level), 255}
}

// drawMatrixScroller draws the board information as a moving sine wave in the
// reserved bottom band, using board-local time and the session node.
func drawMatrixScroller(img *image.NRGBA, elapsed float64, node int, now time.Time, tint color.NRGBA) {
	message := fmt.Sprintf("ViSiON/3 %s   *   %s   *   NODE %d   *   ", version.Display(), now.Format("Monday  02 Jan 2006  3:04 PM MST"), node)
	const step = 8
	// The first character enters from the right edge and travels left;
	// subsequent characters follow, with no text drawn ahead of them.
	distance := int(math.Max(0, elapsed) * 42)
	first := max(0, (distance-img.Bounds().Dx())/step-1)
	last := distance / step
	d := font.Drawer{Dst: img, Face: basicfont.Face7x13}
	for i := first; i <= last; i++ {
		idx := i % len(message)
		px := img.Bounds().Dx() - distance + i*step
		if px >= img.Bounds().Dx() {
			continue
		}
		if px < -step {
			continue
		}
		y := 350 + int(math.Round(5*math.Sin(float64(px)*.025-elapsed*2)))
		glow := .5 + .5*math.Sin(float64(px)*.018-elapsed*2)
		d.Src = image.NewUniform(matrixScrollerColour(glow, tint))
		d.Dot = fixed.P(px, y)
		d.DrawString(string(message[idx]))
	}
}
