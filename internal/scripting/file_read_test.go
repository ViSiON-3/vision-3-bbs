package scripting

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/google/uuid"
)

// fileReadHarness builds two areas, ART (open) and PRIV (whose download ACS
// the caller fails), with an artpack and a text file in ART and a text file
// in PRIV, and an engine whose CanDownload refuses PRIV.
func fileReadHarness(t *testing.T, can func(*file.FileArea) bool) *scriptHarness {
	t.Helper()
	return newHarness(t, harnessOpts{providers: fileAreasProviders(t, can)})
}

// fileAreasProviders builds the file manager behind fileReadHarness.
func fileAreasProviders(t *testing.T, can func(*file.FileArea) bool) *Providers {
	t.Helper()
	dataDir, cfgDir := t.TempDir(), t.TempDir()
	areas := `[{"id": 1, "tag": "ART", "name": "Art", "path": "art"},
	           {"id": 2, "tag": "PRIV", "name": "Private", "path": "priv", "acs_download": "s255"}]`
	if err := os.WriteFile(filepath.Join(cfgDir, "file_areas.json"), []byte(areas), 0o644); err != nil {
		t.Fatal(err)
	}
	fm, err := file.NewFileManager(dataDir, cfgDir)
	if err != nil {
		t.Fatalf("NewFileManager: %v", err)
	}
	write := func(area, name string, data []byte) {
		dir := filepath.Join(dataDir, "files", area)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// ACID.ZIP: a piece of ANSI art (CP437 shading, an escape sequence) and
	// an empty directory
	var zbuf strings.Builder
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.CreateHeader(&zip.FileHeader{Name: "ACID-01.ANS", Method: zip.Deflate, Modified: time.Unix(800000000, 0)})
	_, _ = w.Write([]byte("\x1b[1;35m\xb0\xb1\xb2\xdb\x1b[0m\r\n"))
	_, _ = zw.Create("FONTS/")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	write("art", "ACID.ZIP", []byte(zbuf.String()))
	write("art", "README.TXT", []byte("caf\x82 \xc9\xcd\xbb\r\n"))
	write("priv", "SECRET.TXT", []byte("not for you"))

	up := time.Unix(1700000000, 0)
	for _, r := range []file.FileRecord{
		{ID: uuid.New(), AreaID: 1, Filename: "ACID.ZIP", UploadedAt: up},
		{ID: uuid.New(), AreaID: 1, Filename: "README.TXT", UploadedAt: up},
		{ID: uuid.New(), AreaID: 2, Filename: "SECRET.TXT", UploadedAt: up},
	} {
		if err := fm.AddFileRecord(r); err != nil {
			t.Fatalf("AddFileRecord: %v", err)
		}
	}
	return &Providers{FileMgr: fm, CanDownload: can}
}

func TestFileRead(t *testing.T) {
	h := fileReadHarness(t, func(a *file.FileArea) bool { return a.ACSDownload == "" })
	tests := []struct{ expr, want string }{
		// CP437 high bytes become their characters; everything else is kept
		{`v3.file.read(1, "README.TXT")`, "café ╔═╗\r\n"},
		{`v3.file.read(1, "readme.txt").length`, "10"},
		{`v3.file.members(1, "ACID.ZIP").map(function(m){return m.name+":"+m.size+":"+m.isDir}).join(",")`,
			"ACID-01.ANS:17:false,FONTS/:0:true"},
		{`v3.file.members(1, "ACID.ZIP")[0].date`, "800000000"},
		{`v3.file.readMember(1, "ACID.ZIP", "ACID-01.ANS")`, "\x1b[1;35m░▒▓█\x1b[0m\r\n"},
		// not a ZIP, no such member, a directory, no such file
		{`String(v3.file.members(1, "README.TXT"))`, "null"},
		{`String(v3.file.readMember(1, "ACID.ZIP", "NOPE.ANS"))`, "null"},
		{`String(v3.file.readMember(1, "ACID.ZIP", "FONTS/"))`, "null"},
		{`String(v3.file.read(1, "MISSING.TXT"))`, "null"},
		{`String(v3.file.read(99, "README.TXT"))`, "null"},
		{`String(v3.file.read(1))`, "null"},
		// the caller can't download from PRIV
		{`String(v3.file.read(2, "SECRET.TXT"))`, "null"},
	}
	for _, tt := range tests {
		if got := h.eval(tt.expr).String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.expr, got, tt.want)
		}
	}
}

// With no CanDownload there is nothing to read.
func TestFileReadNeedsACSCheck(t *testing.T) {
	h := fileReadHarness(t, nil)
	for _, expr := range []string{`v3.file.read(1, "README.TXT")`, `v3.file.members(1, "ACID.ZIP")`,
		`v3.file.readMember(1, "ACID.ZIP", "ACID-01.ANS")`} {
		if got := h.eval("String(" + expr + ")").String(); got != "null" {
			t.Errorf("%s without CanDownload = %q, want null", expr, got)
		}
	}
}

func TestReadCapped(t *testing.T) {
	if _, ok := readCapped(strings.NewReader(strings.Repeat("x", maxScriptRead))); !ok {
		t.Error("a file of exactly the cap was refused")
	}
	if _, ok := readCapped(strings.NewReader(strings.Repeat("x", maxScriptRead+1))); ok {
		t.Error("a file over the cap was read")
	}
}

// readMember caps what it unpacks at 4 MB, whatever the entry's header says:
// a member that unpacks past the cap is refused even when its header claims
// it is small.
func TestReadMemberDecompressedCap(t *testing.T) {
	p := fileAreasProviders(t, func(a *file.FileArea) bool { return true })
	big := bytes.Repeat([]byte{' '}, maxScriptRead+1)
	var packed bytes.Buffer
	fw, _ := flate.NewWriter(&packed, flate.BestCompression)
	_, _ = fw.Write(big)
	_ = fw.Close()

	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("HONEST.TXT")
	_, _ = w.Write(big)
	// LIAR.TXT's header claims 10 bytes; the data unpacks past the cap.
	raw, err := zw.CreateRaw(&zip.FileHeader{Name: "LIAR.TXT", Method: zip.Deflate,
		CRC32: crc32.ChecksumIEEE(big), CompressedSize64: uint64(packed.Len()), UncompressedSize64: 10})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = raw.Write(packed.Bytes())
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path, err := p.FileMgr.GetFilePath(p.FileMgr.GetFilesForArea(1)[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "BOMB.ZIP"), zbuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.FileMgr.AddFileRecord(file.FileRecord{ID: uuid.New(), AreaID: 1, Filename: "BOMB.ZIP", UploadedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, harnessOpts{providers: p})
	for _, member := range []string{"HONEST.TXT", "LIAR.TXT"} {
		expr := `String(v3.file.readMember(1, "BOMB.ZIP", "` + member + `"))`
		if got := h.eval(expr).String(); got != "null" {
			t.Errorf("%s returned %d characters, want null", expr, len(got))
		}
	}
}
