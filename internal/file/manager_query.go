package file

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GetFileRecordByID looks up a file record by UUID across all areas.
func (fm *FileManager) GetFileRecordByID(fileID uuid.UUID) (*FileRecord, error) {
	fm.muFiles.RLock()
	defer fm.muFiles.RUnlock()

	for _, records := range fm.fileRecords {
		for i := range records {
			if records[i].ID == fileID {
				rec := records[i]
				return &rec, nil
			}
		}
	}
	return nil, fmt.Errorf("file record with ID %s not found", fileID)
}

// SearchFiles returns file records whose filename or description contains
// query (case-insensitive) across all areas.
func (fm *FileManager) SearchFiles(query string) []FileRecord {
	fm.muFiles.RLock()
	defer fm.muFiles.RUnlock()

	lowerQuery := strings.ToLower(query)
	var results []FileRecord
	for _, records := range fm.fileRecords {
		for _, rec := range records {
			if strings.Contains(strings.ToLower(rec.Filename), lowerQuery) ||
				strings.Contains(strings.ToLower(rec.Description), lowerQuery) {
				results = append(results, rec)
			}
		}
	}
	return results
}

// GetFilesNewerThan returns file records in the given area uploaded after since.
func (fm *FileManager) GetFilesNewerThan(areaID int, since time.Time) []FileRecord {
	fm.muFiles.RLock()
	defer fm.muFiles.RUnlock()

	var results []FileRecord
	for _, rec := range fm.fileRecords[areaID] {
		if rec.UploadedAt.After(since) {
			results = append(results, rec)
		}
	}
	return results
}

// GetUnreviewedFiles returns file records in the given area where Reviewed is false.
func (fm *FileManager) GetUnreviewedFiles(areaID int) []FileRecord {
	fm.muFiles.RLock()
	defer fm.muFiles.RUnlock()

	var results []FileRecord
	for _, rec := range fm.fileRecords[areaID] {
		if !rec.Reviewed {
			results = append(results, rec)
		}
	}
	return results
}

// GetFilesForArea returns a slice of FileRecord for a given area ID.
// Returns an empty slice if the area doesn't exist or has no files.
func (fm *FileManager) GetFilesForArea(areaID int) []FileRecord {
	fm.muFiles.RLock()
	defer fm.muFiles.RUnlock()

	records, exists := fm.fileRecords[areaID]
	if !exists {
		return []FileRecord{} // Return empty slice, not nil
	}

	// Return a copy to prevent external modification of the internal slice
	// TODO: Consider sorting options here (filename, date, etc.)
	recordsCopy := make([]FileRecord, len(records))
	copy(recordsCopy, records)
	return recordsCopy
}

// GetFileCountForArea returns the total number of file records for a given area ID.
// Returns 0 if the area doesn't exist or has no files.
func (fm *FileManager) GetFileCountForArea(areaID int) (int, error) {
	fm.muFiles.RLock()         // Acquire read lock for accessing file records
	defer fm.muFiles.RUnlock() // Ensure lock is released

	records, exists := fm.fileRecords[areaID]
	if !exists {
		// Area might not exist or simply hasn't had metadata loaded/saved yet.
		// Returning 0 is appropriate for the caller (runListFiles).
		slog.Debug("area not found or unloaded; returning file count 0", "id", areaID)
		return 0, nil
	}

	return len(records), nil
}

// GetTotalFileCount returns the total number of files across all areas.
func (fm *FileManager) GetTotalFileCount() int {
	areas := fm.ListAreas()
	total := 0
	for _, area := range areas {
		count, err := fm.GetFileCountForArea(area.ID)
		if err != nil {
			continue
		}
		total += count
	}
	return total
}

// GetFilesForAreaPaginated returns a slice of FileRecord for a given area ID,
// limited to the specified page and pageSize.
// Returns an empty slice if the area doesn't exist, has no files, or the page is out of bounds.
func (fm *FileManager) GetFilesForAreaPaginated(areaID int, page int, pageSize int) ([]FileRecord, error) {
	fm.muFiles.RLock()
	defer fm.muFiles.RUnlock()

	if page <= 0 || pageSize <= 0 {
		slog.Warn("invalid page or pageSize for paginated file listing", "page", page, "pageSize", pageSize)
		// Optionally return an error, but returning empty slice might be simpler for the caller
		return []FileRecord{}, fmt.Errorf("invalid page number or page size")
	}

	records, exists := fm.fileRecords[areaID]
	if !exists || len(records) == 0 {
		// log.Printf("DEBUG: GetFilesForAreaPaginated called for non-existent, unloaded, or empty area ID %d.", areaID)
		return []FileRecord{}, nil // Return empty slice if area empty or doesn't exist
	}

	totalFiles := len(records)
	startIndex := (page - 1) * pageSize

	// Check if start index is out of bounds
	if startIndex >= totalFiles {
		slog.Debug("requested page out of bounds for file listing", "page", page, "total", totalFiles, "pageSize", pageSize)
		return []FileRecord{}, nil // Requested page is beyond the last file
	}

	endIndex := startIndex + pageSize
	if endIndex > totalFiles {
		endIndex = totalFiles // Adjust end index if it exceeds the total number of files
	}

	// Slice the records for the current page
	pageRecords := records[startIndex:endIndex]

	// Return a copy to prevent external modification
	recordsCopy := make([]FileRecord, len(pageRecords))
	copy(recordsCopy, pageRecords)

	// log.Printf("DEBUG: GetFilesForAreaPaginated returning %d records for area %d, page %d", len(recordsCopy), areaID, page)
	return recordsCopy, nil
}
