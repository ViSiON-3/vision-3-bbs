package ziplab

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// recorderScript writes a shell script that appends its arguments, one per
// line, to log, and returns its path.
func recorderScript(t *testing.T, dir, log string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script as the external tool")
	}
	script := filepath.Join(dir, "record.sh")
	body := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\" >> '" + log + "'; done\n"
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	return script
}

func readLog(t *testing.T, log string) []string {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("tool did not run: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// External archivers are defined in archivers.json, whose commands name the
// archive {ARCHIVE}, the extraction directory {OUTDIR}, and the comment or
// added file {FILE}. ZipLab must fill in those names; passing them through
// literally made every non-ZIP upload fail its integrity test.
func TestExternalArchiverPlaceholders(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "args.log")
	tool := recorderScript(t, dir, log)

	archive := filepath.Join(dir, "upload.rar")
	if err := os.WriteFile(archive, []byte("rar"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ZCOMMENT.TXT", "BBS.AD"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := DefaultConfig()
	cfg.ArchiveTypes = []ArchiveType{{
		Extension:      ".rar",
		TestCommand:    tool,
		TestArgs:       []string{"t", "{ARCHIVE}"},
		ExtractCommand: tool,
		ExtractArgs:    []string{"x", "{ARCHIVE}", "{OUTDIR}"},
		CommentCommand: tool,
		CommentArgs:    []string{"c", "{ARCHIVE}", "{FILE}"},
		AddCommand:     tool,
		AddArgs:        []string{"a", "{ARCHIVE}", "{FILE}"},
	}}
	p := NewProcessor(cfg, dir)

	if err := p.StepTestIntegrity(archive); err != nil {
		t.Fatalf("test step: %v", err)
	}
	workDir, err := p.StepExtract(archive)
	if err != nil {
		t.Fatalf("extract step: %v", err)
	}
	defer os.RemoveAll(workDir)
	if err := p.StepAddComment(archive); err != nil {
		t.Fatalf("comment step: %v", err)
	}
	if err := p.StepIncludeFile(archive); err != nil {
		t.Fatalf("include step: %v", err)
	}

	want := []string{
		"t", archive,
		"x", archive, workDir,
		"c", archive, filepath.Join(dir, "ZCOMMENT.TXT"),
		"a", archive, filepath.Join(dir, "BBS.AD"),
	}
	got := readLog(t, log)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("tool arguments:\n got  %q\n want %q", got, want)
	}
}

// The virus scanner gets the extracted files as {WORKDIR} and the archive as
// {FILE}; {FILE} used to be passed empty.
func TestVirusScanPlaceholders(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "args.log")
	cfg := DefaultConfig()
	cfg.Steps.VirusScan.Enabled = true
	cfg.Steps.VirusScan.Command = recorderScript(t, dir, log)
	cfg.Steps.VirusScan.Args = []string{"{WORKDIR}", "{FILE}"}
	p := NewProcessor(cfg, dir)

	archive := filepath.Join(dir, "upload.zip")
	workDir := t.TempDir()
	if err := p.StepVirusScan(archive, workDir); err != nil {
		t.Fatalf("virus scan: %v", err)
	}
	if got, want := readLog(t, log), []string{workDir, archive}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("scanner arguments = %q, want %q", got, want)
	}
}

// Commands written before archivers.json's names were honoured used {FILE}
// for the archive and {WORKDIR} for the extraction directory. They must keep
// working, while a step that sets {FILE} itself (the comment and ad steps)
// keeps its own meaning.
func TestExternalArchiverLegacyPlaceholders(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "args.log")
	tool := recorderScript(t, dir, log)

	archive := filepath.Join(dir, "upload.rar")
	if err := os.WriteFile(archive, []byte("rar"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "BBS.AD"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.ArchiveTypes = []ArchiveType{{
		Extension:      ".rar",
		TestCommand:    tool,
		TestArgs:       []string{"t", "{FILE}"},
		ExtractCommand: tool,
		ExtractArgs:    []string{"x", "{FILE}", "{WORKDIR}"},
		AddCommand:     tool,
		AddArgs:        []string{"a", "{ARCHIVE}", "{FILE}"},
	}}
	p := NewProcessor(cfg, dir)

	if err := p.StepTestIntegrity(archive); err != nil {
		t.Fatalf("test step: %v", err)
	}
	workDir, err := p.StepExtract(archive)
	if err != nil {
		t.Fatalf("extract step: %v", err)
	}
	defer os.RemoveAll(workDir)
	if err := p.StepIncludeFile(archive); err != nil {
		t.Fatalf("include step: %v", err)
	}

	want := []string{
		"t", archive,
		"x", archive, workDir,
		"a", archive, filepath.Join(dir, "BBS.AD"),
	}
	if got := readLog(t, log); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("tool arguments:\n got  %q\n want %q", got, want)
	}
}
