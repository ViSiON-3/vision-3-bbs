package file

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/archiver"
	"github.com/google/uuid"
)

// GetFilePath returns the full, absolute path to a file given its record ID.
// The path is constructed safely: the record's filename must be a plain name,
// and the result must resolve inside the base directory.
//
// It does NOT check that the file exists on disk -- callers that need that must
// stat the returned path themselves.
func (fm *FileManager) GetFilePath(fileID uuid.UUID) (string, error) {
	// A record another process added is found too.
	fm.refreshAll()

	// muAreas and muFiles are never held together (see the FileManager doc
	// comment): copy what is needed from each domain under its own lock.
	fm.muFiles.RLock()
	foundAreaID := -1
	var foundFilename string
searchLoop:
	for areaID, records := range fm.fileRecords {
		for i := range records {
			if records[i].ID == fileID {
				foundAreaID = areaID
				foundFilename = records[i].Filename
				break searchLoop
			}
		}
	}
	fm.muFiles.RUnlock()

	if foundAreaID == -1 {
		return "", fmt.Errorf("file record with ID %s not found", fileID)
	}

	fm.muAreas.RLock()
	area, areaExists := fm.fileAreas[foundAreaID]
	var areaPath string
	if areaExists {
		areaPath = area.Path
	}
	fm.muAreas.RUnlock()
	if !areaExists {
		// Should not happen if data is consistent
		return "", fmt.Errorf("internal inconsistency: area %d not found for file %s", foundAreaID, fileID)
	}

	// Construct path safely
	// Base path should be absolute for security
	absBasePath, err := filepath.Abs(fm.basePath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute base path: %w", err)
	}
	// Area path is relative to base path
	// Filename should be just the base name
	safeFilename, err := validateFilename(foundFilename)
	if err != nil {
		return "", fmt.Errorf("file record %s: %w", fileID, err)
	}

	fullPath := filepath.Join(absBasePath, areaPath, safeFilename)

	// Final check: Ensure the resolved path is still within the intended base directory
	if !strings.HasPrefix(fullPath, absBasePath) {
		return "", fmt.Errorf("constructed file path '%s' is outside base directory '%s'", fullPath, absBasePath)
	}

	return fullPath, nil
}

// GetAreaUploadPath returns the absolute filesystem path for an area's file directory.
func (fm *FileManager) GetAreaUploadPath(areaID int) (string, error) {
	fm.muAreas.RLock()
	defer fm.muAreas.RUnlock()

	area, exists := fm.fileAreas[areaID]
	if !exists {
		return "", fmt.Errorf("file area %d not found", areaID)
	}

	absBasePath, err := filepath.Abs(fm.basePath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute base path: %w", err)
	}

	fullPath := filepath.Join(absBasePath, area.Path)
	if !strings.HasPrefix(fullPath, absBasePath) {
		return "", fmt.Errorf("area path outside base directory")
	}

	return fullPath, nil
}

// IsSupportedArchive detects an archive from the file at path using the manager's
// configured archivers directory. Content signatures take priority over suffixes.
// Detection errors return false; the viewer reports file-open errors as usual.
func (fm *FileManager) IsSupportedArchive(path string) bool {
	arcCfg, err := archiver.LoadConfig(filepath.Dir(fm.configPath))
	if err != nil {
		slog.Warn("failed to load archivers config, falling back to ZIP defaults", "error", err)
		arcCfg = archiver.Config{Archivers: archiver.DefaultConfig().Archivers[:1]}
	}
	_, ok, err := arcCfg.DetectFile(path)
	if err != nil {
		slog.Warn("failed to detect archive", "path", path, "error", err)
		return false
	}
	return ok
}
