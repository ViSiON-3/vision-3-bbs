package ziplab

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ViSiON-3/vision-3-bbs/internal/archiver"
)

// zipEntry is one file in an ordered test archive. A name ending in "/" is a
// directory entry.
type zipEntry struct{ Name, Content string }

// writeOrderedZip creates a ZIP with entries in the given order (duplicates
// allowed), stored uncompressed, and an optional archive comment.
func writeOrderedZip(t *testing.T, zipPath, comment string, entries []zipEntry) {
	t.Helper()
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	if err := w.SetComment(comment); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		fw, err := w.CreateHeader(&zip.FileHeader{Name: e.Name, Method: zip.Store})
		if err != nil {
			t.Fatalf("add %s: %v", e.Name, err)
		}
		if _, err := fw.Write([]byte(e.Content)); err != nil {
			t.Fatalf("write %s: %v", e.Name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// readZip returns the archive comment and its entries in order.
func readZip(t *testing.T, zipPath string) (string, []zipEntry) {
	t.Helper()
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("open %s: %v", zipPath, err)
	}
	defer r.Close()
	var entries []zipEntry
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open entry %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read entry %s: %v", f.Name, err)
		}
		entries = append(entries, zipEntry{f.Name, string(data)})
	}
	return r.Comment, entries
}

func entryNames(entries []zipEntry) string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return strings.Join(names, ",")
}

// shellTool writes an executable POSIX shell script and returns its path.
func shellTool(t *testing.T, name, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script as the external tool")
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

// --- zip primitives ---

func TestStepTestIntegrity_CorruptEntryData(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "bitrot.zip")
	writeOrderedZip(t, zipPath, "", []zipEntry{{"data.bin", "AAAAAAAAAAAAAAAA"}})

	// Flip one byte of the stored data: the archive still opens, but the
	// entry no longer matches its CRC.
	raw, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(raw, []byte("AAAAAAAAAAAAAAAA"))
	if at < 0 {
		t.Fatal("stored entry data not found in archive")
	}
	raw[at+3] = 'B'
	if err := os.WriteFile(zipPath, raw, 0644); err != nil {
		t.Fatal(err)
	}

	err = NewProcessor(DefaultConfig(), "").StepTestIntegrity(zipPath)
	if err == nil || !strings.Contains(err.Error(), "corrupt data in data.bin") {
		t.Fatalf("StepTestIntegrity = %v, want corrupt data error naming the entry", err)
	}
}

func TestExtractZip_DirectoriesAndNestedFiles(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "tree.zip")
	writeOrderedZip(t, zipPath, "", []zipEntry{
		{"empty/", ""},
		{"docs/guide/intro.txt", "intro"},
		{"top.txt", "top"},
	})

	workDir, err := NewProcessor(DefaultConfig(), "").StepExtract(zipPath)
	if err != nil {
		t.Fatalf("StepExtract: %v", err)
	}
	defer os.RemoveAll(workDir)

	if info, err := os.Stat(filepath.Join(workDir, "empty")); err != nil || !info.IsDir() {
		t.Errorf("directory entry not extracted as a directory: %v", err)
	}
	for rel, want := range map[string]string{"docs/guide/intro.txt": "intro", "top.txt": "top"} {
		data, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(rel)))
		if err != nil || string(data) != want {
			t.Errorf("%s = %q, err %v; want %q", rel, data, err, want)
		}
	}
}

