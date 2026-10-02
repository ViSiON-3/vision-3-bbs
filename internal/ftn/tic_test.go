package ftn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleTIC = "Area TQW_LINUXFILES\r\n" +
	"Areadesc Linux files\r\n" +
	"Origin 1337:1/100\r\n" +
	"From 1337:1/100@tqwnet\r\n" +
	"To Some Sysop, 1337:3/150\r\n" +
	"File TOOLS.ZIP\r\n" +
	"Lfile tools-1.2.zip\r\n" +
	"Size 1234\r\n" +
	"Crc 0a1b2c3d\r\n" +
	"Desc Handy   tools\r\n" +
	"Ldesc Handy tools v1.2\r\n" +
	"Ldesc   for Linux\r\n" +
	"Path 1337:1/100 1700000000 Mon Nov 14 22:13:20 2023 UTC\r\n" +
	"Seenby 1337:1/100\r\n" +
	"Seenby 1337:3/150\r\n" +
	"Pw SECRET\r\n" +
	"Magic TOOLS\r\n"

func TestParseTIC(t *testing.T) {
	tic, err := ParseTIC(strings.NewReader(sampleTIC))
	if err != nil {
		t.Fatalf("ParseTIC: %v", err)
	}
	checks := []struct{ name, got, want string }{
		{"Area", tic.Area, "TQW_LINUXFILES"},
		{"AreaDesc", tic.AreaDesc, "Linux files"},
		{"Origin", tic.Origin, "1337:1/100"},
		{"From", tic.From, "1337:1/100@tqwnet"},
		{"File", tic.File, "TOOLS.ZIP"},
		{"LongName", tic.LongName, "tools-1.2.zip"},
		{"Password", tic.Password, "SECRET"},
		// Only the separator is collapsed; spacing inside a value is kept.
		{"Desc", strings.Join(tic.Desc, "|"), "Handy   tools"},
		{"Description", tic.Description(), "Handy tools v1.2\nfor Linux"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if tic.Size != 1234 {
		t.Errorf("Size = %d, want 1234", tic.Size)
	}
	if !tic.HasCRC || tic.CRC != 0x0A1B2C3D {
		t.Errorf("CRC = %08X (has %v), want 0A1B2C3D", tic.CRC, tic.HasCRC)
	}
	if len(tic.SeenBy) != 2 || len(tic.Path) != 1 {
		t.Errorf("SeenBy %d, Path %d; want 2 and 1", len(tic.SeenBy), len(tic.Path))
	}
	addr, err := tic.FromAddress()
	if err != nil || addr.String() != "1337:1/100" {
		t.Errorf("FromAddress = %v, %v; want 1337:1/100", addr, err)
	}
}

func TestParseTICKeywordsAreCaseInsensitive(t *testing.T) {
	tic, err := ParseTIC(strings.NewReader("AREA FSX_DAT\nFILE A.ZIP\nFULLNAME a-long-name.zip\nCRC FFFFFFFF\n"))
	if err != nil {
		t.Fatal(err)
	}
	if tic.Area != "FSX_DAT" || tic.File != "A.ZIP" || tic.LongName != "a-long-name.zip" || tic.CRC != 0xFFFFFFFF {
		t.Errorf("got %+v", tic)
	}
	if tic.Size != -1 {
		t.Errorf("Size = %d with no Size line, want -1", tic.Size)
	}
}

// The single description is used when there is no long one.
func TestTICDescriptionFallsBackToDesc(t *testing.T) {
	tic, err := ParseTIC(strings.NewReader("Area X\nFile A.ZIP\nCrc 0\nDesc One line\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := tic.Description(); got != "One line" {
		t.Errorf("Description = %q", got)
	}
}

func TestParseTICRejectsUnusable(t *testing.T) {
	cases := map[string]string{
		"no area":  "File A.ZIP\nCrc 0\n",
		"no file":  "Area X\nCrc 0\n",
		"no crc":   "Area X\nFile A.ZIP\n",
		"bad crc":  "Area X\nFile A.ZIP\nCrc nothex\n",
		"bad size": "Area X\nFile A.ZIP\nCrc 0\nSize -3\n",
	}
	for name, body := range cases {
		tic, err := ParseTIC(strings.NewReader(body))
		if err == nil {
			t.Errorf("%s: ParseTIC accepted %q", name, body)
		}
		// What was read is still returned, so the file can be set aside.
		if tic == nil {
			t.Errorf("%s: no partial TIC returned", name)
		}
	}
}

func TestFileCRC32(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("123456789"), 0644); err != nil {
		t.Fatal(err)
	}
	crc, err := FileCRC32(path)
	if err != nil {
		t.Fatal(err)
	}
	// The standard CRC-32 check value.
	if got := FormatCRC32(crc); got != "CBF43926" {
		t.Errorf("CRC = %s, want CBF43926", got)
	}
}

func TestParseTICReplaces(t *testing.T) {
	tic, err := ParseTIC(strings.NewReader("Area X\nFile NODELIST.Z19\nCrc 0\nReplaces NODELIST.*\nREPLACES old[1].zip\nReplaces\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tic.Replaces, "|") != "NODELIST.*|old[1].zip" {
		t.Errorf("Replaces = %q", tic.Replaces)
	}
	cases := []struct {
		name string
		want bool
	}{
		{"NODELIST.Z12", true},
		{"nodelist.z12", true}, // case-insensitive
		{"NODELIST", false},
		{"NODEDIFF.Z12", false},
		{"OLD[1].ZIP", true}, // brackets are literal, not a character class
		{"old1.zip", false},
	}
	for _, c := range cases {
		if got := tic.ReplacesFile(c.name); got != c.want {
			t.Errorf("ReplacesFile(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTICReplacesWildcards(t *testing.T) {
	tic := &TIC{Replaces: []string{"FSXNET.Z??"}}
	for name, want := range map[string]bool{"FSXNET.Z75": true, "fsxnet.z82": true, "FSXNET.Z7": false, "FSXNET.ZIP7": false} {
		if got := tic.ReplacesFile(name); got != want {
			t.Errorf("ReplacesFile(%q) = %v, want %v", name, got, want)
		}
	}
	if (&TIC{}).ReplacesFile("ANY.ZIP") {
		t.Error("a TIC with no Replaces line replaces nothing")
	}
}
