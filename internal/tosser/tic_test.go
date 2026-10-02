package tosser

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/google/uuid"
)

// ticEnv is a tosser for "testnet" (link 21:4/158) with file areas: LINUX is
// fed by file echo TQW_LINUXFILES on testnet, OTHER by the same tag on
// another network, LOCAL by nothing. inboundDir is the secure inbound;
// unsecuredDir is the inbound sessions without a password write to.
type ticEnv struct {
	*testEnv
	files        *file.FileManager
	tosser       *Tosser
	unsecuredDir string
}

func setupTICEnv(t *testing.T, link linkConfig) *ticEnv {
	t.Helper()
	env := setupTestEnv(t)
	areas := []file.FileArea{
		{ID: 1, Tag: "LINUX", Name: "Linux", Path: "linux", Network: "testnet", FileEcho: "TQW_LINUXFILES"},
		{ID: 2, Tag: "OTHER", Name: "Other", Path: "other", Network: "othernet", FileEcho: "TQW_LINUXFILES"},
		{ID: 3, Tag: "LOCAL", Name: "Local", Path: "local"},
	}
	data, _ := json.Marshal(areas)
	if err := os.WriteFile(filepath.Join(env.configDir, "file_areas.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	fm, err := file.NewFileManager(env.dataDir, env.configDir)
	if err != nil {
		t.Fatalf("NewFileManager: %v", err)
	}
	unsecured := env.inboundDir
	secure := filepath.Join(env.dataDir, "ftn", "secure_in")
	if err := os.MkdirAll(secure, 0755); err != nil {
		t.Fatal(err)
	}
	env.globalCfg.SecureInboundPath = secure
	env.inboundDir = secure

	env.netCfg.Links = []linkConfig{link}
	tsr, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tsr.SetFileAreas(fm)
	return &ticEnv{testEnv: env, files: fm, tosser: tsr, unsecuredDir: unsecured}
}

func hub() linkConfig { return linkConfig{Address: "21:4/158", Name: "Hub"} }

// writeTIC drops a file and its TIC into the inbound. extra lines are added
// to the TIC verbatim; a "Crc" or "Size" line there replaces the correct one.
func writeTIC(t *testing.T, dir, ticName, fileName, content string, extra ...string) (ticPath, filePath string) {
	t.Helper()
	filePath = filepath.Join(dir, fileName)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		"Area TQW_LINUXFILES",
		"Origin 21:4/158",
		"From 21:4/158",
		"File " + strings.ToUpper(fileName),
		"Desc Some tools",
	}
	hasCRC, hasSize := false, false
	for _, e := range extra {
		hasCRC = hasCRC || strings.HasPrefix(e, "Crc ")
		hasSize = hasSize || strings.HasPrefix(e, "Size ")
	}
	if !hasCRC {
		lines = append(lines, fmt.Sprintf("Crc %08X", crc32.ChecksumIEEE([]byte(content))))
	}
	if !hasSize {
		lines = append(lines, fmt.Sprintf("Size %d", len(content)))
	}
	lines = append(lines, extra...)
	ticPath = filepath.Join(dir, ticName)
	if err := os.WriteFile(ticPath, []byte(strings.Join(lines, "\r\n")+"\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return ticPath, filePath
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (e *ticEnv) areaFile(area, name string) string {
	return filepath.Join(e.dataDir, "files", area, name)
}

func TestTICDeliversFileIntoLinkedArea(t *testing.T) {
	e := setupTICEnv(t, hub())
	ticPath, filePath := writeTIC(t, e.inboundDir, "abc00001.tic", "tools.zip", "zipdata",
		"Ldesc Some tools", "Ldesc for Linux")

	result := e.tosser.ProcessInbound()

	if result.FilesImported != 1 || len(result.Errors) != 0 {
		t.Fatalf("FilesImported = %d, errors %v; want 1 and none", result.FilesImported, result.Errors)
	}
	if exists(ticPath) || exists(filePath) {
		t.Error("the TIC or its file is still in the inbound")
	}
	if !exists(e.areaFile("linux", "tools.zip")) {
		t.Fatal("the file was not moved into the LINUX area")
	}
	recs := e.files.GetFilesForArea(1)
	if len(recs) != 1 {
		t.Fatalf("LINUX has %d records, want 1", len(recs))
	}
	r := recs[0]
	if r.Filename != "tools.zip" || r.Size != 7 || r.Description != "Some tools\nfor Linux" ||
		r.UploadedBy != "21:4/158" || !r.Reviewed || r.CRC32 != fmt.Sprintf("%08X", crc32.ChecksumIEEE([]byte("zipdata"))) {
		t.Errorf("record = %+v", r)
	}
	if n := len(e.files.GetFilesForArea(2)); n != 0 {
		t.Errorf("the same tag on another network got %d records", n)
	}
}

func TestTICDuplicateIsDropped(t *testing.T) {
	e := setupTICEnv(t, hub())
	writeTIC(t, e.inboundDir, "a.tic", "tools.zip", "zipdata")
	e.tosser.ProcessInbound()

	ticPath, filePath := writeTIC(t, e.inboundDir, "b.tic", "tools.zip", "zipdata")
	result := e.tosser.ProcessInbound()

	if result.FilesDuped != 1 || result.FilesImported != 0 {
		t.Fatalf("FilesDuped = %d, FilesImported = %d; want 1 and 0", result.FilesDuped, result.FilesImported)
	}
	if exists(ticPath) || exists(filePath) {
		t.Error("the duplicate was left in the inbound")
	}
	if n := len(e.files.GetFilesForArea(1)); n != 1 {
		t.Errorf("LINUX has %d records, want 1", n)
	}
}

// Same name, new content: a new version replaces the old file and keeps its
// record, so the download count survives.
func TestTICNewVersionReplacesFile(t *testing.T) {
	e := setupTICEnv(t, hub())
	writeTIC(t, e.inboundDir, "a.tic", "nodelist.zip", "week one")
	e.tosser.ProcessInbound()
	old := e.files.GetFilesForArea(1)[0]
	if err := e.files.IncrementDownloadCount(old.ID); err != nil {
		t.Fatal(err)
	}

	writeTIC(t, e.inboundDir, "b.tic", "nodelist.zip", "week two!", "Desc Week two")
	result := e.tosser.ProcessInbound()

	if result.FilesImported != 1 || len(result.Errors) != 0 {
		t.Fatalf("FilesImported = %d, errors %v", result.FilesImported, result.Errors)
	}
	recs := e.files.GetFilesForArea(1)
	if len(recs) != 1 || recs[0].ID != old.ID {
		t.Fatalf("records = %+v, want the original record updated", recs)
	}
	if recs[0].Size != 9 || recs[0].DownloadCount != 1 || !strings.Contains(recs[0].Description, "Week two") {
		t.Errorf("record = %+v", recs[0])
	}
	got, _ := os.ReadFile(e.areaFile("linux", "nodelist.zip"))
	if string(got) != "week two!" {
		t.Errorf("area file holds %q, want the new version", got)
	}
}

func TestTICRejections(t *testing.T) {
	cases := []struct {
		name  string
		link  linkConfig
		extra []string
		want  string
	}{
		{"bad CRC", hub(), []string{"Crc DEADBEEF"}, "CRC"},
		{"bad size", hub(), []string{"Size 99"}, "bytes"},
		{"wrong password", linkConfig{Address: "21:4/158", TICPassword: "secret"}, []string{"Pw nope"}, "password"},
		{"missing password", linkConfig{Address: "21:4/158", TICPassword: "secret"}, nil, "password"},
		{"no linked area", hub(), []string{"Area TQW_NOSUCH"}, "no file area"},
		// A malformed line: the TIC is unusable, and still takes its file.
		{"unparsable", hub(), []string{"Size lots"}, "unusable TIC"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := setupTICEnv(t, c.link)
			ticPath, filePath := writeTIC(t, e.inboundDir, "a.tic", "tools.zip", "zipdata", c.extra...)

			result := e.tosser.ProcessInbound()

			if result.FilesBad != 1 || result.FilesImported != 0 {
				t.Fatalf("FilesBad = %d, FilesImported = %d; want 1 and 0", result.FilesBad, result.FilesImported)
			}
			if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], c.want) {
				t.Errorf("errors = %v, want one mentioning %q", result.Errors, c.want)
			}
			bad := filepath.Join(e.tempDir, BadTICDirName)
			if exists(ticPath) || exists(filePath) {
				t.Error("the rejected TIC or its file is still in the inbound")
			}
			if !exists(filepath.Join(bad, "a.tic")) || !exists(filepath.Join(bad, "tools.zip")) {
				t.Error("the TIC and its file were not both moved to the bad TIC directory")
			}
			if n := len(e.files.GetFilesForArea(1)); n != 0 {
				t.Errorf("a rejected file got %d records", n)
			}
		})
	}
}

// A later Area line wins in ParseTIC, which is what lets the case above
// override the area; this pins the password match as case-insensitive.
func TestTICPasswordIsCaseInsensitive(t *testing.T) {
	e := setupTICEnv(t, linkConfig{Address: "21:4/158", TICPassword: "Secret"})
	writeTIC(t, e.inboundDir, "a.tic", "tools.zip", "zipdata", "Pw SECRET")
	if result := e.tosser.ProcessInbound(); result.FilesImported != 1 {
		t.Fatalf("FilesImported = %d, errors %v", result.FilesImported, result.Errors)
	}
}

// A TIC naming a path, or the area's own metadata, never reaches the disk
// outside the inbound or overwrites the file list.
func TestTICUnsafeFileNameIsNeverDelivered(t *testing.T) {
	for _, name := range []string{"../../escape.zip", "metadata.json"} {
		e := setupTICEnv(t, hub())
		// The file the name would refer to, so only the name check stops it.
		_ = os.WriteFile(filepath.Join(e.inboundDir, "metadata.json"), []byte("[]"), 0644)
		tic := "Area TQW_LINUXFILES\r\nFrom 21:4/158\r\nFile " + name + "\r\nCrc 0\r\n"
		ticPath := filepath.Join(e.inboundDir, "a.tic")
		if err := os.WriteFile(ticPath, []byte(tic), 0644); err != nil {
			t.Fatal(err)
		}

		result := e.tosser.ProcessInbound()

		if result.FilesImported != 0 {
			t.Errorf("%s: a file was delivered", name)
		}
		if len(result.WaitingTICs) != 1 {
			t.Errorf("%s: WaitingTICs = %v, want the TIC treated as waiting for a file it cannot match", name, result.WaitingTICs)
		}
	}
}

// A TIC whose file has not arrived waits, and is claimed rather than
// unclaimed; once it is old enough it is moved aside.
func TestTICWaitsForItsFile(t *testing.T) {
	e := setupTICEnv(t, hub())
	ticPath, filePath := writeTIC(t, e.inboundDir, "a.tic", "tools.zip", "zipdata")
	if err := os.Remove(filePath); err != nil {
		t.Fatal(err)
	}

	result := e.tosser.ProcessInbound()
	if len(result.WaitingTICs) != 1 || result.WaitingTICs[0] != ticPath || len(result.Errors) != 0 {
		t.Fatalf("WaitingTICs = %v, errors %v", result.WaitingTICs, result.Errors)
	}
	if !exists(ticPath) {
		t.Fatal("a waiting TIC was removed")
	}

	old := time.Now().Add(-ticWaitFor - time.Hour)
	if err := os.Chtimes(ticPath, old, old); err != nil {
		t.Fatal(err)
	}
	result = e.tosser.ProcessInbound()
	if result.FilesBad != 1 || exists(ticPath) {
		t.Errorf("FilesBad = %d, TIC still present %v; want a stale TIC moved aside", result.FilesBad, exists(ticPath))
	}
}

// A TIC from an address none of this network's links have is left for
// another network, and reported if no network takes it.
func TestTICFromUnknownLinkIsSkippedAndReportedUnclaimed(t *testing.T) {
	e := setupTICEnv(t, hub())
	ticPath, filePath := writeTIC(t, e.inboundDir, "a.tic", "tools.zip", "zipdata")
	body, _ := os.ReadFile(ticPath)
	body = []byte(strings.ReplaceAll(string(body), "From 21:4/158", "From 1337:3/123@tqwnet"))
	if err := os.WriteFile(ticPath, body, 0644); err != nil {
		t.Fatal(err)
	}

	result := e.tosser.ProcessInbound()
	if got := result.SkippedByFile[ticPath]["1337:3/123"]; got != 1 {
		t.Fatalf("SkippedByFile = %v, want the TIC's origin recorded", result.SkippedByFile)
	}
	if !exists(ticPath) || !exists(filePath) {
		t.Fatal("a foreign TIC was touched")
	}

	report := FindUnclaimed(e.globalCfg, result.SkippedByFile)
	if len(report.Files) != 1 || report.Files[0] != ticPath {
		t.Fatalf("unclaimed = %v, want the TIC", report.Files)
	}
	old := time.Now().Add(-quarantineAfter - time.Hour)
	if err := os.Chtimes(ticPath, old, old); err != nil {
		t.Fatal(err)
	}
	report.QuarantineStale(e.tempDir)
	dest := filepath.Join(e.tempDir, UnclaimedDirName)
	if !exists(filepath.Join(dest, "a.tic")) || !exists(filepath.Join(dest, "tools.zip")) {
		t.Error("the unclaimed TIC was not quarantined together with its file")
	}
}

// Without file areas the tosser leaves TICs alone, and an unexamined TIC is
// not reported as unclaimed.
func TestTICIgnoredWithoutFileAreas(t *testing.T) {
	env := setupTestEnv(t)
	tsr, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatal(err)
	}
	ticPath, _ := writeTIC(t, env.inboundDir, "a.tic", "tools.zip", "zipdata")

	result := tsr.ProcessInbound()
	if !exists(ticPath) || result.FilesImported != 0 {
		t.Fatal("a TIC was processed without file areas")
	}
	if r := FindUnclaimed(env.globalCfg, result.SkippedByFile); len(r.Files) != 0 {
		t.Errorf("unclaimed = %v, want none", r.Files)
	}
}