func TestExtractZip_RejectsPathTraversal(t *testing.T) {
	base := t.TempDir()
	zipPath := filepath.Join(base, "slip.zip")
	writeOrderedZip(t, zipPath, "", []zipEntry{
		{"ok.txt", "fine"},
		{"../escaped.txt", "should never be written"},
	})
	destDir := filepath.Join(base, "work")
	if err := os.Mkdir(destDir, 0755); err != nil {
		t.Fatal(err)
	}

	err := NewProcessor(DefaultConfig(), "").extractZip(zipPath, destDir)
	if err == nil || !strings.Contains(err.Error(), "illegal file path in zip: ../escaped.txt") {
		t.Fatalf("extractZip = %v, want illegal path error", err)
	}
	if _, err := os.Stat(filepath.Join(base, "escaped.txt")); !os.IsNotExist(err) {
		t.Errorf("entry escaped the work directory (stat err = %v)", err)
	}

	// Through the step, the half-filled work directory is not handed back.
	workDir, err := NewProcessor(DefaultConfig(), "").StepExtract(zipPath)
	if err == nil || workDir != "" {
		t.Errorf("StepExtract = %q, %v; want no work dir and an error", workDir, err)
	}
}

func TestExtractZip_TargetObstructed(t *testing.T) {
	base := t.TempDir()
	zipPath := filepath.Join(base, "clash.zip")
	// "a" is a file, so "a/b.txt" has nowhere to go.
	writeOrderedZip(t, zipPath, "", []zipEntry{{"a", "file"}, {"a/b.txt", "child"}})
	destDir := filepath.Join(base, "work")
	if err := os.Mkdir(destDir, 0755); err != nil {
		t.Fatal(err)
	}
	p := NewProcessor(DefaultConfig(), "")

	err := p.extractZip(zipPath, destDir)
	if err == nil || !strings.Contains(err.Error(), "failed to create parent directory") {
		t.Errorf("file/dir clash: err = %v, want parent directory failure", err)
	}

	// The reverse: a directory already sits where a file entry should go.
	writeOrderedZip(t, zipPath, "", []zipEntry{{"a/b.txt", "child"}, {"a", "file"}})
	destDir2 := filepath.Join(base, "work2")
	if err := os.Mkdir(destDir2, 0755); err != nil {
		t.Fatal(err)
	}
	err = p.extractZip(zipPath, destDir2)
	if err == nil || !strings.Contains(err.Error(), "failed to create") {
		t.Errorf("dir/file clash: err = %v, want create failure", err)
	}

	if err := p.extractZip(filepath.Join(base, "missing.zip"), destDir); err == nil || !strings.Contains(err.Error(), "failed to open zip") {
		t.Errorf("missing archive: err = %v, want open failure", err)
	}
}

