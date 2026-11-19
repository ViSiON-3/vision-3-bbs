package menu

import (
	"encoding/base64"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
)

// The audio commands the matrix sends, in SyncTERM's APC form.
const (
	apcAudio       = "\x1b_SyncTERM:A;"
	loadMoveTone   = apcAudio + "LoadBlob;S=250;"
	loadChooseTone = apcAudio + "LoadBlob;S=251;"
	playMoveTone   = apcAudio + "Flush;C=2\x1b\\" + apcAudio + "Copy;S=250;D=252\x1b\\" + apcAudio + "Queue;C=2;S=252\x1b\\"
	playChooseTone = apcAudio + "Flush;C=2\x1b\\" + apcAudio + "Copy;S=251;D=252\x1b\\" + apcAudio + "Queue;C=2;S=252\x1b\\"
)

// testWAV returns a WAV file with the given format and n zero-level samples.
func testWAV(format, channels uint16, rate uint32, bits uint16, n int) []byte {
	align := channels * bits / 8
	fmtChunk := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtChunk[0:], format)
	binary.LittleEndian.PutUint16(fmtChunk[2:], channels)
	binary.LittleEndian.PutUint32(fmtChunk[4:], rate)
	binary.LittleEndian.PutUint32(fmtChunk[8:], rate*uint32(align))
	binary.LittleEndian.PutUint16(fmtChunk[12:], align)
	binary.LittleEndian.PutUint16(fmtChunk[14:], bits)
	data := make([]byte, n*int(align))
	for i := range data {
		data[i] = 128
	}
	var body []byte
	body = append(body, "WAVE"...)
	body = append(body, "fmt "...)
	body = binary.LittleEndian.AppendUint32(body, 16)
	body = append(body, fmtChunk...)
	body = append(body, "data"...)
	body = binary.LittleEndian.AppendUint32(body, uint32(len(data)))
	body = append(body, data...)
	out := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...)
	return append(out, body...)
}

