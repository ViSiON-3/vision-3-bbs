package file

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/atomicfile"
	"github.com/ViSiON-3/vision-3-bbs/internal/filelock"
	"github.com/google/uuid"
)

// Each area's file list lives in a metadata.json in the area's directory, and
// more than one process writes it: the BBS (uploads, download counts, sysop
// edits), v3mail (files arriving by TIC) and helper (bulk imports). The BBS
// keeps every list in memory, so the file on disk is the shared truth and the
// in-memory copy is a cache of it:
//
//   - Every write is a read-modify-write under a cross-process lock on the
//     file (internal/filelock): read the list from disk, apply the change to
//     that, write it back atomically. A change is never applied to the cached
//     copy and the cache written out, because that would erase whatever
//     another process added since the cache was loaded.
//   - Every read first checks whether the file changed on disk since it was
//     cached (refreshArea), and re-reads it if so. Writes replace the file
//     with a rename, so a changed list is always a different file; comparing
//     identity as well as size and mtime catches a rewrite within one mtime
//     tick.
//
// UpdateAreaMetadata is the same read-modify-write for programs that work on
// an area directory without a FileManager.

// MetadataFileName is the name of the file list kept in each area directory.
const MetadataFileName = "metadata.json"

// IsMetadataFile reports whether name, a file in an area directory, belongs
// to the area's bookkeeping rather than being a file in the area: the list
// itself, its lock sidecar, or a temp file left by an interrupted write.
func IsMetadataFile(name string) bool {
	return name == MetadataFileName ||
		name == filelock.SidecarPath(MetadataFileName) ||
		strings.HasPrefix(name, MetadataFileName+".tmp")
}

// readMetadata reads a file list and stats the file it read. A missing file
// is an empty list with a nil FileInfo.
func readMetadata(path string) ([]FileRecord, os.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []FileRecord{}, nil, nil
		}
		return nil, nil, err
	}
	defer func() { _ = f.Close() }() // read-only

	// Stat the open file, not the path, so the stamp describes exactly what
	// was read even if a writer replaces the file in between.
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	var records []FileRecord
	if err := json.NewDecoder(f).Decode(&records); err != nil {
		return nil, nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if records == nil {
		records = []FileRecord{}
	}
	return records, info, nil
}

// writeMetadata replaces a file list atomically and stats the result.
func writeMetadata(path string, records []FileRecord) (os.FileInfo, error) {
	if records == nil {
		records = []FileRecord{} // "[]", never "null"
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", path, err)
	}
	if err := atomicfile.WriteFile(path, data, 0644); err != nil {
		return nil, err
	}
	return os.Stat(path)
}

// sameStamp reports whether two stats of a file list describe the same
// contents. Both nil means the file was missing both times.
func sameStamp(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// UpdateAreaMetadata applies fn to the file list in areaDir under the
// cross-process lock, writing back what fn returns. fn receives the list as it
// is on disk at that moment. If fn returns an error nothing is written.
func UpdateAreaMetadata(areaDir string, fn func([]FileRecord) ([]FileRecord, error)) error {
	path := filepath.Join(areaDir, MetadataFileName)
	lock, err := filelock.Acquire(path, filelock.DefaultTimeout)
	if err != nil {
		return err
	}
	defer lock.Release()

	records, _, err := readMetadata(path)
	if err != nil {
		return err
	}
	updated, err := fn(records)
	if err != nil {
		return err
	}
	_, err = writeMetadata(path, updated)
	return err
}

// ReadAreaMetadata returns the file list in areaDir, empty if it has none.
func ReadAreaMetadata(areaDir string) ([]FileRecord, error) {
	records, _, err := readMetadata(filepath.Join(areaDir, MetadataFileName))
	return records, err
}

// metadataPath returns the file list path for an area, or false if the area
// is not defined. Takes muAreas; the caller must not hold muFiles.
func (fm *FileManager) metadataPath(areaID int) (string, bool) {
	fm.muAreas.RLock()
	defer fm.muAreas.RUnlock()
	area, ok := fm.fileAreas[areaID]
	if !ok {
		return "", false
	}
	return filepath.Join(fm.basePath, area.Path, MetadataFileName), true
}

// adopt installs a list read from or written to disk as the cached copy.
func (fm *FileManager) adopt(areaID int, records []FileRecord, info os.FileInfo) {
	fm.muFiles.Lock()
	defer fm.muFiles.Unlock()
	fm.fileRecords[areaID] = records
	fm.stamps[areaID] = info
}

// refreshArea re-reads an area's file list if it changed on disk since it was
// cached. A failed read keeps the cached copy: a list that cannot be read now
// is better shown as it last was than as empty.
func (fm *FileManager) refreshArea(areaID int) {
	path, ok := fm.metadataPath(areaID)
	if !ok {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("cannot stat file area metadata", "path", path, "error", err)
			return
		}
		info = nil
	}

	fm.muFiles.RLock()
	cached, loaded := fm.stamps[areaID]
	fm.muFiles.RUnlock()
	if loaded && sameStamp(cached, info) {
		return
	}

	// No lock needed to read: writers replace the file with a rename, so a
	// reader sees the whole old list or the whole new one.
	records, info, err := readMetadata(path)
	if err != nil {
		slog.Error("failed to re-read file area metadata", "path", path, "error", err)
		return
	}
	fm.adopt(areaID, records, info)
	slog.Debug("file area metadata changed on disk, reloaded", "id", areaID, "count", len(records))
}

