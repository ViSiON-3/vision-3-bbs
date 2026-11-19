package menu

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// writeOpaqueMatrixPNG writes a fully opaque w x h picture, which a scene
// can be built from.
func writeOpaqueMatrixPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	img.SetNRGBA(w/2, h/2, color.NRGBA{R: 200, G: 40, B: 180, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	chatcovWriteFile(t, path, out.String())
}

// TestMatrixSceneHidesTextScreen checks that a terminal shown the projected
// cube scene is never sent the PDMATRIX.ANS text screen or the text choices,
// on the first draw or on a space-bar redraw, and that the text screen is
// still drawn whenever the scene is not.
func TestMatrixSceneHidesTextScreen(t *testing.T) {
	const efx = `{"heat":true,"textFade":true,"projectText":true,"layout":"side"}`
	for _, tc := range []struct {
		name      string
		sixel     bool // the terminal answered the connect probe with sixel
		efx       string
		wantScene bool
	}{
		{"sixel terminal with scene", true, efx, true},
		{"terminal without sixel", false, efx, false},
		{"no effect file", true, "", false},
		{"effect file is not JSON", true, "{", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newMenuEnv(t)
			set := chatcovMatrixSet(t, env)
			writeOpaqueMatrixPNG(t, filepath.Join(set, "ansi", "PDMATRIX.PNG"), 320, 240)
			if tc.efx != "" {
				if err := os.WriteFile(filepath.Join(set, "ansi", "PDMATRIX.EFX"), []byte(tc.efx), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			env.outputMode = ansi.OutputModeCP437
			r := env.run(func(c *cmdCtx, _ string) (*user.User, string, error) {
				s := &matrixGraphicSession{testSession: c.s.(*testSession), term: "xterm"}
				storeSessionSixel(s, tc.sixel)
				defer sessionSixelCaps.Delete(s)
				defer resetSessionIH(s)
				action, u, err := c.e.RunMatrixScreen(s, c.terminal, c.userManager, c.nodeNumber, c.outputMode, c.termWidth, c.termHeight)
				return u, action, err
			}, nil, "", " D")
			if r.err != nil || r.next != "DISCONNECT" {
				t.Fatalf("matrix action=%q err=%v", r.next, r.err)
			}
			textParts := []string{chatcovMatrixArt, " Journey onward.", " Create an account.", " Check your access.", " Disconnect."}
			if !r.has("Disconnecting...") {
				t.Error("D did not disconnect")
			}
			if tc.wantScene {
				// A full frame carries the raster size; the animation sends
				// only changes. The first draw and the space-bar redraw each
				// send one.
				if n := strings.Count(r.raw, "\x1bP0;1;0q\"1;1;640;368"); n < 2 {
					t.Fatalf("got %d full scene frames, want one for the first draw and one for the redraw", n)
				}
				for _, part := range textParts {
					if strings.Contains(r.text(), part) {
						t.Errorf("text screen sent with the scene: found %q", part)
					}
				}
				return
			}
			for _, part := range textParts {
				if !strings.Contains(r.text(), part) {
					t.Errorf("text screen missing %q", part)
				}
			}
		})
	}
}
