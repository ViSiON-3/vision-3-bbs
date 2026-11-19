package scripting

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/ViSiON-3/vision-3-bbs/internal/sixel"
	"github.com/dop251/goja"
)

// maxCanvasWidth and maxCanvasHeight cap each side of a canvas. Checking the
// sides, not the pixel count, keeps a canvas inside a 1280x1024 screen.
const (
	maxCanvasWidth  = 1280
	maxCanvasHeight = 1024
)

// registerCanvas installs a small server-side bitmap API. The size limit and
// canvas count bound memory even when a script keeps every canvas alive.
func registerCanvas(gfx *goja.Object, eng *Engine) {
	vm := eng.vm
	count := 0
	fail := func(format string, args ...any) { panic(vm.NewGoError(fmt.Errorf(format, args...))) }
	jsutil.Set(gfx, "canvas", func(call goja.FunctionCall) goja.Value {
		w, h := call.Argument(0).ToInteger(), call.Argument(1).ToInteger()
		if w <= 0 || h <= 0 || w > maxCanvasWidth || h > maxCanvasHeight {
			fail("canvas is %dx%d; it must be 1x1 to %dx%d", w, h, maxCanvasWidth, maxCanvasHeight)
		}
		if count >= 4 {
			fail("a script may create at most four canvases")
		}
		var pal color.Palette
		if len(call.Arguments) > 2 && !goja.IsUndefined(call.Argument(2)) && !goja.IsNull(call.Argument(2)) {
			path := call.Argument(2).String()
			img, _, err := gfxPicture(sandboxRoot(eng.cfg), path)
			if err != nil {
				fail("canvas palette: %v", err)
			}
			pal = sixel.Palette(img)
			if len(pal) == 0 {
				fail("canvas palette must contain 1 to 256 opaque colours")
			}
		} else {
			pal = sixel.DefaultPalette()
		}
		count++
		img := image.NewNRGBA(image.Rect(0, 0, int(w), int(h)))
		var previous []int16
		previousOpaque := false
		paletteCache := make(map[color.NRGBA]int16)
		paletteIndexes := func() []int16 {
			indexes := make([]int16, int(w*h))
			for i := range indexes {
				p := i * 4
				if img.Pix[p+3] < 128 {
					indexes[i] = -1
					continue
				}
				c := color.NRGBA{R: img.Pix[p], G: img.Pix[p+1], B: img.Pix[p+2], A: 255}
				index, ok := paletteCache[c]
				if !ok {
					index = int16(pal.Index(c))
					if len(paletteCache) < 65536 {
						paletteCache[c] = index
					}
				}
				indexes[i] = index
			}
			return indexes
		}
		obj := vm.NewObject()
		jsutil.Set(obj, "width", int(w))
		jsutil.Set(obj, "height", int(h))
		clamp := func(v int64) uint8 {
			if v < 0 {
				return 0
			}
			if v > 255 {
				return 255
			}
			return uint8(v)
		}
		rgba := func(c goja.FunctionCall, offset int) color.NRGBA {
			a := int64(255)
			if len(c.Arguments) > offset+3 {
				a = c.Argument(offset + 3).ToInteger()
			}
			return color.NRGBA{R: clamp(c.Argument(offset).ToInteger()), G: clamp(c.Argument(offset + 1).ToInteger()), B: clamp(c.Argument(offset + 2).ToInteger()), A: clamp(a)}
		}
		jsutil.Set(obj, "clear", func(c goja.FunctionCall) goja.Value {
			draw.Draw(img, img.Bounds(), &image.Uniform{C: rgba(c, 0)}, image.Point{}, draw.Src)
			return goja.Undefined()
		})
		jsutil.Set(obj, "fillRect", func(c goja.FunctionCall) goja.Value {
			x, y, rw, rh := c.Argument(0).ToInteger(), c.Argument(1).ToInteger(), c.Argument(2).ToInteger(), c.Argument(3).ToInteger()
			if rw <= 0 || rh <= 0 {
				return goja.Undefined()
			}
			// Clip before addition to avoid integer overflow from hostile coordinates.
			if x >= w || y >= h || x < -maxGfxPixels || y < -maxGfxPixels {
				return goja.Undefined()
			}
			if rw > 2*maxGfxPixels {
				rw = 2 * maxGfxPixels
			}
			if rh > 2*maxGfxPixels {
				rh = 2 * maxGfxPixels
			}
			rect := image.Rect(int(x), int(y), int(x+rw), int(y+rh)).Intersect(img.Bounds())
			draw.Draw(img, rect, &image.Uniform{C: rgba(c, 4)}, image.Point{}, draw.Src)
			return goja.Undefined()
		})
		jsutil.Set(obj, "setPixel", func(c goja.FunctionCall) goja.Value {
			x, y := c.Argument(0).ToInteger(), c.Argument(1).ToInteger()
			if x >= 0 && y >= 0 && x < w && y < h {
				img.SetNRGBA(int(x), int(y), rgba(c, 2))
			}
			return goja.Undefined()
		})
		jsutil.Set(obj, "getPixel", func(c goja.FunctionCall) goja.Value {
			x, y := c.Argument(0).ToInteger(), c.Argument(1).ToInteger()
			if x < 0 || y < 0 || x >= w || y >= h {
				return goja.Null()
			}
			p := img.NRGBAAt(int(x), int(y))
			return vm.ToValue([]int{int(p.R), int(p.G), int(p.B), int(p.A)})
		})
		jsutil.Set(obj, "drawImage", func(c goja.FunctionCall) goja.Value {
			source, _, err := gfxPicture(sandboxRoot(eng.cfg), c.Argument(0).String())
			if err != nil {
				fail("canvas image: %v", err)
			}
			x, y := c.Argument(1).ToInteger(), c.Argument(2).ToInteger()
			if x >= w || y >= h || x < -maxGfxPixels || y < -maxGfxPixels {
				return goja.Undefined()
			}
			bounds := source.Bounds()
			target := image.Rect(int(x), int(y), int(x)+bounds.Dx(), int(y)+bounds.Dy())
			draw.Draw(img, target, source, bounds.Min, draw.Over)
			return goja.Undefined()
		})
		// wave shifts each row horizontally; pixels exposed at the edge become
		// transparent, or opaque black when the fourth argument is true.
		jsutil.Set(obj, "wave", func(c goja.FunctionCall) goja.Value {
			amplitude, period, phase := c.Argument(0).ToFloat(), c.Argument(1).ToFloat(), c.Argument(2).ToFloat()
			if math.IsNaN(amplitude) || math.IsInf(amplitude, 0) || math.IsNaN(period) || math.IsInf(period, 0) || math.IsNaN(phase) || math.IsInf(phase, 0) || period < 0.001 || math.Abs(amplitude) > float64(w) {
				fail("invalid wave parameters")
			}
			row := make([]byte, int(w)*4)
			opaque := len(c.Arguments) > 3 && c.Argument(3).ToBoolean()
			for y := 0; y < int(h); y++ {
				start := y * img.Stride
				copy(row, img.Pix[start:start+len(row)])
				clear(img.Pix[start : start+len(row)])
				if opaque {
					for x := 3; x < len(row); x += 4 {
						img.Pix[start+x] = 255
					}
				}
				shift := int(math.Round(amplitude * math.Sin(float64(y)*2*math.Pi/period+phase)))
				if shift >= 0 {
					copy(img.Pix[start+shift*4:start+len(row)], row[:len(row)-shift*4])
				} else {
					copy(img.Pix[start:start+len(row)+shift*4], row[-shift*4:])
				}
			}
			return goja.Undefined()
		})
		// Heat applies a rising four-octave noise, sampled on a grid for
		// terminal speeds.
		jsutil.Set(obj, "heat", func(c goja.FunctionCall) goja.Value {
			time, strength := c.Argument(0).ToFloat(), c.Argument(1).ToFloat()
			if math.IsNaN(time) || math.IsInf(time, 0) || math.Abs(time) > 1e6 || math.IsNaN(strength) || math.IsInf(strength, 0) || strength < 0 || strength > 0.1 {
				fail("invalid heat parameters")
			}
			hotOnly := len(c.Arguments) > 2 && c.Argument(2).ToBoolean()
			HeatCanvas(img, time, strength, hotOnly)
			return goja.Undefined()
		})
		jsutil.Set(obj, "present", func() bool {
			if !eng.session.Sixel {
				return false
			}
			eng.writeBytes(sixel.EncodePalette(img, pal))
			previous = paletteIndexes()
			previousOpaque = true
			for i := 3; i < len(img.Pix); i += 4 {
				if img.Pix[i] != 255 {
					previousOpaque = false
					break
				}
			}
			return true
		})
		// presentDelta assumes an opaque canvas and the same cursor position as
		// its previous presentation. Unchanged pixels stay transparent in the
		// sixel update; changed black pixels are drawn to erase old sparks.
		jsutil.Set(obj, "presentDelta", func() bool {
			if !eng.session.Sixel {
				return false
			}
			for i := 3; i < len(img.Pix); i += 4 {
				if img.Pix[i] != 255 {
					fail("presentDelta requires an opaque canvas")
				}
			}
			if previous == nil {
				eng.writeBytes(sixel.EncodePalette(img, pal))
				previous = paletteIndexes()
				previousOpaque = true
				return true
			}
			if !previousOpaque {
				fail("presentDelta requires an opaque previous frame")
			}
			current := paletteIndexes()
			delta := make([]int16, len(current))
			changed := false
			for i, p := range current {
				if p == previous[i] {
					delta[i] = -1
				} else {
					delta[i] = p
					changed = true
				}
			}
			if changed {
				eng.writeBytes(sixel.EncodeIndexedReuse(img.Bounds(), pal, delta))
			}
			previous = current
			return true
		})
		return obj
	})
}

