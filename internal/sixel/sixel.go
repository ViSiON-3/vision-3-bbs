// Package sixel encodes images as DEC sixel graphics, the inline picture
// format SyncTERM, xterm and others draw at the cursor.
package sixel

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"strconv"
)

// maxColors is the number of colour registers an image is encoded with.
// Terminals that draw sixel take at least this many.
const maxColors = 256

// Encode returns img as a sixel sequence, from the DCS introducer to the
// string terminator, ready to write to a terminal with the cursor at the
// picture's top-left cell. An image of more than 256 colours is reduced to
// a fixed palette with error diffusion. Pixels less than half opaque are
// left undrawn, so what is already on the screen shows through. An empty
// image encodes to nil.
func Encode(img image.Image) []byte {
	if img.Bounds().Empty() {
		return nil
	}
	pal, px := indexed(img)
	return encode(img.Bounds(), pal, px, true)
}

// EncodePalette is Encode with the colour registers given: pal[n] is
// register n, and each pixel takes the register of its colour, or of the
// nearest one when pal lacks it. Only the first 256 colours of pal are used.
//
// Some terminals, SyncTERM among them, keep one set of registers for the
// whole screen, so a picture that redefines a register recolours what an
// earlier picture drew with it. Pictures drawn over one another, such as
// the frames of an animation, must therefore share a palette.
func EncodePalette(img image.Image, pal color.Palette) []byte {
	return encodePalette(img, pal, true)
}

// EncodePaletteReuse encodes pixels with an already-defined palette. Callers
// must have sent the same registers to the terminal and not changed them.
func EncodePaletteReuse(img image.Image, pal color.Palette) []byte {
	return encodePalette(img, pal, false)
}

// EncodeIndexedReuse encodes palette indexes using previously defined
// registers. A negative index leaves the corresponding pixel unchanged.
func EncodeIndexedReuse(bounds image.Rectangle, pal color.Palette, pixels []int16) []byte {
	if bounds.Empty() || len(pal) == 0 || len(pal) > maxColors || len(pixels) != bounds.Dx()*bounds.Dy() {
		return nil
	}
	for _, p := range pixels {
		if p < -1 || int(p) >= len(pal) {
			return nil
		}
	}
	return encode(bounds, pal, pixels, false)
}

func encodePalette(img image.Image, pal color.Palette, define bool) []byte {
	b := img.Bounds()
	if b.Empty() || len(pal) == 0 {
		return nil
	}
	if len(pal) > maxColors {
		pal = pal[:maxColors]
	}
	w, h := b.Dx(), b.Dy()
	px := make([]int16, w*h)
	index := map[color.NRGBA]int16{}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			if c.A < 128 {
				px[y*w+x] = -1
				continue
			}
			c.A = 255
			i, ok := index[c]
			if !ok {
				i = int16(pal.Index(c))
				index[c] = i
			}
			px[y*w+x] = i
		}
	}
	return encode(b, pal, px, define)
}

// Palette returns the colours of img in the order Encode gives them
// registers, for EncodePalette. It is nil when img has more than 256.
func Palette(img image.Image) color.Palette {
	b := img.Bounds()
	seen := map[color.NRGBA]bool{}
	var pal color.Palette
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if c.A < 128 {
				continue
			}
			c.A = 255
			if !seen[c] {
				if len(pal) == maxColors {
					return nil
				}
				seen[c] = true
				pal = append(pal, c)
			}
		}
	}
	return pal
}

