package file

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
)

// AddFileRecord adds a new file record to the specified area and saves.
func (fm *FileManager) AddFileRecord(record FileRecord) error {
	// Basic validation
	if record.ID == uuid.Nil {
		return fmt.Errorf("file record must have a valid ID")
	}
	if record.AreaID <= 0 {
		return fmt.Errorf("file record must have a valid AreaID")
	}
	if record.Filename == "" {
		return fmt.Errorf("file record must have a Filename")
	}
	if _, err := validateFilename(record.Filename); err != nil {
		return fmt.Errorf("file record %s: %w", record.ID, err)
	}

	fm.muAreas.RLock() // Check if area exists
	_, areaExists := fm.fileAreas[record.AreaID]
	fm.muAreas.RUnlock()
	if !areaExists {
		return fmt.Errorf("cannot add record to non-existent area ID %d", record.AreaID)
	}

	fm.muFiles.Lock()
	defer fm.muFiles.Unlock()

	// Check for duplicate filename within the same area?
	// Or should uploads handle overwrites/renaming?
	// For now, allow duplicates, but log a warning.
	existingRecords := fm.fileRecords[record.AreaID]
	for _, existing := range existingRecords {
		if strings.EqualFold(existing.Filename, record.Filename) {
			slog.Warn("adding file record with duplicate filename", "file", record.Filename, "area", record.AreaID)
			break
		}
	}

	fm.fileRecords[record.AreaID] = append(fm.fileRecords[record.AreaID], record)

	// Save changes for this area - release lock during save
	fm.muFiles.Unlock()
	err := fm.saveFileRecords(record.AreaID)
	fm.muFiles.Lock() // Re-acquire lock before returning

	if err != nil {
		// Attempt to rollback the in-memory addition? Complex.
		slog.Error("failed to save file records after adding, in-memory state might be inconsistent", "file", record.Filename, "area", record.AreaID, "error", err)
		// Maybe remove the last added record if save fails?
		// fm.fileRecords[record.AreaID] = fm.fileRecords[record.AreaID][:len(fm.fileRecords[record.AreaID])-1]
		return err // Propagate save error
	}

	slog.Info("added file record", "file", record.Filename, "id", record.ID, "area", record.AreaID)
	return nil
}

// IncrementDownloadCount increments the download count for a file and saves.
func (fm *FileManager) IncrementDownloadCount(fileID uuid.UUID) error {
	fm.muFiles.Lock()
	defer fm.muFiles.Unlock()

	foundAreaID := -1
	foundIndex := -1

	// Find the file across all areas
searchLoop:
	for areaID, records := range fm.fileRecords {
		for i := range records {
			if records[i].ID == fileID {
				foundAreaID = areaID
				foundIndex = i
				break searchLoop
			}
		}
	}

	if foundAreaID == -1 {
		return fmt.Errorf("file record with ID %s not found", fileID)
	}

	// Increment count directly on the pointer within the slice
	fm.fileRecords[foundAreaID][foundIndex].DownloadCount++
	newCount := fm.fileRecords[foundAreaID][foundIndex].DownloadCount
	filename := fm.fileRecords[foundAreaID][foundIndex].Filename

	// Save changes for this area - release lock during save
	fm.muFiles.Unlock()
	err := fm.saveFileRecords(foundAreaID)
	fm.muFiles.Lock() // Re-acquire lock before returning

	if err != nil {
		slog.Error("failed to save file records after incrementing download count, in-memory state might be inconsistent", "file", filename, "id", fileID, "error", err)
		// Attempt rollback?
		// fm.fileRecords[foundAreaID][foundIndex].DownloadCount--
		return err
	}

	slog.Debug("incremented download count for file", "file", filename, "id", fileID, "count", newCount)
	return nil
}

// UpdateFileRecord finds a file record by ID and applies the given update function.
func (fm *FileManager) UpdateFileRecord(fileID uuid.UUID, updateFunc func(*FileRecord)) error {
	fm.muFiles.Lock()
	defer fm.muFiles.Unlock()

	foundAreaID := -1
	foundIndex := -1

searchLoop:
	for areaID, records := range fm.fileRecords {
		for i := range records {
			if records[i].ID == fileID {
				foundAreaID = areaID
				foundIndex = i
				break searchLoop
			}
		}
	}

	if foundAreaID == -1 {
		return fmt.Errorf("file record with ID %s not found", fileID)
	}

	updateFunc(&fm.fileRecords[foundAreaID][foundIndex])
	filename := fm.fileRecords[foundAreaID][foundIndex].Filename

	fm.muFiles.Unlock()
	err := fm.saveFileRecords(foundAreaID)
	fm.muFiles.Lock()

	if err != nil {
		slog.Error("failed to save file records after updating", "file", filename, "id", fileID, "error", err)
		return err
	}

	slog.Debug("updated file record", "file", filename, "id", fileID)
	return nil
}

// UpdateFileDescription updates the description of a file record.
func (fm *FileManager) UpdateFileDescription(fileID uuid.UUID, description string) error {
	return fm.UpdateFileRecord(fileID, func(r *FileRecord) {
		r.Description = description
	})
}