// refreshAll refreshes every defined area. See refreshArea.
func (fm *FileManager) refreshAll() {
	fm.muAreas.RLock()
	ids := make([]int, 0, len(fm.fileAreas))
	for id := range fm.fileAreas {
		ids = append(ids, id)
	}
	fm.muAreas.RUnlock()
	for _, id := range ids {
		fm.refreshArea(id)
	}
}

// findRecordArea returns the area holding a record, refreshing first so a
// record another process added is found. -1 if no area holds it.
func (fm *FileManager) findRecordArea(fileID uuid.UUID) int {
	fm.refreshAll()
	fm.muFiles.RLock()
	defer fm.muFiles.RUnlock()
	for areaID, records := range fm.fileRecords {
		for i := range records {
			if records[i].ID == fileID {
				return areaID
			}
		}
	}
	return -1
}

// mutateAreas is the read-modify-write behind every change to file records.
// It locks the lists of the given areas (in ID order, so two callers locking
// the same pair cannot deadlock), reads each from disk, and passes them to fn
// keyed by area ID. fn edits the map in place; it may also act on the files
// themselves, returning an undo that mutateAreas calls if the lists then fail
// to save, so the files and the lists stay in step. On success every list is
// written back and becomes the cached copy.
//
// No in-process mutex is held while waiting for the file locks or while fn
// runs. The file lock is per open file description, so it serialises
// goroutines in this process as well as other processes.
func (fm *FileManager) mutateAreas(areaIDs []int, fn func(map[int][]FileRecord) (undo func(), err error)) error {
	ids := append([]int(nil), areaIDs...)
	sort.Ints(ids)
	paths := make(map[int]string, len(ids))
	for _, id := range ids {
		path, ok := fm.metadataPath(id)
		if !ok {
			return fmt.Errorf("file area %d not found", id)
		}
		paths[id] = path
	}

	for _, id := range ids {
		lock, err := filelock.Acquire(paths[id], filelock.DefaultTimeout)
		if err != nil {
			return err
		}
		defer lock.Release()
	}

	lists := make(map[int][]FileRecord, len(ids))
	original := make(map[int][]FileRecord, len(ids))
	for _, id := range ids {
		records, info, err := readMetadata(paths[id])
		if err != nil {
			return err
		}
		// What was just read is current, whatever fn decides.
		fm.adopt(id, records, info)
		original[id] = append([]FileRecord(nil), records...)
		lists[id] = append([]FileRecord(nil), records...)
	}

	undo, err := fn(lists)
	if err != nil {
		return err
	}

	var written []int
	for _, id := range ids {
		info, err := writeMetadata(paths[id], lists[id])
		if err != nil {
			if undo != nil {
				undo()
			}
			// Put back any list already written, so it matches the files
			// again now that the undo has run.
			for _, w := range written {
				if info, rerr := writeMetadata(paths[w], original[w]); rerr != nil {
					slog.Error("failed to restore file area metadata after a failed save", "path", paths[w], "error", rerr)
				} else {
					fm.adopt(w, original[w], info)
				}
			}
			return fmt.Errorf("saving %s: %w", paths[id], err)
		}
		fm.adopt(id, lists[id], info)
		written = append(written, id)
	}
	return nil
}

// loadAllFileRecords iterates through loaded areas and loads their metadata.
func (fm *FileManager) loadAllFileRecords() error {
	// muAreas and muFiles are never held together (see the FileManager doc
	// comment): snapshot the area list first, then load records under muFiles
	// alone. The metadata reads happen against the snapshot, so an area added
	// or removed mid-load is simply picked up by the next load.
	type areaInfo struct {
		id   int
		path string
		tag  string
	}
	fm.muAreas.RLock()
	areas := make([]areaInfo, 0, len(fm.fileAreas))
	for areaID, area := range fm.fileAreas {
		areas = append(areas, areaInfo{id: areaID, path: area.Path, tag: area.Tag})
	}
	fm.muAreas.RUnlock()

	fm.muFiles.Lock() // Need write lock on fileRecords map
	defer fm.muFiles.Unlock()

	fm.fileRecords = make(map[int][]FileRecord) // Reset records
	fm.stamps = make(map[int]os.FileInfo)
	var totalFilesLoaded int
	var errorsEncountered bool

	for _, area := range areas {
		metadataPath := filepath.Join(fm.basePath, area.path, MetadataFileName)
		records, info, err := readMetadata(metadataPath)
		if err != nil {
			slog.Error("failed to read metadata file for area", "path", metadataPath, "area", area.tag, "error", err)
			errorsEncountered = true
			continue // Skip this area on error; refreshArea retries it
		}

		// TODO: Validate records? Ensure filenames exist?
		fm.fileRecords[area.id] = records
		fm.stamps[area.id] = info
		totalFilesLoaded += len(records)
		slog.Debug("loaded file records for area", "count", len(records), "area", area.tag, "id", area.id)
	}

	slog.Info("loaded metadata for areas", "areas", len(areas), "count", totalFilesLoaded)
	if errorsEncountered {
		return fmt.Errorf("encountered errors while loading file records")
	}
	return nil
}
