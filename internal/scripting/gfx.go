package scripting

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"  // decoders for v3.gfx.image
	_ "image/jpeg" // decoders for v3.gfx.image
	_ "image/png"  // decoders for v3.gfx.image
	"log/slog"
	"os"
	"sync"

	"github.com/ViSiON-3/vision-3-bbs/internal/jsutil"
	"github.com/ViSiON-3/vision-3-bbs/internal/sixel"
	"github.com/dop251/goja"
)

// maxGfxPixels caps the size of a picture v3.gfx.image draws: larger than
// any text screen in pixels, and small enough to encode in well under a
// frame.
const maxGfxPixels = 1280 * 1024

// gfxCache holds encoded pictures by file, size and modification time (and
// the same for the palette picture), so a menu that redraws a picture does
// not decode and encode it each time.
var gfxCache sync.Map

// registerGfx creates the v3.gfx object: whether the caller's terminal
// draws pixel graphics, and image(), which draws a picture file.
func registerGfx(v3 *goja.Object, eng *Engine) {
	vm := eng.vm
	obj := vm.NewObject()

	jsutil.Set(obj, "sixel", eng.session.Sixel)
	registerCanvas(obj, eng)

	// image(path[, palette]) — draw a PNG, GIF or JPEG from the script's
	// data directory with its top-left corner at the cursor's cell. Returns
	// false, and draws nothing, when the terminal has no sixel support or
	// the file cannot be used, so the script draws its text version.
	// palette names a picture whose colours, in order, are the colour
	// registers to draw with; pictures drawn over one another must share
	// one (see sixel.EncodePalette).
	jsutil.Set(obj, "image", func(call goja.FunctionCall) goja.Value {
		if !eng.session.Sixel || len(call.Arguments) == 0 {
			return vm.ToValue(false)
		}
		path, palette := call.Arguments[0].String(), ""
		if len(call.Arguments) > 1 && !goja.IsUndefined(call.Arguments[1]) {
			palette = call.Arguments[1].String()
		}
		data, err := eng.gfxEncoded(path, palette)
		if err != nil {
			slog.Warn("v3.gfx.image: picture not drawn", "node", eng.session.NodeNumber, "file", path, "error", err)
			return vm.ToValue(false)
		}
		eng.writeBytes(data)
		return vm.ToValue(true)
	})

	jsutil.Set(v3, "gfx", obj)
}

// gfxEncoded returns the sixel encoding of the picture at path in the
// script's data directory, drawn with the colours of the picture at
// palette when that is not empty.
func (eng *Engine) gfxEncoded(path, palette string) ([]byte, error) {
	sandbox := sandboxRoot(eng.cfg)
	img, key, err := gfxPicture(sandbox, path)
	if err != nil {
		return nil, err
	}
	var pal color.Palette
	if palette != "" {
		palImg, palKey, err := gfxPicture(sandbox, palette)
		if err != nil {
			return nil, fmt.Errorf("palette: %w", err)
		}
		if pal = sixel.Palette(palImg); pal == nil {
			return nil, fmt.Errorf("palette %s has more than 256 colours", palette)
		}
		key += "\x00" + palKey
	}
	if cached, ok := gfxCache.Load(key); ok {
		return cached.([]byte), nil
	}
	var out []byte
	if pal != nil {
		out = sixel.EncodePalette(img, pal)
	} else {
		out = sixel.Encode(img)
	}
	gfxCache.Store(key, out)
	return out, nil
}

// gfxPictures holds decoded pictures by the key gfxPicture returns.
var gfxPictures sync.Map

// gfxPicture decodes the picture at path under sandbox. key names that
// file as it is now: its path, size and modification time.
func gfxPicture(sandbox, path string) (img image.Image, key string, err error) {
	err = withSandbox(sandbox, path, false, func(root *os.Root, rel string) error {
		info, err := root.Stat(rel)
		if err != nil {
			return err
		}
		key = fmt.Sprintf("%s\x00%s\x00%d\x00%d", sandbox, rel, info.Size(), info.ModTime().UnixNano())
		if cached, ok := gfxPictures.Load(key); ok {
			img = cached.(image.Image)
			return nil
		}
		raw, err := readSandboxFile(root, rel)
		if err != nil {
			return err
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			return err
		}
		if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxGfxPixels {
			return fmt.Errorf("picture is %dx%d, over the %d pixel limit", cfg.Width, cfg.Height, maxGfxPixels)
		}
		if img, _, err = image.Decode(bytes.NewReader(raw)); err != nil {
			return err
		}
		gfxPictures.Store(key, img)
		return nil
	})
	return img, key, err
}