func TestStepAddComment_ReplacesCommentKeepsEntries(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "c.zip")
	entries := []zipEntry{{"one.txt", "1"}, {"two.txt", "2"}}
	writeOrderedZip(t, zipPath, "old comment", entries)
	if err := os.WriteFile(filepath.Join(dir, "ZCOMMENT.TXT"), []byte("\r\n  New BBS comment  \r\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// The default comment file name is resolved against the base directory.
	if err := NewProcessor(DefaultConfig(), dir).StepAddComment(zipPath); err != nil {
		t.Fatalf("StepAddComment: %v", err)
	}
	comment, got := readZip(t, zipPath)
	if comment != "New BBS comment" {
		t.Errorf("comment = %q, want trimmed comment file contents", comment)
	}
	if len(got) != 2 || got[0] != entries[0] || got[1] != entries[1] {
		t.Errorf("entries = %v, want %v", got, entries)
	}
	if _, err := os.Stat(zipPath + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp archive left behind (stat err = %v)", err)
	}
}

func TestZipRewriteFailuresLeaveArchiveIntact(t *testing.T) {
	entries := []zipEntry{{"one.txt", "1"}, {"BBS.AD", "already here"}}

	tests := []struct {
		name    string
		tmpDir  bool // put a directory where the temp archive goes
		run     func(p *Processor, zipPath string) error
		wantErr string
	}{
		{
			name:    "comment too long for a zip",
			run:     func(p *Processor, z string) error { return p.setZipComment(z, strings.Repeat("x", 70000)) },
			wantErr: "failed to set zip comment",
		},
		{
			name:    "included file already in archive",
			run:     func(p *Processor, z string) error { return p.addFileToZip(z, "BBS.AD", []byte("new")) },
			wantErr: "entry BBS.AD already exists in archive",
		},
		{
			name:    "comment: temp archive cannot be created",
			tmpDir:  true,
			run:     func(p *Processor, z string) error { return p.setZipComment(z, "c") },
			wantErr: "failed to create temp zip",
		},
		{
			name:    "include: temp archive cannot be created",
			tmpDir:  true,
			run:     func(p *Processor, z string) error { return p.addFileToZip(z, "NEW.AD", []byte("new")) },
			wantErr: "failed to create temp zip",
		},
		{
			name:    "remove: temp archive cannot be created",
			tmpDir:  true,
			run:     func(p *Processor, z string) error { return p.removeFilesFromZip(z, []string{"BBS.AD"}) },
			wantErr: "failed to create temp zip",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zipPath := filepath.Join(t.TempDir(), "keep.zip")
			writeOrderedZip(t, zipPath, "original", entries)
			if tt.tmpDir {
				if err := os.Mkdir(zipPath+".tmp", 0755); err != nil {
					t.Fatal(err)
				}
			}

			err := tt.run(NewProcessor(DefaultConfig(), ""), zipPath)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
			comment, got := readZip(t, zipPath)
			if comment != "original" || len(got) != 2 || got[0] != entries[0] || got[1] != entries[1] {
				t.Errorf("archive changed by failed rewrite: comment %q, entries %v", comment, got)
			}
			if info, err := os.Stat(zipPath + ".tmp"); !tt.tmpDir && !os.IsNotExist(err) {
				t.Errorf("temp archive left behind: %v, err %v", info, err)
			}
		})
	}

	// A missing archive is reported by each rewrite.
	p := NewProcessor(DefaultConfig(), "")
	missing := filepath.Join(t.TempDir(), "missing.zip")
	for name, err := range map[string]error{
		"setZipComment":      p.setZipComment(missing, "c"),
		"addFileToZip":       p.addFileToZip(missing, "a", nil),
		"removeFilesFromZip": p.removeFilesFromZip(missing, []string{"a"}),
	} {
		if err == nil || !strings.Contains(err.Error(), "failed to open zip") {
			t.Errorf("%s on a missing archive = %v, want open failure", name, err)
		}
	}
}

