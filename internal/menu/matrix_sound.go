package menu

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/gliderlabs/ssh"
)

// The login matrix's tones are theme files beside PDMATRIX.ANS. A theme
// without them, or with one of them, plays what it has.
const (
	matrixMoveToneFile   = "PDMATRIX.MOVE.WAV"
	matrixChooseToneFile = "PDMATRIX.CHOOSE.WAV"
	// matrixMusicFile is a tune played once while the caller is on the
	// matrix, in the same format as the tones. When it ends the cube
	// comes to rest.
	matrixMusicFile = "PDMATRIX.MUSIC.WAV"
)

// maxMatrixTone bounds a tone file the matrix sends. It is far inside the
// terminals' own limits measured in testing (IcyTERM takes a LoadBlob of
// up to 32 MiB) and still a second and more of sound in the board's format.
const maxMatrixTone = 64 << 10

// maxMatrixMusic bounds the tune: three minutes of the board's format.
const maxMatrixMusic = 4 << 20

// Where the tones live in the terminal. SyncTERM and IcyTERM have 256
// sample slots; Queue in SyncTERM hands a slot's sound to the mixer and
// empties the slot, so each tone is loaded once into a slot of its own and
// copied into matrixPlaySlot to be played. Channels 0 and 1 belong to the
// terminal; 2 is the first one APC commands may use.
const (
	matrixMoveSlot     = 250
	matrixChooseSlot   = 251
	matrixPlaySlot     = 252
	matrixToneChannel  = 2
	matrixToneMove     = 0
	matrixToneChoose   = 1
	matrixToneSampleHz = 22050
	// The tune is kept in its own slot, copied to a play slot when it
	// starts, and played on a channel of its own so a tone never cuts it.
	matrixMusicSlot     = 248
	matrixMusicPlaySlot = 249
	matrixMusicChannel  = 3
	// matrixMusicFadeMs is how long a tune stopped early takes to fade out.
	matrixMusicFadeMs = 900
)

// matrixTones plays the matrix's tones on one session. A nil *matrixTones
// plays nothing.
type matrixTones struct {
	w      io.Writer
	loaded [2]bool // by matrixToneMove, matrixToneChoose
	tune   []byte  // the tune, read and not yet sent
	length float64 // seconds until the tune's last sound
	music  bool    // the tune was sent and may still be playing
}

// loadMatrixTones sends the theme's tones to s's terminal, through w, if
// the terminal said at connect that it plays the board's audio format. It
// returns nil, having sent nothing, when it did not or the theme has
// neither tone.
func (e *MenuExecutor) loadMatrixTones(s ssh.Session, w io.Writer, node int) *matrixTones {
	if !sessionAudio(s) {
		return nil
	}
	t := &matrixTones{w: w}
	for i, f := range [...]struct {
		name string
		slot int
	}{{matrixMoveToneFile, matrixMoveSlot}, {matrixChooseToneFile, matrixChooseSlot}} {
		b, ok := e.matrixToneFile(f.name, maxMatrixTone, node)
		if !ok {
			continue
		}
		cmd := fmt.Sprintf("\x1b_SyncTERM:A;LoadBlob;S=%d;%s\x1b\\", f.slot, base64.StdEncoding.EncodeToString(b))
		if _, err := io.WriteString(w, cmd); err != nil {
			return nil
		}
		t.loaded[i] = true
	}
	// The tune is read now and sent by startMusic, once the picture is up.
	if b, ok := e.matrixToneFile(matrixMusicFile, maxMatrixMusic, node); ok {
		t.tune, t.length = b, matrixTuneSounding(b)
	}
	if !t.loaded[matrixToneMove] && !t.loaded[matrixToneChoose] && t.tune == nil {
		return nil
	}
	return t
}

// startMusic sends the tune and plays it through once. The matrix calls it
// when its picture has been drawn, so the tune's bytes are not ahead of the
// picture's on the way to the caller. It returns the seconds until the
// tune's last sound, or zero if there was none to start.
func (t *matrixTones) startMusic() float64 {
	if t == nil || t.tune == nil {
		return 0
	}
	cmd := fmt.Sprintf("\x1b_SyncTERM:A;LoadBlob;S=%d;%s\x1b\\\x1b_SyncTERM:A;Copy;S=%d;D=%d\x1b\\\x1b_SyncTERM:A;Queue;C=%d;S=%d\x1b\\",
		matrixMusicSlot, base64.StdEncoding.EncodeToString(t.tune),
		matrixMusicSlot, matrixMusicPlaySlot, matrixMusicChannel, matrixMusicPlaySlot)
	t.tune = nil
	if _, err := io.WriteString(t.w, cmd); err != nil {
		return 0
	}
	t.music = true
	return t.length
}

