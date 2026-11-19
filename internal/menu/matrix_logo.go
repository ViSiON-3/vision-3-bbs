package menu

import (
	"image"
	"image/color"
	"math"

	"github.com/ViSiON-3/vision-3-bbs/internal/scripting"
)

// The matrix logo burns the way the ViSiON/3 theme's main menu logo does:
// this file ports findEmberSources, emberSource, paintEmbers, paintLight,
// paintWeld and the heat call from menus/vision3/scripts/mainmenu.js with
// the same routine and the same numbers.
//
// The script's numbers are for its 480x160 logo. On another picture
// positions are scaled by the picture's width over 480 (x) and height over
// 160 (y), and distances (rise, sway, drift, radii, the weld star and its
// spatter) by the smaller of the two, so the effects keep their shape. At
// 480x160 both factors are 1 and the numbers are the script's.

// matrixLogoBurn holds the hot pixels embers start from and the scale.
type matrixLogoBurn struct {
	sources []image.Point
	// reach is each source's running share of the heat, as emberReach.
	reach []float64
	// sx and sy scale positions, k distances.
	sx, sy, k float64
}

// newMatrixLogoBurn finds the hot letter pixels of img, as findEmberSources.
func newMatrixLogoBurn(img *image.NRGBA) *matrixLogoBurn {
	b := img.Bounds()
	burn := &matrixLogoBurn{sx: float64(b.Dx()) / 480, sy: float64(b.Dy()) / 160}
	burn.k = math.Min(burn.sx, burn.sy)
	seen := map[image.Point]bool{}
	for uy := 12.0; uy < 150; uy += 4 {
		for ux := 4.0; ux < 300; ux += 4 {
			p := image.Pt(b.Min.X+int(math.Round(ux*burn.sx)), b.Min.Y+int(math.Round(uy*burn.sy)))
			if seen[p] || !p.In(b) {
				continue
			}
			seen[p] = true
			c := img.NRGBAAt(p.X, p.Y)
			if c.R > 150 && c.G > 55 && float64(c.R) > float64(c.B)*1.5 {
				burn.sources = append(burn.sources, p)
			}
		}
	}
	// The metal is hottest at the far-left V and cools along the word, so
	// embers come off thickest there and thin out to the right.
	left := math.Inf(1)
	for _, p := range burn.sources {
		left = math.Min(left, float64(p.X))
	}
	total := 0.0
	for _, p := range burn.sources {
		total += math.Exp(-(float64(p.X) - left) / (65 * burn.sx))
		burn.reach = append(burn.reach, total)
	}
	return burn
}