func TestStepIncludeFile_KeepsCommentAndDropsDuplicateEntries(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "dup.zip")
	writeOrderedZip(t, zipPath, "sysop comment", []zipEntry{
		{"a.txt", "first a"},
		{"b.txt", "b"},
		{"a.txt", "second a"},
	})
	adFile := filepath.Join(dir, "BBS.AD")
	if err := os.WriteFile(adFile, []byte("Call the BBS"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := NewProcessor(DefaultConfig(), dir).StepIncludeFile(zipPath); err != nil {
		t.Fatalf("StepIncludeFile: %v", err)
	}
	comment, got := readZip(t, zipPath)
	if comment != "sysop comment" {
		t.Errorf("comment = %q, want it preserved", comment)
	}
	want := []zipEntry{{"a.txt", "first a"}, {"b.txt", "b"}, {"BBS.AD", "Call the BBS"}}
	if len(got) != len(want) {
		t.Fatalf("entries = %s, want %s", entryNames(got), entryNames(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestStepRemoveAdsAndDIZ_RewritesArchive(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "ads.zip")
	writeOrderedZip(t, zipPath, "keep this comment", []zipEntry{
		{"game.exe", "binary"},
		{"OTHERBBS.AD", "call us"},
		{"sub/otherbbs.ad", "call us too"},
		{"game.exe", "stale duplicate"},
		{"readme.txt", "docs"},
	})
	patterns := "; files other boards add\r\n\r\n  otherbbs.ad  \r\nNOTHERE.TXT\r\n"
	if err := os.WriteFile(filepath.Join(dir, "REMOVE.TXT"), []byte(patterns), 0644); err != nil {
		t.Fatal(err)
	}
	p := NewProcessor(DefaultConfig(), dir)
	workDir, err := p.StepExtract(zipPath)
	if err != nil {
		t.Fatalf("StepExtract: %v", err)
	}
	defer os.RemoveAll(workDir)

	if _, err := p.StepRemoveAdsAndDIZ(workDir, zipPath); err != nil {
		t.Fatalf("StepRemoveAdsAndDIZ: %v", err)
	}

	comment, got := readZip(t, zipPath)
	if comment != "keep this comment" {
		t.Errorf("comment = %q, want it preserved", comment)
	}
	// Ads go wherever they sit in the archive; the duplicate entry is dropped.
	if names := entryNames(got); names != "game.exe,readme.txt" {
		t.Errorf("entries = %s, want game.exe,readme.txt", names)
	}
	if got[0].Content != "binary" {
		t.Errorf("game.exe content = %q, want the first copy", got[0].Content)
	}
	// In the work directory only the top level is cleaned.
	if _, err := os.Stat(filepath.Join(workDir, "OTHERBBS.AD")); !os.IsNotExist(err) {
		t.Errorf("ad file still in work dir (stat err = %v)", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "readme.txt")); err != nil {
		t.Errorf("readme.txt removed from work dir: %v", err)
	}
}

func TestStepRemoveAdsAndDIZ_NothingToRemove(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "clean.zip")
	writeOrderedZip(t, zipPath, "", []zipEntry{{"readme.txt", "docs"}})
	before, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "REMOVE.TXT"), []byte("OTHERBBS.AD\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// A work directory that has vanished is tolerated.
	p := NewProcessor(DefaultConfig(), dir)
	if _, err := p.StepRemoveAdsAndDIZ(filepath.Join(dir, "gone"), zipPath); err != nil {
		t.Fatalf("StepRemoveAdsAndDIZ: %v", err)
	}
	after, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("archive rewritten although nothing matched")
	}
	if _, err := os.Stat(zipPath + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp archive left behind (stat err = %v)", err)
	}
}

// --- step argument and configuration errors ---

func TestSteps_UnsupportedArchiveType(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "upload.xyz")
	if err := os.WriteFile(archive, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	p := NewProcessor(DefaultConfig(), dir)

	_, extractErr := p.StepExtract(archive)
	for step, err := range map[string]error{
		"test integrity": p.StepTestIntegrity(archive),
		"extract":        extractErr,
		"add comment":    p.StepAddComment(archive),
		"include file":   p.StepIncludeFile(archive),
	} {
		if err == nil || !strings.Contains(err.Error(), "unsupported archive type: .xyz") {
			t.Errorf("%s on .xyz = %v, want unsupported archive type", step, err)
		}
	}
}

func TestSteps_MissingSupportFiles(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "plain.zip")
	writeOrderedZip(t, zipPath, "", []zipEntry{{"readme.txt", "docs"}})
	before, _ := os.ReadFile(zipPath)
	p := NewProcessor(DefaultConfig(), dir) // no ZCOMMENT.TXT or BBS.AD in dir

	if err := p.StepAddComment(zipPath); err == nil || !strings.Contains(err.Error(), "failed to read comment file") {
		t.Errorf("StepAddComment = %v, want comment file error", err)
	}
	if err := p.StepIncludeFile(zipPath); err == nil || !strings.Contains(err.Error(), "failed to read include file") {
		t.Errorf("StepIncludeFile = %v, want include file error", err)
	}
	if after, _ := os.ReadFile(zipPath); !bytes.Equal(before, after) {
		t.Error("archive modified by steps that failed")
	}
}

func TestExternalCommand_Failures(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "upload.rar")
	if err := os.WriteFile(archive, []byte("rar"), 0644); err != nil {
		t.Fatal(err)
	}

	// An archive type with no test command configured.
	cfg := DefaultConfig()
	cfg.ArchiveTypes = []ArchiveType{{Extension: ".rar"}}
	err := NewProcessor(cfg, dir).StepTestIntegrity(archive)
	if err == nil || !strings.Contains(err.Error(), "no command configured") {
		t.Errorf("no test command: err = %v", err)
	}

	// A tool that reports failure: its output is included for the sysop log,
	// and a failed extraction does not hand back a work directory.
	failing := shellTool(t, "fail.sh", `echo "CRC error in $1"; exit 3`)
	cfg.ArchiveTypes = []ArchiveType{{
		Extension:   ".rar",
		TestCommand: failing, TestArgs: []string{"{ARCHIVE}"},
		ExtractCommand: failing, ExtractArgs: []string{"{ARCHIVE}"},
	}}
	p := NewProcessor(cfg, dir)
	err = p.StepTestIntegrity(archive)
	if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "CRC error in "+archive) {
		t.Errorf("failing test command: err = %v", err)
	}
	workDir, err := p.StepExtract(archive)
	if err == nil || workDir != "" {
		t.Errorf("failing extract command: workDir %q, err %v", workDir, err)
	}
}

