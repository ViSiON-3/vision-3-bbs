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
	fm.muFiles.RLock() // Need read lock to find the file record
	defer fm.muFiles.RUnlock()
	fm.muAreas.RLock() // Need read lock to get area path
	defer fm.muAreas.RUnlock()

	var foundArea *FileArea
	var foundRecord *FileRecord

searchLoop:
	for areaID, records := range fm.fileRecords {
		for i := range records {
			if records[i].ID == fileID {
				// Get corresponding area
				area, areaExists := fm.fileAreas[areaID]
				if !areaExists {
					// Should not happen if data is consistent
					return "", fmt.Errorf("internal inconsistency: area %d not found for file %s", areaID, fileID)
				}
				foundArea = area
				foundRecord = &records[i] // Get pointer to the record
				break searchLoop
			}
		}
	}

	if foundRecord == nil {
		return "", fmt.Errorf("file record with ID %s not found", fileID)
	}

	// Construct path safely
	// Base path should be absolute for security
	absBasePath, err := filepath.Abs(fm.basePath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute base path: %w", err)
	}
	// Area path is relative to base path
	// Filename should be just the base name
	safeFilename, err := validateFilename(foundRecord.Filename)
	if err != nil {
		return "", fmt.Errorf("file record %s: %w", fileID, err)
	}

	fullPath := filepath.Join(absBasePath, foundArea.Path, safeFilename)

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

// IsSupportedArchive checks if the filename suggests a supported archive type.
// Uses the central archivers.json configuration to determine supported formats.
func (fm *FileManager) IsSupportedArchive(filename string) bool {
	// Load the central archiver config. Defaults are used if archivers.json is missing.
	arcCfg, err := archiver.LoadConfig("configs")
	if err != nil {
		slog.Warn("failed to load archivers config, falling back to .zip only", "error", err)
		return strings.HasSuffix(strings.ToLower(filename), ".zip")
	}
	return arcCfg.IsSupported(filename)
}
