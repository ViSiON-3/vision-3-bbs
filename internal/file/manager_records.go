package file

import (
	"errors"
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

	if _, ok := fm.metadataPath(record.AreaID); !ok {
		return fmt.Errorf("cannot add record to non-existent area ID %d", record.AreaID)
	}

	err := fm.mutateAreas([]int{record.AreaID}, func(lists map[int][]FileRecord) (func(), error) {
		// Duplicate filenames are allowed (uploads handle overwrites and
		// renaming themselves), but worth a warning.
		for _, existing := range lists[record.AreaID] {
			if strings.EqualFold(existing.Filename, record.Filename) {
				slog.Warn("adding file record with duplicate filename", "file", record.Filename, "area", record.AreaID)
				break
			}
		}
		lists[record.AreaID] = append(lists[record.AreaID], record)
		return nil, nil
	})
	if err != nil {
		slog.Error("failed to save file records after adding", "file", record.Filename, "area", record.AreaID, "error", err)
		return err
	}

	slog.Info("added file record", "file", record.Filename, "id", record.ID, "area", record.AreaID)
	return nil
}

// IncrementDownloadCount increments the download count for a file and saves.
func (fm *FileManager) IncrementDownloadCount(fileID uuid.UUID) error {
	var newCount int
	err := fm.UpdateFileRecord(fileID, func(r *FileRecord) {
		r.DownloadCount++
		newCount = r.DownloadCount
	})
	if err != nil {
		return err
	}
	slog.Debug("incremented download count for file", "id", fileID, "count", newCount)
	return nil
}

// UpdateFileRecord finds a file record by ID and applies the given update
// function to it. updateFunc sees the record as it is on disk, not as last
// cached, so a change another process made meanwhile is not lost.
func (fm *FileManager) UpdateFileRecord(fileID uuid.UUID, updateFunc func(*FileRecord)) error {
	areaID := fm.findRecordArea(fileID)
	if areaID == -1 {
		return fmt.Errorf("file record with ID %s not found", fileID)
	}

	var filename string
	err := fm.mutateAreas([]int{areaID}, func(lists map[int][]FileRecord) (func(), error) {
		records := lists[areaID]
		for i := range records {
			if records[i].ID == fileID {
				updateFunc(&records[i])
				filename = records[i].Filename
				return nil, nil
			}
		}
		// Gone from the list on disk since it was found in the cache.
		return nil, fmt.Errorf("file record with ID %s not found", fileID)
	})
	if err != nil {
		slog.Error("failed to save file records after updating", "id", fileID, "error", err)
		return err
	}

	slog.Debug("updated file record", "file", filename, "id", fileID)
	return nil
}

// ErrRecordMoved is returned by the ...InArea / ...FromArea operations when
// the record isn't in the area the caller expected, read under that area's
// lock: it was moved or deleted meanwhile. Nothing is changed.
var ErrRecordMoved = errors.New("the file is no longer in that area")

// UpdateFileRecordInArea is UpdateFileRecord for a file the caller expects
// in areaID: updateFunc runs only if the record is in that area's list on
// disk, under its lock, and ErrRecordMoved is returned otherwise. A caller
// authorised for areaID alone can't change a file that has been moved to
// another area meanwhile.
func (fm *FileManager) UpdateFileRecordInArea(fileID uuid.UUID, areaID int, updateFunc func(*FileRecord)) error {
	if _, ok := fm.GetAreaByID(areaID); !ok {
		return ErrRecordMoved
	}
	return fm.mutateAreas([]int{areaID}, func(lists map[int][]FileRecord) (func(), error) {
		records := lists[areaID]
		for i := range records {
			if records[i].ID == fileID {
				updateFunc(&records[i])
				return nil, nil
			}
		}
		return nil, ErrRecordMoved
	})
}

// UpdateFileDescription updates the description of a file record.
func (fm *FileManager) UpdateFileDescription(fileID uuid.UUID, description string) error {
	return fm.UpdateFileRecord(fileID, func(r *FileRecord) {
		r.Description = description
	})
}