func TestStepVirusScan_Timeout(t *testing.T) {
	// exec replaces the shell so the kill reaches the sleeping process.
	slow := shellTool(t, "slow.sh", "exec sleep 30")
	cfg := DefaultConfig()
	cfg.Steps.VirusScan = VirusScanConfig{StepConfig: StepConfig{Enabled: true}, Command: slow, Timeout: 1}
	dir := t.TempDir()

	err := NewProcessor(cfg, dir).StepVirusScan(filepath.Join(dir, "x.zip"), dir)
	if err == nil || !strings.Contains(err.Error(), "timed out after 1s") {
		t.Fatalf("StepVirusScan = %v, want timeout after 1s", err)
	}
}

// --- pipeline ---

// statuses renders step results as "1P 2P 3F" for comparison.
func statuses(results []StepResult) string {
	parts := make([]string, len(results))
	for i, sr := range results {
		parts[i] = entryKey(int(sr.Step), sr.Status)
	}
	return strings.Join(parts, " ")
}

func TestRunPipeline_VirusFoundStopsAndDeletes(t *testing.T) {
	scanner := shellTool(t, "scan.sh", `echo "EICAR FOUND in $1"; exit 1`)
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "infected.zip")
	writeOrderedZip(t, zipPath, "", []zipEntry{{"virus.com", "X5O!"}, {"FILE_ID.DIZ", "Totally safe"}})

	cfg := DefaultConfig()
	cfg.Steps.VirusScan = VirusScanConfig{StepConfig: StepConfig{Enabled: true}, Command: scanner, Args: []string{"{WORKDIR}"}}
	result := NewProcessor(cfg, dir).RunPipeline(zipPath, nil)

	if result.Success || result.Error == nil || !strings.Contains(result.Error.Error(), "virus scan failed") ||
		!strings.Contains(result.Error.Error(), "EICAR FOUND") {
		t.Errorf("result success=%v error=%v, want virus scan failure with scanner output", result.Success, result.Error)
	}
	// Nothing after the scan runs, so no description is taken from the upload.
	if got := statuses(result.StepResults); got != "1P 2P 3F" {
		t.Errorf("steps = %q, want 1P 2P 3F", got)
	}
	if result.Description != "" {
		t.Errorf("description = %q, want none from an infected upload", result.Description)
	}
	if _, err := os.Stat(zipPath); !os.IsNotExist(err) {
		t.Errorf("infected upload not deleted (stat err = %v)", err)
	}
}

