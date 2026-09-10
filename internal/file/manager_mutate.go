package file

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// DeleteFileRecord removes a file record by ID. If deleteFromDisk is true,
// the physical file is also removed from the filesystem.
func (fm *FileManager) DeleteFileRecord(fileID uuid.UUID, deleteFromDisk bool) error {
	// muAreas and muFiles are never held together (see the FileManager doc
	// comment): find the record under a read lock, copy the area path under
	// muAreas, then re-validate by ID under the write lock before mutating —
	// the index from the first search can go stale in between.
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
		return fmt.Errorf("file record with ID %s not found", fileID)
	}

	var fullPath string
	if deleteFromDisk {
		fm.muAreas.RLock()
		area, areaExists := fm.fileAreas[foundAreaID]
		var areaPath string
		if areaExists {
			areaPath = area.Path
		}
		fm.muAreas.RUnlock()
		if !areaExists {
			return fmt.Errorf("internal inconsistency: area %d not found", foundAreaID)
		}
		absBasePath, err := filepath.Abs(fm.basePath)
		if err != nil {
			return fmt.Errorf("failed to get absolute base path: %w", err)
		}
		// Only gate the disk delete: a record with a corrupt filename must
		// still be removable from metadata (deleteFromDisk=false), or the
		// sysop has no way to clear it.
		safeName, err := validateFilename(foundFilename)
		if err != nil {
			return fmt.Errorf("refusing to delete from disk: %w", err)
		}
		fullPath = filepath.Join(absBasePath, areaPath, safeName)
	}

	fm.muFiles.Lock()
	defer fm.muFiles.Unlock()

	// Re-search under the write lock: another writer may have moved or
	// removed the record while no lock was held.
	foundIndex := -1
	for i := range fm.fileRecords[foundAreaID] {
		if fm.fileRecords[foundAreaID][i].ID == fileID {
			foundIndex = i
			break
		}
	}
	if foundIndex == -1 {
		return fmt.Errorf("file record with ID %s not found", fileID)
	}

	// If requested, delete from disk first — before touching metadata.
	// This way a failure leaves metadata intact and the operation is retryable.
	if deleteFromDisk {
		if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to delete file from disk", "path", fullPath, "error", err)
			return fmt.Errorf("failed to delete file from disk: %w", err)
		}
		slog.Info("deleted file from disk", "path", fullPath)
	}

	// Remove record from slice and persist.
	records := fm.fileRecords[foundAreaID]
	fm.fileRecords[foundAreaID] = append(records[:foundIndex], records[foundIndex+1:]...)

	fm.muFiles.Unlock()
	saveErr := fm.saveFileRecords(foundAreaID)
	fm.muFiles.Lock()

	if saveErr != nil {
		slog.Error("failed to save file records after deleting", "file", foundFilename, "id", fileID, "error", saveErr)
		return saveErr
	}

	slog.Info("deleted file record", "file", foundFilename, "id", fileID, "area", foundAreaID)
	return nil
}

