package ziplab

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The ZipLab pipeline steps. Each Step* method is one stage a sysop can enable
// or disable; the zip mechanics they call live in the processor_* files.

// Processor runs the ZipLab pipeline steps against an uploaded archive.
type Processor struct {
	config  Config
	baseDir string // Base directory for resolving relative paths
}

// NewProcessor creates a new ZipLab processor.
func NewProcessor(cfg Config, baseDir string) *Processor {
	return &Processor{
		config:  cfg,
		baseDir: baseDir,
	}
}

// resolvePath resolves a config file path against baseDir when relative.
func (p *Processor) resolvePath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(p.baseDir, path)
}

// StepTestIntegrity (Step 1) tests the archive for corruption.
// For native ZIP, it opens and reads every file entry.
// For external formats, it runs the configured test command.
func (p *Processor) StepTestIntegrity(archivePath string) error {
	if !p.config.Steps.TestIntegrity.Enabled {
		slog.Info("step 1 (test integrity) skipped — disabled")
		return nil
	}

	at, ok := p.config.GetArchiveType(archivePath)
	if !ok {
		return fmt.Errorf("unsupported archive type: %s", filepath.Ext(archivePath))
	}

	if at.Native {
		return p.testZipIntegrity(archivePath)
	}
	return p.runExternalCommand(at.TestCommand, at.TestArgs, archivePath, "", 0)
}

// StepExtract (Step 2) extracts the archive to a temporary work directory.
// Returns the path to the work directory.
func (p *Processor) StepExtract(archivePath string) (string, error) {
	if !p.config.Steps.ExtractToTemp.Enabled {
		slog.Info("step 2 (extract) skipped — disabled")
		return "", nil
	}

	at, ok := p.config.GetArchiveType(archivePath)
	if !ok {
		return "", fmt.Errorf("unsupported archive type: %s", filepath.Ext(archivePath))
	}

	workDir, err := os.MkdirTemp("", "ziplab-extract-*")
	if err != nil {
		return "", fmt.Errorf("failed to create work directory: %w", err)
	}

	if at.Native {
		if err := p.extractZip(archivePath, workDir); err != nil {
			_ = os.RemoveAll(workDir) // cleanup on error path
			return "", err
		}
		return workDir, nil
	}

	if err := p.runExternalCommand(at.ExtractCommand, at.ExtractArgs, archivePath, workDir, 0); err != nil {
		_ = os.RemoveAll(workDir) // cleanup on error path
		return "", err
	}
	return workDir, nil
}

// StepVirusScan (Step 3) runs a configurable external virus scanner.
func (p *Processor) StepVirusScan(workDir string) error {
	if !p.config.Steps.VirusScan.Enabled {
		slog.Info("step 3 (virus scan) skipped — disabled")
		return nil
	}

	step := p.config.Steps.VirusScan
	return p.runExternalCommand(step.Command, step.Args, "", workDir, step.Timeout)
}

// StepRemoveAdsAndDIZ (Step 5) extracts FILE_ID.DIZ content and removes
// unwanted files matching patterns from REMOVE.TXT.
// workDir is used to find FILE_ID.DIZ; archivePath is the ZIP to strip ad files from.
func (p *Processor) StepRemoveAdsAndDIZ(workDir, archivePath string) (string, error) {
	if !p.config.Steps.RemoveAds.Enabled {
		slog.Info("step 5 (remove ads/DIZ) skipped — disabled")
		return "", nil
	}

	// Extract FILE_ID.DIZ (case-insensitive search)
	diz := p.findAndReadDIZ(workDir)

	// Load removal patterns
	patterns := p.loadRemovePatterns()

	// Remove matching files from work directory
	for _, pattern := range patterns {
		p.removeMatchingFiles(workDir, pattern)
	}

	// Remove matching files from the archive itself
	if len(patterns) > 0 && archivePath != "" {
		at, ok := p.config.GetArchiveType(archivePath)
		if ok && at.Native {
			if err := p.removeFilesFromZip(archivePath, patterns); err != nil {
				slog.Warn("failed to remove ad files from archive", "error", err)
			}
		}
	}

	return diz, nil
}

// StepAddComment (Step 6) adds a ZIP comment from the configured comment file.
func (p *Processor) StepAddComment(archivePath string) error {
	if !p.config.Steps.AddComment.Enabled {
		slog.Info("step 6 (add comment) skipped — disabled")
		return nil
	}

	at, ok := p.config.GetArchiveType(archivePath)
	if !ok {
		return fmt.Errorf("unsupported archive type: %s", filepath.Ext(archivePath))
	}

	commentFile := p.resolvePath(p.config.Steps.AddComment.CommentFile)
	commentData, err := os.ReadFile(commentFile)
	if err != nil {
		return fmt.Errorf("failed to read comment file %s: %w", commentFile, err)
	}
	comment := strings.TrimSpace(string(commentData))

	if at.Native {
		return p.setZipComment(archivePath, comment)
	}
	return p.runExternalCommand(at.CommentCommand, at.CommentArgs, archivePath, "", 0)
}

// StepIncludeFile (Step 7) adds a file (e.g., BBS.AD) into the archive.
func (p *Processor) StepIncludeFile(archivePath string) error {
	if !p.config.Steps.IncludeFile.Enabled {
		slog.Info("step 7 (include file) skipped — disabled")
		return nil
	}

	at, ok := p.config.GetArchiveType(archivePath)
	if !ok {
		return fmt.Errorf("unsupported archive type: %s", filepath.Ext(archivePath))
	}

	includeFilePath := p.resolvePath(p.config.Steps.IncludeFile.FilePath)
	includeData, err := os.ReadFile(includeFilePath)
	if err != nil {
		return fmt.Errorf("failed to read include file %s: %w", includeFilePath, err)
	}

	if at.Native {
		return p.addFileToZip(archivePath, filepath.Base(includeFilePath), includeData)
	}
	return p.runExternalCommand(at.AddCommand, at.AddArgs, archivePath, "", 0)
}

// runExternalCommand runs an external command with placeholder substitution.
// timeoutSeconds of 0 uses the default (60s).
func (p *Processor) runExternalCommand(command string, args []string, archivePath, workDir string, timeoutSeconds int) error {
	if command == "" {
		return fmt.Errorf("no command configured")
	}

	expandedArgs := make([]string, len(args))
	for i, arg := range args {
		arg = strings.ReplaceAll(arg, "{FILE}", archivePath)
		arg = strings.ReplaceAll(arg, "{WORKDIR}", workDir)
		expandedArgs[i] = arg
	}

	timeout := 60 * time.Second
	if timeoutSeconds > 0 {
		timeout = time.Duration(timeoutSeconds) * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, expandedArgs...)
	cmd.Dir = workDir

	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("command %s timed out after %v", command, timeout)
	}
	if err != nil {
		return fmt.Errorf("command %s failed: %w (output: %s)", command, err, string(output))
	}
	return nil
}