func TestRunPipeline_ExtractionFailure(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "broken.zip")
	if err := os.WriteFile(zipPath, []byte("not a zip"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Steps.TestIntegrity.Enabled = false

	result := NewProcessor(cfg, dir).RunPipeline(zipPath, nil)
	if result.Success || result.Error == nil || !strings.Contains(result.Error.Error(), "extraction failed") {
		t.Errorf("result success=%v error=%v, want extraction failure", result.Success, result.Error)
	}
	if got := statuses(result.StepResults); got != "2F" {
		t.Errorf("steps = %q, want 2F", got)
	}
}

func TestRunPipeline_LateStepFailuresAreNotFatal(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "ok.zip")
	writeOrderedZip(t, zipPath, "", []zipEntry{{"FILE_ID.DIZ", "A fine upload"}, {"prog.exe", "bin"}})

	// No ZCOMMENT.TXT or BBS.AD exists, so steps 6 and 7 fail.
	result := NewProcessor(DefaultConfig(), dir).RunPipeline(zipPath, nil)
	if !result.Success || result.Error != nil {
		t.Errorf("result success=%v error=%v, want success despite late failures", result.Success, result.Error)
	}
	if got := statuses(result.StepResults); got != "1P 2P 5P 6F 7F" {
		t.Errorf("steps = %q, want 1P 2P 5P 6F 7F", got)
	}
	if result.Description != "A fine upload" {
		t.Errorf("description = %q, want the FILE_ID.DIZ text", result.Description)
	}
}

func TestDisplayPipeline_NoScreenAssets(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "ok.zip")
	writeOrderedZip(t, zipPath, "", []zipEntry{{"prog.exe", "bin"}})
	cfg := DefaultConfig()
	cfg.Steps.AddComment.Enabled = false
	cfg.Steps.IncludeFile.Enabled = false

	// Without ZIPLAB.ANS or ZIPLAB.NFO the pipeline runs silently.
	var screen bytes.Buffer
	result := NewProcessor(cfg, dir).DisplayPipeline(&screen, nil, nil, zipPath)
	if !result.Success {
		t.Fatalf("pipeline failed: %v", result.Error)
	}
	if screen.Len() != 0 {
		t.Errorf("wrote %q to the screen with no assets", screen.String())
	}
}

func TestHandleScanFailure_QuarantineFallbacks(t *testing.T) {
	newUpload := func(t *testing.T) (string, string) {
		dir := t.TempDir()
		path := filepath.Join(dir, "infected.zip")
		if err := os.WriteFile(path, []byte("bad"), 0644); err != nil {
			t.Fatal(err)
		}
		return dir, path
	}

	t.Run("no quarantine path configured", func(t *testing.T) {
		_, upload := newUpload(t)
		cfg := DefaultConfig()
		cfg.ScanFailBehavior = "quarantine"
		NewProcessor(cfg, "").handleScanFailure(upload)
		if _, err := os.Stat(upload); !os.IsNotExist(err) {
			t.Errorf("infected upload left in place (stat err = %v)", err)
		}
	})

	t.Run("quarantine directory cannot be created", func(t *testing.T) {
		dir, upload := newUpload(t)
		blocker := filepath.Join(dir, "blocker")
		if err := os.WriteFile(blocker, nil, 0644); err != nil {
			t.Fatal(err)
		}
		cfg := DefaultConfig()
		cfg.ScanFailBehavior = "quarantine"
		cfg.QuarantinePath = filepath.Join(blocker, "quarantine")
		NewProcessor(cfg, "").handleScanFailure(upload)
		// Better gone than left where callers can download it.
		if _, err := os.Stat(upload); !os.IsNotExist(err) {
			t.Errorf("infected upload left in place (stat err = %v)", err)
		}
	})

	t.Run("upload already gone", func(t *testing.T) {
		dir, upload := newUpload(t)
		if err := os.Remove(upload); err != nil {
			t.Fatal(err)
		}
		quarantine := filepath.Join(dir, "quarantine")
		for _, behavior := range []string{"quarantine", "delete"} {
			cfg := DefaultConfig()
			cfg.ScanFailBehavior = behavior
			cfg.QuarantinePath = quarantine
			NewProcessor(cfg, "").handleScanFailure(upload) // must not panic
		}
		if entries, err := os.ReadDir(quarantine); err != nil || len(entries) != 0 {
			t.Errorf("quarantine dir entries = %v, err %v; want an empty directory", entries, err)
		}
	})
}

