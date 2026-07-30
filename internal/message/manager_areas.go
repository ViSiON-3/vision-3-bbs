package message

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// GetAreaByID retrieves a message area by its ID.
func (mm *MessageManager) GetAreaByID(id int) (*MessageArea, bool) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	area, exists := mm.areasByID[id]
	return area, exists
}

// GetAreaByTag retrieves a message area by its tag.
func (mm *MessageManager) GetAreaByTag(tag string) (*MessageArea, bool) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	area, exists := mm.areasByTag[tag]
	return area, exists
}

// echoTagConflict reports an area that already claims echoTag, excluding
// excludeID. Two kinds of claim count:
//
//   - Another area with the same EchoTag on the same network. areasByEchoTag
//     keeps only the last writer, so that echo's mail silently lands in
//     whichever area was indexed last. Areas on DIFFERENT networks may
//     legitimately share an echo tag -- the same echo name exists on separate
//     FTN networks -- and the tosser disambiguates by network when routing
//     (internal/tosser/import.go).
//
//   - Any area whose Tag equals echoTag, on any network. The tosser tries
//     GetAreaByTag first and that lookup is NOT network-gated, so a tag match
//     always wins and the echo-tagged area would never receive anything.
//
// Comparison is exact, matching the areasByEchoTag key. Caller must hold mm.mu.
func (mm *MessageManager) echoTagConflict(echoTag, network string, excludeID int) *MessageArea {
	if echoTag == "" {
		return nil
	}
	for _, a := range mm.areasByID {
		if a.ID == excludeID {
			continue
		}
		if a.Tag == echoTag {
			return a
		}
		if a.EchoTag != "" && a.EchoTag == echoTag && strings.EqualFold(a.Network, network) {
			return a
		}
	}
	return nil
}

// echoTagConflictError describes why echoTag cannot be used, naming the area
// that already claims it and how.
func echoTagConflictError(echoTag string, c *MessageArea) error {
	if c.Tag == echoTag {
		return fmt.Errorf("echo tag %q is the local tag of area %q (id %d); inbound mail for it already routes there",
			echoTag, c.Tag, c.ID)
	}
	return fmt.Errorf("echo tag %q already used by area %q (id %d) on network %q",
		echoTag, c.Tag, c.ID, c.Network)
}

// GetAreaByEchoTag retrieves a message area by its FTN echo tag.
// Used when areas have a local tag-prefix (e.g. Tag="FD_LINUX", EchoTag="LINUX").
func (mm *MessageManager) GetAreaByEchoTag(echoTag string) (*MessageArea, bool) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	area, exists := mm.areasByEchoTag[echoTag]
	return area, exists
}

// UpdateAreaByID replaces the message area with the given ID with a copy of updated.
// Callers must not modify areas by writing through pointers returned from GetAreaByID;
// use this method so the update is performed under the manager's lock and avoids races.
// Returns ErrAreaNotFound if no area has the given ID. The updated area's ID must match id.
func (mm *MessageManager) UpdateAreaByID(id int, updated MessageArea) error {
	if updated.ID != id {
		return fmt.Errorf("message area ID mismatch: got %d, want %d", updated.ID, id)
	}
	mm.mu.Lock()
	defer mm.mu.Unlock()
	old, exists := mm.areasByID[id]
	if !exists {
		return ErrAreaNotFound
	}
	oldTag := old.Tag
	oldEchoTag := old.EchoTag
	replacement := new(MessageArea)
	*replacement = updated
	// Validate everything before touching any index, so a rejected update
	// leaves the manager exactly as it was.
	if oldTag != updated.Tag {
		if existing, ok := mm.areasByTag[updated.Tag]; ok && existing.ID != id {
			return fmt.Errorf("tag %q already in use by area %d", updated.Tag, existing.ID)
		}
	}
	if conflict := mm.echoTagConflict(updated.EchoTag, updated.Network, id); conflict != nil {
		return echoTagConflictError(updated.EchoTag, conflict)
	}
	if oldTag != updated.Tag {
		delete(mm.areasByTag, oldTag)
	}

	// Keep areasByEchoTag in sync when EchoTag changes.
	if oldEchoTag != "" && oldEchoTag != old.Tag {
		delete(mm.areasByEchoTag, oldEchoTag)
	}
	if updated.EchoTag != "" && updated.EchoTag != updated.Tag {
		mm.areasByEchoTag[updated.EchoTag] = replacement
	}
	mm.areasByID[id] = replacement
	mm.areasByTag[updated.Tag] = replacement
	return nil
}

// AddArea inserts a new message area, auto-assigning the next available ID
// and Position. The area's Tag must be unique. After insertion the area list
// is persisted to disk. Returns the assigned ID.
func (mm *MessageManager) AddArea(area MessageArea) (int, error) {
	mm.mu.Lock()

	// Check tag uniqueness.
	if _, exists := mm.areasByTag[area.Tag]; exists {
		mm.mu.Unlock()
		return 0, fmt.Errorf("message area tag %q already exists", area.Tag)
	}

	if conflict := mm.echoTagConflict(area.EchoTag, area.Network, 0); conflict != nil {
		mm.mu.Unlock()
		return 0, echoTagConflictError(area.EchoTag, conflict)
	}

	// Assign next ID and position.
	maxID := 0
	maxPos := 0
	for _, a := range mm.areasByID {
		if a.ID > maxID {
			maxID = a.ID
		}
		if a.Position > maxPos {
			maxPos = a.Position
		}
	}
	area.ID = maxID + 1
	area.Position = maxPos + 1

	// Default base path if empty.
	if area.BasePath == "" {
		area.BasePath = fmt.Sprintf("msgbases/area_%d", area.ID)
	}

	ptr := new(MessageArea)
	*ptr = area
	mm.areasByID[area.ID] = ptr
	mm.areasByTag[area.Tag] = ptr
	if area.EchoTag != "" && area.EchoTag != area.Tag {
		mm.areasByEchoTag[area.EchoTag] = ptr
	}
	mm.mu.Unlock()

	slog.Info("auto-created message area", "id", area.ID, "tag", area.Tag, "type", area.AreaType)

	if err := mm.SaveAreas(); err != nil {
		// Rollback in-memory state so it stays consistent with disk.
		mm.mu.Lock()
		delete(mm.areasByID, area.ID)
		delete(mm.areasByTag, area.Tag)
		if area.EchoTag != "" && area.EchoTag != area.Tag {
			delete(mm.areasByEchoTag, area.EchoTag)
		}
		mm.mu.Unlock()
		slog.Error("rolling back area after save failure", "tag", area.Tag, "error", err)
		return 0, fmt.Errorf("save areas after add: %w", err)
	}
	return area.ID, nil
}

// ListAreas returns all loaded areas sorted by Position.
func (mm *MessageManager) ListAreas() []*MessageArea {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	list := make([]*MessageArea, 0, len(mm.areasByID))
	for _, area := range mm.areasByID {
		list = append(list, area)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Position != list[j].Position {
			return list[i].Position < list[j].Position
		}
		return list[i].ID < list[j].ID
	})
	return list
}