// runSoundMatrix runs the stock matrix with the given tone files in its
// theme, on a session that answered the audio probe with audio, and types
// input.
func runSoundMatrix(t *testing.T, audio bool, files map[string][]byte, input string) runResult {
	t.Helper()
	env := newMenuEnv(t)
	set := chatcovMatrixSet(t, env)
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(set, "ansi", name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env.outputMode = ansi.OutputModeCP437
	return env.run(func(c *cmdCtx, _ string) (*user.User, string, error) {
		s := &matrixGraphicSession{testSession: c.s.(*testSession), term: "xterm"}
		storeSessionAudio(s, audio)
		defer sessionAudioCaps.Delete(s)
		defer resetSessionIH(s)
		action, u, err := c.e.RunMatrixScreen(s, c.terminal, c.userManager, c.nodeNumber, c.outputMode, c.termWidth, c.termHeight)
		return u, action, err
	}, nil, "", input)
}

// riffSize returns wav with its RIFF header declaring size bytes after it.
func riffSize(wav []byte, size uint32) []byte {
	binary.LittleEndian.PutUint32(wav[4:], size)
	return wav
}

// dataSize returns wav with its data chunk declaring size bytes.
func dataSize(wav []byte, size uint32) []byte {
	i := strings.Index(string(wav), "data")
	binary.LittleEndian.PutUint32(wav[i+4:], size)
	return wav
}

// fmtField returns wav with the 16-bit fmt field at offset off set to v.
func fmtField(wav []byte, off int, v uint16) []byte {
	i := strings.Index(string(wav), "fmt ")
	binary.LittleEndian.PutUint16(wav[i+8+off:], v)
	return wav
}

// appendChunk returns wav with a chunk header declaring size and body
// appended, and the RIFF size counting them.
func appendChunk(wav []byte, id string, size uint32, body []byte) []byte {
	wav = append(wav, id...)
	wav = binary.LittleEndian.AppendUint32(wav, size)
	wav = append(wav, body...)
	return riffSize(wav, uint32(len(wav)-8))
}

// secondFmt returns a 16-bit WAV with an 8-bit fmt chunk added after its
// own, before the data.
func secondFmt() []byte {
	wav16 := testWAV(1, 1, 22050, 16, 400)
	wav8 := testWAV(1, 1, 22050, 8, 400)
	data := strings.Index(string(wav16), "data")
	out := append([]byte(nil), wav16[:data]...)
	out = append(out, wav8[12:12+8+16]...) // the 8-bit fmt chunk
	out = append(out, wav8[data:]...)      // 8-bit data
	return riffSize(out, uint32(len(out)-8))
}

// dataFirst returns a valid 8-bit WAV with its data chunk before fmt.
func dataFirst() []byte {
	wav := testWAV(1, 1, 22050, 8, 400)
	fmtChunk := wav[12 : 12+8+16]
	out := append([]byte(nil), wav[:12]...)
	out = append(out, wav[12+8+16:]...)
	return append(out, fmtChunk...)
}

func validTones() map[string][]byte {
	return map[string][]byte{
		"PDMATRIX.MOVE.WAV":   testWAV(1, 1, 22050, 8, 400),
		"PDMATRIX.CHOOSE.WAV": testWAV(1, 1, 22050, 8, 2000),
	}
}

// On an audio session each move of the highlight plays the move tone once,
// after the highlight is drawn, and a choice plays the choose tone before
// the matrix acts on it.
func TestMatrixTonesPlay(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		moves       []string // the choice highlighted by each move, in order
	}{
		{"Enter on a choice", keyUp + "\r", []string{"Disconnect."}},
		{"hot key", keyDown + keyDown + "D", []string{"Create an account.", "Check your access."}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tones := validTones()
			r := runSoundMatrix(t, true, tones, tc.input)
			if r.err != nil || r.next != "DISCONNECT" || !r.has("Disconnecting") {
				t.Fatalf("matrix action=%q err=%v", r.next, r.err)
			}
			out := r.raw
			for _, l := range []struct{ prefix, file string }{{loadMoveTone, "PDMATRIX.MOVE.WAV"}, {loadChooseTone, "PDMATRIX.CHOOSE.WAV"}} {
				want := l.prefix + base64.StdEncoding.EncodeToString(tones[l.file]) + "\x1b\\"
				if got := strings.Count(out, want); got != 1 {
					t.Errorf("%s loaded whole %d times, want once", l.file, got)
				}
			}
			if got := strings.Count(out, playMoveTone); got != len(tc.moves) {
				t.Fatalf("move tone played %d times, want %d", got, len(tc.moves))
			}
			// Each tone is loaded before the first copy out of its slot.
			for _, slot := range []string{"250", "251"} {
				load := strings.Index(out, apcAudio+"LoadBlob;S="+slot+";")
				first := strings.Index(out, apcAudio+"Copy;S="+slot+";")
				if load < 0 || first < 0 || load > first {
					t.Errorf("slot %s loaded at %d, first copied at %d", slot, load, first)
				}
			}
			// Each move tone comes after the highlight of the choice it
			// moved to, and before the next move's highlight.
			from := strings.Index(out, apcAudio)
			for i, text := range tc.moves {
				hl := strings.Index(out[from:], chatcovHighlight(text))
				tone := strings.Index(out[from:], playMoveTone)
				if hl < 0 || tone < 0 || hl > tone {
					t.Fatalf("move %d: highlight of %q at %d, tone at %d after offset %d", i+1, text, hl, tone, from)
				}
				from += tone + len(playMoveTone)
			}
			if got := strings.Count(out, playChooseTone); got != 1 {
				t.Errorf("choose tone played %d times, want 1", got)
			}
			// The highlight on Disconnect is drawn before its tone.
			if hl, tone := strings.LastIndex(out, chatcovHighlight("Disconnect.")), strings.Index(out, playChooseTone); hl < 0 || hl > tone {
				t.Errorf("highlight at %d, choose tone at %d: the tone came first", hl, tone)
			}
			if tone, act := strings.Index(out, playChooseTone), strings.Index(out, "Disconnecting"); tone > act {
				t.Errorf("choose tone at %d, after the action at %d", tone, act)
			}
		})
	}
}

// Every move tone first flushes the channel, so a held arrow key replaces
// the tone instead of queueing one per key.
func TestMatrixTonesNoBacklog(t *testing.T) {
	r := runSoundMatrix(t, true, validTones(), strings.Repeat(keyDown, 20)+"D")
	if got := strings.Count(r.raw, playMoveTone); got != 20 {
		t.Fatalf("move tone played %d times, want 20", got)
	}
	if got := strings.Count(r.raw, playChooseTone); got != 1 {
		t.Fatalf("choose tone played %d times, want 1", got)
	}
	// Those 21 sequences are the only flushes and queues sent, so every
	// tone was started on an emptied channel.
	if f, q := strings.Count(r.raw, apcAudio+"Flush;"), strings.Count(r.raw, apcAudio+"Queue;"); f != 21 || q != 21 {
		t.Errorf("%d flushes and %d queues, want 21 of each", f, q)
	}
}

