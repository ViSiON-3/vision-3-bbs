package lha

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The archives in testdata were made with LHa for UNIX 1.14i from AmyList.255
// (a made-up AmigaNet nodelist): AMYLIST.L55 the way AmigaNet packs its own,
// the rest one per compression method and header level. two-blocks.lzh holds
// a 150KB list, long enough that its Huffman trees change partway through.

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestExtract(t *testing.T) {
	want := readTestdata(t, "AmyList.255")
	cases := []struct {
		archive, method string
	}{
		{"AMYLIST.L55", "-lh5-"}, // header level 1
		{"lh5-level0.lzh", "-lh5-"},
		{"lh6-level2.lzh", "-lh6-"},
		{"lh7-level0.lzh", "-lh7-"},
		{"lh0-level2.lzh", "-lh0-"},
	}
	for _, c := range cases {
		data := readTestdata(t, c.archive)
		if !IsArchive(data) {
			t.Errorf("%s: IsArchive = false", c.archive)
		}
		files, err := Read(data)
		if err != nil {
			t.Fatalf("%s: %v", c.archive, err)
		}
		if len(files) != 1 {
			t.Fatalf("%s: %d members, want 1", c.archive, len(files))
		}
		f := files[0]
		if f.Name != "AmyList.255" || f.Method != c.method || f.Size != int64(len(want)) {
			t.Errorf("%s: member %q %s %d bytes, want AmyList.255 %s %d bytes", c.archive, f.Name, f.Method, f.Size, c.method, len(want))
		}
		got, err := f.Extract(1 << 20)
		if err != nil {
			t.Fatalf("%s: %v", c.archive, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: extracted data differs from AmyList.255", c.archive)
		}
	}
}

func TestExtractAcrossBlocks(t *testing.T) {
	files, err := Read(readTestdata(t, "two-blocks.lzh"))
	if err != nil {
		t.Fatal(err)
	}
	// Extract checks the data against the CRC in the header.
	got, err := files[0].Extract(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 153879 || !bytes.HasPrefix(got, []byte(";A AmigaNet Nodelist")) {
		t.Errorf("extracted %d bytes starting %q", len(got), got[:min(len(got), 20)])
	}
}

func TestReadSeveralMembers(t *testing.T) {
	files, err := Read(readTestdata(t, "multi.lzh"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.Name+" "+f.Method)
		if _, err := f.Extract(1 << 20); err != nil {
			t.Errorf("%s: %v", f.Name, err)
		}
	}
	if want := "AmyList.255 -lh5-, FILE_ID.DIZ -lh0-"; strings.Join(got, ", ") != want {
		t.Errorf("members %q, want %q", strings.Join(got, ", "), want)
	}
}

func TestExtractRefusesBadData(t *testing.T) {
	archive := readTestdata(t, "AMYLIST.L55")
	files, err := Read(archive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := files[0].Extract(100); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("over the limit: err = %v, want a limit error", err)
	}

	// Damage a byte in the middle of the compressed data.
	damaged := bytes.Clone(archive)
	damaged[len(damaged)/2] ^= 0x55
	files, err = Read(damaged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := files[0].Extract(1 << 20); err == nil {
		t.Error("damaged data extracted without an error")
	}

	if _, err := Read(archive[:len(archive)-100]); err == nil {
		t.Error("truncated archive read without an error")
	}
}

func TestReadRefusesDamagedHeader(t *testing.T) {
	// AMYLIST.L55 has a level 1 header: its name starts at byte 22.
	archive := bytes.Clone(readTestdata(t, "AMYLIST.L55"))
	archive[22] = 'X'
	if _, err := Read(archive); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Errorf("damaged name: err = %v, want a checksum error", err)
	}

	// A packed size near the top of the 32-bit range must not overflow the
	// bounds check.
	archive = bytes.Clone(readTestdata(t, "lh0-level2.lzh"))
	copy(archive[7:11], []byte{0xF0, 0xFF, 0xFF, 0x7F})
	if _, err := Read(archive); err == nil {
		t.Error("huge packed size read without an error")
	}
}

// bitWriter writes bits most significant first, as -lh5- data is read.
type bitWriter struct {
	buf   []byte
	nbits int
}

func (w *bitWriter) bits(v, n int) {
	for i := n - 1; i >= 0; i-- {
		if w.nbits%8 == 0 {
			w.buf = append(w.buf, 0)
		}
		if v>>i&1 != 0 {
			w.buf[len(w.buf)-1] |= 0x80 >> (w.nbits % 8)
		}
		w.nbits++
	}
}

// spaceMatchArchive is a one-member -lh5- archive (header level 0) whose data
// is "head", then a three-byte match six bytes back, then "tail". With a
// two-byte head the match reaches before the start of the data, into the
// spaces the window starts out filled with. One block, coded with:
//
//   - c: every literal 9 bits (100000000 + the byte), symbol 256 (a
//     three-byte match) "00", and symbol 257 "01" (unused, to complete the
//     code).
//   - t, which codes the c lengths: symbol 4 (length 2) "0" and symbol 11
//     (length 9) "1".
//   - p: the one symbol 3, sent with no bits, so a distance is 4 plus two
//     extra bits.
func spaceMatchArchive(head, tail string) []byte {
	var w bitWriter
	w.bits(len(head)+1+len(tail), 16) // symbols in the block

	w.bits(12, 5) // t lengths: 12 of them
	for i := range 12 {
		l := 0
		if i == 4 || i == 11 {
			l = 1
		}
		w.bits(l, 3)
		if i == 2 {
			w.bits(0, 2) // no extra zero lengths after the third
		}
	}

	w.bits(258, 9) // c lengths: 258 of them
	for i := range 258 {
		if i < 256 {
			w.bits(1, 1) // t symbol 11: length 9
		} else {
			w.bits(0, 1) // t symbol 4: length 2
		}
	}

	w.bits(0, 4) // p: one symbol,
	w.bits(3, 4) // symbol 3

	for _, b := range []byte(head) {
		w.bits(0x100|int(b), 9)
	}
	w.bits(0, 2) // symbol 256: a three-byte match
	w.bits(2, 2) // at distance 4+2: six bytes back, plus one
	for _, b := range []byte(tail) {
		w.bits(0x100|int(b), 9)
	}

	out := []byte(head + "   " + tail)
	name := "AmyList.255"
	h := []byte{byte(22 + len(name)), 0}
	h = append(h, "-lh5-"...)
	h = binary.LittleEndian.AppendUint32(h, uint32(len(w.buf)))
	h = binary.LittleEndian.AppendUint32(h, uint32(len(out)))
	h = append(h, 0, 0, 0x21, 0x5B, 0x20, 0) // DOS time, attribute, level 0
	h = append(h, byte(len(name)))
	h = append(h, name...)
	h = binary.LittleEndian.AppendUint16(h, crc16(out))
	for _, b := range h[2:] {
		h[1] += b
	}
	return append(append(h, w.buf...), 0)
}

// The window starts out full of spaces, and a match may copy them before
// the data has that many bytes of its own.
func TestExtractMatchIntoInitialWindow(t *testing.T) {
	archive := spaceMatchArchive(";A", "AmigaNet Nodelist\r\n")
	if path := os.Getenv("LHA_WRITE_SPACE_MATCH"); path != "" {
		_ = os.WriteFile(path, archive, 0o644) // for checking with another LHA tool
	}
	files, err := Read(archive)
	if err != nil {
		t.Fatal(err)
	}
	got, err := files[0].Extract(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	if want := ";A   AmigaNet Nodelist\r\n"; string(got) != want {
		t.Errorf("extracted %q, want %q", got, want)
	}
}

func TestExtractUnsupportedMethod(t *testing.T) {
	archive := bytes.Clone(readTestdata(t, "lh0-level2.lzh"))
	copy(archive[2:7], "-lh1-")
	files, err := Read(archive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := files[0].Extract(1 << 20); err == nil || !strings.Contains(err.Error(), "-lh1-") {
		t.Errorf("err = %v, want an unsupported -lh1- error", err)
	}
}

func TestIsArchive(t *testing.T) {
	for name, data := range map[string][]byte{
		"zip":   []byte("PK\x03\x04\x14\x00\x00\x00\x08\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"),
		"text":  []byte(";A AmigaNet Nodelist for Friday, September 12, 2025"),
		"empty": nil,
	} {
		if IsArchive(data) {
			t.Errorf("%s: IsArchive = true", name)
		}
	}
}

func TestBaseName(t *testing.T) {
	for in, want := range map[string]string{
		"AmyList.255":           "AmyList.255",
		"nodelist/AmyList.255":  "AmyList.255",
		"NODELIST\\AMYLIST.255": "AMYLIST.255",
		"nodelist\xffAmyList":   "AmyList",
	} {
		if got := baseName(in); got != want {
			t.Errorf("baseName(%q) = %q, want %q", in, got, want)
		}
	}
}
