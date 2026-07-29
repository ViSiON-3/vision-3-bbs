package menu

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/ansi"
	"github.com/ViSiON-3/vision-3-bbs/internal/file"
	"github.com/ViSiON-3/vision-3-bbs/internal/terminalio"
	"github.com/ViSiON-3/vision-3-bbs/internal/user"
	"github.com/ViSiON-3/vision-3-bbs/internal/ziplab"
	"github.com/gliderlabs/ssh"
	"github.com/google/uuid"
	"golang.org/x/term"
)

// registerUploadedFiles processes each received file: it runs ZipLab
// extraction where applicable, prompts for a description, creates the
// FileRecord, moves the file into the area directory, saves it to the area,
// and finally credits the uploader's upload count. It returns the number of
// files successfully registered and the number rejected as duplicates.
func (e *MenuExecutor) registerUploadedFiles(
	s ssh.Session,
	terminal *term.Terminal,
	outputMode ansi.OutputMode,
	nodeNumber int,
	newFiles []newFileInfo,
	incomingDir string,
	targetDir string,
	currentAreaID int,
	currentUser *user.User,
	userManager *user.UserMgr,
	existingNames map[string]bool,
) (successCount int, duplicateCount int) {
	// Load ZipLab config once for all files
	zlCfg, zlErr := ziplab.LoadConfig(e.RootConfigPath)
	if zlErr != nil {
		slog.Warn("failed to load ziplab config", "node", nodeNumber, "error", zlErr)
	}

	for _, nf := range newFiles {
		incomingPath := filepath.Join(incomingDir, nf.name)

		if skip, dup := checkUploadDuplicates(nf, incomingPath, existingNames, terminal, outputMode, nodeNumber); skip {
			if dup {
				duplicateCount++
			}
			continue
		}

		// ZipLab processing for supported archive types (runs on file in incoming dir)
		var description string
		filePath := incomingPath

		if zlErr == nil && zlCfg.Enabled && zlCfg.RunOnUpload && zlCfg.IsArchiveSupported(nf.name) {
			slog.Info("running ziplab pipeline", "node", nodeNumber, "name", nf.name)

			zlBaseDir := filepath.Join(filepath.Dir(e.RootConfigPath), "ziplab")
			proc := ziplab.NewProcessor(zlCfg, zlBaseDir)

			// Load ZIPLAB.ANS and ZIPLAB.NFO for visual display
			ansiPath := filepath.Join(e.MenuSetPath, "ansi", "ZIPLAB.ANS")
			nfoPath := filepath.Join(e.MenuSetPath, "ansi", "ZIPLAB.NFO")

			ansiContent, _ := ansi.GetAnsiFileContent(ansiPath)
			nfo, _ := ziplab.ParseNFO(nfoPath)

			var result ziplab.PipelineResult
			if ansiContent != nil {
				result = proc.DisplayPipeline(terminal, nfo, ansiContent, filePath)
			} else {
				result = proc.RunPipeline(filePath, nil)
			}

			if !result.Success {
				slog.Error("ziplab pipeline failed", "node", nodeNumber, "name", nf.name, "error", result.Error)
				errMsg := fmt.Sprintf("\r\n|01ZipLab processing failed for '%s'.|07\r\n", nf.name)
				terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
				time.Sleep(2 * time.Second)
				continue
			}

			if result.Description != "" {
				description = sanitizeControlChars(strings.TrimRight(result.Description, " \t\r\n"))
				slog.Info("using FILE_ID.DIZ description", "node", nodeNumber, "name", nf.name, "description", description)
			}
		}

		// Prompt for description if ZipLab didn't extract one
		if description == "" {
			pauseEnter(s, terminal, outputMode, e.LoadedStrings.FilePausePrompt)
			terminalio.WriteProcessedBytes(terminal, []byte(ansi.ClearScreen()), outputMode)
			descPrompt := fmt.Sprintf("\r\n|15%s|07 (%d bytes)\r\n|11Desc:|07 ", nf.name, nf.size)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(descPrompt)), outputMode)

			descInput, err := readLineFromSessionIH(s, terminal)
			if err != nil {
				// If the session died (e.g. SSH client disconnected during the
				// transfer wait), preserve the upload with a default description
				// rather than LOGOFFing the user mid-file-processing.
				slog.Warn("session lost during description prompt, using default description", "node", nodeNumber, "name", nf.name, "error", err)
				description = "No description"
			} else {
				description = sanitizeControlChars(strings.TrimSpace(descInput))
			}
			if len([]rune(description)) > 60 {
				description = string([]rune(description)[:60])
			}
		}
		if description == "" {
			description = "No description"
		}

		// Re-stat file to get post-pipeline size (ZipLab may have modified it)
		if fi, statErr := os.Stat(incomingPath); statErr != nil {
			slog.Warn("failed to stat file after pipeline, using original size", "node", nodeNumber, "name", nf.name, "error", statErr)
		} else {
			nf.size = fi.Size()
		}

		// Create and add FileRecord
		record := file.FileRecord{
			ID:            uuid.New(),
			AreaID:        currentAreaID,
			Filename:      nf.name,
			Description:   description,
			Size:          nf.size,
			UploadedAt:    time.Now(),
			UploadedBy:    currentUser.Handle,
			DownloadCount: 0,
		}

		// Move file from incoming to target directory
		finalPath := filepath.Join(targetDir, nf.name)
		// Guard against clobbering a file that exists on disk but is absent from
		// the metadata duplicate check above: os.Rename would overwrite it.
		if _, statErr := os.Stat(finalPath); statErr == nil {
			slog.Warn("upload rejected, file already exists on disk", "node", nodeNumber, "name", nf.name)
			duplicateCount++
			_ = os.Remove(incomingPath) // best-effort cleanup of rejected upload
			dupMsg := fmt.Sprintf("\r\n|09'%s' already exists in this area. Rejected.|07\r\n", nf.name)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(dupMsg)), outputMode)
			continue
		}
		if moveErr := os.Rename(incomingPath, finalPath); moveErr != nil {
			slog.Error("failed to move file to area", "node", nodeNumber, "name", nf.name, "error", moveErr)
			errMsg := fmt.Sprintf("\r\n|01Failed to accept '%s'.|07\r\n", nf.name)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
			continue
		}

		if addErr := e.FileMgr.AddFileRecord(record); addErr != nil {
			slog.Error("failed to add file record", "node", nodeNumber, "name", nf.name, "error", addErr)
			errMsg := fmt.Sprintf("\r\n|01Failed to register '%s'.|07\r\n", nf.name)
			terminalio.WriteProcessedBytes(terminal, ansi.ReplacePipeCodes([]byte(errMsg)), outputMode)
			if removeErr := os.Remove(finalPath); removeErr != nil {
				slog.Error("failed to clean up orphaned file", "node", nodeNumber, "name", nf.name, "error", removeErr)
			}
			continue
		}

		slog.Info("added file record", "node", nodeNumber, "name", nf.name, "fileID", record.ID)
		successCount++
		existingNames[strings.ToLower(nf.name)] = true
	}

	// 9. Update user upload count
	if successCount > 0 {
		currentUser.NumUploads += successCount
		if updateErr := userManager.UpdateUser(currentUser); updateErr != nil {
			slog.Error("failed to update user upload count", "node", nodeNumber, "error", updateErr)
		}
	}

	return successCount, duplicateCount
}