// A session that did not answer the audio probe with audio gets no audio
// command at all, and neither does a theme without the tone files.
func TestMatrixTonesNotSent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		audio bool
		files map[string][]byte
	}{
		{"no audio", false, validTones()},
		{"theme without tones", true, nil},
		{"16-bit tones", true, map[string][]byte{
			"PDMATRIX.MOVE.WAV":   testWAV(1, 1, 22050, 16, 400),
			"PDMATRIX.CHOOSE.WAV": testWAV(1, 1, 22050, 16, 400),
		}},
		{"stereo and 44100 Hz", true, map[string][]byte{
			"PDMATRIX.MOVE.WAV":   testWAV(1, 2, 22050, 8, 400),
			"PDMATRIX.CHOOSE.WAV": testWAV(1, 1, 44100, 8, 400),
		}},
		{"not a WAV", true, map[string][]byte{"PDMATRIX.MOVE.WAV": []byte("not a wav"), "PDMATRIX.CHOOSE.WAV": nil}},
		{"too large", true, map[string][]byte{"PDMATRIX.MOVE.WAV": testWAV(1, 1, 22050, 8, maxMatrixTone)}},
		{"chunks past the declared RIFF size", true, map[string][]byte{"PDMATRIX.MOVE.WAV": riffSize(testWAV(1, 1, 22050, 8, 400), 4)}},
		{"block alignment 2", true, map[string][]byte{"PDMATRIX.MOVE.WAV": fmtField(testWAV(1, 1, 22050, 8, 400), 12, 2)}},
		{"truncated chunk after data", true, map[string][]byte{"PDMATRIX.MOVE.WAV": appendChunk(testWAV(1, 1, 22050, 8, 400), "JUNK", 4, nil)}},
		{"odd data without its pad byte", true, map[string][]byte{"PDMATRIX.MOVE.WAV": testWAV(1, 1, 22050, 8, 401)}},
		{"second fmt chunk", true, map[string][]byte{"PDMATRIX.MOVE.WAV": secondFmt()}},
		{"data before fmt", true, map[string][]byte{"PDMATRIX.MOVE.WAV": dataFirst()}},
		{"chunk size past the file", true, map[string][]byte{"PDMATRIX.MOVE.WAV": dataSize(testWAV(1, 1, 22050, 8, 400), 0xffffffff)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := runSoundMatrix(t, tc.audio, tc.files, keyDown+keyDown+"D")
			// Running out of input also returns DISCONNECT, without the message.
			if r.err != nil || r.next != "DISCONNECT" || !r.has("Disconnecting") {
				t.Fatalf("matrix action=%q err=%v, want D's disconnect", r.next, r.err)
			}
			if i := strings.Index(r.raw, "\x1b_"); i >= 0 {
				t.Errorf("audio command sent: %q", r.raw[i:min(len(r.raw), i+40)])
			}
		})
	}
}

// A theme may ship one tone and leave the other out.
func TestMatrixTonesOnlyMove(t *testing.T) {
	r := runSoundMatrix(t, true, map[string][]byte{"PDMATRIX.MOVE.WAV": testWAV(1, 1, 22050, 8, 400)}, keyDown+"D")
	if got := strings.Count(r.raw, playMoveTone); got != 1 {
		t.Errorf("move tone played %d times, want 1", got)
	}
	if strings.Contains(r.raw, loadChooseTone) || strings.Contains(r.raw, "S=251") {
		t.Error("choose tone sent though the theme has none")
	}
}

// The parser accepts an odd-length tone with its pad byte and a complete
// chunk after the data, and counts the samples.
func TestMatrixToneSamplesAccepts(t *testing.T) {
	padded := append(testWAV(1, 1, 22050, 8, 401), 0)
	padded = riffSize(padded, uint32(len(padded)-8))
	for _, tc := range []struct {
		name string
		wav  []byte
		want int
	}{
		{"even", testWAV(1, 1, 22050, 8, 400), 400},
		{"odd with pad byte", padded, 401},
		{"chunk after data", appendChunk(testWAV(1, 1, 22050, 8, 400), "LIST", 4, []byte("INFO")), 400},
	} {
		if n, ok := matrixToneSamples(tc.wav); !ok || n != tc.want {
			t.Errorf("%s: matrixToneSamples = %d, %v; want %d, true", tc.name, n, ok, tc.want)
		}
	}
}

