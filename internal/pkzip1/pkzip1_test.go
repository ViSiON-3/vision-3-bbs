package pkzip1

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"
)

// The archives in testdata were made by testdata/gen (see its README):
// every member's text was made up there and compressed with one PKZIP 1.x
// method. Info-ZIP's unzip -t passes shrink.zip and implode.zip; nothing
// independent checks reduce.zip. Each member's CRC is the CRC of the text
// it was made from, which Open checks.

// readAll opens every member of a testdata archive with Open and returns
// how many it read.
func readAll(t *testing.T, name string, wantMethod func(uint16) bool) int {
	t.Helper()
	zr, err := zip.OpenReader("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	n := 0
	for _, f := range zr.File {
		if !wantMethod(f.Method) {
			t.Errorf("%s: method %d", f.Name, f.Method)
		}
		if _, err := f.Open(); !errors.Is(err, zip.ErrAlgorithm) {
			t.Errorf("%s: archive/zip opened it (%v); the test isn't testing this package", f.Name, err)
		}
		rc, err := Open(f, 1<<20)
		if err != nil {
			t.Errorf("%s: %v", f.Name, err)
			continue
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil || uint64(len(data)) != f.UncompressedSize64 {
			t.Errorf("%s: %d bytes, %v", f.Name, len(data), err)
		}
		n++
	}
	return n
}

// TestShrunk reads shrunk members, one big enough that the table fills and
// is partly cleared twice.
func TestShrunk(t *testing.T) {
	if n := readAll(t, "shrink.zip", func(m uint16) bool { return m == Shrunk }); n != 3 {
		t.Errorf("read %d members, want 3", n)
	}
}

// TestImploded reads imploded members with each combination of window (4K
// and 8K) and trees (two, or three with a literal tree).
func TestImploded(t *testing.T) {
	if n := readAll(t, "implode.zip", func(m uint16) bool { return m == Imploded }); n != 12 {
		t.Errorf("read %d members, want 12", n)
	}
}

// TestReduced reads reduced members at each compression factor.
func TestReduced(t *testing.T) {
	if n := readAll(t, "reduce.zip", func(m uint16) bool { return m >= Reduced1 && m <= Reduced4 }); n != 12 {
		t.Errorf("read %d members, want 12", n)
	}
}

// members returns the compressed data, flags, method and size of every
// member of a testdata archive.
func members(t testing.TB, name string) []member {
	t.Helper()
	zr, err := zip.OpenReader("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	var out []member
	for _, f := range zr.File {
		raw, err := f.OpenRaw()
		if err != nil {
			t.Fatal(err)
		}
		src, err := io.ReadAll(raw)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, member{f.Method, f.Flags, src, int(f.UncompressedSize64)})
	}
	return out
}

type member struct {
	method, flags uint16
	src           []byte
	size          int
}

// TestDamagedData cuts and corrupts every member: each attempt either
// fails or unpacks to the stated size, and none panics or hangs.
func TestDamagedData(t *testing.T) {
	for _, archive := range []string{"shrink.zip", "implode.zip", "reduce.zip"} {
		for _, m := range members(t, archive) {
			for _, cut := range []int{0, 1, 2, len(m.src) / 3, len(m.src) - 1} {
				if _, err := Decompress(m.method, m.flags, m.src[:cut], m.size); err == nil {
					t.Errorf("%s method %d: cut to %d bytes, no error", archive, m.method, cut)
				}
			}
			for i := 0; i < len(m.src); i += 1 + len(m.src)/40 {
				bad := append([]byte(nil), m.src...)
				bad[i] ^= 0xA5
				if out, err := Decompress(m.method, m.flags, bad, m.size); err == nil && len(out) != m.size {
					t.Errorf("%s method %d: corrupt byte %d gave %d bytes", archive, m.method, i, len(out))
				}
			}
		}
	}
}

// TestOpenPassesThrough checks that Open leaves stored and deflated members
// to archive/zip, and refuses what it can't read: other methods, encrypted
// members, members over the limit and members that fail their CRC.
func TestOpenPassesThrough(t *testing.T) {
	zr, err := zip.OpenReader("testdata/shrink.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	f := zr.File[0]
	if _, err := Open(f, 10); !errors.Is(err, ErrTooLarge) {
		t.Errorf("over the limit: %v", err)
	}
	enc := *f
	enc.Flags |= 1
	if _, err := Open(&enc, 1<<20); !errors.Is(err, zip.ErrAlgorithm) {
		t.Errorf("encrypted: %v", err)
	}
	ur, err := zip.OpenReader("testdata/unreadable.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ur.Close() }()
	for _, m := range ur.File {
		if _, err := Open(m, 1<<20); !errors.Is(err, zip.ErrAlgorithm) {
			t.Errorf("%s (method %d, flags %d): %v", m.Name, m.Method, m.Flags, err)
		}
	}
	other := *f
	other.Method = 12 // bzip2
	if _, err := Open(&other, 1<<20); !errors.Is(err, zip.ErrAlgorithm) || Supported(12) {
		t.Errorf("bzip2: %v", err)
	}
	badCRC := *f
	badCRC.CRC32 ^= 1
	if _, err := Open(&badCRC, 1<<20); !errors.Is(err, zip.ErrChecksum) {
		t.Errorf("bad CRC: %v", err)
	}
	if !Supported(zip.Store) || !Supported(zip.Deflate) || !Supported(Imploded) || Supported(7) {
		t.Error("Supported")
	}
}

// FuzzDecompress feeds arbitrary data to each method: it may fail, but
// must not panic, and anything it returns is the size asked for.
func FuzzDecompress(f *testing.F) {
	for _, archive := range []string{"shrink.zip", "implode.zip", "reduce.zip"} {
		for _, m := range members(f, archive) {
			if len(m.src) < 4000 {
				f.Add(m.method, m.flags, m.src, m.size)
			}
		}
	}
	f.Fuzz(func(t *testing.T, method, flags uint16, src []byte, size int) {
		if size < 0 || size > 1<<16 {
			return
		}
		out, err := Decompress(method%7, flags, src, size)
		if err == nil && len(out) != size {
			t.Errorf("method %d: %d bytes, want %d", method%7, len(out), size)
		}
	})
}

// TestSample checks that Sample gives the start of every member, whatever
// its method, as Open would read it, and refuses what Open refuses.
func TestSample(t *testing.T) {
	for _, archive := range []string{"shrink.zip", "implode.zip", "reduce.zip", "oldpack.zip", "store+deflate"} {
		var files []*zip.File
		if archive == "store+deflate" {
			files = storedAndDeflated(t)
		} else {
			zr, err := zip.OpenReader("testdata/" + archive)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = zr.Close() }()
			files = zr.File
		}
		for _, f := range files {
			if !Supported(f.Method) || Encrypted(f) || f.FileInfo().IsDir() {
				continue
			}
			rc, err := Open(f, 1<<20)
			if err != nil {
				t.Fatalf("%s %s: %v", archive, f.Name, err)
			}
			full, _ := io.ReadAll(rc)
			_ = rc.Close()
			for _, n := range []int{-1, 0, 1, 100, 4096, len(full) - 1, len(full), len(full) + 10} {
				got, err := Sample(f, n, 1<<20)
				want := full[:max(0, min(n, len(full)))]
				if err != nil || !bytes.Equal(got, want) {
					t.Errorf("%s %s (method %d): Sample(%d) = %d bytes, %v; want %d", archive, f.Name, f.Method, n, len(got), err, len(want))
				}
			}
		}
	}

	zr, err := zip.OpenReader("testdata/shrink.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	f := zr.File[0]
	if _, err := Sample(f, 4096, 10); !errors.Is(err, ErrTooLarge) {
		t.Errorf("over the limit: %v", err)
	}
	enc := *f
	enc.Flags |= 1
	if _, err := Sample(&enc, 4096, 1<<20); !errors.Is(err, zip.ErrAlgorithm) {
		t.Errorf("encrypted: %v", err)
	}
	other := *f
	other.Method = 12 // bzip2
	if _, err := Sample(&other, 4096, 1<<20); !errors.Is(err, zip.ErrAlgorithm) {
		t.Errorf("bzip2: %v", err)
	}
	// The CRC covers the whole member, so it's checked only when the sample
	// is all of it.
	badCRC := *f
	badCRC.CRC32 ^= 1
	if _, err := Sample(&badCRC, int(f.UncompressedSize64), 1<<20); !errors.Is(err, zip.ErrChecksum) {
		t.Errorf("bad CRC, whole member: %v", err)
	}
	if _, err := Sample(&badCRC, 10, 1<<20); err != nil {
		t.Errorf("bad CRC, first 10 bytes: %v", err)
	}
}

// storedAndDeflated returns a stored and a deflated member, made here: the
// testdata archives hold only PKZIP 1.x methods.
func storedAndDeflated(t *testing.T) []*zip.File {
	t.Helper()
	text := bytes.Repeat([]byte("The quick brown fox jumps over the lazy dog.\r\n"), 200)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, m := range []uint16{zip.Store, zip.Deflate} {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: fmt.Sprintf("method%d.txt", m), Method: m})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(text); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr.File
}

// TestSamplePackedBound checks samplePacked's reasoning on every fixture
// member: the first n bytes unpack from no more than 3n bytes of packed
// data plus a small fixed overhead (1 KB here, against the 64 KB Sample
// allows).
func TestSamplePackedBound(t *testing.T) {
	for _, archive := range []string{"shrink.zip", "implode.zip", "reduce.zip", "oldpack.zip"} {
		for _, m := range members(t, archive) {
			full, err := Decompress(m.method, m.flags, m.src, m.size)
			if err != nil {
				t.Fatalf("%s method %d: %v", archive, m.method, err)
			}
			for _, n := range []int{1, 2, 10, 100, 1000, 4096, m.size - 1} {
				if n <= 0 || n >= m.size {
					continue
				}
				src := m.src[:min(len(m.src), 3*n+1024)]
				got, err := Decompress(m.method, m.flags, src, n)
				if err != nil || !bytes.Equal(got, full[:n]) {
					t.Errorf("%s method %d: first %d bytes from %d packed: %v", archive, m.method, n, len(src), err)
				}
			}
		}
	}
}
