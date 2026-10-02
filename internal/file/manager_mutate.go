package file

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// indexOfRecord returns the position of the record with id in records, or -1.
func indexOfRecord(records []FileRecord, id uuid.UUID) int {
	for i := range records {
		if records[i].ID == id {
			return i
		}
	}
	return -1
}

// areaDir returns the absolute directory of an area.
func (fm *FileManager) areaDir(areaID int) (string, error) {
	fm.muAreas.RLock()
	area, ok := fm.fileAreas[areaID]
	var areaPath string
	if ok {
		areaPath = area.Path
	}
	fm.muAreas.RUnlock()
	if !ok {
		return "", fmt.Errorf("file area %d not found", areaID)
	}
	absBasePath, err := filepath.Abs(fm.basePath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute base path: %w", err)
	}
	return filepath.Join(absBasePath, areaPath), nil
}

// DeleteFileRecord removes a file record by ID. If deleteFromDisk is true,
// the physical file is also removed from the filesystem.
func (fm *FileManager) DeleteFileRecord(fileID uuid.UUID, deleteFromDisk bool) error {
	foundAreaID := fm.findRecordArea(fileID)
	if foundAreaID == -1 {
		return fmt.Errorf("file record with ID %s not found", fileID)
	}

	var areaDir string
	if deleteFromDisk {
		dir, err := fm.areaDir(foundAreaID)
		if err != nil {
			return fmt.Errorf("internal inconsistency: %w", err)
		}
		areaDir = dir
	}

	var filename, staged string
	err := fm.mutateAreas([]int{foundAreaID}, func(lists map[int][]FileRecord) (func(), error) {
		records := lists[foundAreaID]
		idx := indexOfRecord(records, fileID)
		if idx == -1 {
			return nil, fmt.Errorf("file record with ID %s not found", fileID)
		}
		// The on-disk name comes from the record as it is on disk NOW, under
		// the file lock — not from the cache, where another writer may since
		// have renamed it. A stale name here could delete a different file
		// that has since taken it over.
		filename = records[idx].Filename

		// The file is only renamed out of the way here, and removed once the
		// list without its record is saved: if that save fails, the undo
		// puts the file back under its record.
		var undo func()
		if deleteFromDisk {
			// Only gate the disk delete: a record with a corrupt filename
			// must still be removable from metadata (deleteFromDisk=false),
			// or the sysop has no way to clear it.
			safeName, err := validateFilename(filename)
			if err != nil {
				return nil, fmt.Errorf("refusing to delete from disk: %w", err)
			}
			fullPath := filepath.Join(areaDir, safeName)
			tmp := filepath.Join(areaDir, ".deleting-"+uuid.NewString())
			if err := os.Rename(fullPath, tmp); err != nil {
				if !os.IsNotExist(err) {
					slog.Warn("failed to delete file from disk", "path", fullPath, "error", err)
					return nil, fmt.Errorf("failed to delete file from disk: %w", err)
				}
			} else {
				staged = tmp
				undo = func() {
					if err := os.Rename(tmp, fullPath); err != nil {
						slog.Error("failed to restore file after metadata save failure", "path", fullPath, "staged", tmp, "error", err)
					}
					staged = ""
				}
			}
		}

		lists[foundAreaID] = append(records[:idx], records[idx+1:]...)
		return undo, nil
	})
	if err != nil {
		slog.Error("failed to delete file record", "id", fileID, "error", err)
		return err
	}
	if staged != "" {
		if err := os.Remove(staged); err != nil {
			slog.Warn("record deleted but its file could not be removed", "path", staged, "error", err)
		} else {
			slog.Info("deleted file from disk", "file", filename, "area", foundAreaID)
		}
	}

	slog.Info("deleted file record", "file", filename, "id", fileID, "area", foundAreaID)
	return nil
}

// MoveFileRecord moves a file record to a different area, renaming the file on disk.
func (fm *FileManager) MoveFileRecord(fileID uuid.UUID, targetAreaID int) error {
	srcAreaID := fm.findRecordArea(fileID)
	if srcAreaID == -1 {
		return fmt.Errorf("file record with ID %s not found", fileID)
	}
	if srcAreaID == targetAreaID {
		return fmt.Errorf("file is already in area %d", targetAreaID)
	}

	targetDir, err := fm.areaDir(targetAreaID)
	if err != nil {
		return fmt.Errorf("target area ID %d not found", targetAreaID)
	}
	srcDir, err := fm.areaDir(srcAreaID)
	if err != nil {
		return fmt.Errorf("internal inconsistency: source area %d not found", srcAreaID)
	}

	var safeFilename string
	err = fm.mutateAreas([]int{srcAreaID, targetAreaID}, func(lists map[int][]FileRecord) (func(), error) {
		srcRecords := lists[srcAreaID]
		idx := indexOfRecord(srcRecords, fileID)
		if idx == -1 {
			return nil, fmt.Errorf("file record with ID %s not found", fileID)
		}
		record := srcRecords[idx]

		// The name comes from the list on disk, under the file lock, for
		// the same reason as in DeleteFileRecord.
		name, err := validateFilename(record.Filename)
		if err != nil {
			return nil, fmt.Errorf("refusing to move file record %s: %w", fileID, err)
		}
		safeFilename = name
		srcPath := filepath.Join(srcDir, safeFilename)
		dstPath := filepath.Join(targetDir, safeFilename)

		// Guard against silently overwriting an existing file in the target area.
		if _, err := os.Stat(dstPath); err == nil {
			return nil, fmt.Errorf("file %q already exists in target area %d", safeFilename, targetAreaID)
		}
		if err := os.Rename(srcPath, dstPath); err != nil {
			return nil, fmt.Errorf("failed to move file from %s to %s: %w", srcPath, dstPath, err)
		}

		lists[srcAreaID] = append(srcRecords[:idx], srcRecords[idx+1:]...)
		record.AreaID = targetAreaID
		lists[targetAreaID] = append(lists[targetAreaID], record)

		// If a list then fails to save, put the file back so the operation
		// is retryable.
		undo := func() {
			if err := os.Rename(dstPath, srcPath); err != nil {
				slog.Error("failed to roll back file rename after metadata save failure", "from", dstPath, "to", srcPath, "error", err)
			}
		}
		return undo, nil
	})
	if err != nil {
		slog.Error("failed to move file record", "id", fileID, "from", srcAreaID, "to", targetAreaID, "error", err)
		return err
	}

	slog.Info("moved file", "file", safeFilename, "id", fileID, "from", srcAreaID, "to", targetAreaID)
	return nil
}