// The tune is loaded once, played once on its own channel, and stopped
// when the matrix hands the caller on.
func TestMatrixMusicLoopsAndStops(t *testing.T) {
	files := validTones()
	files[matrixMusicFile] = testWAV(1, 1, 22050, 8, 9000)
	r := runSoundMatrix(t, true, files, keyUp+"\r")
	if r.err != nil || r.next != "DISCONNECT" {
		t.Fatalf("matrix action=%q err=%v", r.next, r.err)
	}
	start := apcAudio + "LoadBlob;S=248;" + base64.StdEncoding.EncodeToString(files[matrixMusicFile]) + "\x1b\\" +
		apcAudio + "Copy;S=248;D=249\x1b\\" + apcAudio + "Queue;C=3;S=249\x1b\\"
	stop := apcAudio + "Flush;C=3\x1b\\"
	if got := strings.Count(r.raw, start); got != 1 {
		t.Fatalf("tune loaded and played %d times, want once", got)
	}
	if got := strings.Count(r.raw, stop); got != 1 {
		t.Fatalf("tune stopped %d times, want once", got)
	}
	if strings.Index(r.raw, stop) < strings.Index(r.raw, start) {
		t.Error("the tune was stopped before it started")
	}
	// The choose tone still plays over it, on its own channel.
	if !strings.Contains(r.raw, playChooseTone) {
		t.Error("the choose tone did not play")
	}
}

func TestMatrixMusicNotSent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		audio bool
		tune  []byte
	}{
		{"no audio", false, testWAV(1, 1, 22050, 8, 9000)},
		{"16-bit", true, testWAV(1, 1, 22050, 16, 9000)},
		{"stereo", true, testWAV(1, 2, 22050, 8, 9000)},
		{"over the limit", true, testWAV(1, 1, 22050, 8, maxMatrixMusic)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := validTones()
			files[matrixMusicFile] = tc.tune
			r := runSoundMatrix(t, tc.audio, files, "D")
			if r.err != nil || r.next != "DISCONNECT" {
				t.Fatalf("matrix action=%q err=%v", r.next, r.err)
			}
			for _, part := range []string{"LoadBlob;S=248", "Queue;C=3", "Flush;C=3"} {
				if strings.Contains(r.raw, part) {
					t.Errorf("%q was sent", part)
				}
			}
		})
	}
}

func TestPMSExampleMatrixMusic(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "templates", "prelogin-example-pms", "ansi", matrixMusicFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := matrixToneSamples(b); !ok {
		t.Errorf("%s is not WAV, 8-bit unsigned PCM, 22050 Hz, mono", matrixMusicFile)
	}
	if len(b) > maxMatrixMusic {
		t.Errorf("%s is %d bytes, over the %d the matrix sends", matrixMusicFile, len(b), maxMatrixMusic)
	}
}

// After settleAt the cube stops turning and comes to rest with a face
// square to the caller, and stays there.
func TestMatrixCubeSettlesSquare(t *testing.T) {
	const at = 20.0
	y0, p0, r0 := matrixCubeAngles(at)
	if p0 == 0 || r0 == 0 || math.Mod(y0, math.Pi/2) == 0 {
		t.Fatal("the test needs a cube that is mid-turn at settleAt")
	}
	if y, p, r := matrixCubePose(at-1, at); y == 0 || p == 0 || r == 0 {
		t.Error("the cube is at rest before settleAt")
	}
	for _, later := range []float64{matrixSettleSeconds, matrixSettleSeconds + 7.3, 600} {
		yaw, pitch, roll := matrixCubePose(at+later, at)
		quarter := yaw / (math.Pi / 2)
		if pitch != 0 || roll != 0 || math.Abs(quarter-math.Round(quarter)) > 1e-9 {
			t.Errorf("%.1f s after settleAt: yaw %.4f pitch %.4f roll %.4f, want a face square on", later, yaw, pitch, roll)
		}
		if math.Abs(yaw-y0) > math.Pi/4+1e-9 {
			t.Errorf("%.1f s after settleAt: the cube turned %.3f to settle, more than to its nearest face", later, yaw-y0)
		}
	}
	// With no settleAt the pose is the free turn.
	if y, p, r := matrixCubePose(at+5, 0); y == y0 || p == p0 || r == r0 {
		t.Error("a cube with no settleAt stopped")
	}
}

// A tune's sounding length leaves out the silence at its end.
func TestMatrixTuneSounding(t *testing.T) {
	wav := testWAV(1, 1, 22050, 8, 22050*3)
	data := wav[len(wav)-22050*3:]
	for i := 0; i < 22050*2; i++ {
		data[i] = 200
	}
	if got := matrixTuneSounding(wav); got != 2 {
		t.Errorf("sounding length %.3f s, want 2: one of the three seconds is silence", got)
	}
	if got := matrixTuneSounding([]byte("not a wav")); got != 0 {
		t.Errorf("sounding length of a file that is not a tune is %.3f, want 0", got)
	}
}
