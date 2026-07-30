package file

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// FileManager manages file areas and their associated file records.
type FileManager struct {
	basePath    string               // Base directory for all file areas (e.g., "data/files")
	configPath  string               // Path to file_areas.json
	muAreas     sync.RWMutex         // Mutex for accessing file area definitions
	muFiles     sync.RWMutex         // Mutex for accessing file records (might need finer-grained locking later)
	fileAreas   map[int]*FileArea    // Map AreaID to FileArea definition
	fileTags    map[string]int       // Map Area Tag (uppercase) to AreaID
	fileRecords map[int][]FileRecord // Map AreaID to a slice of its FileRecords
}

// NewFileManager creates and initializes a new FileManager.
func NewFileManager(baseDataPath, baseConfigPath string) (*FileManager, error) {
	fm := &FileManager{
		basePath:    filepath.Join(baseDataPath, "files"),             // e.g., data/files
		configPath:  filepath.Join(baseConfigPath, "file_areas.json"), // e.g., configs/file_areas.json
		fileAreas:   make(map[int]*FileArea),
		fileTags:    make(map[string]int),
		fileRecords: make(map[int][]FileRecord),
	}

	slog.Info("loading file areas", "path", fm.configPath)
	if err := fm.loadAreas(); err != nil {
		return nil, fmt.Errorf("failed to load file areas: %w", err)
	}

	slog.Info("loading file records", "path", fm.basePath)
	if err := fm.loadAllFileRecords(); err != nil {
		// Log error but potentially continue if some areas loaded?
		slog.Error("failed to load one or more file record sets", "error", err)
		// Decide if this should be fatal. For now, let's allow startup.
		// return nil, fmt.Errorf("failed to load file records: %w", err)
	}

	return fm, nil
}

// loadAreas loads the FileArea definitions from the configuration file.
func (fm *FileManager) loadAreas() error {
	fm.muAreas.Lock()
	defer fm.muAreas.Unlock()

	data, err := os.ReadFile(fm.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Warn("file areas config not found, no file areas loaded", "path", fm.configPath)
			// Create an empty file?
			emptyJSON := []byte("[]")
			if writeErr := os.WriteFile(fm.configPath, emptyJSON, 0644); writeErr != nil {
				slog.Error("failed to create empty file areas config", "path", fm.configPath, "error", writeErr)
			}
			return nil // Not a fatal error if file doesn't exist initially
		}
		return fmt.Errorf("reading file areas config %s: %w", fm.configPath, err)
	}

	var areas []FileArea
	if err := json.Unmarshal(data, &areas); err != nil {
		return fmt.Errorf("parsing file areas config %s: %w", fm.configPath, err)
	}

	// Clear existing maps before loading new data
	fm.fileAreas = make(map[int]*FileArea)
	fm.fileTags = make(map[string]int)

	for i := range areas {
		area := &areas[i] // Take pointer to the element in the slice
		if area.ID <= 0 {
			slog.Warn("skipping file area with invalid ID <= 0", "area", fmt.Sprintf("%+v", area))
			continue
		}
		if area.Tag == "" {
			slog.Warn("skipping file area with empty tag", "id", area.ID)
			continue
		}
		// Ensure path is clean and relative (security measure)
		area.Path = filepath.Clean(area.Path)
		if filepath.IsAbs(area.Path) || strings.HasPrefix(area.Path, "..") {
			slog.Warn("skipping file area with invalid path (absolute or traversing up)", "path", area.Path, "id", area.ID)
			continue
		}

		// Ensure area directory exists
		fullAreaPath := filepath.Join(fm.basePath, area.Path)
		if err := os.MkdirAll(fullAreaPath, 0755); err != nil {
			slog.Error("failed to create directory for file area, skipping area", "area", area.Tag, "path", fullAreaPath, "error", err)
			continue
		}

		ucTag := strings.ToUpper(area.Tag)
		if _, exists := fm.fileTags[ucTag]; exists {
			slog.Warn("duplicate file area tag found, skipping duplicate definition", "area", area.Tag, "id", area.ID)
			continue
		}
		if _, exists := fm.fileAreas[area.ID]; exists {
			slog.Warn("duplicate file area id found, skipping duplicate definition", "id", area.ID, "area", area.Tag)
			continue
		}

		fm.fileAreas[area.ID] = area
		fm.fileTags[ucTag] = area.ID
		slog.Debug("loaded file area", "id", area.ID, "area", area.Tag, "name", area.Name, "path", area.Path)
	}

	slog.Info("successfully loaded file areas", "count", len(fm.fileAreas))
	return nil
}

// --- Public API Methods --- //

// ListAreas returns a sorted list of FileArea definitions.
// Filtering by ACS should be done by the caller.
func (fm *FileManager) ListAreas() []FileArea {
	fm.muAreas.RLock()
	defer fm.muAreas.RUnlock()

	areas := make([]FileArea, 0, len(fm.fileAreas))
	for _, area := range fm.fileAreas {
		areas = append(areas, *area) // Append a copy
	}

	// Sort by ID for consistent listing
	sort.Slice(areas, func(i, j int) bool {
		return areas[i].ID < areas[j].ID
	})

	return areas
}

// GetAreaByTag returns a FileArea definition by its tag (case-insensitive).
func (fm *FileManager) GetAreaByTag(tag string) (*FileArea, bool) {
	fm.muAreas.RLock()
	defer fm.muAreas.RUnlock()

	areaID, exists := fm.fileTags[strings.ToUpper(tag)]
	if !exists {
		return nil, false
	}
	area, exists := fm.fileAreas[areaID]
	return area, exists // Return pointer directly
}

// GetAreaByID returns a FileArea definition by its ID.
func (fm *FileManager) GetAreaByID(id int) (*FileArea, bool) {
	fm.muAreas.RLock()
	defer fm.muAreas.RUnlock()

	area, exists := fm.fileAreas[id]
	return area, exists // Return pointer directly
}