// realZip writes a genuine ZIP archive holding one non-packet file, which
// passes the ZIP magic check a mail bundle is recognised by.
func realZip(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("README.TXT")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("not mail"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// A file echo's .zip is never tossed as a mail bundle — that deleted it —
// even when its TIC belongs to another network or this run has no file areas.
func TestTICFileIsNotTossedAsBundle(t *testing.T) {
	env := setupTestEnv(t)
	tsr, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(env.inboundDir, "tools.zip")
	realZip(t, zipPath)
	tic := "Area TQW_LINUXFILES\r\nFrom 1337:3/123\r\nFile TOOLS.ZIP\r\n"
	if err := os.WriteFile(filepath.Join(env.inboundDir, "a.tic"), []byte(tic), 0644); err != nil {
		t.Fatal(err)
	}

	tsr.ProcessInbound()

	if !exists(zipPath) {
		t.Fatal("the file echo's zip was tossed as a bundle and deleted")
	}
}

// A ZIP holding no packets is not mail; it is left in place, not deleted.
func TestZIPWithoutPacketsIsLeftInPlace(t *testing.T) {
	env := setupTestEnv(t)
	tsr, err := New("testnet", env.netCfg, env.globalCfg, env.dupeDB, env.msgMgr)
	if err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(env.inboundDir, "stray.zip")
	realZip(t, zipPath)

	tsr.ProcessInbound()

	if !exists(zipPath) {
		t.Fatal("a ZIP with no packets in it was deleted")
	}
}

// A TIC from the unsecured inbound is believed only with a password: its
// From line is just text.
func TestTICFromUnsecuredInboundNeedsPassword(t *testing.T) {
	e := setupTICEnv(t, hub())
	ticPath, filePath := writeTIC(t, e.unsecuredDir, "a.tic", "tools.zip", "zipdata")

	result := e.tosser.ProcessInbound()

	if result.FilesBad != 1 || result.FilesImported != 0 || exists(ticPath) || exists(filePath) {
		t.Fatalf("FilesBad = %d, FilesImported = %d; want the TIC and its file moved aside", result.FilesBad, result.FilesImported)
	}
	if !strings.Contains(strings.Join(result.Errors, " "), "unsecured inbound") {
		t.Errorf("errors = %v", result.Errors)
	}

	withPw := setupTICEnv(t, linkConfig{Address: "21:4/158", TICPassword: "pw"})
	writeTIC(t, withPw.unsecuredDir, "a.tic", "tools.zip", "zipdata", "Pw pw")
	if r := withPw.tosser.ProcessInbound(); r.FilesImported != 1 {
		t.Errorf("a TIC with the right password was refused from the unsecured inbound: %v", r.Errors)
	}
}

func TestTICWithoutCRCIsRejected(t *testing.T) {
	e := setupTICEnv(t, hub())
	_, filePath := writeTIC(t, e.inboundDir, "a.tic", "tools.zip", "zipdata", "Crc ")
	result := e.tosser.ProcessInbound()
	if result.FilesBad != 1 || exists(filePath) {
		t.Errorf("FilesBad = %d; want a TIC without a usable Crc rejected with its file", result.FilesBad)
	}
}

// failingRecords is a file area store whose record writes fail.
type failingRecords struct{ *file.FileManager }

func (failingRecords) AddFileRecord(file.FileRecord) error { return errors.New("disk full") }
func (failingRecords) UpdateFileRecord(uuid.UUID, func(*file.FileRecord)) error {
	return errors.New("disk full")
}

// When the record cannot be written the file goes back to the inbound, so
// the next toss retries it, and a file it replaced is restored.
func TestTICRecordFailureRollsBack(t *testing.T) {
	e := setupTICEnv(t, hub())
	writeTIC(t, e.inboundDir, "a.tic", "nodelist.zip", "week one")
	e.tosser.ProcessInbound()

	e.tosser.SetFileAreas(failingRecords{e.files})
	ticPath, filePath := writeTIC(t, e.inboundDir, "b.tic", "nodelist.zip", "week two!")
	result := e.tosser.ProcessInbound()

	if len(result.Errors) != 1 || result.FilesImported != 0 {
		t.Fatalf("errors = %v, FilesImported = %d", result.Errors, result.FilesImported)
	}
	if !exists(ticPath) || !exists(filePath) {
		t.Error("the TIC and its file are not both back in the inbound for a retry")
	}
	got, _ := os.ReadFile(e.areaFile("linux", "nodelist.zip"))
	if string(got) != "week one" {
		t.Errorf("area file holds %q, want the old version restored", got)
	}

	e.tosser.SetFileAreas(e.files)
	if r := e.tosser.ProcessInbound(); r.FilesImported != 1 {
		t.Errorf("retry: FilesImported = %d, errors %v", r.FilesImported, r.Errors)
	}
}

// A replacement is network content like a new file: reviewed.
func TestTICReplacementIsReviewed(t *testing.T) {
	e := setupTICEnv(t, hub())
	writeTIC(t, e.inboundDir, "a.tic", "nodelist.zip", "week one")
	e.tosser.ProcessInbound()
	id := e.files.GetFilesForArea(1)[0].ID
	if err := e.files.UpdateFileRecord(id, func(r *file.FileRecord) { r.Reviewed = false }); err != nil {
		t.Fatal(err)
	}
	writeTIC(t, e.inboundDir, "b.tic", "nodelist.zip", "week two!")
	e.tosser.ProcessInbound()
	if !e.files.GetFilesForArea(1)[0].Reviewed {
		t.Error("the replaced record is still unreviewed")
	}
}
