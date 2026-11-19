package menu

import (
	"image"
	"image/color"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// matrixTextCube projects the actual BAR choices onto all six faces of an
// independently rotating cube. The selection is part of the texture, and
// the far-side lettering is softly visible through the glass.
// matrixClearGlassFilm is how much of a face's own shading a tinted cube
// keeps. The rest of what is seen on a face is the particles inside, the
// lettering and the far faces showing through.
const matrixClearGlassFilm = .14

type matrixCubeEffects struct {
	magnet, weld, activate float64
	// tint makes the cube clear glass of this colour in place of amber
	// glass; a zero alpha leaves it amber.
	tint color.NRGBA
	// glitter swirls the particles round the cube's axis as flakes that
	// flash white, in place of embers rising in a funnel.
	glitter bool
	// pixelAspect is the height of a screen pixel over its width; zero
	// means square pixels.
	pixelAspect float64
}

func matrixTextCube(elapsed float64, options []LightbarOption, selected int, extras ...matrixCubeEffects) *image.NRGBA {
	var fx matrixCubeEffects
	if len(extras) > 0 {
		fx = extras[0]
	}
	const w, h = 320, 240
	// across and rise scale the projection in pixel columns and rows so the
	// cube is as tall as it is wide to the eye. Pixels taller than they are
	// wide shorten it in rows; pixels wider than they are tall narrow it in
	// columns, so it never grows past the canvas.
	across, rise := 1., 1.
	if fx.pixelAspect > 1 {
		rise = 1 / fx.pixelAspect
	} else if fx.pixelAspect > 0 {
		across = fx.pixelAspect
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	const tw, th = 336, 152
	parsed, _ := opentype.Parse(gobold.TTF)
	typeface, _ := opentype.NewFace(parsed, &opentype.FaceOptions{Size: 20, DPI: 72, Hinting: font.HintingFull})
	defer func() { _ = typeface.Close() }()
	tex := image.NewRGBA(image.Rect(0, 0, tw, th))
	for i, opt := range options {
		if i >= 4 {
			break
		}
		text := "  " + opt.Text + "  "
		if i == selected {
			text = "> " + opt.Text + " <"
		}
		runes := []rune(text)
		for len(runes) > 0 && font.MeasureString(typeface, string(runes)).Ceil() > tw-8 {
			runes = runes[:len(runes)-1]
		}
		start := max(2, (tw-font.MeasureString(typeface, string(runes)).Ceil())/2)
		d := font.Drawer{Dst: tex, Face: typeface, Dot: fixed.P(start, 30+i*36)}
		sweep := math.Mod(elapsed*5+float64(i)*2, float64(len(runes)+10)) - 5
		for j, r := range runes {
			glow := math.Max(0, 1-math.Abs(float64(j)-sweep)/5)
			base := 95.
			if i == selected {
				base = 175
			}
			v := uint8(math.Round(base + (255-base)*glow))
			// Dark orange lettering heats up as the highlight moves across it.
			ink := color.RGBA{
				uint8(220 + 35*glow),
				uint8(105 + 65*glow),
				uint8(22 + 33*glow), 255,
			}
			if i == selected {
				ink = color.RGBA{255, uint8(225 + float64(v)*.11), uint8(175 + float64(v)*.31), 255}
			}
			d.Src = image.NewUniform(ink)
			d.DrawString(string(r))
		}
	}
	// Use a slow, continuous turn: each of the six faces carries the same
	// choices, so a fresh face comes into view as the previous one turns away.
	yaw := elapsed*.34 + .18*math.Sin(elapsed*.19)
	if fx.activate > 0 {
		target := math.Round(yaw/(math.Pi/2)) * (math.Pi / 2)
		yaw += (target - yaw) * math.Min(1, fx.activate)
	}
	pitch := .16 + .22*math.Sin(elapsed*.21)
	roll := .12*math.Sin(elapsed*.17) + .06*math.Sin(elapsed*.43)
	cs, sn := math.Cos(yaw), math.Sin(yaw)
	ct, st := math.Cos(pitch), math.Sin(pitch)
	cr, sr := math.Cos(roll), math.Sin(roll)
	type point struct{ x, y, z, q float64 }
	pts := [8]point{}
	for i := range pts {
		x := float64((i&1)*2 - 1)
		y := float64(((i>>1)&1)*2 - 1)
		z := float64(((i>>2)&1)*2 - 1)
		rx := x*cs + z*sn
		rz := z*cs - x*sn
		ry := y*ct - rz*st
		rz = y*st + rz*ct
		rx, ry = rx*cr-ry*sr, rx*sr+ry*cr
		q := 1 / (4.8 + rz)
		pts[i] = point{w/2 + rx*q*3.8*72.25*across, 120 - ry*q*3.8*72.25*rise, rz, q}
	}
	depth := make([]float64, w*h)
	for i := range depth {
		depth[i] = math.Inf(1)
	}
	type face struct {
		v        [4]int
		textured bool
	}
	faces := []face{{[4]int{2, 3, 1, 0}, true}, {[4]int{3, 7, 5, 1}, true}, {[4]int{7, 6, 4, 5}, true}, {[4]int{6, 2, 0, 4}, true}, {[4]int{6, 7, 3, 2}, true}, {[4]int{0, 1, 5, 4}, true}}
	type uv struct{ u, v float64 }
	coords := [4]uv{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	cross := func(a, b point, x, y float64) float64 { return (x-a.x)*(b.y-a.y) - (y-a.y)*(b.x-a.x) }
	triangle := func(a, b, c point, ta, tb, tc uv, textured, rear bool) {
		area := cross(a, b, c.x, c.y)
		if math.Abs(area) < .01 {
			return
		}
		minx := max(0, int(math.Floor(math.Min(a.x, math.Min(b.x, c.x)))))
		maxx := min(w-1, int(math.Ceil(math.Max(a.x, math.Max(b.x, c.x)))))
		miny := max(0, int(math.Floor(math.Min(a.y, math.Min(b.y, c.y)))))
		maxy := min(h-1, int(math.Ceil(math.Max(a.y, math.Max(b.y, c.y)))))
		for y := miny; y <= maxy; y++ {
			for x := minx; x <= maxx; x++ {
				px, py := float64(x)+.5, float64(y)+.5
				wa, wb, wc := cross(b, c, px, py)/area, cross(c, a, px, py)/area, cross(a, b, px, py)/area
				if wa < 0 || wb < 0 || wc < 0 {
					continue
				}
				inv := wa*a.q + wb*b.q + wc*c.q
				if inv <= 0 {
					continue
				}
				distance := 1 / inv
				idx := y*w + x
				if rear {
					if !textured || distance <= depth[idx]+.05 {
						continue
					}
				} else {
					if distance >= depth[idx] {
						continue
					}
					depth[idx] = distance
				}
				u := (wa*ta.u*a.q + wb*tb.u*b.q + wc*tc.u*c.q) / inv
				v := (wa*ta.v*a.q + wb*tb.v*b.q + wc*tc.v*c.q) / inv
				if rear {
					if u < 0 || u > 1 || v < 0 || v > 1 {
						continue
					}
					tx, ty := min(tw-1, int(u*tw)), min(th-1, int(v*th))
					sample := tex.RGBAAt(tx, ty)
					if sample.A != 0 {
						old := img.NRGBAAt(x, y)
						// Rear engraving is deliberately subdued, retaining contrast
						// with the front face's full-brightness lettering.
						through := .23
						if fx.tint.A != 0 {
							// Clear glass hides less of the far face, but its
							// lettering must stay well below the near face's.
							through = .3
						}
						alpha := float64(sample.A) / 255 * through
						if old.R < 150 {
							img.SetNRGBA(x, y, color.NRGBA{uint8(float64(old.R)*(1-alpha) + float64(sample.R)*through), uint8(float64(old.G)*(1-alpha) + float64(sample.G)*through*.78), uint8(float64(old.B)*(1-alpha) + float64(sample.B)*through*.52), 255})
						}
					}
					continue
				}
				// Translucent amber glass: dark interior, soft molten currents and
				// narrow reflections moving over the turning faces.
				reflection := v + .22*u + .12*math.Sin(elapsed*.34)
				band := math.Exp(-square((reflection - .28) / .11))
				glint := math.Exp(-square((reflection - .69) / .035))
				flow := .5 + .5*math.Sin(u*9+v*5-elapsed*.8)*math.Sin(v*8-u*4+elapsed*.45)
				if fx.tint.A != 0 {
					// Clear glass: no reflections sweep its faces.
					band, glint = 0, 0
				}
				glow := 12 + 26*band + 34*glint + 24*flow
				// Soft light cast across the glass around the selected row.
				selectedV := (float64(selected)*36 + 22) / 152
				bloom := math.Exp(-square((v-selectedV)/.12)) * (.6 + .12*math.Sin(elapsed*2))
				glow += 18*bloom + 35*bloom*fx.activate
				// The hot letters sit above and to the left of the cube. Their
				// flickering light falls off across its surface, rather than
				// moving with the cube's texture.
				distanceToLogo := math.Hypot((px+45)/190, (py+65)/190)
				light := math.Max(0, 1-distanceToLogo/2.7)
				light *= .88 + .08*math.Sin(elapsed*3.1) + .04*math.Sin(elapsed*7.3)
				c0 := color.NRGBA{
					uint8(math.Min(255, glow*1.6+100*light)),
					uint8(math.Min(255, glow*.60+48*light)),
					uint8(math.Min(255, glow*.16+8*light)), 255,
				}
				if fx.tint.A != 0 {
					// Clear glass: the face itself is only a faint film, so what
					// is inside the cube and behind it shows through.
					c0 = color.NRGBA{uint8(float64(c0.R) * matrixClearGlassFilm), uint8(float64(c0.G) * matrixClearGlassFilm), uint8(float64(c0.B) * matrixClearGlassFilm), 255}
				}
				if textured && u >= 0 && u <= 1 && v >= 0 && v <= 1 {
					tx := min(tw-1, int(u*tw))
					ty := min(th-1, int(v*th))
					sample := tex.RGBAAt(tx, ty)
					// The selected row's band in the texture. The lettering's
					// baseline is at 30 in each 36-row step, so its lowercase
					// body runs from about 16 to 30 with tails to 35. A tinted
					// cube draws a bar here, so its band is set evenly about
					// the lettering; the amber cube keeps its band.
					bandTop, bandBottom := selected*36+5, selected*36+34
					if fx.tint.A != 0 {
						bandTop, bandBottom = selected*36+9, selected*36+38
					}
					onSelectedRow := ty >= bandTop && ty < bandBottom
					if fx.tint.A != 0 && onSelectedRow && u > .03 && u < .97 {
						// Clear glass gives the selected choice nothing to stand
						// against, so a bar of the tint is laid behind it, with a
						// brighter rule along its top and bottom.
						bar := 74.
						if ty < bandTop+2 || ty >= bandBottom-2 {
							bar = 150
						}
						c0.R = uint8(math.Min(255, float64(c0.R)+bar))
					}
					if sample.A == 0 && onSelectedRow {
						// A tight amber halo hugs the selected engraved letters.
						var halo uint8
						for dy := -2; dy <= 2; dy++ {
							for dx := -2; dx <= 2; dx++ {
								if dx*dx+dy*dy > 4 {
									continue
								}
								p := image.Pt(tx+dx, ty+dy)
								if p.In(tex.Bounds()) {
									halo = max(halo, tex.RGBAAt(p.X, p.Y).A)
								}
							}
						}
						strength := float64(halo) / 255 * (.72 + .12*math.Sin(elapsed*4))
						c0.R = uint8(math.Min(255, float64(c0.R)+105*strength))
						c0.G = uint8(math.Min(255, float64(c0.G)+40*strength))
						c0.B = uint8(math.Min(255, float64(c0.B)+5*strength))
					}
					if sample.A == 0 && !onSelectedRow {
						// A fine dark outline separates lettering from bright glass.
						var edge uint8
						for _, d := range []image.Point{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
							p := image.Pt(tx+d.X, ty+d.Y)
							if p.In(tex.Bounds()) {
								edge = max(edge, tex.RGBAAt(p.X, p.Y).A)
							}
						}
						shade := 1 - .72*float64(edge)/255
						c0.R = uint8(float64(c0.R) * shade)
						c0.G = uint8(float64(c0.G) * shade)
						c0.B = uint8(float64(c0.B) * shade)
					}
					if sample.A != 0 {
						// Keep the antialiased etched choices legible through the glow.
						alpha := float64(sample.A) / 255
						ink := 1.
						if fx.tint.A != 0 && !onSelectedRow {
							// The other choices step back so the selected one,
							// drawn white, is the brightest thing on the face.
							ink = .62
						}
						c0 = color.NRGBA{
							uint8(math.Min(255, float64(c0.R)*(1-alpha)+float64(sample.R)*ink)),
							uint8(math.Min(255, float64(c0.G)*(1-alpha)+float64(sample.G)*ink)),
							uint8(math.Min(255, float64(c0.B)*(1-alpha)+float64(sample.B)*.92*ink)), 255,
						}
					}
				}
				img.SetNRGBA(x, y, c0)
			}
		}
	}
	for _, f := range faces {
		triangle(pts[f.v[0]], pts[f.v[1]], pts[f.v[2]], coords[0], coords[1], coords[2], f.textured, false)
		triangle(pts[f.v[0]], pts[f.v[2]], pts[f.v[3]], coords[0], coords[2], coords[3], f.textured, false)
	}
	// Floating embers occupy positions inside the three-dimensional volume.
	// Project them through the same camera as the glass and keep them dim
	// enough that the lettering remains the dominant element.
	for i := 0; i < 448; i++ {
		// Stable per-particle seeds give each ember its own path without
		// introducing frame-to-frame random jumps.
		seed := func(n int) float64 { v := math.Sin(float64(i*31+n*79)*12.9898) * 43758.5453; return v - math.Floor(v) }
		if fx.glitter {
			// Glitter: every particle circles the cube's upright axis, the
			// inner ones fastest, bobbing a little, and is a faint speck
			// that flashes white for an instant as it catches the light.
			radius := (.10 + .74*math.Sqrt(seed(5))) * (.92 + .08*math.Sin(elapsed*.4+seed(6)*6.28))
			turn := .45 + 1.5*(1-radius)
			if seed(8) < .18 {
				turn = -.6 * turn // a few drift against the swirl
			}
			angle := seed(7)*6.28 + elapsed*turn
			y := -.80 + 1.60*seed(3) + .07*math.Sin(elapsed*(.3+.5*seed(4))+seed(1)*6.28)
			x, z := radius*math.Cos(angle), radius*math.Sin(angle)
			rx, rz := x*cs+z*sn, z*cs-x*sn
			ry := y*ct - rz*st
			rz = y*st + rz*ct
			rx, ry = rx*cr-ry*sr, rx*sr+ry*cr
			q := 1 / (4.8 + rz)
			px, py := int(math.Round(w/2+rx*q*3.8*72.25*across)), int(math.Round(120-ry*q*3.8*72.25*rise))
			if !image.Pt(px, py).In(img.Bounds()) || math.IsInf(depth[py*w+px], 1) {
				continue
			}
			flash := math.Pow(math.Max(0, math.Sin(elapsed*(2.2+5.5*seed(2))+seed(1)*6.28)), 7)
			// Nearer specks are a little brighter.
			near := 1.15 - .3*(rz+1)/2
			bright := (34 + 221*flash) * near
			spark := func(x, y int, amount float64) {
				p := image.Pt(x, y)
				if !p.In(img.Bounds()) || math.IsInf(depth[p.Y*w+p.X], 1) {
					return
				}
				old := img.NRGBAAt(p.X, p.Y)
				if old.R >= 180 && old.G >= 75 {
					return // leave the lettering alone
				}
				// Equal parts of every channel come out white; the red
				// part alone takes the cube's colour.
				add, white := bright*amount, bright*amount*flash
				img.SetNRGBA(p.X, p.Y, color.NRGBA{uint8(math.Min(255, float64(old.R)+add)), uint8(math.Min(255, float64(old.G)+white)), uint8(math.Min(255, float64(old.B)+white)), 255})
			}
			spark(px, py, 1)
			// The larger flakes throw a small cross of light at the peak.
			if seed(13) > .72 && flash > .45 {
				for _, d := range []image.Point{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
					spark(px+d.X, py+d.Y, .55)
				}
				if flash > .8 {
					for _, d := range []image.Point{{-2, 0}, {2, 0}, {0, -2}, {0, 2}} {
						spark(px+d.X, py+d.Y, .28)
					}
				}
			}
			continue
		}
		phase := seed(1)*6.28 + elapsed*(.7+seed(2)*2.1)
		// The broad rising funnel counter-rotates against the cube. Smaller
		// eddies make individual particles spiral and drift independently.
		height := math.Mod(seed(3)+elapsed*(.045+seed(4)*.09), 1)
		y := -.76 + 1.52*height + .04*math.Sin(phase*.7)
		// A selection briefly draws nearby particles toward its row.
		targetY := .55 - float64(selected)*.36
		attraction := fx.magnet * math.Exp(-square((y-targetY)/.5)) * (.3 + .4*seed(14))
		y += (targetY - y) * attraction
		radius := (.25 + .49*height) * (.65 + .35*seed(5))
		radius *= .8 + .2*math.Sin(phase*.43+seed(6)*6.28)
		angle := seed(7)*6.28 + yaw + elapsed*(.48+seed(8)*1.15) + height*(2+seed(9)*5) + .5*math.Sin(phase*.55)
		x := radius * math.Cos(angle)
		z := radius * math.Sin(angle)
		rx, rz := x*cs+z*sn, z*cs-x*sn
		ry := y*ct - rz*st
		rz = y*st + rz*ct
		rx, ry = rx*cr-ry*sr, rx*sr+ry*cr
		q := 1 / (4.8 + rz)
		px, py := int(math.Round(w/2+rx*q*3.8*72.25*across)), int(math.Round(120-ry*q*3.8*72.25*rise))
		if !image.Pt(px, py).In(img.Bounds()) || math.IsInf(depth[py*w+px], 1) {
			continue
		}
		old := img.NRGBAAt(px, py)
		if old.R >= 180 && old.G >= 75 {
			continue
		}
		flicker := (.4 + .6*math.Sin(phase)*math.Sin(phase)) * math.Min(1, math.Min(height, 1-height)*10)
		bright := (75 + 120*seed(10)) * flicker
		// Fresh particles start yellow-white, cooling toward red as they rise.
		heat := 1 - height
		green, blue := bright*(.18+.65*heat), bright*.40*heat*heat
		if fx.tint.A != 0 {
			// In a tinted cube a particle at its hottest is white and cools
			// to the tint: equal parts of every channel here come out white
			// once the cube is recoloured below.
			white := math.Min(1, heat*1.25)
			green, blue = bright*white, bright*white
		}
		img.SetNRGBA(px, py, color.NRGBA{uint8(math.Min(255, float64(old.R)+bright)), uint8(math.Min(255, float64(old.G)+green)), uint8(math.Min(255, float64(old.B)+blue)), 255})
		if seed(13) > .88 && flicker > .5 {
			p := image.Pt(px+1, py)
			if p.In(img.Bounds()) && !math.IsInf(depth[p.Y*w+p.X], 1) {
				c := img.NRGBAAt(p.X, p.Y)
				if c.R < 150 {
					img.SetNRGBA(p.X, p.Y, color.NRGBA{uint8(math.Min(255, float64(c.R)+bright*.3)), uint8(math.Min(255, float64(c.G)+bright*.12)), c.B, 255})
				}
			}
		}
	}
	// A second pass projects the hidden face through the nearest surface.
	for _, f := range faces {
		if !f.textured {
			continue
		}
		triangle(pts[f.v[0]], pts[f.v[1]], pts[f.v[2]], coords[0], coords[1], coords[2], true, true)
		triangle(pts[f.v[0]], pts[f.v[2]], pts[f.v[3]], coords[0], coords[2], coords[3], true, true)
	}
	// Dim rear edges remain visible through the glass; the nearer edges glow.
	edges := [][2]int{{0, 1}, {2, 3}, {4, 5}, {6, 7}, {0, 2}, {1, 3}, {4, 6}, {5, 7}, {0, 4}, {1, 5}, {2, 6}, {3, 7}}
	for edgeIndex, e := range edges {
		a, b := pts[e[0]], pts[e[1]]
		n := max(1, int(math.Ceil(math.Max(math.Abs(b.x-a.x), math.Abs(b.y-a.y)))))
		for i := 0; i <= n; i++ {
			u := float64(i) / float64(n)
			x := int(math.Round(a.x + (b.x-a.x)*u))
			y := int(math.Round(a.y + (b.y-a.y)*u))
			if !image.Pt(x, y).In(img.Bounds()) {
				continue
			}
			z := 1 / ((1-u)*a.q + u*b.q)
			if z <= depth[y*w+x]+.04 {
				edgeColor := color.NRGBA{255, 169, 64, 255}
				if fx.weld > 0 && x > w/2 {
					amount := math.Min(1, fx.weld*.8)
					edgeColor = color.NRGBA{uint8(255 - 55*amount), uint8(169 + 70*amount), uint8(64 + 191*amount), 255}
				}
				// A short glint travels over one edge, with quiet gaps between.
				cycle := math.Mod(elapsed, 4.7)
				if cycle < 1.5 && edgeIndex == int(elapsed/4.7)%len(edges) {
					glint := math.Max(0, 1-math.Abs(u-cycle/1.5)*18)
					edgeColor.G = uint8(169 + 86*glint)
					edgeColor.B = uint8(64 + 191*glint)
				}
				img.SetNRGBA(x, y, edgeColor)
			} else {
				old := img.NRGBAAt(x, y)
				// Avoid obscuring the bright engraved text with a rear edge.
				if old.R < 150 {
					img.SetNRGBA(x, y, color.NRGBA{uint8(math.Min(255, float64(old.R)+52)), uint8(math.Min(255, float64(old.G)+24)), uint8(math.Min(255, float64(old.B)+5)), 255})
				}
			}
		}
	}
	// A sharp specular star blooms briefly on the exposed polished rim.
	cycle := math.Mod(elapsed, 4.7)
	if cycle < 1.5 {
		e := edges[int(elapsed/4.7)%len(edges)]
		a, b := pts[e[0]], pts[e[1]]
		u := cycle / 1.5
		x, y := int(math.Round(a.x+(b.x-a.x)*u)), int(math.Round(a.y+(b.y-a.y)*u))
		z := 1 / ((1-u)*a.q + u*b.q)
		if image.Pt(x, y).In(img.Bounds()) && z <= depth[y*w+x]+.04 {
			intensity := math.Min(1, 1.45*math.Sin(math.Pi*u))
			for _, d := range []image.Point{{0, 0}, {-1, 0}, {1, 0}, {0, -1}, {0, 1}, {-2, 0}, {2, 0}, {0, -2}, {0, 2}, {-3, 0}, {3, 0}, {0, -3}, {0, 3}, {-4, 0}, {4, 0}, {0, -4}, {0, 4}} {
				p := image.Pt(x+d.X, y+d.Y)
				if !p.In(img.Bounds()) {
					continue
				}
				strength := intensity
				if d.X != 0 || d.Y != 0 {
					strength *= .85
				}
				old := img.NRGBAAt(p.X, p.Y)
				img.SetNRGBA(p.X, p.Y, color.NRGBA{uint8(float64(old.R)*(1-strength) + 255*strength), uint8(float64(old.G)*(1-strength) + 255*strength), uint8(float64(old.B)*(1-strength) + 255*strength), 255})
			}
		}
	}
	if fx.tint.A != 0 {
		// Every amber shade becomes the same brightness of the tint, and
		// near-white stays white.
		for p := 0; p < len(img.Pix); p += 4 {
			r, g, b := float64(img.Pix[p]), float64(img.Pix[p+1]), float64(img.Pix[p+2])
			hi, lo := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
			if hi == 0 {
				continue
			}
			white := lo / hi
			level := hi / 255
			mix := func(light uint8) uint8 {
				return uint8(math.Min(255, level*(float64(light)*(1-white)+255*white)))
			}
			img.Pix[p], img.Pix[p+1], img.Pix[p+2] = mix(fx.tint.R), mix(fx.tint.G), mix(fx.tint.B)
		}
	}
	return img
}

// square is x times x.
func square(x float64) float64 { return x * x }