// --- configuration ---

func TestArchiverTypesFromConfig(t *testing.T) {
	cfg := archiver.Config{Archivers: []archiver.Archiver{
		{
			ID: "lha", Extension: ".lha", Extensions: []string{".LHA", ".lzh"}, Enabled: true,
			Unpack:  archiver.CommandDef{Command: "lha", Args: []string{"x", "{ARCHIVE}"}},
			Test:    archiver.CommandDef{Command: "lha", Args: []string{"t", "{ARCHIVE}"}},
			AddFile: archiver.CommandDef{Command: "lha", Args: []string{"a", "{ARCHIVE}", "{FILE}"}},
			Comment: archiver.CommandDef{Command: "lhacomment"},
		},
		{ID: "rar", Extension: ".rar", Enabled: false},
		{ID: "zip", Extension: ".zip", Native: true, Enabled: true},
	}}

	types := archiverTypesFromConfig(cfg)
	var exts []string
	for _, at := range types {
		exts = append(exts, at.Extension)
	}
	// The extra extension repeats the primary one in another case and is
	// skipped; the disabled archiver is left out.
	if got := strings.Join(exts, ","); got != ".lha,.lzh,.zip" {
		t.Fatalf("extensions = %s, want .lha,.lzh,.zip", got)
	}
	lzh := types[1]
	if lzh.Native || lzh.ExtractCommand != "lha" || strings.Join(lzh.ExtractArgs, " ") != "x {ARCHIVE}" ||
		lzh.TestCommand != "lha" || lzh.AddCommand != "lha" || lzh.CommentCommand != "lhacomment" {
		t.Errorf(".lzh type does not carry the archiver's commands: %+v", lzh)
	}
	if !types[2].Native {
		t.Errorf(".zip type = %+v, want native", types[2])
	}

	// With every archiver disabled, native ZIP is still handled.
	fallback := archiverTypesFromConfig(archiver.Config{Archivers: []archiver.Archiver{{Extension: ".rar"}}})
	if len(fallback) != 1 || fallback[0].Extension != ".zip" || !fallback[0].Native {
		t.Errorf("fallback types = %+v, want native .zip only", fallback)
	}
}

func TestConfigFileErrors(t *testing.T) {
	t.Run("ziplab.json unreadable", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "ziplab.json"), 0755); err != nil {
			t.Fatal(err)
		}
		for name, load := range map[string]func(string) (Config, error){"ReadConfig": ReadConfig, "LoadConfig": LoadConfig} {
			if _, err := load(dir); err == nil || !strings.Contains(err.Error(), "failed to read ziplab config") {
				t.Errorf("%s = %v, want read failure", name, err)
			}
		}
	})

	t.Run("archivers.json invalid", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "archivers.json"), []byte("{not json"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "ziplab.json"), []byte(`{"scanFailBehavior":"quarantine"}`), 0644); err != nil {
			t.Fatal(err)
		}
		// ZipLab's own settings still load, with the built-in archive types.
		cfg, err := LoadConfig(dir)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if cfg.ScanFailBehavior != "quarantine" {
			t.Errorf("ScanFailBehavior = %q, want quarantine", cfg.ScanFailBehavior)
		}
		if len(cfg.ArchiveTypes) != 1 || cfg.ArchiveTypes[0].Extension != ".zip" || !cfg.ArchiveTypes[0].Native {
			t.Errorf("ArchiveTypes = %+v, want the native .zip default", cfg.ArchiveTypes)
		}
	})

	t.Run("save into missing directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "no-such-dir")
		if err := SaveConfig(dir, DefaultConfig()); err == nil || !strings.Contains(err.Error(), "ziplab.json") {
			t.Errorf("SaveConfig = %v, want write failure naming the file", err)
		}
	})
}