// HeatCanvas approximates the Authentik heat shader's two animated FBM fields
// on an eight-pixel grid, then interpolates displacement at each pixel. The
// source uses bilinear sampling with edge protection. The final sixel encoder
// maps its colours to the shared palette. Coordinates are converted from image
// top-down to GLSL UVs.
func HeatCanvas(img *image.NRGBA, t, strength float64, hotOnly bool) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if strength == 0 {
		return
	}
	const step = 8
	gw, gh := (w-1)/step+2, (h-1)/step+2
	type displacement struct{ x, y float64 }
	grid := make([]displacement, gw*gh)
	for gy := 0; gy < gh; gy++ {
		for gx := 0; gx < gw; gx++ {
			u := math.Min(float64(gx*step)/float64(w), 1)
			v := 1 - math.Min(float64(gy*step)/float64(h), 1)
			nx, ny := u*6.5, v*4.2-t*0.30
			grid[gy*gw+gx] = displacement{heatFBM(nx, ny) - 0.5, heatFBM(nx+5.2+t*0.05, ny+1.3+t*0.05) - 0.5}
		}
	}
	src := append([]byte(nil), img.Pix...)
	// Fade heat away near the silhouette instead of abruptly switching it off.
	// A distance field keeps the antialiased rim stable without drawing a
	// visible seam between the protected rim and the moving metal.
	var edgeDistance []int
	if hotOnly {
		edgeDistance = make([]int, w*h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				p := y*img.Stride + x*4
				if src[p] < 24 && src[p+1] < 24 && src[p+2] < 24 {
					edgeDistance[y*w+x] = 0
				} else {
					edgeDistance[y*w+x] = 1000
				}
			}
		}
		update := func(x, y, nx, ny int) {
			if nx >= 0 && nx < w && ny >= 0 && ny < h && edgeDistance[ny*w+nx]+1 < edgeDistance[y*w+x] {
				edgeDistance[y*w+x] = edgeDistance[ny*w+nx] + 1
			}
		}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				update(x, y, x-1, y)
				update(x, y, x, y-1)
				update(x, y, x-1, y-1)
				update(x, y, x+1, y-1)
			}
		}
		for y := h - 1; y >= 0; y-- {
			for x := w - 1; x >= 0; x-- {
				update(x, y, x+1, y)
				update(x, y, x, y+1)
				update(x, y, x+1, y+1)
				update(x, y, x-1, y+1)
			}
		}
	}
	hot := func(i int) float64 {
		r, g, b := float64(src[i]), float64(src[i+1]), float64(src[i+2])
		warm := math.Max(0, math.Min(1, (r-b-25)/100))
		bright := math.Max(0, math.Min(1, (0.30*r+0.59*g+0.11*b-190)/55))
		return math.Max(warm, bright)
	}
	clamp := func(n, max int) int {
		if n < 0 {
			return 0
		}
		if n >= max {
			return max - 1
		}
		return n
	}
	for y := 0; y < h; y++ {
		gy, fy := y/step, float64(y%step)/step
		mask := math.Min(float64(y)/float64(h)*1.136363636, 1)
		mask = mask * mask * (3 - 2*mask)
		for x := 0; x < w; x++ {
			localMask := mask
			if hotOnly {
				// The source is the hot metal itself, including the upper
				// letters; the unrestricted mode retains the rising bottom mask.
				localMask = hot(y*img.Stride + x*4)
				if localMask > 0 {
					total, count := localMask*4, 4.0
					for _, p := range [][2]int{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}} {
						if p[0] >= 0 && p[0] < w && p[1] >= 0 && p[1] < h {
							total += hot(p[1]*img.Stride + p[0]*4)
							count++
						}
					}
					localMask = total / count
				}
				edgeFade := math.Max(0, math.Min(1, (float64(edgeDistance[y*w+x])-2)/12))
				localMask *= edgeFade * edgeFade * (3 - 2*edgeFade)
			}
			gx, fx := x/step, float64(x%step)/step
			a, b, c, d := grid[gy*gw+gx], grid[gy*gw+gx+1], grid[(gy+1)*gw+gx], grid[(gy+1)*gw+gx+1]
			dx := (a.x*(1-fx)+b.x*fx)*(1-fy) + (c.x*(1-fx)+d.x*fx)*fy
			dy := (a.y*(1-fx)+b.y*fx)*(1-fy) + (c.y*(1-fx)+d.y*fx)*fy
			if hotOnly && localMask == 0 {
				continue
			}
			sampleX := math.Max(0, math.Min(float64(w-1), float64(x)+dx*strength*localMask*float64(w)))
			sampleY := math.Max(0, math.Min(float64(h-1), float64(y)-dy*strength*localMask*float64(h)))
			x0, y0 := int(math.Floor(sampleX)), int(math.Floor(sampleY))
			x1, y1 := clamp(x0+1, w), clamp(y0+1, h)
			ax, ay := sampleX-float64(x0), sampleY-float64(y0)
			indices := [4]int{y0*img.Stride + x0*4, y0*img.Stride + x1*4, y1*img.Stride + x0*4, y1*img.Stride + x1*4}
			weights := [4]float64{(1 - ax) * (1 - ay), ax * (1 - ay), (1 - ax) * ay, ax * ay}
			var sum float64
			var channels [4]float64
			original := y*img.Stride + x*4
			originalPeak := math.Max(float64(src[original]), math.Max(float64(src[original+1]), float64(src[original+2])))
			for i, p := range indices {
				// Keep samples on the same hot metal, so dark background cannot
				// cut holes in a letter or make its outline crawl. Dark edge
				// shading is hot-coloured too, so reject samples substantially
				// darker than the original pixel to avoid black bands.
				peak := math.Max(float64(src[p]), math.Max(float64(src[p+1]), float64(src[p+2])))
				if hotOnly && (hot(p) == 0 || peak < originalPeak*0.72) {
					continue
				}
				sum += weights[i]
				for ch := 0; ch < 4; ch++ {
					channels[ch] += float64(src[p+ch]) * weights[i]
				}
			}
			if sum > 0 {
				p := y*img.Stride + x*4
				for ch := 0; ch < 4; ch++ {
					img.Pix[p+ch] = uint8(math.Round(channels[ch] / sum))
				}
			}
		}
	}
}

func heatFBM(x, y float64) float64 {
	hash := func(x, y float64) float64 { n := math.Sin(x*127.1+y*311.7) * 43758.5453123; return n - math.Floor(n) }
	var value float64
	amp := 0.5
	for k := 0; k < 4; k++ {
		ix, iy := math.Floor(x), math.Floor(y)
		fx, fy := x-ix, y-iy
		fx = fx * fx * (3 - 2*fx)
		fy = fy * fy * (3 - 2*fy)
		a := hash(ix, iy)*(1-fx) + hash(ix+1, iy)*fx
		b := hash(ix, iy+1)*(1-fx) + hash(ix+1, iy+1)*fx
		value += amp * (a*(1-fy) + b*fy)
		x *= 2.03
		y *= 2.03
		amp *= 0.5
	}
	return value
}
