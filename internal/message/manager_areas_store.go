package message

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ViSiON-3/vision-3-bbs/internal/jam"
)

// loadMessageAreas loads area definitions from JSON.
func (mm *MessageManager) loadMessageAreas() error {
	data, err := os.ReadFile(mm.areasPath)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}

	var areasList []*MessageArea
	if err := json.Unmarshal(data, &areasList); err != nil {
		return fmt.Errorf("failed to unmarshal areas from %s: %w", mm.areasPath, err)
	}

	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.areasByID = make(map[int]*MessageArea)
	mm.areasByTag = make(map[string]*MessageArea)
	mm.areasByEchoTag = make(map[string]*MessageArea)

	for _, area := range areasList {
		if area == nil {
			continue
		}
		if _, exists := mm.areasByID[area.ID]; exists {
			slog.Warn("duplicate area ID; skipping", "id", area.ID)
			continue
		}
		mm.areasByID[area.ID] = area
		mm.areasByTag[area.Tag] = area
		// Also index by EchoTag for FTN inbound routing when tag-prefix is in use.
		// Existing configs may already contain duplicates, so load stays tolerant
		// and warns rather than failing; AddArea/UpdateAreaByID reject new ones.
		if area.EchoTag != "" && area.EchoTag != area.Tag {
			if prev, dup := mm.areasByEchoTag[area.EchoTag]; dup {
				slog.Warn("duplicate FTN echo tag; inbound mail for it will route to the last area loaded",
					"echo_tag", area.EchoTag,
					"kept_area", area.Tag, "kept_id", area.ID, "kept_network", area.Network,
					"shadowed_area", prev.Tag, "shadowed_id", prev.ID, "shadowed_network", prev.Network)
			}
			mm.areasByEchoTag[area.EchoTag] = area
		}
		slog.Debug("loaded area", "id", area.ID, "tag", area.Tag, "type", area.AreaType)
	}

	// Migration: assign positions to any areas that have Position <= 0.
	// Finds the current max position and assigns sequentially after it.
	maxPos := 0
	hasUnset := false
	for _, area := range mm.areasByID {
		if area.Position > maxPos {
			maxPos = area.Position
		}
		if area.Position <= 0 {
			hasUnset = true
		}
	}
	if hasUnset && len(mm.areasByID) > 0 {
		sorted := make([]*MessageArea, 0, len(mm.areasByID))
		for _, area := range mm.areasByID {
			if area.Position <= 0 {
				sorted = append(sorted, area)
			}
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].ID < sorted[j].ID
		})
		for _, area := range sorted {
			maxPos++
			area.Position = maxPos
		}
		slog.Info("auto-assigned message area positions (migration)", "count", len(sorted))
	}

	return nil
}

// openBase opens a JAM base on-demand. The caller must close it when done.
// This method does not hold any locks and should be called after releasing mm.mu.
// Returns ErrAreaNotFound if the area doesn't exist.
func (mm *MessageManager) openBase(areaID int) (*jam.Base, *MessageArea, error) {
	mm.mu.RLock()
	area, exists := mm.areasByID[areaID]
	mm.mu.RUnlock()

	if !exists {
		return nil, nil, ErrAreaNotFound
	}

	basePath := mm.resolveBasePath(area)
	b, err := jam.Open(basePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open JAM base for area %d: %w", areaID, err)
	}

	return b, area, nil
}

// resolveBasePath returns the absolute path for a JAM base.
func (mm *MessageManager) resolveBasePath(area *MessageArea) string {
	bp := area.BasePath
	if bp == "" {
		bp = "msgbases/" + strings.ToLower(area.Tag)
	}
	if filepath.IsAbs(bp) {
		return bp
	}
	return filepath.Join(mm.dataPath, bp)
}

// SaveAreas persists all message areas to message_areas.json atomically.
// The file is written to a temp file alongside the target and then renamed
// to avoid partial-write corruption.
func (mm *MessageManager) SaveAreas() error {
	mm.mu.RLock()
	list := make([]MessageArea, 0, len(mm.areasByID))
	for _, area := range mm.areasByID {
		if area != nil {
			list = append(list, *area)
		}
	}
	mm.mu.RUnlock()

	sort.Slice(list, func(i, j int) bool {
		if list[i].Position != list[j].Position {
			return list[i].Position < list[j].Position
		}
		return list[i].ID < list[j].ID
	})

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal message areas: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(mm.areasPath)
	tmp, err := os.CreateTemp(dir, "message_areas_*.json.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file for areas: %w", err)
	}
	tmpName := tmp.Name()

	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to write temp areas file: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to sync temp areas file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to close temp areas file: %w", err)
	}
	if err = os.Rename(tmpName, mm.areasPath); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to rename temp areas file: %w", err)
	}
	return nil
}

// PackAndLinkArea packs the JAM base for the given area (removing deleted
// messages and renumbering) then rebuilds reply threading chains. Caches
// are invalidated afterward.
func (mm *MessageManager) PackAndLinkArea(areaID int) error {
	b, _, err := mm.openBase(areaID)
	if err != nil {
		return fmt.Errorf("open base for area %d: %w", areaID, err)
	}
	defer func() {
		if cerr := b.Close(); cerr != nil {
			slog.Warn("closing JAM base", "error", cerr)
		}
	}()
	if _, err := b.Pack(); err != nil {
		return fmt.Errorf("pack area %d: %w", areaID, err)
	}
	if _, err := b.Link(); err != nil {
		return fmt.Errorf("link area %d: %w", areaID, err)
	}
	mm.invalidateThreadIndex(areaID)
	return nil
}
