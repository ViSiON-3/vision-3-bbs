package sixel

import (
	"bytes"
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestEncodeSmallImage(t *testing.T) {
	// Two columns: red over the first band, then transparent, then blue.
	img := image.NewNRGBA(image.Rect(0, 0, 2, 7))
	for y := 0; y < 6; y++ {
		img.Set(0, y, color.NRGBA{255, 0, 0, 255})
	}
	img.Set(1, 6, color.NRGBA{0, 0, 255, 255})

	got := string(Encode(img))
	want := "\x1bP0;1;0q\"1;1;2;7" +
		"#0;2;100;0;0#1;2;0;0;100" +
		"#0~?-" + // column 0 red, column 1 clears the band's mask
		"#1?@-" + // second band: column 1, top row, blue
		"\x1b\\"
	if got != want {
		t.Errorf("Encode =\n%q\nwant\n%q", got, want)
	}
}

func TestEncodeRunLength(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 10, 1))
	for x := 0; x < 10; x++ {
		img.Set(x, 0, color.NRGBA{0, 255, 0, 255})
	}
	if got := string(Encode(img)); !strings.Contains(got, "#0!10@-") {
		t.Errorf("Encode = %q, want a run of ten", got)
	}
}

func TestEncodeManyColoursUsesFixedPalette(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.NRGBA{uint8(x * 4), uint8(y * 4), uint8(x + y), 255})
		}
	}
	got := Encode(img)
	if !bytes.HasPrefix(got, []byte("\x1bP0;1;0q\"1;1;64;64#0;2;")) || !bytes.HasSuffix(got, []byte("\x1b\\")) {
		t.Fatalf("Encode framing wrong: %q ... %q", got[:30], got[len(got)-4:])
	}
	if n := bytes.Count(got, []byte(";2;")); n != 252 {
		t.Errorf("palette has %d colours, want the fixed 252", n)
	}
}

func TestEncodeEmpty(t *testing.T) {
	if got := Encode(image.NewNRGBA(image.Rect(0, 0, 0, 0))); got != nil {
		t.Errorf("Encode of an empty image = %q, want nil", got)
	}
}

func TestEncodePaletteKeepsRegisters(t *testing.T) {
	red, blue := color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 0, 255, 255}
	pal := color.Palette{red, blue}
	// A picture of blue alone still draws with register 1, and defines
	// register 0 as red, as a picture of both would.
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, blue)
	want := "\x1bP0;1;0q\"1;1;1;1#0;2;100;0;0#1;2;0;0;100#1@-\x1b\\"
	if got := string(EncodePalette(img, pal)); got != want {
		t.Errorf("EncodePalette =\n%q\nwant\n%q", got, want)
	}
	// A colour the palette lacks takes the nearest register.
	img.Set(0, 0, color.NRGBA{10, 0, 240, 255})
	if got := string(EncodePalette(img, pal)); got != want {
		t.Errorf("nearest colour: EncodePalette = %q", got)
	}
}

func TestEncodePaletteReuse(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	pal := color.Palette{color.NRGBA{255, 0, 0, 255}}
	if got, want := string(EncodePaletteReuse(img, pal)), "\x1bP0;1;0q\"1;1#0@-\x1b\\"; got != want {
		t.Errorf("reused palette = %q, want %q", got, want)
	}
}

func TestDeltaClearsMaskForEmptyBands(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 12))
	img.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	pal := color.Palette{color.NRGBA{255, 0, 0, 255}}
	if got := string(EncodePaletteReuse(img, pal)); !strings.Contains(got, "#0@?-#0??-") {
		t.Fatalf("transparent band must reset the terminal mask: %q", got)
	}
}

func TestPaletteOrderMatchesEncode(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.NRGBA{0, 255, 0, 255})
	img.Set(1, 0, color.NRGBA{255, 0, 0, 255})
	if got, want := string(EncodePalette(img, Palette(img))), string(Encode(img)); got != want {
		t.Errorf("EncodePalette with Palette =\n%q\nEncode =\n%q", got, want)
	}
}