// stopMusic ends the tune, if it was started. The tones are untouched.
func (t *matrixTones) stopMusic() {
	if t == nil || !t.music {
		return
	}
	t.music = false
	// O= is a fade-out in milliseconds: the tune dies away, it is not cut.
	_, _ = fmt.Fprintf(t.w, "\x1b_SyncTERM:A;Flush;C=%d;O=%d\x1b\\", matrixMusicChannel, matrixMusicFadeMs) // best-effort sound
}

// matrixToneFile reads a tone from the theme. A missing file is no tone;
// one that is not in the board's format, or too large, is logged and is no
// tone either, since the terminal said only that it plays that format.
func (e *MenuExecutor) matrixToneFile(name string, limit int64, node int) ([]byte, bool) {
	path, _, ok, err := e.Menus().Locate("ansi", name)
	if err != nil {
		slog.Warn("resolving matrix tone", "node", node, "file", name, "error", err)
		return nil, false
	}
	if !ok {
		return nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		slog.Warn("cannot open matrix tone", "node", node, "file", path, "error", err)
		return nil, false
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		slog.Warn("matrix tone too large or unreadable", "node", node, "file", path, "limit", limit, "error", err)
		return nil, false
	}
	if _, ok := matrixToneSamples(b); !ok {
		slog.Warn("matrix tone is not WAV, 8-bit unsigned PCM, 22050 Hz, mono", "node", node, "file", path)
		return nil, false
	}
	return b, true
}

// play starts tone, cutting off whatever tone is still playing, so keys
// pressed faster than the tones last never build a queue.
func (t *matrixTones) play(tone int) {
	if t == nil || !t.loaded[tone] {
		return
	}
	slot := matrixMoveSlot
	if tone == matrixToneChoose {
		slot = matrixChooseSlot
	}
	cmd := fmt.Sprintf("\x1b_SyncTERM:A;Flush;C=%d\x1b\\\x1b_SyncTERM:A;Copy;S=%d;D=%d\x1b\\\x1b_SyncTERM:A;Queue;C=%d;S=%d\x1b\\",
		matrixToneChannel, slot, matrixPlaySlot, matrixToneChannel, matrixPlaySlot)
	_, _ = io.WriteString(t.w, cmd) // best-effort sound
}

// matrixToneSamples reports whether b is a WAV file in the board's audio
// format, WAV with 8-bit unsigned PCM at 22050 Hz, mono, and how many
// samples it holds.
func matrixToneSamples(b []byte) (int, bool) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return 0, false
	}
	// Chunks are read only inside the container the header declares.
	riffEnd := uint64(binary.LittleEndian.Uint32(b[4:8])) + 8
	if riffEnd > uint64(len(b)) {
		return 0, false
	}
	end := int(riffEnd)
	formatOK, haveFormat, samples, haveData := false, false, 0, false
	for p := 12; p < end; {
		if end-p < 8 {
			return 0, false
		}
		id := string(b[p : p+4])
		size := uint64(binary.LittleEndian.Uint32(b[p+4 : p+8]))
		body := p + 8
		// A chunk of odd length is followed by a pad byte.
		if size+size%2 > uint64(end-body) {
			return 0, false
		}
		n := int(size)
		switch id {
		case "fmt ":
			// One format chunk, before the data: a decoder reads the first
			// and expects it there.
			if n < 16 || haveFormat || haveData {
				return 0, false
			}
			haveFormat = true
			f := b[body:]
			formatOK = binary.LittleEndian.Uint16(f[0:]) == 1 && // PCM
				binary.LittleEndian.Uint16(f[2:]) == 1 && // mono
				binary.LittleEndian.Uint32(f[4:]) == matrixToneSampleHz &&
				binary.LittleEndian.Uint32(f[8:]) == matrixToneSampleHz && // bytes per second
				binary.LittleEndian.Uint16(f[12:]) == 1 && // bytes per sample
				binary.LittleEndian.Uint16(f[14:]) == 8 // 8-bit PCM in WAV is unsigned
		case "data":
			if haveData || !formatOK {
				return 0, false
			}
			samples, haveData = n, true
		}
		p = body + n + n%2
	}
	return samples, formatOK && haveData && samples > 0
}

// matrixTuneSounding is how many seconds of tune b, a file matrixToneSamples
// accepts, pass before its last sound: its length less the silence at its
// end. A tune may end in silence so the terminal plays its last note out.
func matrixTuneSounding(b []byte) float64 {
	samples, ok := matrixToneSamples(b)
	if !ok {
		return 0
	}
	// The data chunk is the last samples bytes before any trailing chunk;
	// find it as the parser does.
	for p := 12; p+8 <= len(b); {
		size := int(binary.LittleEndian.Uint32(b[p+4 : p+8]))
		if string(b[p:p+4]) == "data" {
			data := b[p+8 : p+8+samples]
			n := len(data)
			// 8-bit silence is 128; a step either side is noise.
			for n > 0 && data[n-1] >= 127 && data[n-1] <= 129 {
				n--
			}
			return float64(n) / matrixToneSampleHz
		}
		p += 8 + size + size%2
	}
	return 0
}