// source is the source that share u (0 to 1) of the heat reaches, as
// emberSource.
func (b *matrixLogoBurn) source(u float64) image.Point {
	want := u * b.reach[len(b.reach)-1]
	lo, hi := 0, len(b.reach)-1
	for lo < hi {
		mid := (lo + hi) >> 1
		if b.reach[mid] < want {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return b.sources[lo]
}

// paint draws one logo frame's heat, embers and weld burst on img, which
// holds the picture, as the main menu's logo loop does. Pixels the weld
// burst and spatter change are also set on weld, which the centred layout
// draws above the cube. It returns the weld arc's power, 0 when none is burning.
func (b *matrixLogoBurn) paint(img, weld *image.NRGBA, elapsed float64) float64 {
	scripting.HeatCanvas(img, elapsed, 0.04, true)
	b.paintEmbers(img, elapsed)
	tick := int(math.Floor(elapsed / 0.16))
	step := tick%48 - 8
	if step >= 0 && step <= 14 {
		return b.paintWeld(img, weld, tick, step)
	}
	return 0
}

// addLight adds a light colour to the pixel at x, y when it is on img, and
// copies the result to layer when layer is not nil and the pixel changed.
func addLight(img, layer *image.NRGBA, x, y int, r, g, bl float64) {
	if !image.Pt(x, y).In(img.Bounds()) {
		return
	}
	old := img.NRGBAAt(x, y)
	c := color.NRGBA{
		R: uint8(math.Min(255, math.Round(float64(old.R)+r))),
		G: uint8(math.Min(255, math.Round(float64(old.G)+g))),
		B: uint8(math.Min(255, math.Round(float64(old.B)+bl))),
		A: 255,
	}
	img.SetNRGBA(x, y, c)
	if layer != nil && c != old {
		layer.SetNRGBA(x, y, c)
	}
}

// paintLight lights only visible metal round x, y, leaving the black
// background untouched, as the script's paintLight. radius is in pixels.
func paintLight(img, layer *image.NRGBA, x, y int, radius, red, green, blue, intensity float64) {
	n := int(math.Ceil(radius))
	for dy := -n; dy <= n; dy++ {
		for dx := -n; dx <= n; dx++ {
			distance := math.Sqrt(float64(dx*dx+dy*dy)) / (radius + 1)
			if distance >= 1 || !image.Pt(x+dx, y+dy).In(img.Bounds()) {
				continue
			}
			old := img.NRGBAAt(x+dx, y+dy)
			if max(old.R, old.G, old.B) < 28 {
				continue
			}
			light := intensity * (1 - distance) * (1 - distance)
			addLight(img, layer, x+dx, y+dy, red*light, green*light, blue*light)
		}
	}
}

// matrixEmberRandom is the script's Math.sin based random.
func matrixEmberRandom(n float64) float64 {
	v := math.Sin(n*127.1+311.7) * 43758.5453123
	return v - math.Floor(v)
}

// matrixEmberRise is how far, in the script's logo pixels, embers of each
// kind can rise on the convection: the climb goes to 1 and the life to 1.
var matrixEmberRise = [3]float64{24, 88, 120}

// paintEmbers is the script's paintEmbers: 64 embers, each with its own
// stamina, cooling curve and burn-out, rising from hot metal on a slow
// shared current.
func (b *matrixLogoBurn) paintEmbers(img *image.NRGBA, time float64) {
	if len(b.sources) == 0 {
		return
	}
	k, o := b.k, img.Bounds().Min
	random := matrixEmberRandom
	// The current at a point: -1 pushes left, 1 right.
	current := func(x, y float64) float64 {
		return math.Sin(x*0.021 + math.Sin(y*0.047-time*0.7)*1.6 + time*0.33)
	}
	for i := 0; i < 64; i++ {
		seed := float64(i*7 + 1)
		phase, kind := random(seed), random(seed+1)
		rate, accel, rise, radius := 0.08, 1.7, matrixEmberRise[2], 3.0
		switch {
		case kind < 0.45:
			rate, accel, rise, radius = 0.23, 4.0, matrixEmberRise[0], 1
		case kind < 0.86:
			rate, rise, radius = 0.13, matrixEmberRise[1], 2
		}
		stamina := 0.3 + 0.7*math.Pow(random(seed+5), 0.8)
		burn := math.Mod(time*rate/stamina+phase, 1)
		life := burn * stamina
		climb := 1 - math.Exp(-accel*life)
		// Each relight picks a new source, weighted to the hottest metal.
		source := b.source(random(seed + 3 + math.Floor(time*rate/stamina+phase)*17.3))
		sx, sy := float64(source.X), float64(source.Y)
		y := math.Round(sy - climb*rise*k - life*8*k)
		sway := math.Sin(life*6.28+phase*6.28) * (2 + 6*climb) * k
		// The current and the gusts are sampled where the ember would be on
		// the script's 480x160 logo.
		ux := (sx - float64(o.X)) / b.sx
		uy := (sy-float64(o.Y))/b.sy - climb*rise - life*8
		drift := current(ux, math.Round(uy))
		x := math.Round(sx + sway + drift*(4+22*climb)*k)
		fade := math.Min(1, burn/0.05) * math.Min(1, (1-burn)/(0.05+0.3*random(seed+8)))
		temp := math.Pow(1-burn, 0.6+1.6*random(seed+6))
		gust := 0.5 + 0.5*math.Sin(time*(2.1+3.7*random(seed+7))+phase*40+math.Round(uy)*0.09)
		draft := 1 - math.Abs(drift)
		intensity := fade * (0.6 + 0.4*temp) * (0.65 + 0.8*(0.6*gust+0.4*draft))
		red := math.Round(150 + 105*math.Min(1, temp*3))
		green := math.Round(45 + 177*temp)
		blue := math.Round(8 + 135*temp*temp)
		px, py := int(x), int(y)
		if i%2 == 0 && intensity > 0.12 {
			paintLight(img, nil, px, py, 4*k, 105, 35, 4, intensity*0.55)
		}
		r := radius * k
		n := int(math.Floor(r + 0.4))
		for dy := -n; dy <= n; dy++ {
			for dx := -n; dx <= n; dx++ {
				d := math.Sqrt(float64(dx*dx+dy*dy)) / (r + 0.4)
				if d >= 1 {
					continue
				}
				light := intensity * (1 - d) * (1 - d)
				addLight(img, nil, px+dx, py+dy, red*light, green*light, blue*light)
			}
		}
	}
}

// matrixWeldSpots are the script's strike centres on the finished N, slash
// and 3 of the 480x160 logo.
var matrixWeldSpots = [...][2]float64{{304, 20}, {327, 56}, {355, 20}, {401, 52}, {425, 96}, {449, 112}, {450, 137}, {461, 74}}

// paintWeld is the script's paintWeld: a brief tack-weld arc, a cropped
// uneven star, and spatter that keeps moving after the arc goes out. It
// returns the arc's power.
func (b *matrixLogoBurn) paintWeld(img, weld *image.NRGBA, tick, step int) float64 {
	strengths := [...]float64{1, 0.12, 0.9, 0.15, 0.65}
	power := 0.0
	if step < len(strengths) {
		power = strengths[step]
	}
	k := b.k
	strike := tick / 48
	spot := matrixWeldSpots[strike%len(matrixWeldSpots)]
	o := img.Bounds().Min
	cx := o.X + int(math.Round(spot[0]*b.sx))
	cy := o.Y + int(math.Round(spot[1]*b.sy))
	angle := float64(strike) * 1.71
	cut := 1.0
	if strike%2 == 1 {
		cut = -1
	}
	// The arc lights the logo's metal; only the star and the spatter go on
	// the weld layer, which is drawn over the cube.
	if power > 0 {
		paintLight(img, nil, cx, cy, 16*k, 65, 105, 175, power*0.65)
	}
	// The star is laid out in the script's logo pixels (u) and drawn at
	// the picture's scale.
	nx, ny := int(math.Ceil(15*k)), int(math.Ceil(11*k))
	for y := -ny; y <= ny; y++ {
		for x := -nx; x <= nx; x++ {
			ux, uy := float64(x)/k, float64(y)/k
			distance := math.Sqrt(ux*ux + uy*uy*1.9)
			if distance > 14 {
				continue
			}
			theta := math.Atan2(uy, ux) - angle
			core := math.Max(0, 1-distance/3.2)
			spike := math.Pow(math.Max(0, math.Cos(theta*4)), 20) * math.Max(0, 1-distance/14)
			occlusion := 1.0
			if ux*cut < -3 {
				occlusion = 0.3
			}
			light := power * occlusion * math.Min(1, core+spike*0.65)
			if light < 0.025 {
				continue
			}
			addLight(img, weld, cx+x, cy+y, 205*light, 225*light, 255*light)
		}
	}
	// Each spatter particle has its own launch time, velocity, cooling and
	// short fading motion trail.
	for i := 0; i < 20; i++ {
		age := float64(step - i%4)
		if age < 0 || age > 10 {
			continue
		}
		spread := float64(i%7-3) * 0.24
		vx := cut*(1.7+float64(i%5)*0.55) + spread
		vy := -1.7 + float64(i%6)*0.57
		for trail := 2; trail >= 0; trail-- {
			t := age - float64(trail)*0.65
			if t < 0 {
				continue
			}
			sx := int(math.Round(float64(cx) + vx*t*k))
			sy := int(math.Round(float64(cy) + (vy*t+0.22*t*t)*k))
			glow := math.Max(0, 1-age/11) * (1 - float64(trail)*0.25)
			addLight(img, weld, sx, sy, 255*glow, (65+145*glow)*glow, 50*glow*glow)
		}
	}
	return power
}
