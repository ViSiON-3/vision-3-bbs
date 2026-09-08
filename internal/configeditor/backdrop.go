package configeditor

import "github.com/ViSiON-3/vision-3-bbs/internal/tuiart"

// The backdrop rasterizer and its embedded ANSI art live in internal/tuiart so
// the string editor can paint the same screen. These aliases keep the config
// editor's call sites unchanged.

type backdrop = tuiart.Backdrop

// pickBackdropArt chooses one embedded backdrop screen at random.
func pickBackdropArt() []byte { return tuiart.Pick() }

// loadBackdropFrom composites art bytes onto a width×height canvas.
func loadBackdropFrom(data []byte, width, height int) *backdrop {
	return tuiart.LoadFrom(data, width, height)
}
