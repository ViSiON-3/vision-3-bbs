package lha

import (
	"bytes"
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
