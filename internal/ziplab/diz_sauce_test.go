package ziplab

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/archiver"
)

// sauceRecord builds a 128-byte SAUCE record declaring the given number of
// comment lines.
func sauceRecord(comments int) []byte {
	rec := make([]byte, 128)
	copy(rec, "SAUCE00")
	copy(rec[7:], "Title of the art")
	rec[104] = byte(comments)
	return rec
}

// commentBlock builds the COMNT block that precedes a SAUCE record.
func commentBlock(lines int) []byte {
	block := append([]byte("COMNT"), bytes.Repeat([]byte{' '}, lines*64)...)
	copy(block[5:], "drawn by someone")
	return block
}

func TestStripSauceMetadata(t *testing.T) {
	art := []byte("\x1b[1;31mFILE ART\x1b[0m\r\nline two")
	join := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	eof := []byte{0x1A}

	tests := []struct {
		name string
		in   []byte
		want []byte
	}{
		{"shorter than a signature", []byte("abc"), []byte("abc")},
		{"no SAUCE record", art, art},
		{"record after EOF marker", join(art, eof, sauceRecord(0)), art},
		{"record without EOF marker", join(art, sauceRecord(0)), art},
		{"record with comment block", join(art, eof, commentBlock(2), sauceRecord(2)), art},
		{
			// The record claims comments but no COMNT block precedes it:
			// only the record itself is removed.
			name: "comment count without a block",
			in:   join(art, eof, sauceRecord(3)),
			want: art,
		},
		{
			name: "truncated record keeps nothing after the signature",
			in:   join(art, eof, []byte("SAUCE00 partial")),
			want: art,
		},
		{
			// A "SAUCE00" far from the end is message text, not metadata.
			name: "signature more than 512 bytes from the end",
			in:   join([]byte("see SAUCE00 spec\r\n"), bytes.Repeat([]byte("x"), 600)),
			want: join([]byte("see SAUCE00 spec\r\n"), bytes.Repeat([]byte("x"), 600)),
		},
		{"nothing but a record", sauceRecord(0), []byte{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripSauceMetadata(tt.in); !bytes.Equal(got, tt.want) {
				t.Errorf("stripSauceMetadata = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCleanDIZ_ConvertsCP437(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain ASCII", "Just text", "Just text"},
		{"CP437 shading and blocks", "\xb0\xb1\xb2 \xdb\xdb\xdb", "░▒▓ ███"},
		{"already UTF-8", "café ░", "café ░"},
		{"UTF-8 and CP437 mixed", "caf\xc3\xa9 \xdb\xdb", "café ██"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanDIZ(tt.in + " \r\n\x1a"); got != tt.want {
				t.Errorf("cleanDIZ(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExtractDIZFromZip_PrefersANSAndStripsSauce(t *testing.T) {
	ans := string(bytes.Join([][]byte{[]byte("\xdb ANSI description \xdb"), {0x1A}, sauceRecord(0)}, nil))

	// FILE_ID.ANS wins whether it comes before or after FILE_ID.DIZ.
	for _, order := range [][]zipEntry{
		{{"FILE_ID.DIZ", "plain description"}, {"file_id.ans", ans}},
		{{"file_id.ans", ans}, {"FILE_ID.DIZ", "plain description"}},
	} {
		zipPath := filepath.Join(t.TempDir(), "art.zip")
		writeOrderedZip(t, zipPath, "", order)
		got, err := ExtractDIZFromZip(zipPath)
		if err != nil {
			t.Fatalf("ExtractDIZFromZip: %v", err)
		}
		if got != "█ ANSI description █" {
			t.Errorf("entries %s: description = %q, want the ANS text without SAUCE", entryNames(order), got)
		}
	}
}

func TestFindAndReadDIZ(t *testing.T) {
	p := NewProcessor(DefaultConfig(), "")
	write := func(t *testing.T, root, rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("ANS preferred over DIZ", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "FILE_ID.DIZ", "plain")
		write(t, dir, "File_Id.Ans", "ansi art\x1a"+string(sauceRecord(0)))
		if got := p.findAndReadDIZ(dir); got != "ansi art" {
			t.Errorf("description = %q, want the ANS text", got)
		}
	})

	t.Run("found in a subdirectory", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "release/file_id.diz", "nested description\r\n")
		if got := p.findAndReadDIZ(dir); got != "nested description" {
			t.Errorf("description = %q, want the nested DIZ", got)
		}
	})

	t.Run("too deep to be the archive's own", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "a/b/c/FILE_ID.DIZ", "belongs to a bundled archive")
		if got := p.findAndReadDIZ(dir); got != "" {
			t.Errorf("description = %q, want none", got)
		}
	})

	t.Run("missing work directory", func(t *testing.T) {
		if got := p.findAndReadDIZ(filepath.Join(t.TempDir(), "gone")); got != "" {
			t.Errorf("description = %q, want none", got)
		}
	})
}

func TestExtractDIZFromArchive(t *testing.T) {
	t.Run("native zip with no config files", func(t *testing.T) {
		zipPath := filepath.Join(t.TempDir(), "up.ZIP")
		writeOrderedZip(t, zipPath, "", []zipEntry{{"FILE_ID.DIZ", "Zip description\r\n"}})
		got, err := ExtractDIZFromArchive(zipPath, t.TempDir())
		if err != nil || got != "Zip description" {
			t.Errorf("ExtractDIZFromArchive = %q, %v; want the DIZ text", got, err)
		}
	})

	t.Run("broken ziplab.json falls back to defaults", func(t *testing.T) {
		cfgDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(cfgDir, "ziplab.json"), []byte("{broken"), 0644); err != nil {
			t.Fatal(err)
		}
		zipPath := filepath.Join(t.TempDir(), "up.zip")
		writeOrderedZip(t, zipPath, "", []zipEntry{{"FILE_ID.DIZ", "Still described"}})
		got, err := ExtractDIZFromArchive(zipPath, cfgDir)
		if err != nil || got != "Still described" {
			t.Errorf("ExtractDIZFromArchive = %q, %v; want the DIZ text", got, err)
		}
	})

	t.Run("unsupported extension", func(t *testing.T) {
		got, err := ExtractDIZFromArchive(filepath.Join(t.TempDir(), "notes.txt"), t.TempDir())
		if err != nil || got != "" {
			t.Errorf("ExtractDIZFromArchive = %q, %v; want empty and no error", got, err)
		}
	})

	// External formats are described by archivers.json in the config dir.
	// Every command is written out, empty or not, so the entry is exactly
	// what the test says it is.
	writeArchivers := func(t *testing.T, unpack archiver.CommandDef) string {
		t.Helper()
		cfgDir := t.TempDir()
		args := unpack.Args
		if args == nil {
			args = []string{}
		}
		cmd := map[string]any{"command": unpack.Command, "args": args}
		none := map[string]any{"command": "", "args": []string{}}
		data, err := json.Marshal(map[string]any{"archivers": []map[string]any{{
			"id": "tst", "name": "Test format", "extension": ".tst", "extensions": []string{},
			"magic": "", "native": false, "enabled": true,
			"pack": none, "unpack": cmd, "test": none, "list": none, "comment": none, "addFile": none,
		}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfgDir, "archivers.json"), data, 0644); err != nil {
			t.Fatal(err)
		}
		return cfgDir
	}
	archive := filepath.Join(t.TempDir(), "upload.tst")
	if err := os.WriteFile(archive, []byte("opaque"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("external format without an extract command", func(t *testing.T) {
		_, err := ExtractDIZFromArchive(archive, writeArchivers(t, archiver.CommandDef{}))
		if err == nil || !strings.Contains(err.Error(), "no extract command configured for .tst") {
			t.Errorf("err = %v, want missing extract command", err)
		}
	})

	t.Run("external format extracted to find the DIZ", func(t *testing.T) {
		unpacker := shellTool(t, "unpack.sh", `printf 'From %s\r\n\032' "$(basename "$1")" > "$2/file_id.diz"; echo "$2" > "$1.outdir"`)
		cfgDir := writeArchivers(t, archiver.CommandDef{Command: unpacker, Args: []string{"{ARCHIVE}", "{OUTDIR}"}})
		got, err := ExtractDIZFromArchive(archive, cfgDir)
		if err != nil || got != "From upload.tst" {
			t.Fatalf("ExtractDIZFromArchive = %q, %v; want the extracted DIZ", got, err)
		}
		// The temporary extraction directory is removed afterwards.
		outDir, err := os.ReadFile(archive + ".outdir")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(strings.TrimSpace(string(outDir))); !os.IsNotExist(err) {
			t.Errorf("work dir %s left behind (stat err = %v)", strings.TrimSpace(string(outDir)), err)
		}
	})

	t.Run("external extract command fails", func(t *testing.T) {
		failing := shellTool(t, "fail.sh", "exit 2")
		_, err := ExtractDIZFromArchive(archive, writeArchivers(t, archiver.CommandDef{Command: failing}))
		if err == nil || !strings.Contains(err.Error(), "extraction failed") {
			t.Errorf("err = %v, want extraction failure", err)
		}
	})
}

func TestParseNFO_SkipsMalformedLines(t *testing.T) {
	content := strings.Join([]string{
		"1D = 45,10,112,116,OK",
		"no equals sign",
		"D = 1,2,3,4,X",             // key too short
		"xD = 1,2,3,4,X",            // step is not a number
		"2X = 1,2,3,4,X",            // unknown status letter
		"2D = 1,2,3,4",              // too few values
		"2D = a,2,3,4,X",            // bad column
		"2D = 1,b,3,4,X",            // bad row
		"2D = 1,2,c,4,X",            // bad normal colour
		"2D = 1,2,3,d,X",            // bad highlight colour
		"3f = 7 , 8 , 9 , 30 , a,b", // lower-case status; display text keeps its comma
	}, "\n")
	path := filepath.Join(t.TempDir(), "ZIPLAB.NFO")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	nfo, err := ParseNFO(path)
	if err != nil {
		t.Fatalf("ParseNFO: %v", err)
	}
	if len(nfo.Entries) != 2 {
		t.Errorf("parsed %d entries, want 2: %+v", len(nfo.Entries), nfo.Entries)
	}
	want := NFOEntry{Step: 3, Status: StatusFail, Col: 7, Row: 8, NormalColor: 9, HiColor: 30, DisplayChars: "a,b"}
	if got, ok := nfo.GetEntry(3, StatusFail); !ok || got != want {
		t.Errorf("entry 3F = %+v (found %v), want %+v", got, ok, want)
	}
	if nfo.HasStep(2) || !nfo.HasStep(1) {
		t.Errorf("HasStep(1)=%v HasStep(2)=%v, want true and false", nfo.HasStep(1), nfo.HasStep(2))
	}

	// A failed step is drawn in the highlight colour: DOS attribute 30 is
	// bright yellow (14) on blue (1).
	if got, want := nfo.BuildStatusSequence(3, StatusFail), "\x1b[8;7H\x1b[1;33;44ma,b\x1b[0m"; got != want {
		t.Errorf("BuildStatusSequence(3, F) = %q, want %q", got, want)
	}
	if got := nfo.BuildStatusSequence(9, StatusPass); got != "" {
		t.Errorf("BuildStatusSequence for an unknown step = %q, want empty", got)
	}
}

func TestParseNFO_LineTooLong(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ZIPLAB.NFO")
	if err := os.WriteFile(path, []byte("1D = 1,2,3,4,"+strings.Repeat("x", 70000)), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseNFO(path); err == nil || !strings.Contains(err.Error(), "error reading NFO file") {
		t.Errorf("ParseNFO = %v, want read error", err)
	}
}