// encode writes the sixel sequence for pixels px, palette indexes in row
// order with -1 for a pixel left undrawn.
func encode(b image.Rectangle, pal color.Palette, px []int16, define bool) []byte {
	w, h := b.Dx(), b.Dy()

	var out bytes.Buffer
	// P2=1 leaves unset pixels as they are. On delta updates omit the raster
	// dimensions: SyncTERM otherwise paints the first column of every band
	// as background even when its pixels are transparent. Keep 1:1 aspect.
	out.WriteString("\x1bP0;1;0q\"1;1")
	if define {
		out.WriteByte(';')
		out.WriteString(strconv.Itoa(w))
		out.WriteByte(';')
		out.WriteString(strconv.Itoa(h))
	}
	for i, c := range pal {
		if !define {
			break
		}
		r, g, bl, _ := c.RGBA()
		out.WriteByte('#')
		out.WriteString(strconv.Itoa(i))
		out.WriteString(";2;")
		out.WriteString(strconv.Itoa(int(r) * 100 / 0xffff))
		out.WriteByte(';')
		out.WriteString(strconv.Itoa(int(g) * 100 / 0xffff))
		out.WriteByte(';')
		out.WriteString(strconv.Itoa(int(bl) * 100 / 0xffff))
	}

	row := make([]byte, w)
	for top := 0; top < h; top += 6 {
		// The colours this band of six rows uses, in palette order.
		var used [maxColors]bool
		for y := top; y < top+6 && y < h; y++ {
			for x := 0; x < w; x++ {
				if v := px[y*w+x]; v >= 0 {
					used[v] = true
				}
			}
		}
		first := true
		for c := range pal {
			if !used[c] {
				continue
			}
			firstPass := first
			if !firstPass {
				out.WriteByte('$') // back to the band's left edge
			}
			first = false
			for x := 0; x < w; x++ {
				var bits byte
				for dy := 0; dy < 6 && top+dy < h; dy++ {
					if px[(top+dy)*w+x] == int16(c) {
						bits |= 1 << dy
					}
				}
				row[x] = '?' + bits
			}
			out.WriteByte('#')
			out.WriteString(strconv.Itoa(c))
			writeRuns(&out, row, firstPass)
		}
		if first && !define {
			// SyncTERM retains the previous band's mask until its first pass.
			// Even a fully transparent delta band must reset it completely.
			for x := range row {
				row[x] = '?'
			}
			out.WriteString("#0")
			writeRuns(&out, row, true)
		}
		out.WriteByte('-') // next band
	}
	out.WriteString("\x1b\\")
	return out.Bytes()
}

// writeRuns writes one colour's sixel characters for a band, run-length
// encoded. The first colour must cover the whole band, including blank
// columns: SyncTERM resets its transparency mask during that first pass.
func writeRuns(out *bytes.Buffer, row []byte, fullWidth bool) {
	end := len(row)
	for !fullWidth && end > 0 && row[end-1] == '?' {
		end--
	}
	for i := 0; i < end; {
		j := i
		for j < end && row[j] == row[i] {
			j++
		}
		if n := j - i; n > 3 {
			out.WriteByte('!')
			out.WriteString(strconv.Itoa(n))
			out.WriteByte(row[i])
		} else {
			for k := 0; k < n; k++ {
				out.WriteByte(row[i])
			}
		}
		i = j
	}
}

// indexed returns img's palette and its pixels as palette indexes in row
// order, -1 for a pixel left undrawn.
func indexed(img image.Image) (color.Palette, []int16) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	px := make([]int16, w*h)

	// An image that already has few enough colours keeps them exactly.
	index := map[color.NRGBA]int16{}
	var pal color.Palette
	exact := true
scan:
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			if c.A < 128 {
				px[y*w+x] = -1
				continue
			}
			c.A = 255
			i, ok := index[c]
			if !ok {
				if len(pal) == maxColors {
					exact = false
					break scan
				}
				i = int16(len(pal))
				index[c] = i
				pal = append(pal, c)
			}
			px[y*w+x] = i
		}
	}
	if exact {
		return pal, px
	}

	pal = DefaultPalette()
	dst := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	draw.FloydSteinberg.Draw(dst, dst.Bounds(), img, b.Min)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			_, _, _, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if a < 0x8000 {
				px[y*w+x] = -1
				continue
			}
			px[y*w+x] = int16(dst.ColorIndexAt(x, y))
		}
	}
	return pal, px
}

// DefaultPalette is a 6x7x6 colour cube, 252 colours, with more steps of
// green than of red or blue because the eye tells greens apart best.
func DefaultPalette() color.Palette {
	pal := make(color.Palette, 0, 6*7*6)
	for r := 0; r < 6; r++ {
		for g := 0; g < 7; g++ {
			for b := 0; b < 6; b++ {
				pal = append(pal, color.NRGBA{uint8(r * 255 / 5), uint8(g * 255 / 6), uint8(b * 255 / 5), 255})
			}
		}
	}
	return pal
}