// MoveFileRecord moves a file record to a different area, renaming the file on disk.
func (fm *FileManager) MoveFileRecord(fileID uuid.UUID, targetAreaID int) error {
	// muAreas and muFiles are never held together (see the FileManager doc
	// comment): locate the record under a read lock, copy both area paths
	// under muAreas, then re-validate by ID under the write lock — the index
	// from the first search can go stale in between. Disk operations stay
	// under the write lock, as before, so concurrent moves of the same file
	// serialize on it.
	fm.muFiles.RLock()
	srcAreaID := -1
	var recordFilename string
searchLoop:
	for areaID, records := range fm.fileRecords {
		for i := range records {
			if records[i].ID == fileID {
				srcAreaID = areaID
				recordFilename = records[i].Filename
				break searchLoop
			}
		}
	}
	fm.muFiles.RUnlock()

	if srcAreaID == -1 {
		return fmt.Errorf("file record with ID %s not found", fileID)
	}
	if srcAreaID == targetAreaID {
		return fmt.Errorf("file is already in area %d", targetAreaID)
	}

	fm.muAreas.RLock()
	targetArea, targetExists := fm.fileAreas[targetAreaID]
	srcArea, srcExists := fm.fileAreas[srcAreaID]
	var srcAreaPath, targetAreaPath string
	if targetExists {
		targetAreaPath = targetArea.Path
	}
	if srcExists {
		srcAreaPath = srcArea.Path
	}
	fm.muAreas.RUnlock()
	if !targetExists {
		return fmt.Errorf("target area ID %d not found", targetAreaID)
	}
	if !srcExists {
		return fmt.Errorf("internal inconsistency: source area %d not found", srcAreaID)
	}

	absBasePath, err := filepath.Abs(fm.basePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute base path: %w", err)
	}
	safeFilename, err := validateFilename(recordFilename)
	if err != nil {
		return fmt.Errorf("refusing to move file record %s: %w", fileID, err)
	}
	srcPath := filepath.Join(absBasePath, srcAreaPath, safeFilename)
	dstPath := filepath.Join(absBasePath, targetAreaPath, safeFilename)

	fm.muFiles.Lock()
	defer fm.muFiles.Unlock()

	// Re-search under the write lock: another writer may have moved or
	// removed the record while no lock was held.
	srcIndex := -1
	for i := range fm.fileRecords[srcAreaID] {
		if fm.fileRecords[srcAreaID][i].ID == fileID {
			srcIndex = i
			break
		}
	}
	if srcIndex == -1 {
		return fmt.Errorf("file record with ID %s not found", fileID)
	}
	record := fm.fileRecords[srcAreaID][srcIndex]

	// Guard against silently overwriting an existing file in the target area.
	if _, err := os.Stat(dstPath); err == nil {
		return fmt.Errorf("file %q already exists in target area %d", safeFilename, targetAreaID)
	}

	if err := os.Rename(srcPath, dstPath); err != nil {
		return fmt.Errorf("failed to move file from %s to %s: %w", srcPath, dstPath, err)
	}

	// Update in-memory state.
	srcRecords := fm.fileRecords[srcAreaID]
	fm.fileRecords[srcAreaID] = append(srcRecords[:srcIndex], srcRecords[srcIndex+1:]...)
	record.AreaID = targetAreaID
	fm.fileRecords[targetAreaID] = append(fm.fileRecords[targetAreaID], record)

	fm.muFiles.Unlock()
	errSrc := fm.saveFileRecords(srcAreaID)
	errDst := fm.saveFileRecords(targetAreaID)
	fm.muFiles.Lock()

	if errSrc != nil || errDst != nil {
		// At least one metadata save failed. Roll back the filesystem rename so
		// the file returns to the source directory and the operation is retryable.
		if renameBackErr := os.Rename(dstPath, srcPath); renameBackErr != nil {
			slog.Error("failed to roll back file rename after metadata save failure", "from", dstPath, "to", srcPath, "error", renameBackErr)
		} else {
			// Restore in-memory state.
			// By identity, not position: muFiles was released for the saves
			// above, so another writer may have appended in the meantime.
			fm.fileRecords[targetAreaID] = removeRecordByID(fm.fileRecords[targetAreaID], fileID)
			record.AreaID = srcAreaID
			fm.fileRecords[srcAreaID] = append(fm.fileRecords[srcAreaID], record)
			// Re-persist both areas so disk reflects the restored in-memory state.
			// A partial save (e.g. errSrc==nil but errDst!=nil) may have already
			// written one side; re-saving corrects any such divergence.
			fm.muFiles.Unlock()
			if resaveErr := fm.saveFileRecords(srcAreaID); resaveErr != nil {
				slog.Error("failed to re-save source area during rollback", "area", srcAreaID, "error", resaveErr)
			}
			if resaveErr := fm.saveFileRecords(targetAreaID); resaveErr != nil {
				slog.Error("failed to re-save target area during rollback", "area", targetAreaID, "error", resaveErr)
			}
			fm.muFiles.Lock()
		}
		if errSrc != nil {
			slog.Error("failed to save source area after moving", "area", srcAreaID, "file", safeFilename, "error", errSrc)
			return errSrc
		}
		slog.Error("failed to save target area after moving", "area", targetAreaID, "file", safeFilename, "error", errDst)
		return errDst
	}

	slog.Info("moved file", "file", safeFilename, "id", fileID, "from", srcAreaID, "to", targetAreaID)
	return nil
}
