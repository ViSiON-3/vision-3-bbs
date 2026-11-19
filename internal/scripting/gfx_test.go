package scripting

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePNG puts a 4x4 red picture in the harness's data directory.
func writePNG(t *testing.T, h *scriptHarness, name string) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.NRGBA{255, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.dataDir, name), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGfxImage(t *testing.T) {
	sixelOn := func(sc *SessionContext) { sc.Sixel = true }

	t.Run("draws on a sixel terminal", func(t *testing.T) {
		h := newHarness(t, harnessOpts{session: sixelOn})
		writePNG(t, h, "pic.png")
		if !h.eval(`v3.gfx.sixel`).ToBoolean() {
			t.Error("v3.gfx.sixel = false")
		}
		if !h.eval(`v3.gfx.image("pic.png")`).ToBoolean() {
			t.Error("image() = false")
		}
		out := h.output()
		if !strings.HasPrefix(out, "\x1bP0;1;0q\"1;1;4;4#0;2;100;0;0") || !strings.HasSuffix(out, "\x1b\\") {
			t.Errorf("output = %q", out)
		}
	})

	t.Run("a palette picture fixes the colour registers", func(t *testing.T) {
		h := newHarness(t, harnessOpts{session: sixelOn})
		writePNG(t, h, "pic.png")
		// The palette has blue before red, so red is register 1.
		pal := image.NewNRGBA(image.Rect(0, 0, 2, 1))
		pal.Set(0, 0, color.NRGBA{0, 0, 255, 255})
		pal.Set(1, 0, color.NRGBA{255, 0, 0, 255})
		var buf bytes.Buffer
		if err := png.Encode(&buf, pal); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(h.dataDir, "pal.png"), buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		if !h.eval(`v3.gfx.image("pic.png", "pal.png")`).ToBoolean() {
			t.Error("image() = false")
		}
		if out := h.output(); !strings.HasPrefix(out, "\x1bP0;1;0q\"1;1;4;4#0;2;0;0;100#1;2;100;0;0#1") {
			t.Errorf("output = %q", out)
		}
		h.resetOutput()
		if h.eval(`v3.gfx.image("pic.png", "nope.png")`).ToBoolean() || h.output() != "" {
			t.Error("a missing palette picture was ignored")
		}
	})

	t.Run("draws nothing on a text terminal", func(t *testing.T) {
		h := newHarness(t, harnessOpts{})
		writePNG(t, h, "pic.png")
		if h.eval(`v3.gfx.sixel`).ToBoolean() || h.eval(`v3.gfx.image("pic.png")`).ToBoolean() {
			t.Error("a text terminal was offered graphics")
		}
		if out := h.output(); out != "" {
			t.Errorf("output = %q", out)
		}
	})

	t.Run("a missing or outside file draws nothing", func(t *testing.T) {
		h := newHarness(t, harnessOpts{session: sixelOn})
		for _, p := range []string{"nope.png", "../../etc/passwd"} {
			if h.eval(`v3.gfx.image("` + p + `")`).ToBoolean() {
				t.Errorf("image(%q) = true", p)
			}
		}
		if out := h.output(); out != "" {
			t.Errorf("output = %q", out)
		}
	})

	t.Run("a file that is not a picture draws nothing", func(t *testing.T) {
		h := newHarness(t, harnessOpts{session: sixelOn})
		if err := os.WriteFile(filepath.Join(h.dataDir, "bad.png"), []byte("not a picture"), 0o644); err != nil {
			t.Fatal(err)
		}
		if h.eval(`v3.gfx.image("bad.png")`).ToBoolean() || h.output() != "" {
			t.Error("a bad file was drawn")
		}
	})
}
