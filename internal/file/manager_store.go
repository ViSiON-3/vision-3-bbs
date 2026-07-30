package file

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// loadAllFileRecords iterates through loaded areas and loads their metadata.
func (fm *FileManager) loadAllFileRecords() error {
	fm.muAreas.RLock() // Need read lock on areas to iterate
	defer fm.muAreas.RUnlock()
	fm.muFiles.Lock() // Need write lock on fileRecords map
	defer fm.muFiles.Unlock()

	fm.fileRecords = make(map[int][]FileRecord) // Reset records
	var totalFilesLoaded int
	var errorsEncountered bool

	for areaID, area := range fm.fileAreas {
		metadataPath := filepath.Join(fm.basePath, area.Path, "metadata.json")
		data, err := os.ReadFile(metadataPath)
		if err != nil {
			if os.IsNotExist(err) {
				slog.Debug("metadata file not found for area, assuming no files", "path", metadataPath, "area", area.Tag, "id", areaID)
				fm.fileRecords[areaID] = []FileRecord{} // Initialize empty slice
				continue
			}
			slog.Error("failed to read metadata file for area", "path", metadataPath, "area", area.Tag, "error", err)
			errorsEncountered = true
			continue // Skip this area on error
		}

		var records []FileRecord
		if err := json.Unmarshal(data, &records); err != nil {
			slog.Error("failed to parse metadata file for area", "path", metadataPath, "area", area.Tag, "error", err)
			errorsEncountered = true
			continue // Skip this area on error
		}

		// TODO: Validate records? Ensure filenames exist?
		fm.fileRecords[areaID] = records
		totalFilesLoaded += len(records)
		slog.Debug("loaded file records for area", "count", len(records), "area", area.Tag, "id", areaID)
	}

	slog.Info("loaded metadata for areas", "areas", len(fm.fileAreas), "count", totalFilesLoaded)
	if errorsEncountered {
		return fmt.Errorf("encountered errors while loading file records")
	}
	return nil
}

// saveFileRecords saves the metadata for a specific file area.
func (fm *FileManager) saveFileRecords(areaID int) error {
	fm.muAreas.RLock() // Need read lock for area path
	area, exists := fm.fileAreas[areaID]
	if !exists {
		fm.muAreas.RUnlock()
		return fmt.Errorf("cannot save records for non-existent area ID %d", areaID)
	}
	metadataPath := filepath.Join(fm.basePath, area.Path, "metadata.json")
	fm.muAreas.RUnlock()

	fm.muFiles.Lock() // Need write lock to marshal and write
	defer fm.muFiles.Unlock()

	records, exists := fm.fileRecords[areaID]
	if !exists {
		// This might happen if an area was loaded but metadata failed initially
		// Or if called for a newly created area before any records added.
		slog.Debug("no records found in memory for area during save, saving empty list", "id", areaID)
		records = []FileRecord{} // Ensure we save an empty list, not nil
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal file records for area %d: %w", areaID, err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file %s: %w", metadataPath, err)
	}

	slog.Debug("saved file records for area", "count", len(records), "area", area.Tag, "id", areaID, "path", metadataPath)
	return nil
}
